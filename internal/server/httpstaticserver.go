package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"regexp"

	"github.com/go-yaml/yaml"
	"github.com/gorilla/mux"
	"github.com/shogo82148/androidbinary/apk"

	"github.com/codeskyblue/gohttpserver/internal/auth"
	"github.com/codeskyblue/gohttpserver/internal/ipa"
	"github.com/codeskyblue/gohttpserver/internal/util"
	"github.com/codeskyblue/gohttpserver/internal/zipx"
)

const YAMLCONF = ".ghs.yml"

// ContentSecurityPolicy 由最外层安全响应头中间件应用于每个响应。
const ContentSecurityPolicy = "default-src 'self'; script-src 'self' 'unsafe-inline' https://www.google-analytics.com https://www.googletagmanager.com; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://www.google-analytics.com https://www.googletagmanager.com; font-src 'self' data:; media-src 'self'; connect-src 'self' https://www.google-analytics.com https://www.googletagmanager.com; form-action 'self'; base-uri 'self'; object-src 'none'; frame-ancestors 'none'"

// maxUploadSize 限制单个上传文件；maxUploadBytes 限制整个 multipart 请求体，使超限请求在解析并缓冲到 os.TempDir() 之前就被拒绝。
const maxUploadSize int64 = 10 * 1024 * 1024 * 1024

const maxUploadBytes int64 = maxUploadSize + (64 << 20)

const defaultPlistProxy = "https://plistproxy.herokuapp.com/plist"

type ApkInfo struct {
	PackageName  string `json:"packageName"`
	MainActivity string `json:"mainActivity"`
	Version      struct {
		Code int    `json:"code"`
		Name string `json:"name"`
	} `json:"version"`
}

type IndexFileItem struct {
	Path string
	Info os.FileInfo
	// Lower 是索引时一次性小写化的 Path，避免每次查询重复转换。
	Lower string
}

// dirSizeShards 是分片数，必须是 2 的幂以便用位掩码取模。
const dirSizeShards = 32

// shardedDirSize 分片目录大小缓存以减少锁争用。
type shardedDirSize struct {
	shards [dirSizeShards]dirSizeShard
}

type dirSizeShard struct {
	mu   sync.RWMutex
	data map[string]int64
}

func (s *shardedDirSize) shard(key string) *dirSizeShard {
	h := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return &s.shards[h&(dirSizeShards-1)]
}

func (s *shardedDirSize) peek(key string) (int64, bool) {
	sh := s.shard(key)
	sh.mu.RLock()
	v, ok := sh.data[key]
	sh.mu.RUnlock()
	return v, ok
}

func (s *shardedDirSize) mergeIn(totals map[string]int64) {
	if len(totals) == 0 {
		return
	}
	// 按分片分组，缩小锁范围。
	byShard := make(map[*dirSizeShard]map[string]int64, dirSizeShards)
	for k, v := range totals {
		sh := s.shard(k)
		if byShard[sh] == nil {
			byShard[sh] = make(map[string]int64)
		}
		byShard[sh][k] = v
	}
	for sh, m := range byShard {
		sh.mu.Lock()
		if sh.data == nil {
			sh.data = make(map[string]int64)
		}
		for k, v := range m {
			if _, exists := sh.data[k]; !exists {
				sh.data[k] = v
			}
		}
		sh.mu.Unlock()
	}
}

func (s *shardedDirSize) addDelta(relFile string, delta int64) {
	if delta == 0 {
		return
	}
	dir := path.Dir(filepath.ToSlash(filepath.Clean(relFile)))
	for {
		sh := s.shard(dir)
		sh.mu.Lock()
		if sh.data != nil {
			if _, ok := sh.data[dir]; ok {
				sh.data[dir] += delta
			}
		}
		sh.mu.Unlock()
		if dir == "." || dir == "/" {
			break
		}
		parent := path.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
}

func (s *shardedDirSize) invalidatePath(relPath string) {
	dir := filepath.ToSlash(filepath.Clean(relPath))
	for {
		sh := s.shard(dir)
		sh.mu.Lock()
		if sh.data != nil {
			delete(sh.data, dir)
		}
		sh.mu.Unlock()
		if dir == "." || dir == "/" || dir == "" {
			break
		}
		parent := path.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
}

func (s *shardedDirSize) dropSubtree(dir string) {
	dir = filepath.ToSlash(filepath.Clean(dir))
	prefix := dir + "/"
	for _, sh := range &s.shards {
		sh.mu.Lock()
		if sh.data != nil {
			for k := range sh.data {
				if k == dir || strings.HasPrefix(k, prefix) {
					delete(sh.data, k)
				}
			}
		}
		sh.mu.Unlock()
	}
}

// removedSize 返回即将删除的字节数以及该值是否已知。
func (s *shardedDirSize) removedSize(realPath string, root string) (int64, bool) {
	info, err := os.Stat(realPath)
	if err != nil {
		return 0, false
	}
	if !info.IsDir() {
		return info.Size(), true
	}
	rel := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(realPath, root)))
	if rel == "" {
		rel = "."
	}
	return s.peek(rel)
}

type HTTPStaticServer struct {
	Root             string
	Prefix           string
	Upload           bool
	Delete           bool
	Edit             bool
	Title            string
	Theme            string
	PlistProxy       string
	GoogleTrackerID  string
	AuthType         string
	DeepPathMaxDepth int
	NoIndex          bool
	// Assets 是内嵌的前端文件系统，由 main 在启动时注入。
	Assets http.FileSystem

	// indexes 由 findIndex 读、后台 makeIndex 循环覆盖，原子指针保证读者看到一致快照。
	indexes atomic.Pointer[[]IndexFileItem]

	// accessConfCache 按路径缓存解析后的 .ghs.yml，并按 mtime 失效，外部编辑仍能生效。
	accessConfCache *util.FileCache[rawAccessConf]

	m       *mux.Router
	bufPool sync.Pool // 用 sync.Pool 复用缓冲区，降低 GC 压力
}

func NewHTTPStaticServer(root string, noIndex bool) *HTTPStaticServer {
	root = filepath.ToSlash(filepath.Clean(root))
	if !strings.HasSuffix(root, "/") {
		root = root + "/"
	}
	log.Printf("root path: %s\n", root)
	m := mux.NewRouter()
	s := &HTTPStaticServer{
		Root:            root,
		Theme:           "black",
		m:               m,
		accessConfCache: util.NewFileCache[rawAccessConf](parseAccessConfFile),
		bufPool: sync.Pool{
			New: func() any { return make([]byte, 32*1024) },
		},
		NoIndex: noIndex,
	}

	if !noIndex {
		go func() {
			// 恢复 panic，避免单个坏文件永久终止索引重建协程。
			defer func() {
				if r := recover(); r != nil {
					log.Printf("makeIndex goroutine panic: %v — index rebuild stopped until restart", r)
				}
			}()
			time.Sleep(1 * time.Second)
			for {
				startTime := time.Now()
				log.Println("Started making search index")
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("makeIndex iteration panic: %v", r)
						}
					}()
					s.makeIndex()
				}()
				log.Printf("Completed search index in %v", time.Since(startTime))
				time.Sleep(time.Minute * 10)
			}
		}()
	}

	// Apple *.ipa 相关路由
	m.HandleFunc("/-/ipa/plist/{path:.*}", s.hPlist)
	m.HandleFunc("/-/ipa/link/{path:.*}", s.hIpaLink)
	m.HandleFunc("/-/video-player/{path:.*}", s.hVideoPlayer)

	// 多选打包下载：JSON 请求体列出所选路径，响应为单个 zip，每个条目以 basename 作为顶层名。
	m.HandleFunc("/-/zip", s.hZipMulti).Methods("POST")

	m.HandleFunc("/-/fetch", s.hFetch).Methods("POST")

	m.HandleFunc("/-/info/{path:.*}", s.hInfo).Methods("GET")
	// .apk 专用信息端点（与 /-/info/ 行为一致）。
	m.HandleFunc("/-/apk/info/{path:.*}", s.hInfo).Methods("GET")

	// 流式解出 zip 中的单个条目；字面量 "/-/" 分隔归档路径与 dockerignore 风格 glob，
	// 否则两个贪婪的 .* 变量会合并成一个。
	m.HandleFunc("/-/unzip/{zip_path:.*}/-/{path:.*}", s.hUnzip).Methods("GET")

	m.HandleFunc("/{path:.*}", s.hIndex).Methods("GET", "HEAD")
	m.HandleFunc("/{path:.*}", s.hUploadOrMkdir).Methods("POST")
	m.HandleFunc("/{path:.*}", s.hEdit).Methods("PUT")
	m.HandleFunc("/{path:.*}", s.hDelete).Methods("DELETE")
	return s
}

func (s *HTTPStaticServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// SECURITY: 拒绝 URL 路径中的反斜杠。gorilla/mux 只按 "/" 做路径清理，
	// 而 Windows 上 filepath.Clean（resolvePath 使用）把 "\" 也当分隔符——
	// "..\..\" 形式的路径能通过路由，解析结果落在 s.Root 之上。在此统一拒绝，
	// 一次性封死整类穿越攻击。
	if strings.ContainsRune(r.URL.Path, '\\') {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	s.m.ServeHTTP(w, r)
}

// getRealPath 返回带 / 分隔符的真实路径
func (s *HTTPStaticServer) getRealPath(r *http.Request) string {
	return s.resolvePath(mux.Vars(r)["path"])
}

// relFromRoot 返回相对 s.Root 的路径并统一用斜杠，避免在 JSON 响应中泄露服务器绝对路径。
func (s *HTTPStaticServer) relFromRoot(path string) string {
	rel, err := filepath.Rel(s.Root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// resolvePath 把（gorilla/mux 已解码的）URL 路径转换为 s.Root 下的绝对斜杠路径，
// 由 filepath.Clean 折叠 ".."。
//
// SECURITY: 仅 Clean 不能保证路径仍在 Root 内，凡是会改动文件系统的调用方
// 必须再用 insideRoot / mustResolveRequestPath 复核结果。
func (s *HTTPStaticServer) resolvePath(urlPath string) string {
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	cleanPath := filepath.Clean(urlPath) // 防止 ".." 引发的安全问题
	relativePath, err := filepath.Rel(s.Prefix, cleanPath)
	if err != nil {
		relativePath = cleanPath
	}
	return filepath.ToSlash(filepath.Join(s.Root, relativePath))
}

// insideRoot 判断已解析的路径是 s.Root 本身还是其子孙。
func (s *HTTPStaticServer) insideRoot(resolved string) bool {
	return isUnderRoot(resolved, s.Root)
}

// rootPath 返回清理后的绝对根路径，不带尾部分隔符。
func (s *HTTPStaticServer) rootPath() string {
	return filepath.Clean(s.Root)
}

// resolveRequestPath 解析请求路径并返回是否仍在 Root 内；读写文件的处理器在 ok 为 false 时必须退出。
func (s *HTTPStaticServer) resolveRequestPath(r *http.Request) (string, bool) {
	p := s.getRealPath(r)
	return p, s.insideRoot(p)
}

func (s *HTTPStaticServer) hIndex(w http.ResponseWriter, r *http.Request) {
	path := mux.Vars(r)["path"]
	realPath := s.getRealPath(r)
	if r.FormValue("json") == "true" {
		s.hJSONList(w, r)
		return
	}

	if r.FormValue("op") == "info" {
		s.hInfo(w, r)
		return
	}

	if r.FormValue("op") == "archive" {
		s.hZip(w, r)
		return
	}

	log.Println("GET", util.SanitizeLogInput(path), util.SanitizeLogInput(realPath))
	if r.FormValue("raw") == "false" || util.IsDir(realPath) {
		if r.Method == "HEAD" {
			return
		}
		// 目录列表返回 Vue 3 前端页面
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		f, err := s.Assets.Open("index.html")
		if err != nil {
			http.Error(w, "Frontend not built", http.StatusNotFound)
			return
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		w.Write(data)
	} else {
		// SECURITY: 访问控制文件绝不通过 HTTP 提供——其内容包含所有 users[].token，
		// 正是 canUploadByToken 接受的凭据。
		if filepath.Base(path) == YAMLCONF {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		if r.FormValue("download") == "true" {
			w.Header().Set("Content-Disposition", downloadDisposition(filepath.Base(path)))
		}
		http.ServeFile(w, r, realPath)
	}
}

// downloadDisposition 构造 RFC 6266 的 Content-Disposition：ASCII 回退名加 RFC 5987 的
// filename* 形式，使非 ASCII 文件名在响应头编码后仍能保留。
func downloadDisposition(name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	if ascii == "" {
		ascii = "download"
	}
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s",
		ascii, url.PathEscape(name))
}

func (s *HTTPStaticServer) hDelete(w http.ResponseWriter, req *http.Request) {
	realPath, ok := s.resolveRequestPath(req)
	if !ok {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	// isUnderRoot 对相等路径返回 true，所以裸的 DELETE / 会清空整个共享目录。
	if filepath.Clean(realPath) == s.rootPath() {
		http.Error(w, "Refusing to delete the share root", http.StatusBadRequest)
		return
	}
	auth := s.readAccessConf(realPath)
	if !auth.canDelete(req) {
		http.Error(w, "Delete forbidden", http.StatusForbidden)
		return
	}

	// 先记下即将消失的字节数，以便按增量修正目录大小缓存，而不是整体丢弃。
	removed, sizeKnown := dirInfoSize.removedSize(realPath, s.Root)

	err := os.RemoveAll(realPath)
	if err != nil {
		// 不回显 err —— 其中含服务器绝对路径。
		log.Printf("delete %q failed: %v", util.SanitizeLogInput(realPath), err)
		http.Error(w, "Delete failed", http.StatusInternalServerError)
		return
	}
	rel := s.relFromRoot(realPath)
	if sizeKnown {
		dirInfoSize.addDelta(rel, -removed)
	}
	// 删除子树自身条目；若其大小原本未知，则连祖先一起失效，下次读取时从磁盘重算。
	dirInfoSize.dropSubtree(rel)
	if !sizeKnown {
		dirInfoSize.invalidatePath(rel)
	}
	s.invalidateAccessConf()
	w.Write([]byte("Success"))
}

func (s *HTTPStaticServer) hUploadOrMkdir(w http.ResponseWriter, req *http.Request) {
	dirpath, ok := s.resolveRequestPath(req)
	if !ok {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	auth := s.readAccessConf(dirpath)
	if !auth.canUpload(req) {
		http.Error(w, "Upload forbidden", http.StatusForbidden)
		return
	}

	// SECURITY: 必须在 multipart 解析前限制请求体——解析会把超过 32 MiB 的部分
	// 写入 os.TempDir()，解析后再检查无法阻止客户端占满临时文件系统。
	req.Body = http.MaxBytesReader(w, req.Body, maxUploadBytes)

	file, header, err := req.FormFile("file")
	if req.MultipartForm != nil {
		// 畸形的 multipart 请求体也会把内容落盘，需要清理。
		defer req.MultipartForm.RemoveAll() // FormFile 返回后 req.MultipartForm 非 nil
	}
	if err != nil && !errors.Is(err, http.ErrMissingFile) {
		log.Printf("parse form file: %v", err)
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, fmt.Sprintf("Upload too large (max %d bytes)", maxUploadBytes), http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Failed to parse upload", http.StatusBadRequest)
		return
	}

	if _, err := os.Stat(dirpath); os.IsNotExist(err) {
		if err := os.MkdirAll(dirpath, os.ModePerm); err != nil {
			log.Printf("mkdir %q: %v", util.SanitizeLogInput(dirpath), err)
			http.Error(w, "Directory create failed", http.StatusInternalServerError)
			return
		}
	}

	if file == nil {
		w.Header().Set("Content-Type", "application/json;charset=utf-8")
		json.NewEncoder(w).Encode(map[string]any{
			"success":     true,
			"destination": s.relFromRoot(dirpath),
		})
		return
	}
	defer file.Close()

	// 校验上传大小上限，防止磁盘被写满
	if header.Size > maxUploadSize {
		log.Printf("upload rejected: file size %d exceeds limit %d", header.Size, maxUploadSize)
		http.Error(w, fmt.Sprintf("File too large (max %d bytes)", maxUploadSize), http.StatusRequestEntityTooLarge)
		return
	}

	filename := req.FormValue("filename")
	if filename == "" {
		filename = header.Filename
	}

	// `path` 是文件夹上传用的相对路径字段：覆盖 `filename` 的扁平语义并保留目录结构。
	relPath := req.FormValue("path")
	var dstPath string
	if relPath != "" {
		cleaned := path.Clean(strings.ReplaceAll(relPath, "\\", "/"))
		// 两种 OS 的绝对路径都要拒绝：path.IsAbs 覆盖 "/..."，filepath.IsAbs 还能识别 "C:/foo" 这类盘符路径。
		if path.IsAbs(cleaned) || filepath.IsAbs(cleaned) {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}
		for _, seg := range strings.Split(cleaned, "/") {
			if err := checkPathSegment(seg); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		dstPath = filepath.Join(dirpath, filepath.FromSlash(cleaned))
		// 按相对路径创建缺失的中间目录。
		if parent := filepath.Dir(dstPath); parent != dirpath {
			if err := os.MkdirAll(parent, os.ModePerm); err != nil {
				log.Printf("mkdir parent %q: %v", util.SanitizeLogInput(parent), err)
				http.Error(w, "Directory create failed", http.StatusInternalServerError)
				return
			}
		}
	} else {
		if err := checkFilename(filename); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		dstPath = filepath.Join(dirpath, filename)
	}

	// 记录覆盖前的旧大小，以便目录大小缓存按精确增量修正。
	var oldSize int64
	if fi, statErr := os.Stat(dstPath); statErr == nil && !fi.IsDir() {
		oldSize = fi.Size()
	}

	// 写入同目录的临时文件后再改名替换，失败的或中断的上传既不会破坏也不会截断原文件。
	written, copyErr := s.writeFileAtomic(dstPath, file, 0)
	if copyErr != nil {
		log.Printf("upload copy %q: %v", util.SanitizeLogInput(dstPath), copyErr)
		http.Error(w, "Upload failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json;charset=utf-8")

	// 按精确增量修正目录大小缓存，而不是丢弃整个缓存。
	dirInfoSize.addDelta(s.relFromRoot(dstPath), written-oldSize)
	s.invalidateAccessConf()

	if req.FormValue("unzip") == "true" {
		// 流式 NDJSON 进度：每解出一个文件输出一行 JSON。
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		w.Header().Set("X-Accel-Buffering", "no") // 让进度事件绕过代理缓冲
		flusher, _ := w.(http.Flusher)

		writeLine := func(payload string) {
			io.WriteString(w, payload)
			io.WriteString(w, "\n")
			if flusher != nil {
				flusher.Flush()
			}
		}

		err = zipx.UnzipFile(req.Context(), dstPath, dirpath, func(idx, total int, name string) {
			encoded, _ := json.Marshal(name)
			writeLine(fmt.Sprintf(`{"phase":"unzip","current":%d,"total":%d,"file":%s}`, idx, total, string(encoded)))
		})
		// 仅在解压成功后才删除原压缩包。
		if err == nil {
			os.Remove(dstPath)
		}
		// 解压写入了大量文件，此目录及其祖先的缓存已失效：丢掉整条链，下次列表时重新遍历。
		dirInfoSize.invalidatePath(s.relFromRoot(dirpath))
		message := "success"
		if err != nil {
			message = err.Error()
		}
		writeLine(fmt.Sprintf(`{"phase":"done","success":%v,"description":%q}`, err == nil, message))
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"success":     true,
		"destination": s.relFromRoot(dstPath),
	})
}

// maxEditSize 限制网页内编辑的 PUT 请求体：浏览器编辑的是源码文件，而非数 GB 的二进制块。
const maxEditSize int64 = 5 * 1024 * 1024

// writeFileAtomic 先把 src 流式写入 dst 旁的临时文件，复制成功后才改名替换，
// 因此目标文件不会被截断，读者也看不到写了一半的内容。
// limit > 0 时限制可接受大小（0 表示不限），超限返回 errTooLarge 且不改动 dst。
func (s *HTTPStaticServer) writeFileAtomic(dst string, src io.Reader, limit int64) (int64, error) {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dst)+".upload-*")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	// 任何出错路径都会删除临时文件，只有成功改名才保留。
	committed := false
	defer func() {
		if !committed {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	buf := s.bufPool.Get().([]byte)
	defer s.bufPool.Put(buf)

	reader := src
	if limit > 0 {
		// 多读一个字节，使超限请求体被识别为超限，而不是被静默截断。
		reader = io.LimitReader(src, limit+1)
	}
	written, err := io.CopyBuffer(tmp, reader, buf)
	if err != nil {
		return 0, err
	}
	if limit > 0 && written > limit {
		return written, errTooLarge
	}
	if err := tmp.Close(); err != nil {
		return written, err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return written, err
	}
	committed = true
	return written, nil
}

// errTooLarge 在源数据超过调用方限制时由 writeFileAtomic 返回。
var errTooLarge = errors.New("payload exceeds size limit")

// hEdit 处理针对已有文件的 PUT：请求体即新内容。需同时满足全局 --edit 开关与所在目录的 edit 权限。
// token 只从 URL 查询参数或 X-Token 头获取，绝不从请求体取——canEdit 的 r.FormValue 会把本身就是文件内容的请求体读空。
func (s *HTTPStaticServer) hEdit(w http.ResponseWriter, req *http.Request) {
	if !s.Edit {
		http.Error(w, "Edit disabled (enable with --edit)", http.StatusForbidden)
		return
	}

	dstPath, inside := s.resolveRequestPath(req)
	if !inside {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	// 访问控制文件不可通过 HTTP 写入。
	if filepath.Base(dstPath) == YAMLCONF {
		http.Error(w, "Not editable", http.StatusForbidden)
		return
	}

	// 拒绝对目录的写入——建目录走 POST 且不带 multipart。
	fi, err := os.Stat(dstPath)
	if err != nil {
		// 不回显 err —— 其中含服务器绝对路径。
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
		} else {
			log.Printf("edit stat %q: %v", util.SanitizeLogInput(dstPath), err)
			http.Error(w, "Stat failed", http.StatusInternalServerError)
		}
		return
	}
	if fi.IsDir() {
		http.Error(w, "Cannot edit a directory", http.StatusBadRequest)
		return
	}

	// 读取目标文件父目录的 .ghs.yml：目录上的 edit 授权隐式覆盖其内文件。
	ac := s.readAccessConf(filepath.Dir(dstPath))
	token := req.URL.Query().Get("token")
	if token == "" {
		token = req.Header.Get("X-Token")
	}
	var allowed bool
	if token != "" {
		allowed = ac.canEditByToken(token)
	} else {
		allowed = ac.canEditSession(req)
	}
	if !allowed {
		http.Error(w, "Edit forbidden", http.StatusForbidden)
		return
	}

	// 声明的超限请求体提前拒绝；流式拷贝时硬上限仍然生效，谎报的 Content-Length 也有界。
	if req.ContentLength > maxEditSize {
		http.Error(w, fmt.Sprintf("File too large to edit (max %d bytes); re-upload instead", maxEditSize), http.StatusRequestEntityTooLarge)
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, maxEditSize)

	oldSize := fi.Size()
	written, copyErr := s.writeFileAtomic(dstPath, req.Body, maxEditSize)
	if copyErr != nil {
		log.Printf("edit copy %q: %v", util.SanitizeLogInput(dstPath), copyErr)
		if errors.Is(copyErr, errTooLarge) {
			http.Error(w, fmt.Sprintf("File too large to edit (max %d bytes)", maxEditSize), http.StatusRequestEntityTooLarge)
			return
		}
		var maxErr *http.MaxBytesError
		if errors.As(copyErr, &maxErr) {
			http.Error(w, fmt.Sprintf("File too large to edit (max %d bytes)", maxEditSize), http.StatusRequestEntityTooLarge)
			return
		}
		// writeFileAtomic 仅在成功时重命名，原文件未被改动。
		http.Error(w, "Edit failed", http.StatusInternalServerError)
		return
	}

	dirInfoSize.addDelta(s.relFromRoot(dstPath), written-oldSize)
	s.invalidateAccessConf()

	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{
		"success":     true,
		"destination": s.relFromRoot(dstPath),
		"size":        written,
	})
}

// maxFetchSize 限制经本服务转取的远端资源，更大的应由用户的浏览器自行下载。
const maxFetchSize int64 = 1 << 30 // 1 GiB

// fetchTimeout 限制整次远端请求（DNS+连接+读取），避免挂起的连接长期占用工作协程。
const fetchTimeout = 5 * time.Minute

// safeDialContext 拒绝拨号到回环、链路本地、组播及 RFC1918 私网地址，使 POST /-/fetch
// 无法令服务器攻击自身或其所在网络。在 DNS 解析后校验可防 DNS 重绑定：检查的就是即将连接的地址，
// 因此拨号直接使用已校验的 IP，而不是重新解析主机名。
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
			ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() ||
			isPrivateIPv4(ip) {
			return nil, fmt.Errorf("refusing to connect to private/loopback address %s", ip)
		}
	}
	// 连接已校验的 IP 而非重新解析主机名，消除「先检查后使用」的 TOCTOU 窗口。
	dialAddr := net.JoinHostPort(ips[0].String(), port)
	d := net.Dialer{Timeout: 10 * time.Second}
	return d.DialContext(ctx, network, dialAddr)
}

func isPrivateIPv4(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		// 10.0.0.0/8、172.16.0.0/12、192.168.0.0/16
		switch {
		case ip4[0] == 10:
			return true
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
			return true
		case ip4[0] == 192 && ip4[1] == 168:
			return true
		case ip4[0] == 169 && ip4[1] == 254: // 链路本地
			return true
		case ip4[0] == 127: // IPv4 映射形式的回环地址
			return true
		}
	}
	// 唯一本地地址 fc00::/7（IPv6 私有地址）
	if len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc {
		return true
	}
	return false
}

// hFetch 把远端 URL 下载到当前路由下的文件。表单字段：`url`（要抓取的 http(s) 地址）与
// `to`（目标 basename，校验方式与上传一致）。只接受 http/https，且拨号经过 safeDialContext。
func (s *HTTPStaticServer) hFetch(w http.ResponseWriter, req *http.Request) {
	dirpath, inside := s.resolveRequestPath(req)
	if !inside {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// 与 hEdit 相同的保体认证：token 取自 URL 查询参数或 X-Token 头。
	ac := s.readAccessConf(dirpath)
	token := req.URL.Query().Get("token")
	if token == "" {
		token = req.Header.Get("X-Token")
	}
	var allowed bool
	if token != "" {
		allowed = ac.canUploadByToken(token)
	} else {
		allowed = ac.canUploadSession(req)
	}
	if !allowed {
		http.Error(w, "Fetch forbidden", http.StatusForbidden)
		return
	}

	srcURL := req.FormValue("url")
	if srcURL == "" {
		http.Error(w, "Missing 'url' form field", http.StatusBadRequest)
		return
	}
	parsed, perr := url.Parse(srcURL)
	if perr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		http.Error(w, "Invalid URL — only http/https allowed", http.StatusBadRequest)
		return
	}
	if parsed.Host == "" {
		http.Error(w, "URL must have a host", http.StatusBadRequest)
		return
	}

	dstName := req.FormValue("to")
	if dstName == "" {
		http.Error(w, "Missing 'to' form field", http.StatusBadRequest)
		return
	}
	if strings.ContainsAny(dstName, "/\\") {
		http.Error(w, "'to' must be a basename, not a path", http.StatusBadRequest)
		return
	}
	if err := checkFilename(dstName); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dstPath := filepath.Join(dirpath, filepath.FromSlash(dstName))

	// 确保父目录存在（与 hUploadOrMkdir 一致）。
	if _, err := os.Stat(dirpath); os.IsNotExist(err) {
		if err := os.MkdirAll(dirpath, os.ModePerm); err != nil {
			log.Printf("fetch mkdir %q: %v", util.SanitizeLogInput(dirpath), err)
			http.Error(w, "Directory create failed", http.StatusInternalServerError)
			return
		}
	}

	// 不跟随重定向，指向内网地址的 30x 既绕不过 URL 校验也走不过安全拨号器。
	client := &http.Client{
		Timeout: fetchTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext: safeDialContext,
		},
	}
	httpReq, _ := http.NewRequestWithContext(req.Context(), "GET", parsed.String(), nil)
	httpResp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("fetch %q: %v", util.SanitizeLogInput(parsed.String()), err)
		http.Error(w, "Fetch failed", http.StatusBadGateway)
		return
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		http.Error(w, fmt.Sprintf("Remote returned %d %s", httpResp.StatusCode, httpResp.Status), http.StatusBadGateway)
		return
	}

	// 远端声明 Content-Length 时提前拒绝超限资源；下面的拷贝本身也有上限。
	if httpResp.ContentLength > maxFetchSize {
		http.Error(w, fmt.Sprintf("Remote file too large (max %d bytes)", maxFetchSize), http.StatusRequestEntityTooLarge)
		return
	}

	var oldSize int64
	if fi, statErr := os.Stat(dstPath); statErr == nil && !fi.IsDir() {
		oldSize = fi.Size()
	}

	// 原子且有界：失败的或超限的下载既不会留下半截文件，也不会把无界数据流写入磁盘。
	written, copyErr := s.writeFileAtomic(dstPath, httpResp.Body, maxFetchSize)
	if copyErr != nil {
		log.Printf("fetch copy %q: %v", util.SanitizeLogInput(dstPath), copyErr)
		if errors.Is(copyErr, errTooLarge) {
			http.Error(w, fmt.Sprintf("Remote file too large (max %d bytes)", maxFetchSize), http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Fetch copy failed", http.StatusBadGateway)
		return
	}

	dirInfoSize.addDelta(s.relFromRoot(dstPath), written-oldSize)
	s.invalidateAccessConf()

	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{
		"success":     true,
		"destination": s.relFromRoot(dstPath),
		"size":        written,
		"source":      parsed.String(),
	})
}

type FileJSONInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Size    int64  `json:"size"`
	Path    string `json:"path"`
	ModTime int64  `json:"mtime"`
	Extra   any    `json:"extra,omitempty"`
	// Md5 与 Sha256 是十六进制摘要，仅对不超过 maxHashSize 的文件填充；目录与超大文件为空。
	Md5    string `json:"md5,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
}

// maxHashSize 限制 hInfo 中即时哈希的文件大小。
const maxHashSize int64 = 512 * 1024 * 1024 // 512 MiB

// computeFileHash 用 32 KiB 缓冲区单次遍历文件，同步更新 MD5 与 SHA256 并返回两者的十六进制摘要。
func computeFileHash(path string) (md5sum, sha256sum string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	md5h := md5.New()
	sha256h := sha256.New()
	buf := make([]byte, 32*1024)
	for {
		n, e := f.Read(buf)
		if n > 0 {
			md5h.Write(buf[:n])
			sha256h.Write(buf[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", "", e
		}
	}
	return hex.EncodeToString(md5h.Sum(nil)), hex.EncodeToString(sha256h.Sum(nil)), nil
}

// parseApkInfo 解析 APK 包信息；path 须为绝对路径。
func parseApkInfo(path string) (ai *ApkInfo) {
	defer func() {
		if err := recover(); err != nil {
			log.Println("parse-apk-info panic:", err)
		}
	}()
	apkf, err := apk.OpenFile(path)
	if err != nil {
		return
	}
	ai = &ApkInfo{}
	ai.MainActivity, _ = apkf.MainActivity()
	ai.PackageName = apkf.PackageName()
	ai.Version.Code = int(apkf.Manifest().VersionCode.MustInt32())
	ai.Version.Name = apkf.Manifest().VersionName.MustString()
	return
}

func (s *HTTPStaticServer) hInfo(w http.ResponseWriter, r *http.Request) {
	path := mux.Vars(r)["path"]
	relPath := s.getRealPath(r)

	fi, err := os.Stat(relPath)
	if err != nil {
		// 不回显 err —— 其中含服务器绝对路径。
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
		} else {
			log.Printf("info stat %q: %v", util.SanitizeLogInput(relPath), err)
			http.Error(w, "Stat failed", http.StatusInternalServerError)
		}
		return
	}
	fji := &FileJSONInfo{
		Name:    fi.Name(),
		Size:    fi.Size(),
		Path:    path,
		ModTime: fi.ModTime().UnixNano() / 1e6,
	}
	ext := filepath.Ext(path)
	switch ext {
	case ".md":
		fji.Type = "markdown"
	case ".apk":
		fji.Type = "apk"
		fji.Extra = parseApkInfo(relPath)
	case ".ipa":
		// IPA 无效时退化为无元数据，而不是报错。
		fji.Type = "ipa"
		if plinfo, perr := ipa.ParseIPA(relPath); perr == nil && plinfo != nil {
			fji.Extra = plinfo
		} else {
			fji.Extra = nil
		}
	case "":
		fji.Type = "dir"
	default:
		fji.Type = "text"
	}
	// 哈希失败不致命，size/mtime/path 仍有价值。
	if !fi.IsDir() && fi.Size() > 0 && fi.Size() <= maxHashSize {
		if md5sum, sha, herr := computeFileHash(relPath); herr == nil {
			fji.Md5 = md5sum
			fji.Sha256 = sha
		}
	}
	data, _ := json.Marshal(fji)
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (s *HTTPStaticServer) hZip(w http.ResponseWriter, r *http.Request) {
	target, ok := s.resolveRequestPath(r)
	if !ok {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	// Stat 先行：根目录无法 lstat 时 filepath.Walk 会向回调传入 nil 的 os.FileInfo。
	if _, err := os.Stat(target); err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	zipx.CompressToZip(w, target)
}

// zipMultiRequest 是前端多选下载提交的请求体：URL 解码后的路径，每个可为文件或目录。
type zipMultiRequest struct {
	Paths []string `json:"paths"`
}

// hZipMulti 流式生成一个 zip，每个请求条目以其 basename 打包；缺失或不可读的条目跳过——尽力而为的下载，而非事务性归档。
func (s *HTTPStaticServer) hZipMulti(w http.ResponseWriter, r *http.Request) {
	// 解码前先限制请求体，防止超大 JSON 耗尽内存。
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)

	var req zipMultiRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Paths) == 0 {
		http.Error(w, "No paths provided", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="download.zip"`)
	w.Header().Set("X-Accel-Buffering", "no") // 不让代理缓冲压缩包

	zw := &zipx.Zip{Writer: zip.NewWriter(w)}
	defer zw.Close()

	for _, p := range req.Paths {
		realPath := s.resolvePath(p)
		// 复核解析后的路径仍在 Root 之下（纵深防御，防目录穿越）。
		if !isUnderRoot(realPath, s.Root) {
			log.Printf("zip-multi path traversal blocked: %q", util.SanitizeLogInput(realPath))
			continue
		}
		info, err := os.Stat(realPath)
		if err != nil {
			log.Printf("zip-multi skip %q: %v", util.SanitizeLogInput(realPath), err)
			continue
		}

		entryName := filepath.Base(realPath)
		if entryName == "" || entryName == "." || entryName == string(filepath.Separator) {
			continue
		}

		if info.IsDir() {
			dirName := entryName + "/"
			walkErr := filepath.Walk(realPath, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					// 跳过读不了的条目，而不是中断整个归档。
					log.Printf("zip-multi walk skip %q: %v", util.SanitizeLogInput(path), err)
					return nil
				}
				if fi.Name() == YAMLCONF {
					return nil
				}
				rel, relErr := filepath.Rel(realPath, path)
				if relErr != nil {
					return nil
				}
				zipPath := dirName + filepath.ToSlash(rel)
				if fi.IsDir() {
					return zw.Add(zipPath+"/", path)
				}
				return zw.Add(zipPath, path)
			})
			if walkErr != nil {
				log.Printf("zip-multi walk %q: %v", util.SanitizeLogInput(realPath), walkErr)
			}
			continue
		}

		if err := zw.Add(entryName, realPath); err != nil {
			log.Printf("zip-multi add %q: %v", util.SanitizeLogInput(realPath), err)
		}
	}
}

func (s *HTTPStaticServer) hUnzip(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	zipPath, path := vars["zip_path"], vars["path"]

	// zipPath 来自 URL，必须校验其仍在 s.Root 内，否则 /-/unzip/../../etc/passwd/-/foo 能读取磁盘上任意 zip。
	absZip := filepath.Join(s.Root, filepath.FromSlash(zipPath))
	if !isUnderRoot(absZip, s.Root) {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	ctype := mime.TypeByExtension(filepath.Ext(path))
	if ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	err := zipx.ExtractFromZip(absZip, path, w)
	if err != nil {
		// 不回显 err —— 其中含归档文件的绝对路径。
		log.Printf("unzip %q: %v", util.SanitizeLogInput(absZip), err)
		http.Error(w, "Extract failed", http.StatusInternalServerError)
		return
	}
}

func combineURL(r *http.Request, path string) *url.URL {
	return &url.URL{
		Scheme: r.URL.Scheme,
		Host:   r.Host,
		Path:   path,
	}
}

func (s *HTTPStaticServer) hPlist(w http.ResponseWriter, r *http.Request) {
	path := mux.Vars(r)["path"]
	// 把 *.plist 改名为 *.ipa
	if filepath.Ext(path) == ".plist" {
		path = path[0:len(path)-6] + ".ipa"
	}

	relPath := s.getRealPath(r)
	plinfo, err := ipa.ParseIPA(relPath)
	if err != nil {
		// parseIPA 的错误可能含服务器绝对路径。
		log.Printf("plist parse %q: %v", util.SanitizeLogInput(relPath), err)
		http.Error(w, "Failed to parse IPA", http.StatusInternalServerError)
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseURL := &url.URL{
		Scheme: scheme,
		Host:   r.Host,
	}
	data, err := ipa.GenerateDownloadPlist(baseURL, path, plinfo)
	if err != nil {
		log.Printf("plist gen: %v", err)
		http.Error(w, "Failed to generate plist", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/xml")
	w.Write(data)
}

func (s *HTTPStaticServer) hIpaLink(w http.ResponseWriter, r *http.Request) {
	path := mux.Vars(r)["path"]
	var plistUrl string

	if r.URL.Scheme == "https" {
		plistUrl = combineURL(r, "/-/ipa/plist/"+path).String()
	} else if s.PlistProxy != "" {
		httpPlistLink := "http://" + r.Host + "/-/ipa/plist/" + path
		url, err := s.genPlistLink(httpPlistLink)
		if err != nil {
			log.Printf("plist proxy %q: %v", httpPlistLink, err)
			http.Error(w, "Failed to generate plist link", http.StatusInternalServerError)
			return
		}
		plistUrl = url
	} else {
		http.Error(w, "500: Server should be https:// or provide valid plistproxy", 500)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	log.Println("PlistURL:", plistUrl)
	renderHTML(w, "ipa-install", ipaInstallHTML, map[string]string{
		"Name":      filepath.Base(path),
		"PlistLink": plistUrl,
	})
}

// plistProxyClient 使用安全拨号器，防止伪造的 Host 头把 genPlistLink 变成 SSRF 通道，并用超时限制请求。
var plistProxyClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DialContext: safeDialContext,
	},
}

func (s *HTTPStaticServer) genPlistLink(httpPlistLink string) (plistUrl string, err error) {
	pp := s.PlistProxy
	if pp == "" {
		pp = defaultPlistProxy
	}
	resp, err := plistProxyClient.Get(httpPlistLink)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	retData, err := plistProxyClient.Post(pp, "text/xml", bytes.NewBuffer(data))
	if err != nil {
		return
	}
	defer retData.Body.Close()

	jsonData, _ := io.ReadAll(retData.Body)
	var ret map[string]string
	if err = json.Unmarshal(jsonData, &ret); err != nil {
		return
	}
	plistUrl = pp + "/" + ret["key"]
	return
}

type HTTPFileInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Type    string `json:"type"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mtime"`
}

type AccessTable struct {
	Regex string `yaml:"regex"`
	Allow bool   `yaml:"allow"`
}

type UserControl struct {
	Email  string
	Upload bool
	Delete bool
	Edit   bool
	Token  string
}

type AccessConf struct {
	Upload       bool          `yaml:"upload" json:"upload"`
	Delete       bool          `yaml:"delete" json:"delete"`
	Edit         bool          `yaml:"edit" json:"edit"`
	Users        []UserControl `yaml:"users" json:"users"`
	AccessTables []AccessTable `yaml:"accessTables"`
}

// rawAccessConf 是 .ghs.yml 的磁盘结构，用指针字段区分「键未声明」与「显式设为 false/空」，
// 子目录因此只覆盖自己声明的键，其余继承父级。
type rawAccessConf struct {
	Upload       *bool          `yaml:"upload"`
	Delete       *bool          `yaml:"delete"`
	Edit         *bool          `yaml:"edit"`
	Users        *[]UserControl `yaml:"users"`
	AccessTables *[]AccessTable `yaml:"accessTables"`
}

// parseAccessConfFile 解析单个 .ghs.yml；FileCache 未命中或 mtime 变化时调用。
func parseAccessConfFile(data []byte) (*rawAccessConf, error) {
	var raw rawAccessConf
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return &raw, nil
}

// mergeInto 只覆盖本文件显式声明的键；未声明的字段保留继承值，声明的（含 false 或空列表）则替换。
func (raw *rawAccessConf) mergeInto(ac *AccessConf) {
	if raw.Upload != nil {
		ac.Upload = *raw.Upload
	}
	if raw.Delete != nil {
		ac.Delete = *raw.Delete
	}
	if raw.Edit != nil {
		ac.Edit = *raw.Edit
	}
	if raw.Users != nil {
		ac.Users = *raw.Users
	}
	if raw.AccessTables != nil {
		ac.AccessTables = *raw.AccessTables
	}
}

// reCache 缓存已编译的访问表正则，用 RWMutex 保护，因为处理器协程会并发读取。
var (
	reCache   = make(map[string]*regexp.Regexp)
	reCacheMu sync.RWMutex
)

func compileCached(pattern string) *regexp.Regexp {
	reCacheMu.RLock()
	if re, ok := reCache[pattern]; ok {
		reCacheMu.RUnlock()
		return re
	}
	reCacheMu.RUnlock()
	// 在写锁之外编译，慢正则不会阻塞其他读者；regexp.Compile 对非法输入也不 panic。
	re, _ := regexp.Compile(pattern)
	reCacheMu.Lock()
	// 可能已有协程并发写入同一 key；保留先到者，维持调用方已拿到指针的一致性。
	if existing, ok := reCache[pattern]; ok {
		re = existing
	} else {
		reCache[pattern] = re
	}
	reCacheMu.Unlock()
	return re
}

func (c *AccessConf) canAccess(fileName string) bool {
	for _, table := range c.AccessTables {
		pattern := compileCached(table.Regex)
		if pattern == nil {
			continue
		}
		if pattern.MatchString(fileName) {
			return table.Allow
		}
	}
	return true
}

func (c *AccessConf) canDelete(r *http.Request) bool {
	userInfo := auth.UserFromSession(r)
	if userInfo == nil {
		return c.Delete
	}
	for _, rule := range c.Users {
		if rule.Email == userInfo.Email {
			return rule.Delete
		}
	}
	return c.Delete
}

func (c *AccessConf) canUploadByToken(token string) bool {
	for _, rule := range c.Users {
		// 常数时间比较，防止时序攻击逐字节泄露 token。
		if subtle.ConstantTimeCompare([]byte(rule.Token), []byte(token)) == 1 {
			return rule.Upload
		}
	}
	return c.Upload
}

// canEditByToken 是 canEdit 的 token 分支，单独拆出给「请求体即文件内容」的处理器用（r.FormValue 会把请求体读空）。
func (c *AccessConf) canEditByToken(token string) bool {
	for _, rule := range c.Users {
		if subtle.ConstantTimeCompare([]byte(rule.Token), []byte(token)) == 1 {
			return rule.Edit
		}
	}
	return c.Edit
}

func (c *AccessConf) canEditSession(r *http.Request) bool {
	userInfo := auth.UserFromSession(r)
	if userInfo == nil {
		return c.Edit
	}
	for _, rule := range c.Users {
		if rule.Email == userInfo.Email {
			return rule.Edit
		}
	}
	return c.Edit
}

func (c *AccessConf) canUploadSession(r *http.Request) bool {
	userInfo := auth.UserFromSession(r)
	if userInfo == nil {
		return c.Upload
	}
	for _, rule := range c.Users {
		if rule.Email == userInfo.Email {
			return rule.Upload
		}
	}
	return c.Upload
}

func (c *AccessConf) canEdit(r *http.Request) bool {
	token := r.FormValue("token")
	if token != "" {
		return c.canEditByToken(token)
	}
	userInfo := auth.UserFromSession(r)
	if userInfo == nil {
		return c.Edit
	}
	for _, rule := range c.Users {
		if rule.Email == userInfo.Email {
			return rule.Edit
		}
	}
	return c.Edit
}

func (c *AccessConf) canUpload(r *http.Request) bool {
	token := r.FormValue("token")
	if token != "" {
		return c.canUploadByToken(token)
	}
	userInfo := auth.UserFromSession(r)
	if userInfo == nil {
		return c.Upload
	}
	for _, rule := range c.Users {
		if rule.Email == userInfo.Email {
			return rule.Upload
		}
	}
	return c.Upload
}

func (s *HTTPStaticServer) hJSONList(w http.ResponseWriter, r *http.Request) {
	requestPath := mux.Vars(r)["path"]
	realPath := s.getRealPath(r)
	// 目录的根相对斜杠形式（共享根为空串）。搜索索引与目录大小缓存都以
	// 文件系统相对路径为键，而 requestPath 是 URL 形式且带 --prefix，两者不可混用。
	relDir := s.relFromRoot(realPath)
	if relDir == "." {
		relDir = ""
	}
	search := r.FormValue("search")
	auth := s.readAccessConf(realPath)
	auth.Upload = auth.canUpload(r)
	auth.Delete = auth.canDelete(r)
	auth.Edit = auth.canEdit(r)
	maxDepth := s.DeepPathMaxDepth

	fileInfoMap := make(map[string]os.FileInfo, 0)

	if search != "" {
		results := s.findIndex(search)
		if len(results) > 50 { // 最多 50 条
			results = results[:50]
		}
		// 按文件系统相对目录限定范围，而不是 URL 路径：后者带 --prefix，
		// 用它匹配在配置了 prefix 时匹配不到任何索引项。两边统一为斜杠——
		// filepath.HasPrefix 区分分隔符，在 Windows 上有隐蔽问题。
		prefix := relDir
		if prefix != "" {
			prefix += "/"
		}
		matchRoot := prefix == ""
		for _, item := range results {
			p := filepath.ToSlash(item.Path)
			if matchRoot || strings.HasPrefix(p, prefix) || p == relDir {
				// 索引项是根相对路径，而本 API 其余部分输出 URL 形式键；重新拼上 --prefix 使两条分支一致。
				key := strings.TrimPrefix(filepath.ToSlash(path.Join(s.Prefix, p)), "/")
				fileInfoMap[key] = item.Info
			}
		}
	} else {
		entries, err := os.ReadDir(realPath)
		if err != nil {
			// 不回显 err —— 其中含服务器绝对路径。
			log.Printf("readdir %q: %v", util.SanitizeLogInput(realPath), err)
			http.Error(w, "Failed to list directory", http.StatusInternalServerError)
			return
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			// 统一为斜杠：这些值会作为 URL 路径交给 SPA。
			key := strings.TrimPrefix(filepath.ToSlash(filepath.Join(requestPath, entry.Name())), "/")
			fileInfoMap[key] = info
		}
	}

	lrs := make([]HTTPFileInfo, 0)
	for itemPath, info := range fileInfoMap {
		if !auth.canAccess(info.Name()) {
			continue
		}
		lr := HTTPFileInfo{
			Name:    info.Name(),
			Path:    itemPath,
			ModTime: info.ModTime().UnixNano() / 1e6,
		}
		if search != "" {
			// 命中项相对所浏览目录展示；itemPath 是 URL 形式（带 --prefix），需去掉前缀还原为索引形式。
			relToRoot := strings.TrimPrefix(itemPath, strings.TrimPrefix(s.Prefix, "/")+"/")
			if relDir == "" {
				lr.Name = relToRoot
			} else if name, err := filepath.Rel(filepath.FromSlash(relDir), filepath.FromSlash(relToRoot)); err == nil {
				lr.Name = filepath.ToSlash(name)
			} else {
				lr.Name = relToRoot
			}
		}
		if info.IsDir() {
			name := filepath.ToSlash(deepPath(realPath, info.Name(), maxDepth))
			lr.Name = name
			lr.Path = strings.TrimPrefix(path.Join(path.Dir(itemPath), name), "/")
			lr.Type = "dir"
			// 大小缓存以文件系统相对路径为键（深合并名可能跨多层），而非 URL 形式的 lr.Path。
			childReal := filepath.Join(realPath, filepath.FromSlash(name))
			lr.Size = s.historyDirSize(s.relFromRoot(childReal))
		} else {
			lr.Type = "file"
			lr.Size = info.Size()
		}
		lrs = append(lrs, lr)
	}

	// 按名称排序后再序列化：fileInfoMap 遍历顺序随机，同 mtime 的条目否则顺序不定。
	sort.Slice(lrs, func(i, j int) bool {
		return lrs[i].Name < lrs[j].Name
	})

	// 只暴露三个生效的权限标志——原始 AccessConf 还带着每个用户的 upload/edit Token（CWE-200）。
	data, _ := json.Marshal(map[string]any{
		"files": lrs,
		"auth": map[string]bool{
			"upload": auth.Upload,
			"delete": auth.Delete,
			"edit":   auth.Edit,
		},
	})
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

var dirInfoSize = shardedDirSize{}

func (s *HTTPStaticServer) makeIndex() error {
	var indexes = make([]IndexFileItem, 0)
	var err = filepath.Walk(s.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("WARN: Visit path: %s error: %v", strconv.Quote(path), err)
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}

		path, _ = filepath.Rel(s.Root, path)
		path = filepath.ToSlash(path)
		indexes = append(indexes, IndexFileItem{Path: path, Info: info, Lower: strings.ToLower(path)})
		return nil
	})
	// 原子发布快照，使并发的 findIndex 读者不会看到写了一半的切片头；同时清空大小缓存，下次读取时重算。
	s.indexes.Store(&indexes)
	dirInfoSize = shardedDirSize{}
	return err
}

func (s *HTTPStaticServer) historyDirSize(dir string) int64 {
	// 统一为斜杠，使缓存键与各失效路径写入的键一致。
	dir = filepath.ToSlash(filepath.Clean(dir))

	if size, ok := dirInfoSize.peek(dir); ok {
		return size
	}

	// 遍历真实文件系统（而非每 10 分钟才重建的 s.indexes），并为子树内每个目录记录总量，
	// 因此列出含 N 个子目录的目录只需一次遍历。
	absDir := filepath.Join(s.Root, filepath.FromSlash(dir))
	totals := make(map[string]int64)
	filepath.Walk(absDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(s.Root, p)
		if relErr != nil {
			return nil
		}
		// 把文件字节数记入其所在目录及到遍历根之间的每一层（绝不到遍历根之上：
		// 祖先的总量不能由部分子树推导）。
		child := filepath.ToSlash(path.Dir(filepath.ToSlash(rel)))
		for {
			totals[child] += info.Size()
			if child == dir || child == "." || child == "/" || child == "" {
				break
			}
			parent := path.Dir(child)
			if parent == child {
				break
			}
			child = parent
		}
		return nil
	})

	dirInfoSize.mergeIn(totals)
	if size, ok := dirInfoSize.peek(dir); ok {
		return size
	}
	return totals[dir]
}

func (s *HTTPStaticServer) findIndex(text string) []IndexFileItem {
	ret := make([]IndexFileItem, 0)
	// 原子加载索引快照：makeIndex 可能并发替换指针，Store/Load 保证读到完整的新旧切片之一，不会撕裂。
	ptr := s.indexes.Load()
	if ptr == nil {
		return ret
	}
	// 每个关键词只小写一次，而不是每个文件×关键词重复转换。
	keywords := make([]string, 0, 4)
	needs := make([]bool, 0, 4)
	for keyword := range strings.FieldsSeq(text) {
		needContains := true
		if strings.HasPrefix(keyword, "-") {
			needContains = false
			keyword = keyword[1:]
		}
		if keyword == "" {
			continue
		}
		keywords = append(keywords, strings.ToLower(keyword))
		needs = append(needs, needContains)
	}
	for _, item := range *ptr {
		ok := true
		// 搜索算法：空格表示 AND
		for i, keyword := range keywords {
			ok = (needs[i] == strings.Contains(item.Lower, keyword))
			if !ok {
				break
			}
		}
		if ok {
			ret = append(ret, item)
		}
	}
	return ret
}

func (s *HTTPStaticServer) defaultAccessConf() AccessConf {
	return AccessConf{
		Upload: s.Upload,
		Delete: s.Delete,
		Edit:   s.Edit,
	}
}

// readAccessConf 自根向下合并 realPath 各级目录的 .ghs.yml，嵌套文件因此只覆盖自己声明的键。
// 刻意用迭代而非递归：对 Root 之上的路径，向父级递归永不终止（Dir 到卷根后不再变化，
// 而 Rel 持续返回 ".."），曾导致单个请求就把进程打成 "fatal error: stack overflow"。
func (s *HTTPStaticServer) readAccessConf(realPath string) (ac AccessConf) {
	ac = s.defaultAccessConf()

	root := s.rootPath()
	dir := filepath.Clean(realPath)
	if util.IsFile(dir) {
		dir = filepath.Dir(dir)
	}
	// Root 之外的路径不贡献任何配置——也不能被遍历。
	if !s.insideRoot(dir) {
		return ac
	}

	// 先收集 root → dir 链，再从根向下合并。
	chain := make([]string, 0, 8)
	for {
		chain = append(chain, dir)
		if dir == root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		if !s.insideRoot(parent) {
			break
		}
		dir = parent
	}

	for i := len(chain) - 1; i >= 0; i-- {
		cfgFile := filepath.Join(chain[i], YAMLCONF)
		raw, err := s.accessConfCache.Get(cfgFile)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("Err read .ghs.yml: %v", err)
			}
			continue
		}
		if raw == nil {
			continue
		}
		raw.mergeInto(&ac)
	}
	return ac
}

// invalidateAccessConf 清空 .ghs.yml 解析缓存，使处理器改动配置后下一个请求从磁盘重读。
func (s *HTTPStaticServer) invalidateAccessConf() {
	if s.accessConfCache != nil {
		s.accessConfCache.InvalidateAll()
	}
}

// deepPath 把仅含单个子目录的目录链向后合并，最多到 maxDepth 层。
func deepPath(basedir, name string, maxDepth int) string {
	for depth := 0; depth <= maxDepth; depth += 1 {
		entries, err := os.ReadDir(filepath.Join(basedir, name))
		if err != nil || len(entries) != 1 {
			break
		}
		if entries[0].IsDir() {
			name = filepath.ToSlash(filepath.Join(name, entries[0].Name()))
		} else {
			break
		}
	}
	return name
}

const ipaInstallHTML = `<!DOCTYPE html>
<html>
<head>
  <title>[[.Name]] install</title>
  <meta http-equiv="Content-Type" content="text/HTML; charset=utf-8">
  <meta content="target-densitydpi=device-dpi,width=640" name="viewport" id="viewport">
  <script>
    function showById(name) {
      document.getElementById(name).style.display = 'block';
    }
    function checkBrowerAndDownload() {
      var ua = navigator.userAgent.toLowerCase();
      var isIOS = /iphone|ipad|ipod/.test(ua);
      var isAndroid = /android/.test(ua);
      var isWechat = /micromessenger/.test(ua);
      var plistLink = "[[.PlistLink]]";
      var ipaInstallLink = 'itms-services://?action=download-manifest&url=' + plistLink;
      document.getElementById('itms-link').href = ipaInstallLink;
      if (isWechat) {
        showById('safari');
        location.href = ipaInstallLink;
      } else if (isAndroid) {
        showById('android');
      } else if (isIOS) {
        showById('safari');
        location.href = ipaInstallLink;
      } else {
        showById('browser');
      }
    }
  </script>
</head>
<body>
  <div id="browser" style="display:none">
    This is IPA install page, open this link with your iPhone.
  </div>
  <div id="safari" style="display:none">
    If install not started soon, click <a id="itms-link" href="#">here</a>
  </div>
  <div id="android" style="display:none">
    This is IPA install page, not for android.
  </div>
  <script>checkBrowerAndDownload();</script>
</body>
</html>`

const videoPlayerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Video Player - [[.FileName]]</title>
    <style>
        body, html { margin:0; padding:0; height:100%; width:100%; overflow:hidden; background-color:#000; }
        .video-container { display:flex; flex-direction:column; justify-content:center; align-items:center; height:100%; }
        video { max-width:100%; max-height:100%; }
        h1 { color:#fff; font-size:24px; margin-bottom:20px; }
    </style>
</head>
<body>
    <div class="video-container">
        <video id="videoPlayer" controls autoplay>
            <source src="[[.VideoURL]]" type="video/[[.Extension]]">
            Your browser does not support the video tag.
        </video>
    </div>
    <script>
        document.addEventListener('DOMContentLoaded', function() {
            document.getElementById('videoPlayer').focus();
        });
    </script>
</body>
</html>`

var funcMap = template.FuncMap{
	"title": strings.Title,
}

// _tmpls 按名称缓存已编译的 HTML 模板；用 sync.Map 因为 renderHTML 会被多个处理器协程并发调用。
var _tmpls sync.Map

func renderHTML(w http.ResponseWriter, name, content string, v any) {
	var t *template.Template
	if v, ok := _tmpls.Load(name); ok {
		t = v.(*template.Template)
	} else {
		t = template.Must(template.New(name).Funcs(funcMap).Delims("[[", "]]").Parse(content))
		actual, _ := _tmpls.LoadOrStore(name, t)
		t = actual.(*template.Template)
	}
	t.Execute(w, v)
}

func checkFilename(name string) error {
	if strings.ContainsAny(name, "\\/:*<>|") {
		return errors.New("name should not contain \\/:*<>|")
	}
	// SECURITY: .ghs.yml 不允许通过 HTTP 写入——否则仅有上传权限的用户可为自己授予 delete/edit。
	if name == YAMLCONF {
		return errors.New("reserved file name")
	}
	return nil
}

// isUnderRoot 在双方共同 Clean 后判断 path 是 root 本身还是其子孙——破坏性文件操作前的纵深防御。
func isUnderRoot(path, root string) bool {
	cleanRoot := filepath.Clean(root)
	cleanPath := filepath.Clean(path)
	if cleanPath == cleanRoot {
		return true
	}
	return strings.HasPrefix(cleanPath+string(filepath.Separator),
		cleanRoot+string(filepath.Separator))
}

// checkPathSegment 是校验相对路径时逐段的 checkFilename 变体：调用方已按 "/" 拆分，
// 故去掉分隔符规则，并拒绝空、"."、".." 段以封堵向父目录的逃逸。
func checkPathSegment(seg string) error {
	if seg == "" || seg == "." || seg == ".." {
		return errors.New("invalid path segment")
	}
	if strings.ContainsAny(seg, "\\:*<>|\"\x00") {
		return errors.New("path segment should not contain \\:*<>|\"")
	}
	// 同 checkFilename：.ghs.yml 绝不允许通过 HTTP 写入。
	if seg == YAMLCONF {
		return errors.New("reserved file name")
	}
	return nil
}

func (s *HTTPStaticServer) hVideoPlayer(w http.ResponseWriter, r *http.Request) {
	path := mux.Vars(r)["path"]
	realPath := s.getRealPath(r)
	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))

	if _, err := os.Stat(realPath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	fileName := filepath.Base(path)

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	videoURL := fmt.Sprintf("%s://%s/%s", scheme, r.Host, path)

	renderHTML(w, "video-player", videoPlayerHTML, map[string]any{
		"FileName":  fileName,
		"VideoURL":  videoURL,
		"Extension": extension,
	})
}

package server

import (
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
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-yaml/yaml"
	"github.com/gorilla/mux"
	"github.com/shogo82148/androidbinary/apk"

	"github.com/codeskyblue/gohttpserver/internal/auth"
	"github.com/codeskyblue/gohttpserver/internal/ipa"
	"github.com/codeskyblue/gohttpserver/internal/util"
	"github.com/codeskyblue/gohttpserver/internal/zipx"
)

const YAMLCONF = ".ghs.yml"

// ContentSecurityPolicy 由最外层安全响应头中间件应用于每个响应；script-src 不含 'unsafe-inline'
// （前端脚本均为外链，内嵌页面经 nonce 授权，见 renderHTML），白名单不放行 GA/GTM 等统计域名。
const ContentSecurityPolicy = "default-src 'self'; script-src 'self' 'nonce-{CSP_NONCE}'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; media-src 'self'; connect-src 'self'; form-action 'self'; base-uri 'self'; object-src 'none'; frame-ancestors 'none'"

// cspNoncePlaceholder 是 CSP 中的 nonce 占位符：SecurityHeaders 中间件在每请求
// 生成随机 nonce 后替换它，并把同一个 nonce 通过 context 传给 renderHTML。
const cspNoncePlaceholder = "{CSP_NONCE}"

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
	// 按索引遍历：for _, sh := range &s.shards 会值拷贝含 sync.RWMutex 的分片（go vet 报错）。
	for i := range s.shards {
		sh := &s.shards[i]
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

	// indexHTMLCache 缓存内嵌 SPA 外壳字节，避免目录视图每请求 Open+ReadAll。
	indexHTMLCache atomic.Pointer[[]byte]

	m       *mux.Router
	bufPool sync.Pool
}

// accessConfMemo 缓存单次请求内已解析的目录访问配置，打包下载时避免逐文件重复 stat。
// walk 回调单协程顺序执行，无需加锁。
type accessConfMemo struct {
	s    *HTTPStaticServer
	memo map[string]AccessConf
}

func newAccessConfMemo(s *HTTPStaticServer) *accessConfMemo {
	return &accessConfMemo{s: s, memo: make(map[string]AccessConf)}
}

func (m *accessConfMemo) get(dir string) AccessConf {
	key := filepath.Clean(dir)
	if ac, ok := m.memo[key]; ok {
		return ac
	}
	ac := m.s.readAccessConf(dir)
	m.memo[key] = ac
	return ac
}

// spaIndexHTML 返回内嵌前端的 index.html 字节，进程内只读取一次（外壳只在换版本重编译时变化）。
func (s *HTTPStaticServer) spaIndexHTML() []byte {
	if b := s.indexHTMLCache.Load(); b != nil {
		return *b
	}
	if s.Assets == nil {
		return nil
	}
	f, err := s.Assets.Open("index.html")
	if err != nil {
		return nil
	}
	indexHTML, _ := io.ReadAll(f)
	f.Close()
	if len(indexHTML) == 0 {
		return nil
	}
	s.indexHTMLCache.Store(&indexHTML)
	return indexHTML
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

	m.HandleFunc("/-/ipa/plist/{path:.*}", s.hPlist)
	m.HandleFunc("/-/ipa/link/{path:.*}", s.hIpaLink)
	m.HandleFunc("/-/video-player/{path:.*}", s.hVideoPlayer)

	m.HandleFunc("/-/zip", s.hZipMulti).Methods("POST")

	m.HandleFunc("/-/fetch", s.hFetch).Methods("POST")

	m.HandleFunc("/-/info/{path:.*}", s.hInfo).Methods("GET")
	m.HandleFunc("/-/apk/info/{path:.*}", s.hInfo).Methods("GET")

	// 字面量 "/-/" 分隔 zip 路径与条目 glob，否则两个贪婪的 {path:.*} 变量会合并。
	m.HandleFunc("/-/unzip/{zip_path:.*}/-/{path:.*}", s.hUnzip).Methods("GET")

	m.HandleFunc("/{path:.*}", s.hIndex).Methods("GET", "HEAD")
	m.HandleFunc("/{path:.*}", s.hUploadOrMkdir).Methods("POST")
	m.HandleFunc("/{path:.*}", s.hEdit).Methods("PUT")
	m.HandleFunc("/{path:.*}", s.hDelete).Methods("DELETE")
	return s
}

func (s *HTTPStaticServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// SECURITY: 拒绝 URL 路径中的反斜杠——Windows 上 filepath.Clean 把 "\" 当分隔符，
	// "..\..\" 能通过路由并逃逸出 s.Root，在此统一封死整类穿越。
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

// stripPrefix 从（已 Clean、以 "/" 开头的）URL 路径按完整段剥离子路径前缀 prefix，
// "/share" 不会误剥 "/shareX/leak.txt"；返回不带前导 "/" 的相对路径，空串表示根。
func stripPrefix(prefix, urlPath string) string {
	if prefix == "" {
		return strings.TrimPrefix(urlPath, "/")
	}
	rest, ok := strings.CutPrefix(urlPath, prefix)
	if !ok {
		return ""
	}
	if rest == "" {
		return ""
	}
	// 剥完后必须落在路径边界上，否则 prefix 只是目标路径段的字符串前缀。
	if rest[0] != '/' {
		return ""
	}
	return strings.TrimPrefix(rest, "/")
}

// resolvePath 把（gorilla/mux 已解码的）URL 路径转换为 s.Root 下的绝对斜杠路径，由 filepath.Clean 折叠 ".."。
// SECURITY: 仅 Clean 不保证结果仍在 Root 内，会改动文件系统的调用方必须再用 insideRoot / resolveRequestPath 复核。
func (s *HTTPStaticServer) resolvePath(urlPath string) string {
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	cleanPath := filepath.ToSlash(filepath.Clean(urlPath))
	relativePath := stripPrefix(s.Prefix, cleanPath)
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

// isRootPath 判断已解析路径是否为共享根目录本身。
func (s *HTTPStaticServer) isRootPath(p string) bool {
	return filepath.Clean(p) == s.rootPath()
}

// resolveRequestPath 解析请求路径并返回是否仍在 Root 内；读写文件的处理器在 ok 为 false 时必须退出。
func (s *HTTPStaticServer) resolveRequestPath(r *http.Request) (string, bool) {
	p := s.getRealPath(r)
	return p, s.insideRoot(p)
}

func (s *HTTPStaticServer) hIndex(w http.ResponseWriter, r *http.Request) {
	path := mux.Vars(r)["path"]
	realPath, inside := s.resolveRequestPath(r)
	if !inside {
		// SECURITY: hIndex 是主文件读取通道，解析结果必须与写通道一样复核仍在 Root 内，
		// 否则路由拓扑或 prefix 剥离一旦有缺陷即成任意文件读。
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
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
		indexHTML := s.spaIndexHTML()
		if indexHTML == nil {
			http.Error(w, "Frontend not built", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	} else {
		// SECURITY: 访问控制文件含 users[].token，绝不通过 HTTP 提供（变体拦截见 isAccessConfName）。
		if isAccessConfName(filepath.Base(path)) {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		// SECURITY: 服务器私有文件（登录 DB、FRP 密钥）不能经 HTTP 读取——默认 --root 即工作目录，一次 GET 便得密码哈希。
		if isServerStateName(filepath.Base(path)) {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		// SECURITY: accessTables 的 allow:false 在列表接口隐藏文件，下载通道必须同样拒绝，否则隐藏形同虚设。
		if !s.canAccessPath(r, realPath, filepath.Base(path)) {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		// SECURITY: 上传的 HTML/SVG 同源渲染可借 'self' 脚本发起对管理 API 的已认证请求，
		// 强制 attachment 下载而非渲染，封死存储型 XSS 链。
		if forcesAttachment(realPath) {
			w.Header().Set("Content-Disposition", downloadDisposition(filepath.Base(path)))
		} else if r.FormValue("download") == "true" {
			w.Header().Set("Content-Disposition", downloadDisposition(filepath.Base(path)))
		}
		http.ServeFile(w, r, realPath)
	}
}

// inlineSafeExts 是可安全内联渲染、不会在本源执行脚本的扩展名白名单。用白名单而非黑名单：
// 未知扩展名（含无扩展名）一律按 attachment 下发，防 http.ServeFile 内容嗅探成 text/html 绕过。
var inlineSafeExts = map[string]bool{
	".txt": true, ".md": true, ".log": true, ".csv": true, ".tsv": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true,
	".ini": true, ".conf": true, ".cfg": true, ".env": true,
	".go": true, ".py": true, ".js": true, ".mjs": true, ".ts": true, ".tsx": true,
	".jsx": true, ".c": true, ".h": true, ".cpp": true, ".hpp": true, ".cc": true,
	".java": true, ".kt": true, ".rs": true, ".rb": true, ".php": true, ".pl": true,
	".sh": true, ".bat": true, ".ps1": true, ".css": true, ".scss": true, ".less": true,
	".html": false, ".htm": false, ".xhtml": false,
	".svg": false, ".svgz": false,
	".swf": false, ".xsl": false, ".xslt": false,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".bmp": true, ".ico": true, ".avif": true, ".heic": true, ".tiff": true,
	".mp4": true, ".webm": true, ".ogg": true, ".ogv": true, ".mov": true, ".avi": true,
	".mkv": true, ".flv": true, ".mp3": true, ".wav": true, ".flac": true, ".aac": true,
	".m4a": true, ".opus": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	".pdf": true,
}

// riskyInlineExts 是同源渲染时会执行内联脚本/事件的扩展名。白名单模式下仅用于
// 文档与错误信息的显式说明；inlineSafeExts 是实际判定依据。
var riskyInlineExts = map[string]bool{
	".html": true, ".htm": true, ".xhtml": true,
	".svg": true, ".svgz": true,
	".xml": true, ".xsl": true, ".xslt": true,
	".swf": true,
}

// maxSniffCheckBytes 是内容嗅探兜底读取的字节数上限（足够覆盖常见文档头）。
const maxSniffCheckBytes = 512

// forcesAttachment 判断路径是否必须以附件下发，防止浏览器在本源执行脚本：扩展名不在
// inlineSafeExts 内一律 attachment（含无扩展名），白名单扩展名再经内容嗅探兜底。
func forcesAttachment(realPath string) bool {
	base := filepath.Base(realPath)
	// 访问控制文件与服务器私有文件不应走到这里（调用方已拦截），双保险。
	if isAccessConfName(base) || isServerStateName(base) {
		return true
	}

	ext := strings.ToLower(filepath.Ext(realPath))
	safe, known := inlineSafeExts[ext]
	if !known {
		return true
	}
	if !safe {
		return true
	}
	return looksLikeHTMLCached(realPath)
}

// looksLikeHTML 读取文件前若干字节判断是否含 HTML 标签特征（http.ServeFile 会把此类文件
// 判为 text/html 内联渲染）。读取失败时保守返回 false，由调用方按扩展名策略处理。
func looksLikeHTML(realPath string) bool {
	f, err := os.Open(realPath)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, maxSniffCheckBytes)
	n, _ := f.Read(buf)
	if n <= 0 {
		return false
	}
	head := bytes.ToLower(buf[:n])
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf")) // UTF-8 BOM
	head = bytes.TrimSpace(head)
	htmlMarkers := [][]byte{
		[]byte("<!doctype"), []byte("<html"), []byte("<head"),
		[]byte("<body"), []byte("<?xml"), []byte("<svg"),
		[]byte("<script"), []byte("<iframe"), []byte("<!entity"),
	}
	for _, marker := range htmlMarkers {
		if bytes.HasPrefix(head, marker) {
			return true
		}
	}
	return bytes.Contains(head, []byte("<meta")) ||
		bytes.Contains(head, []byte("<title")) ||
		bytes.Contains(head, []byte("<link"))
}

// sniffCacheMax 是嗅探结果缓存的条数上限（固定环形槽位，O(1) 均摊淘汰）。
const sniffCacheMax = 1024

// sniffCacheTTL 是缓存判定的最长陈旧时间，防 mtime 被刻意回拨的文件无限期沿用旧判定。
const sniffCacheTTL = time.Minute

type sniffEntry struct {
	size    int64
	modTime time.Time
	verdict bool
	at      time.Time
}

var (
	sniffCacheMu sync.Mutex
	sniffCache   = make(map[string]sniffEntry, sniffCacheMax)
	sniffRing    [sniffCacheMax]string
	sniffCursor  int
)

// looksLikeHTMLCached 包装 looksLikeHTML，判定按 (size, mtime) 失效并加 TTL 兜底，
// 避免每次下载重复嗅探；文件内容变化必然改变 mtime，下次下载即重嗅探。
func looksLikeHTMLCached(realPath string) bool {
	key := filepath.Clean(realPath)
	fi, err := os.Stat(key)
	if err != nil {
		// stat 失败时直接走原实现，保证判定口径与未缓存时一致。
		return looksLikeHTML(realPath)
	}

	sniffCacheMu.Lock()
	e, ok := sniffCache[key]
	sniffCacheMu.Unlock()
	if ok && e.size == fi.Size() && e.modTime.Equal(fi.ModTime()) && time.Since(e.at) < sniffCacheTTL {
		return e.verdict
	}

	verdict := looksLikeHTML(realPath)

	sniffCacheMu.Lock()
	if len(sniffCache) >= sniffCacheMax {
		// 环形槽位覆盖最旧条目，同键偶发提前淘汰只多一次重嗅探，不影响正确性。
		old := sniffRing[sniffCursor%sniffCacheMax]
		delete(sniffCache, old)
	}
	sniffRing[sniffCursor%sniffCacheMax] = key
	sniffCursor++
	sniffCache[key] = sniffEntry{
		size:    fi.Size(),
		modTime: fi.ModTime(),
		verdict: verdict,
		at:      time.Now(),
	}
	sniffCacheMu.Unlock()
	return verdict
}

// canAccessPath 按目标路径所在目录的 .ghs.yml 判断请求者能否访问该文件名。
// 供列表之外的全部文件读取通道（raw 下载、info、打包）统一调用。
func (s *HTTPStaticServer) canAccessPath(r *http.Request, realPath, baseName string) bool {
	dir := filepath.Dir(realPath)
	if !s.insideRoot(dir) {
		// Root 之外无配置可读；保守起见拒绝。
		return false
	}
	ac := s.readAccessConf(dir)
	return ac.canAccess(baseName)
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
	// SECURITY: 服务器私有文件绝不允许经 HTTP 删除——删除后重启即回退默认凭据而被接管。
	if isServerStateName(filepath.Base(realPath)) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	// SECURITY: 访问控制文件同样不可删除——删除会把整个目录的访问配置重置回全局默认。
	if isAccessConfName(filepath.Base(realPath)) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	// SECURITY: os.RemoveAll 是递归的，删除目录不得连带其内的服务器私有文件。
	if containsServerStateFile(realPath) {
		http.Error(w, "Refusing to delete a directory containing server state files", http.StatusBadRequest)
		return
	}

	// 先记下即将消失的字节数，以便按增量修正目录大小缓存，而不是整体丢弃。
	removed, sizeKnown := dirInfoSize().removedSize(realPath, s.Root)

	err := os.RemoveAll(realPath)
	if err != nil {
		// 不回显 err —— 其中含服务器绝对路径。
		log.Printf("delete %q failed: %v", util.SanitizeLogInput(realPath), err)
		http.Error(w, "Delete failed", http.StatusInternalServerError)
		return
	}
	rel := s.relFromRoot(realPath)
	if sizeKnown {
		dirInfoSize().addDelta(rel, -removed)
	}
	// 删除子树自身条目；若其大小原本未知，则连祖先一起失效，下次读取时从磁盘重算。
	dirInfoSize().dropSubtree(rel)
	if !sizeKnown {
		dirInfoSize().invalidatePath(rel)
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

	// SECURITY: 限制请求体必须在鉴权之前——canUpload 的 r.FormValue 会触发 multipart
	// 全量解析并写入 os.TempDir()，解析后再限制挡不住未认证客户端占满临时目录。
	req.Body = http.MaxBytesReader(w, req.Body, maxUploadBytes)

	auth := s.readAccessConf(dirpath)
	if !auth.canUpload(req) {
		http.Error(w, "Upload forbidden", http.StatusForbidden)
		return
	}

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
		// 纵深防御：逐段校验已拒绝 ".."，仍复核最终路径未离开 Root。
		if !s.insideRoot(dstPath) {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}
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
		// 纵深防御：复核最终路径未离开 Root，兜底 filename 取 ".." 等名义值的意外。
		if !s.insideRoot(dstPath) {
			http.Error(w, "Invalid path", http.StatusForbidden)
			return
		}
	}

	// 记录覆盖前的旧大小，以便目录大小缓存按精确增量修正。
	var oldSize int64
	if fi, statErr := os.Stat(dstPath); statErr == nil && !fi.IsDir() {
		oldSize = fi.Size()
	}

	written, copyErr := s.writeFileAtomic(dstPath, file, 0)
	if copyErr != nil {
		log.Printf("upload copy %q: %v", util.SanitizeLogInput(dstPath), copyErr)
		http.Error(w, "Upload failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json;charset=utf-8")

	// 按精确增量修正目录大小缓存，而不是丢弃整个缓存。
	dirInfoSize().addDelta(s.relFromRoot(dstPath), written-oldSize)
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
		if err == nil {
			os.Remove(dstPath)
		}
		// 解压写入了大量文件，此目录及其祖先的缓存已失效：丢掉整条链，下次列表时重新遍历。
		dirInfoSize().invalidatePath(s.relFromRoot(dirpath))
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

// writeFileAtomic 先把 src 流式写入 dst 旁的临时文件，成功后才改名替换，目标文件不会被截断或出现写了一半的内容。
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
	// 访问控制文件不可通过 HTTP 写入（大小写不敏感 + 8.3 短名，见 isAccessConfName）。
	if isAccessConfName(filepath.Base(dstPath)) {
		http.Error(w, "Not editable", http.StatusForbidden)
		return
	}
	// SECURITY: 服务器私有文件（登录 DB、FRP 密钥）不可经 PUT 覆盖——伪造凭据库重启后即被以已知密码登录。
	if isServerStateName(filepath.Base(dstPath)) {
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
		http.Error(w, "Edit failed", http.StatusInternalServerError)
		return
	}

	dirInfoSize().addDelta(s.relFromRoot(dstPath), written-oldSize)
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

// safeDialContext 拒绝拨号到回环、链路本地、组播及私网地址，使 /-/fetch 无法令服务器攻击自身或所在网络；
// 在 DNS 解析后校验并直接连接已校验的 IP，防 DNS 重绑定的 TOCTOU。
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
			isPrivateIPv4(ip) || isZeroNetOrCGNAT(ip) {
			return nil, fmt.Errorf("refusing to connect to private/loopback address %s", ip)
		}
	}
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

// isZeroNetOrCGNAT 判断两类"准私网"段：0.0.0.0/8（部分平台解析为 localhost）与
// 100.64.0.0/10（CGNAT，拨进去到达网关而非公网主机）；语义上非 RFC1918 私网，SSRF 防护同样拒绝。
func isZeroNetOrCGNAT(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 0 {
			return true // 0.0.0.0/8
		}
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true // 100.64.0.0/10
		}
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

	if _, err := os.Stat(dirpath); os.IsNotExist(err) {
		if err := os.MkdirAll(dirpath, os.ModePerm); err != nil {
			log.Printf("fetch mkdir %q: %v", util.SanitizeLogInput(dirpath), err)
			http.Error(w, "Directory create failed", http.StatusInternalServerError)
			return
		}
	}

	httpReq, _ := http.NewRequestWithContext(req.Context(), "GET", parsed.String(), nil)
	httpResp, err := fetchClient.Do(httpReq)
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

	if httpResp.ContentLength > maxFetchSize {
		http.Error(w, fmt.Sprintf("Remote file too large (max %d bytes)", maxFetchSize), http.StatusRequestEntityTooLarge)
		return
	}

	var oldSize int64
	if fi, statErr := os.Stat(dstPath); statErr == nil && !fi.IsDir() {
		oldSize = fi.Size()
	}

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

	dirInfoSize().addDelta(s.relFromRoot(dstPath), written-oldSize)
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

// maxHashSize 限制 hInfo 即时哈希的文件大小：更大的文件同步读+双哈希会阻塞响应数秒，
// 且任何客户端都可随意触发，应由下载方自行校验。
const maxHashSize int64 = 64 * 1024 * 1024

// fileHashCacheMax 是哈希结果缓存的条数上限（固定环形槽位，O(1) 均摊淘汰）。
const fileHashCacheMax = 512

// fileHashEntry 缓存单个文件的哈希结果，按 (size, mtime) 失效。
type fileHashEntry struct {
	size    int64
	modTime time.Time
	md5sum  string
	sha256  string
}

var (
	fileHashCacheMu sync.Mutex
	fileHashCache   = make(map[string]fileHashEntry, fileHashCacheMax)
	fileHashRing    [fileHashCacheMax]string
	fileHashCursor  int
)

// computeFileHashCached 包装 computeFileHash，命中 (路径, size, mtime) 缓存时零 I/O。
func computeFileHashCached(path string, fi os.FileInfo) (md5sum, sha256sum string) {
	key := filepath.Clean(path)

	fileHashCacheMu.Lock()
	if e, ok := fileHashCache[key]; ok && e.size == fi.Size() && e.modTime.Equal(fi.ModTime()) {
		fileHashCacheMu.Unlock()
		return e.md5sum, e.sha256
	}
	fileHashCacheMu.Unlock()

	m5, s2, err := computeFileHash(path)
	if err != nil {
		return "", ""
	}

	fileHashCacheMu.Lock()
	if len(fileHashCache) >= fileHashCacheMax {
		// 环形槽位覆盖最旧条目，同键偶发提前淘汰只多一次重算，不影响正确性。
		old := fileHashRing[fileHashCursor%fileHashCacheMax]
		delete(fileHashCache, old)
	}
	fileHashRing[fileHashCursor%fileHashCacheMax] = key
	fileHashCursor++
	fileHashCache[key] = fileHashEntry{
		size:    fi.Size(),
		modTime: fi.ModTime(),
		md5sum:  m5,
		sha256:  s2,
	}
	fileHashCacheMu.Unlock()
	return m5, s2
}

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
	// SECURITY: 受保护文件连元数据也不暴露——?op=info 与 /-/info/ 直达本处理器，
	// 泄露的大小/哈希可被离线用于验证对 token/密码的猜测。
	if isAccessConfName(fi.Name()) || isServerStateName(fi.Name()) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	// SECURITY: accessTables 隐藏的目录不能借 ?op=info 泄露 size/mtime；根目录无父配置，始终放行。
	if !s.isRootPath(relPath) && !s.canAccessPath(r, relPath, fi.Name()) {
		http.Error(w, "Not found", http.StatusNotFound)
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
		fji.Md5, fji.Sha256 = computeFileHashCached(relPath, fi)
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
	fi, err := os.Stat(target)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	// SECURITY: accessTables 隐藏的文件不能经单文件打包绕过导出。
	if !fi.IsDir() && !s.canAccessPath(r, target, fi.Name()) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	s.compressToZipFiltered(w, r, target)
}

// compressToZipFiltered 在 CompressToZip 的基础上按访问规则逐文件过滤：
// 被各目录 .ghs.yml 的 accessTables 拒绝的文件不写入归档。
func (s *HTTPStaticServer) compressToZipFiltered(w http.ResponseWriter, r *http.Request, rootDir string) {
	// 同一次打包内目录配置不会变：memo 化避免每文件沿父链 stat。
	acMemo := newAccessConfMemo(s)
	filter := func(path string, fi os.FileInfo) bool {
		if fi.Name() == YAMLCONF || isAccessConfName(fi.Name()) || isServerStateName(fi.Name()) {
			return false
		}
		if fi.IsDir() {
			return true
		}
		dir := filepath.Dir(path)
		if !s.insideRoot(dir) {
			return false
		}
		ac := acMemo.get(dir)
		return ac.canAccess(fi.Name())
	}
	zipx.CompressToZipFiltered(w, rootDir, filter)
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

	zw, flushZip := zipx.NewBufferedZip(w)
	defer func() {
		zw.Close()
		flushZip()
	}()

	// 同一次多选打包内目录配置不会变：memo 化避免每文件沿父链 stat。
	acMemo := newAccessConfMemo(s)

	// 已处理的真实路径：同一路径被请求两次不应重复打包（去重），
	// 只有不同路径才可能产生同名条目需要错开。
	handledPaths := make(map[string]bool)
	// 已使用的 zip 条目名：不同目录的同名文件（/a/x.zip 与 /b/x.zip）若都以
	// basename 入包会产生两个同名条目，解压时互相覆盖静默丢文件——后到者加 (n) 后缀。
	usedEntries := make(map[string]bool)
	uniqueEntry := func(name string) string {
		// 目录条目天然以 "/" 结尾且 walk 内部已带路径，只有文件级条目（含顶层多选时
		// 的单文件）才需要去重；目录名与文件同条重名由 "../../*" 等路径天然区分。
		if !usedEntries[name] {
			usedEntries[name] = true
			return name
		}
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		for i := 2; ; i++ {
			cand := fmt.Sprintf("%s (%d)%s", base, i, ext)
			if !usedEntries[cand] {
				usedEntries[cand] = true
				return cand
			}
		}
	}

	for _, p := range req.Paths {
		if handledPaths[p] {
			continue
		}
		handledPaths[p] = true

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

		// SECURITY: accessTables 隐藏的条目（或其父目录配置了拒绝规则）不得经打包下载导出。
		if !info.IsDir() {
			blocked := false
			if dir := filepath.Dir(realPath); !s.insideRoot(dir) {
				blocked = true
			} else {
				ac := acMemo.get(dir)
				blocked = !ac.canAccess(entryName)
			}
			if blocked {
				log.Printf("zip-multi access denied: %q", util.SanitizeLogInput(realPath))
				continue
			}
		}
		// SECURITY: 受保护文件不得经单文件打包导出——walk 分支逐文件拦截，单文件分支同样拦截。
		if !info.IsDir() && (isAccessConfName(entryName) || isServerStateName(entryName)) {
			log.Printf("zip-multi protected file blocked: %q", util.SanitizeLogInput(realPath))
			continue
		}

		if info.IsDir() {
			dirName := uniqueEntry(entryName + "/")
			walkErr := filepath.Walk(realPath, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					log.Printf("zip-multi walk skip %q: %v", util.SanitizeLogInput(path), err)
					return nil
				}
				if fi.Name() == YAMLCONF || isAccessConfName(fi.Name()) || isServerStateName(fi.Name()) {
					return nil
				}
				if !fi.IsDir() {
					if dir := filepath.Dir(path); s.insideRoot(dir) {
						ac := acMemo.get(dir)
						if !ac.canAccess(fi.Name()) {
							return nil
						}
					}
				}
				rel, relErr := filepath.Rel(realPath, path)
				if relErr != nil {
					return nil
				}
				zipPath := dirName + filepath.ToSlash(rel)
				// walk 已给出 lstat 结果，直接复用省去每文件一次重复 lstat。
				if fi.IsDir() {
					return zw.AddWithInfo(zipPath+"/", path, fi)
				}
				return zw.AddWithInfo(zipPath, path, fi)
			})
			if walkErr != nil {
				log.Printf("zip-multi walk %q: %v", util.SanitizeLogInput(realPath), walkErr)
			}
			continue
		}

		if err := zw.Add(uniqueEntry(entryName), realPath); err != nil {
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

	// SECURITY: zip 本身是受保护文件时拒绝抽取，改名成 .zip 也不能绕过。
	if isAccessConfName(filepath.Base(absZip)) || isServerStateName(filepath.Base(absZip)) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	// SECURITY: accessTables 隐藏的 zip 不能借条目抽取逐个读出内容。
	if fi, err := os.Stat(absZip); err == nil && !fi.IsDir() {
		if !s.canAccessPath(r, absZip, fi.Name()) {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
	}

	// SECURITY: zip 条目可能被嗅探为 text/html 内联渲染，强制 attachment 封死存储型 XSS 通道。
	w.Header().Set("Content-Disposition", "attachment")

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
	if filepath.Ext(path) == ".plist" {
		path = path[0:len(path)-6] + ".ipa"
	}

	relPath, inside := s.resolveRequestPath(r)
	if !inside {
		// SECURITY: hPlist 解析 zip 并写进下发 plist，必须复核解析结果仍在 Root 内。
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	// SECURITY: 与 hIndex 相同的受保护名拦截，否则受保护文件的存在性可借 500/404 差异区分。
	if isAccessConfName(filepath.Base(path)) || isServerStateName(filepath.Base(path)) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if !s.canAccessPath(r, relPath, filepath.Base(path)) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
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
	// SECURITY: path 会拼进下载页与外发 URL，按相同规则拒绝受保护文件（实际读取由 hPlist 兜底）。
	if isAccessConfName(filepath.Base(path)) || isServerStateName(filepath.Base(path)) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
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
	renderHTML(w, r, "ipa-install", ipaInstallHTML, map[string]string{
		"Name":      filepath.Base(path),
		"PlistLink": plistUrl,
	})
}

// fetchClient 是 hFetch 的包级可复用客户端（同 plistProxyClient 模式）：
// 不跟随重定向（指向内网地址的 30x 既绕不过 URL 校验也走不过安全拨号器），
// Transport 复用连接池与 DNS 缓存；此前每请求新建 client+Transport 会泄漏
// 空闲连接与对象直至 GC，高并发 fetch 下累积成连接/TCP 端口耗竭。
var fetchClient = &http.Client{
	Timeout: fetchTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		DialContext:           safeDialContext,
		MaxIdleConns:          64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	},
}

// plistProxyClient 使用安全拨号器，防止伪造的 Host 头把 genPlistLink 变成 SSRF 通道，并用超时限制请求。
var plistProxyClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DialContext: safeDialContext,
	},
}

// plistProxyMaxBody 限制 genPlistLink 对上游响应体的读取量：plist XML 很小，
// 无上限的 ReadAll 可被 30s 超时窗口内的高速灌流放大成内存 DoS（L9）。
const plistProxyMaxBody = 1 << 20

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

	// SECURITY(L9): Host 头由请求方控制，30s 超时窗口内无限读取会把服务器内存灌爆
	//（对比 hFetch 有 1 GiB 硬限）；plist XML 很小，1 MiB 足够。
	data, _ := io.ReadAll(io.LimitReader(resp.Body, plistProxyMaxBody))
	retData, err := plistProxyClient.Post(pp, "text/xml", bytes.NewBuffer(data))
	if err != nil {
		return
	}
	defer retData.Body.Close()

	jsonData, _ := io.ReadAll(io.LimitReader(retData.Body, plistProxyMaxBody))
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

// reCacheMax 限制已编译访问表正则的缓存条数（缓存 reCache 用 RWMutex 保护，处理器并发读）。
// pattern 完全来自用户可上传的 .ghs.yml，无上限会被撑成内存泄漏。
const reCacheMax = 1024

var (
	reCache   = make(map[string]*regexp.Regexp)
	reCacheMu sync.RWMutex
)

func compileCached(pattern string) *regexp.Regexp {
	// SECURITY(M2): Windows 文件系统大小写不敏感，而列表用磁盘真实名、下载用 URL 名
	// （攻击者可控大小写）匹配，规则区分大小写时换一种写法即可绕过隐藏/拦截。
	// 统一按不区分大小写编译；个别规则确需区分大小写时可显式写 (?-i) 覆盖。
	if runtime.GOOS == "windows" {
		pattern = "(?i)" + pattern
	}
	reCacheMu.RLock()
	if re, ok := reCache[pattern]; ok {
		reCacheMu.RUnlock()
		return re
	}
	reCacheMu.RUnlock()
	// 在写锁之外编译，慢正则不会阻塞其他读者；regexp.Compile 对非法输入也不 panic。
	re, err := regexp.Compile(pattern)
	if err != nil {
		re = nil
	}
	reCacheMu.Lock()
	// 可能已有协程并发写入同一 key；保留先到者，维持调用方已拿到指针的一致性。
	if existing, ok := reCache[pattern]; ok {
		reCacheMu.Unlock()
		return existing
	}
	if re == nil {
		// 非法正则原先静默 fail-open（规则失效、本应隐藏的文件直接暴露），至少留痕；
		// 日志里剥掉统一加的 flag 前缀，便于管理员对照 .ghs.yml 原文修正。
		log.Printf("access rule: invalid regex %q is ignored (fail-open); "+
			"fix the accessTables entry in .ghs.yml",
			util.SanitizeLogInput(strings.TrimPrefix(pattern, "(?i)")))
	}
	// 容量上限：超限时整体重置（.ghs.yml 是低频配置，重建的成本可忽略）。
	if len(reCache) >= reCacheMax {
		reCache = make(map[string]*regexp.Regexp)
	}
	reCache[pattern] = re
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

// buildHTTPFileInfo 构造单个条目的响应项；itemPath 为 URL 形式键，realDir 是条目所在真实目录，
// relativize 仅搜索分支为 true（命中项的 Name 需相对所浏览目录展示）。
func (s *HTTPStaticServer) buildHTTPFileInfo(info os.FileInfo, itemPath, realDir string, maxDepth int, relDir string, relativize bool) HTTPFileInfo {
	lr := HTTPFileInfo{
		Name:    info.Name(),
		Path:    itemPath,
		ModTime: info.ModTime().UnixNano() / 1e6,
	}
	if relativize {
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
		name := filepath.ToSlash(deepPath(realDir, info.Name(), maxDepth))
		lr.Name = name
		lr.Path = strings.TrimPrefix(path.Join(path.Dir(itemPath), name), "/")
		lr.Type = "dir"
		// 大小缓存以文件系统相对路径为键（深合并名可能跨多层），而非 URL 形式的 lr.Path。
		childReal := filepath.Join(realDir, filepath.FromSlash(name))
		lr.Size = s.historyDirSize(s.relFromRoot(childReal))
	} else {
		lr.Type = "file"
		lr.Size = info.Size()
	}
	return lr
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

	// 保持非 nil：空结果要序列化为 "files": [] 而非 null。
	lrs := make([]HTTPFileInfo, 0)

	if search != "" {
		results := s.findIndex(search)
		if len(results) > 50 {
			results = results[:50]
		}
		// 按文件系统相对目录限定范围而非 URL 路径：后者带 --prefix，匹配不到任何索引项。
		// 两边统一为斜杠，避开 filepath 系函数在 Windows 上的分隔符问题。
		prefix := relDir
		if prefix != "" {
			prefix += "/"
		}
		matchRoot := prefix == ""
		fileInfoMap := make(map[string]os.FileInfo)
		for _, item := range results {
			p := filepath.ToSlash(item.Path)
			if matchRoot || strings.HasPrefix(p, prefix) || p == relDir {
				// 索引项是根相对路径，而本 API 其余部分输出 URL 形式键；重新拼上 --prefix 使两条分支一致。
				key := strings.TrimPrefix(filepath.ToSlash(path.Join(s.Prefix, p)), "/")
				fileInfoMap[key] = item.Info
			}
		}
		// 目录配置单请求内不变，memo 化避免逐条沿父链 stat。
		accessMemo := newAccessConfMemo(s)
		for itemPath, info := range fileInfoMap {
			if !auth.canAccess(info.Name()) {
				continue
			}
			// SECURITY: 搜索结果来自全局索引，命中文件可能位于配置了更严 accessTables 的
			// 子目录，必须逐项按其所在目录复核，否则泄露被隐藏的文件名。
			relToRoot := strings.TrimPrefix(itemPath, strings.TrimPrefix(s.Prefix, "/")+"/")
			itemReal := filepath.Join(s.Root, filepath.FromSlash(relToRoot))
			dir := filepath.Dir(itemReal)
			blocked := isAccessConfName(info.Name()) || isServerStateName(info.Name()) || !s.insideRoot(dir)
			if !blocked {
				ac := accessMemo.get(dir)
				blocked = !ac.canAccess(info.Name())
			}
			if blocked {
				continue
			}
			lrs = append(lrs, s.buildHTTPFileInfo(info, itemPath, realPath, maxDepth, relDir, true))
		}
	} else {
		entries, err := os.ReadDir(realPath)
		if err != nil {
			// 不回显 err —— 其中含服务器绝对路径。
			log.Printf("readdir %q: %v", util.SanitizeLogInput(realPath), err)
			http.Error(w, "Failed to list directory", http.StatusInternalServerError)
			return
		}
		// 条目名天然唯一，直接建切片，省去 map 分配与二轮遍历。
		lrs = make([]HTTPFileInfo, 0, len(entries))
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if !auth.canAccess(info.Name()) {
				continue
			}
			// SECURITY(M3): 与搜索分支及下载通道对齐——服务器私有文件（凭据库/密钥，
			// 含 8.3 等价名）与访问控制配置本身不从列表暴露，避免泄露存在性与大小。
			if isAccessConfName(info.Name()) || isServerStateName(info.Name()) {
				continue
			}
			// 统一为斜杠：这些值会作为 URL 路径交给 SPA。
			key := strings.TrimPrefix(filepath.ToSlash(filepath.Join(requestPath, entry.Name())), "/")
			lrs = append(lrs, s.buildHTTPFileInfo(info, key, realPath, maxDepth, relDir, false))
		}
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

// dirInfoSizeRef 以原子指针持有目录大小缓存：makeIndex 需要整体替换缓存，裸变量赋值
// 与并发请求的 peek/mergeIn 存在数据竞争，指针替换保证读者原子地看到完整的新旧缓存之一。
var dirInfoSizeRef atomic.Pointer[shardedDirSize]

func dirInfoSize() *shardedDirSize {
	return dirInfoSizeRef.Load()
}

func init() {
	dirInfoSizeRef.Store(&shardedDirSize{})
}

// resetDirInfoSize 用全新空缓存替换当前缓存。
func resetDirInfoSize() {
	dirInfoSizeRef.Store(&shardedDirSize{})
}

func (s *HTTPStaticServer) makeIndex() error {
	var indexes = make([]IndexFileItem, 0)
	// 沿途累计各目录的文件字节总量（与 historyDirSize 同口径：跳过符号链接、
	// 记入所在目录及各祖先直到 "."），重建索引的同时产出一份预热的目录大小缓存。
	totals := make(map[string]int64)
	var err = filepath.Walk(s.Root, func(fpath string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("WARN: Visit path: %s error: %v", strconv.Quote(fpath), err)
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}

		fpath, _ = filepath.Rel(s.Root, fpath)
		fpath = filepath.ToSlash(fpath)
		indexes = append(indexes, IndexFileItem{Path: fpath, Info: info, Lower: strings.ToLower(fpath)})
		// 用 walk 累计的总量合成新缓存原子替换，避免定期清空缓存迫使目录列表全树重走。
		if info.Mode()&os.ModeSymlink == 0 {
			child := path.Dir(fpath)
			for {
				totals[child] += info.Size()
				if child == "." || child == "/" || child == "" {
					break
				}
				parent := path.Dir(child)
				if parent == child {
					break
				}
				child = parent
			}
		}
		return nil
	})
	s.indexes.Store(&indexes)
	fresh := &shardedDirSize{}
	fresh.mergeIn(totals)
	dirInfoSizeRef.Store(fresh)
	return err
}

func (s *HTTPStaticServer) historyDirSize(dir string) int64 {
	// 统一为斜杠，使缓存键与各失效路径写入的键一致。
	dir = filepath.ToSlash(filepath.Clean(dir))

	// 整个读-算-写过程固定在同一份缓存快照上：makeIndex 中途替换指针也不会让
	// totals 写进已被弃用的实例（最坏情况是这次遍历结果作废，下次重算）。
	cache := dirInfoSize()
	if size, ok := cache.peek(dir); ok {
		return size
	}

	// 遍历真实文件系统而非可能过期的 s.indexes，并为子树内每个目录记录总量，一次遍历覆盖全部子目录。
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
		// 把文件字节数记入所在目录至遍历根之间的每一层，绝不超过遍历根（祖先总量不能由部分子树推导）。
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

	cache.mergeIn(totals)
	if size, ok := cache.peek(dir); ok {
		return size
	}
	return totals[dir]
}

func (s *HTTPStaticServer) findIndex(text string) []IndexFileItem {
	ret := make([]IndexFileItem, 0)
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
// 刻意用迭代而非递归：Root 之上的路径向父级递归永不终止，单个请求即可栈溢出。
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
  <script nonce="[[.CSPNonce]]">
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
  <script nonce="[[.CSPNonce]]">checkBrowerAndDownload();</script>
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
    <script nonce="[[.CSPNonce]]">
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

func renderHTML(w http.ResponseWriter, r *http.Request, name, content string, v any) {
	var t *template.Template
	if v, ok := _tmpls.Load(name); ok {
		t = v.(*template.Template)
	} else {
		t = template.Must(template.New(name).Funcs(funcMap).Delims("[[", "]]").Parse(content))
		actual, _ := _tmpls.LoadOrStore(name, t)
		t = actual.(*template.Template)
	}
	// 把当前请求的 CSP nonce 注入模板数据，内联脚本在去 unsafe-inline 的 CSP 下仍能执行。
	if m, ok := v.(map[string]any); ok {
		if _, exists := m["CSPNonce"]; !exists {
			m["CSPNonce"] = util.CSPNonceFromContext(r.Context())
		}
	} else if m, ok := v.(map[string]string); ok {
		if _, exists := m["CSPNonce"]; !exists {
			m["CSPNonce"] = util.CSPNonceFromContext(r.Context())
		}
	}
	t.Execute(w, v)
}

func checkFilename(name string) error {
	if strings.ContainsAny(name, "\\/:*<>|") {
		return errors.New("name should not contain \\/:*<>|")
	}
	// SECURITY: .ghs.yml 不允许通过 HTTP 写入——否则仅有上传权限的用户可为自己授予 delete/edit。
	if isAccessConfName(name) {
		return errors.New("reserved file name")
	}
	// SECURITY: 服务器私有文件（登录 DB、FRP 密钥）同样绝不允许经 HTTP 写入——伪造凭据库重启后即回退默认凭据。
	if isServerStateName(name) {
		return errors.New("reserved file name")
	}
	// 拒绝尾部点与空格：Windows 落盘时会剥离，".ghs.yml." 会变成 ".ghs.yml"。
	if name != strings.TrimRight(name, ". ") {
		return errors.New("name should not end with dot or space")
	}
	return nil
}

// isAccessConfName 判断 name 是否指向访问控制文件；必须大小写不敏感并识别 Windows 8.3 短名，
// 且比较前先做 Windows 名字归一化——尾部点/空格与 ADS 流名与真身同体，否则可被绕过读取全部 token。
func isAccessConfName(name string) bool {
	name = util.WindowsNormaliseName(name)
	if strings.EqualFold(name, YAMLCONF) {
		return true
	}
	if runtime.GOOS == "windows" {
		base := strings.ToUpper(name)
		// 8.3 短名模式：<不超过 8 位的不含点前缀>~<单数字>.<3 位扩展名>
		if idx := strings.IndexByte(base, '~'); idx > 0 && idx <= 7 && len(base) <= 12 {
			rest := base[idx+1:]
			dot := strings.IndexByte(rest, '.')
			if dot == 1 && rest[0] >= '0' && rest[0] <= '9' {
				// 短名可带 ~2/~10 等序号，放宽到多位数字以防多文件冲突。
				if allDigits(rest[:dot]) {
					ext := rest[dot+1:]
					if strings.EqualFold(ext, "yml") || strings.EqualFold(ext, "yaml") {
						return true
					}
				}
			}
		}
	}
	return false
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// serverStateFiles 是服务器自身的私有文件（登录凭据库与 FRP 密钥）。默认 --root 是工作目录，
// 这些文件落在对外服务的目录内，绝不允许经任何 HTTP 通道读取或改写。
var serverStateFiles = []string{
	"gohttpserver.db",
	"gohttpserver.db-wal",
	"gohttpserver.db-shm",
	"frp-encryption.key",
	// 不再读取的遗留状态文件，内容仍是敏感历史。
	"auth-state.json",
	"webdav-accounts.json",
	"storage-usage.json",
}

// RegisterServerStateName 把额外的文件基名并入服务器私有文件保护名单（供 main 注册自定义
// --db 文件名等）。幂等；Windows 名字变体由 isServerStateName 的归一化覆盖，须在开始对外服务之前调用。
func RegisterServerStateName(name string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || filepath.Base(name) != name {
		return
	}
	for _, sf := range serverStateFiles {
		if strings.EqualFold(sf, name) {
			return
		}
	}
	serverStateFiles = append(serverStateFiles, name)
}

// isServerStateName 判断 name 是否指向服务器私有文件；与 isAccessConfName 同样必须大小写
// 不敏感并识别 8.3 短名（util.MatchesSensitiveShortName，server/zipx/webdav 共用），
// 且比较前先做 Windows 名字归一化，封住尾点/ADS 变体。
func isServerStateName(name string) bool {
	name = util.WindowsNormaliseName(name)
	for _, sf := range serverStateFiles {
		if strings.EqualFold(name, sf) {
			return true
		}
	}
	return util.MatchesSensitiveShortName(name)
}

// containsServerStateFile 判断 path 自身或其子树内是否存在服务器私有文件，
// 阻止 DELETE 递归删除连带凭据文件（重启后即回退默认凭据）。
func containsServerStateFile(path string) bool {
	if isServerStateName(filepath.Base(path)) {
		return true
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	found := false
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || found {
			return nil
		}
		if !d.IsDir() && isServerStateName(d.Name()) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
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
	// 同 checkFilename：.ghs.yml 绝不允许通过 HTTP 写入（含大小写与尾点变体）。
	if isAccessConfName(seg) {
		return errors.New("reserved file name")
	}
	// 同理：服务器私有文件（登录 DB、FRP 密钥）不允许经任何写通道覆盖。
	if isServerStateName(seg) {
		return errors.New("reserved file name")
	}
	if seg != strings.TrimRight(seg, ". ") {
		return errors.New("path segment should not end with dot or space")
	}
	return nil
}

func (s *HTTPStaticServer) hVideoPlayer(w http.ResponseWriter, r *http.Request) {
	path := mux.Vars(r)["path"]
	realPath, inside := s.resolveRequestPath(r)
	if !inside {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	// SECURITY: 本处理器只渲染播放页，stat 结果仍可作存在性 oracle，按 hIndex 规则拦截受保护文件。
	if isAccessConfName(filepath.Base(path)) || isServerStateName(filepath.Base(path)) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if !s.canAccessPath(r, realPath, filepath.Base(path)) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
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

	renderHTML(w, r, "video-player", videoPlayerHTML, map[string]any{
		"FileName":  fileName,
		"VideoURL":  videoURL,
		"Extension": extension,
	})
}

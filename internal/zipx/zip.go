package zipx

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	dkignore "github.com/codeskyblue/dockerignore"
	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/codeskyblue/gohttpserver/internal/util"
)

// maxZipExpansionRatio 限制解压/压缩体积比，超过 100:1 几乎必为 zip 炸弹。
const maxZipExpansionRatio = 100

type Zip struct {
	*zip.Writer
}

func sanitizedName(filename string) string {
	if len(filename) > 1 && filename[1] == ':' &&
		runtime.GOOS == "windows" {
		filename = filename[2:]
	}
	filename = strings.TrimLeft(strings.Replace(filename, `\`, "/", -1), `/`)
	filename = filepath.ToSlash(filename)
	filename = filepath.Clean(filename)
	return filename
}

func statFile(filename string) (info os.FileInfo, reader io.ReadCloser, err error) {
	return statFileWithInfo(filename, nil)
}

// statFileWithInfo 与 statFile 相同，但允许调用方传入已取得的 FileInfo：
// filepath.Walk 回调给出的就是 lstat 结果，直接复用可省去重复 lstat
// （第 10 轮性能审查）。known 为 nil 时自行 os.Lstat。
func statFileWithInfo(filename string, known os.FileInfo) (info os.FileInfo, reader io.ReadCloser, err error) {
	if known != nil {
		info = known
	} else {
		info, err = os.Lstat(filename)
		if err != nil {
			return
		}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		var target string
		target, err = os.Readlink(filename)
		if err != nil {
			return
		}
		reader = io.NopCloser(bytes.NewBuffer([]byte(target)))
	} else if !info.IsDir() {
		reader, err = os.Open(filename)
		if err != nil {
			return
		}
	} else {
		reader = io.NopCloser(bytes.NewBuffer(nil))
	}
	return
}

// storeExts 是「压缩收益趋近于零」的扩展名集合：本就是压缩/高熵容器或媒体
// （zip/jar/gz/mp4/jpg 等）。对这些条目改用 Store（不压缩），省去整段 deflate
// CPU，归档体积几乎不变；混合 method 的 zip 是标准格式，通用解压器均支持。
var storeExts = map[string]bool{
	// 压缩容器与归档
	".zip": true, ".jar": true, ".war": true, ".ear": true, ".apk": true,
	".7z": true, ".rar": true, ".gz": true, ".tgz": true, ".bz2": true,
	".tbz": true, ".xz": true, ".txz": true, ".zst": true, ".lz4": true,
	".br": true, ".cab": true, ".iso": true, ".dmg": true,
	".whl": true, ".nupkg": true, ".docx": true, ".xlsx": true, ".pptx": true,
	// 图片（已压缩格式）
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
	".avif": true, ".heic": true, ".heif": true, ".jxl": true,
	// 视频
	".mp4": true, ".m4v": true, ".mov": true, ".mkv": true, ".webm": true,
	".avi": true, ".mpg": true, ".mpeg": true, ".ts": true, ".flv": true, ".wmv": true,
	// 音频（已压缩格式；wav/bmp 等可压格式不在此列）
	".mp3": true, ".flac": true, ".ogg": true, ".opus": true, ".m4a": true, ".aac": true, ".wma": true,
}

// zipMethodFor 按条目扩展名选择压缩方法：不可压缩类型用 Store，其余 Deflate。
func zipMethodFor(name string) uint16 {
	if storeExts[strings.ToLower(filepath.Ext(name))] {
		return zip.Store
	}
	return zip.Deflate
}

// Add 以 relpath 为名把 abspath 加入归档；内部自行 lstat。
func (z *Zip) Add(relpath, abspath string) error {
	return z.AddWithInfo(relpath, abspath, nil)
}

// AddWithInfo 与 Add 相同，但可传入 walk 已给出的 FileInfo 省去重复 lstat；
// info 为 nil 时行为与 Add 完全一致。
func (z *Zip) AddWithInfo(relpath, abspath string, info os.FileInfo) error {
	info, rdc, err := statFileWithInfo(abspath, info)
	if err != nil {
		return err
	}
	defer rdc.Close()

	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = sanitizedName(relpath)
	if info.IsDir() {
		hdr.Name += "/"
	}
	hdr.Method = zipMethodFor(hdr.Name)
	writer, err := z.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, rdc)
	return err
}

// zipBufSize 是 zip 写出缓冲的大小：zip.Writer 每次 Write 都会向底层写出，
// 直连 ResponseWriter 会为每个文件头/数据块产生多次小 syscall。
const zipBufSize = 64 * 1024

// NewBufferedZip 把 w 包装为带 64 KiB 缓冲的 zip 写入器。返回的 flush 必须在
// zw.Close() 之后调用，把 central directory 与残尾字节冲给客户端。
func NewBufferedZip(w io.Writer) (zw *Zip, flush func()) {
	bw := bufio.NewWriterSize(w, zipBufSize)
	zw = &Zip{Writer: zip.NewWriter(bw)}
	return zw, func() { _ = bw.Flush() }
}

// CompressToZip 将 rootDir 以 .zip 附件形式流式发送给客户端。
func CompressToZip(w http.ResponseWriter, rootDir string) {
	CompressToZipFiltered(w, rootDir, nil)
}

// CompressToZipFiltered 与 CompressToZip 相同，但可选 filter：返回 false 的条目
// 不写入归档。filter 为 nil 等价于「除 .ghs.yml 外全部收录」。filter 收到目录时
// 返回 false 仅跳过该目录条目本身，其下文件仍会 walk（逐文件过滤）。
func CompressToZipFiltered(w http.ResponseWriter, rootDir string, filter func(path string, fi os.FileInfo) bool) {
	rootDir = filepath.Clean(rootDir)
	zipFileName := filepath.Base(rootDir) + ".zip"

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+zipFileName+`"`)

	zw, flush := NewBufferedZip(w)
	defer func() {
		zw.Close()
		flush()
	}()

	filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		// 跳过无法 lstat 的条目，避免解引用 nil info。
		if err != nil || info == nil {
			return nil
		}
		if accessConfAliases(info.Name()) {
			return nil
		}
		if filter != nil && !filter(path, info) {
			return nil
		}
		zipPath := path[len(rootDir):]
		if zipPath == "" {
			// 归档单个文件时避免出现 "." 条目。
			zipPath = info.Name()
		}
		// walk 已给出 lstat 结果，直接复用省去每文件一次重复 lstat。
		if addErr := zw.AddWithInfo(zipPath, path, info); addErr != nil {
			// 首次失败即停止，此时客户端通常已断开。
			return addErr
		}
		return nil
	})
}

func ExtractFromZip(zipFile, path string, w io.Writer) (err error) {
	cf, err := zip.OpenReader(zipFile)
	if err != nil {
		return
	}
	defer cf.Close()

	rd := io.NopCloser(bytes.NewBufferString(path))
	patterns, err := dkignore.ReadIgnore(rd)
	if err != nil {
		return
	}

	for _, file := range cf.File {
		matched, _ := dkignore.Matches(file.Name, patterns)
		if !matched {
			continue
		}
		rc, er := file.Open()
		if er != nil {
			err = er
			return
		}
		defer rc.Close()
		_, err = io.Copy(w, rc)
		if err != nil {
			return
		}
		return
	}
	return fmt.Errorf("file %s not found", strconv.Quote(path))
}

// serverStateNames 是服务器私有文件（登录凭据库与 FRP 密钥）。与 server 包的
// serverStateFiles 保持一致——zipx 不能反向 import server（会成环），改动时需同步；
// 自定义 --db 文件名经 RegisterServerStateName 动态并入。
var serverStateNames = []string{
	"gohttpserver.db",
	"gohttpserver.db-wal",
	"gohttpserver.db-shm",
	"frp-encryption.key",
	// 旧版本遗留的同名状态文件，虽不再读取但内容仍是敏感历史。
	"auth-state.json",
	"webdav-accounts.json",
	"storage-usage.json",
}

// RegisterServerStateName 把额外文件基名（如自定义 --db 的文件名）并入解压
// 保护名单，须在启动阶段调用。与 server 包的注册入口配套，main 两者都调。
func RegisterServerStateName(name string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || strings.ContainsAny(name, `/\`) {
		return
	}
	for _, n := range serverStateNames {
		if strings.EqualFold(n, name) {
			return
		}
	}
	serverStateNames = append(serverStateNames, name)
}

// isServerStateAlias 判断 base 是否为服务器私有文件的等价名（大小写不敏感 +
// 8.3 短名 + Windows 名字归一化——尾点变体与 ADS 流名 `::$DATA` 落盘/打开时都与
// 原文件同体）。解压通道曾可借伪装名覆盖真实 DB，服务重启后即回退默认
// admin/admin 凭据。
func isServerStateAlias(base string) bool {
	base = util.WindowsNormaliseName(base)
	for _, n := range serverStateNames {
		if strings.EqualFold(base, n) {
			return true
		}
	}
	upper := strings.ToUpper(base)
	// 8.3 短名模式：<≤7 位前缀>~<数字>.<扩展名>；gohttpserver.db → GOHTTP~1.DB，
	// frp-encryption.key → FRP-EN~1.KEY。
	idx := strings.IndexByte(upper, '~')
	if idx <= 0 || idx > 7 || len(upper) > 14 {
		return false
	}
	rest := upper[idx+1:]
	dot := strings.IndexByte(rest, '.')
	if dot < 1 {
		return false
	}
	digits := rest[:dot]
	if digits == "" {
		return false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false
		}
	}
	ext := rest[dot+1:]
	return strings.HasPrefix(ext, "DB") || ext == "KEY" || ext == "WAL" ||
		ext == "SHM" || ext == "JSO" || ext == "JSON"
}

// MaxUnzipTotalBytes 限制单次解压累计写出的实际字节数（var 以便测试与部署调优）。
// 声明的 UncompressedSize 可被伪造成极小值绕过压缩比检查，因此限额必须按
// io.Copy 的实际写出量在拷贝过程中强制执行，而不是信任归档头。
var MaxUnzipTotalBytes int64 = 20 << 30

// UnzipFile 将 filename 解压到 dest，支持逐条目进度回调与 ctx 取消；
// 压缩比超过 maxZipExpansionRatio 的条目按 zip 炸弹拒收，累计实际写出量
// 超过 MaxUnzipTotalBytes 时中止（按实际字节计，不信任声明尺寸）。
func UnzipFile(ctx context.Context, filename, dest string, onProgress func(idx, total int, name string)) error {
	zr, err := zip.OpenReader(filename)
	if err != nil {
		return err
	}
	defer zr.Close()

	if dest == "" {
		dest = filepath.Dir(filename)
	}

	total := len(zr.File)
	remaining := MaxUnzipTotalBytes
	for i, f := range zr.File {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// zip 炸弹防御（快速路径）：按声明尺寸检查压缩比。
		if f.CompressedSize > 0 && f.UncompressedSize > 0 {
			ratio := float64(f.UncompressedSize) / float64(f.CompressedSize)
			if ratio > maxZipExpansionRatio {
				return fmt.Errorf("zip entry %q has suspicious compression ratio %.1f:1 (max %d:1), possible zip bomb",
					f.Name, ratio, maxZipExpansionRatio)
			}
		}

		if err := extractZipEntry(f, dest, i+1, total, &remaining, onProgress); err != nil {
			return err
		}
	}
	return nil
}

// accessConfAliases 判断 base 是否为 .ghs.yml 的等价名。必须大小写不敏感——
// Windows 文件系统不区分大小写，`.GHS.YML` 落盘后就是 `.ghs.yml` 本身，仅上传
// 权限的攻击者可借 zip 解压植入它为自己授予 delete/edit（提权）。同时识别
// Windows 8.3 短名（GHS~1.YML），以及尾点变体（`.ghs.yml.` 落盘时被剥离为同名）。
// 不放在 server 包是因为 server 依赖本包，反向引入会成环。
func accessConfAliases(base string) bool {
	// Windows 名字归一化：尾部点/空格剥离 + ADS 流名截断（".ghs.yml::$DATA"
	// 曾绕过仅剥尾点的旧实现，直接覆盖真实配置——第 8 轮审计实证）。
	base = util.WindowsNormaliseName(base)
	if strings.EqualFold(base, ".ghs.yml") || strings.EqualFold(base, ".ghs.yaml") {
		return true
	}
	upper := strings.ToUpper(base)
	// 8.3 短名模式：<≤7 位前缀>~<数字>.<扩展名>；.ghs.yml 的短名形如 GHS~1.YML。
	idx := strings.IndexByte(upper, '~')
	if idx <= 0 || idx > 7 {
		return false
	}
	rest := upper[idx+1:]
	dot := strings.IndexByte(rest, '.')
	if dot < 1 {
		return false
	}
	digits := rest[:dot]
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false
		}
	}
	if digits == "" {
		return false
	}
	ext := rest[dot+1:]
	return ext == "YML" || ext == "YAML"
}

// extractZipEntry 写出单个 zip 条目；独立成函数是为了让 defer rc.Close() 逐条执行而非累积。
// remaining 是本次解压的累计写出量余量，由本函数按 io.Copy 实际写出量扣减；
// 条目名指向服务器私有文件（含 Windows 等价名）时跳过。
func extractZipEntry(f *zip.File, dest string, idx, total int, remaining *int64, onProgress func(idx, total int, name string)) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	filename := sanitizedName(f.Name)
	// 与 accessConfAliases 同族：服务器私有文件（DB / FRP 密钥）的等价名不得落盘——
	// 否则上传 + 解压即可覆盖真实 DB，重启后凭据回退 admin/admin。
	if accessConfAliases(filepath.Base(filename)) || isServerStateAlias(filepath.Base(filename)) {
		return nil
	}
	fpath := filepath.Join(dest, filename)

	// 先做 GBK->UTF-8 转码再校验 Zip Slip，确保校验的是最终 fpath。
	if f.Flags&(1<<11) == 0 { // GBK
		tr := simplifiedchinese.GB18030.NewDecoder()
		fpathUtf8, err := tr.String(fpath)
		if err == nil {
			fpath = fpathUtf8
		}
	}

	// Zip Slip 防御：拒绝规范化后逃逸 dest 的条目。filepath.Clean 不会消解开头的 ".."，
	// 因此按 CVE-2018-1002200 的标准做法做带分隔符前缀比对。
	cleanDest := filepath.Clean(dest)
	cleanFpath := filepath.Clean(fpath)
	if cleanFpath != cleanDest &&
		!strings.HasPrefix(cleanFpath+string(filepath.Separator),
			cleanDest+string(filepath.Separator)) {
		return fmt.Errorf("zip entry escapes destination: %q", f.Name)
	}

	if onProgress != nil {
		onProgress(idx, total, filename)
	}

	if f.FileInfo().IsDir() {
		os.MkdirAll(fpath, os.ModePerm)
		return nil
	}

	os.MkdirAll(filepath.Dir(fpath), os.ModePerm)
	outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	// 按实际写出量强制执行累计限额：声明的 UncompressedSize 可被伪造，
	// 限额必须在拷贝过程中由 limitedCopy 自己计量，而不是信任归档头。
	_, err = limitedCopy(outFile, rc, remaining)
	outFile.Close()
	if errors.Is(err, errQuotaExceeded) {
		return fmt.Errorf("unzip aborted: cumulative output exceeds limit (%d bytes)", MaxUnzipTotalBytes)
	}
	return err
}

// errQuotaExceeded 在累计写出量超出剩余配额时由 limitedCopy 返回。
var errQuotaExceeded = errors.New("unzip output exceeds cumulative limit")

// limitedCopy 把 src 拷贝到 dst，最多再写出 *remaining 字节；实际写出量从 *remaining
// 中扣减。伪造极小声明尺寸的条目由此在拷贝过程中被截停。
func limitedCopy(dst io.Writer, src io.Reader, remaining *int64) (int64, error) {
	if remaining == nil {
		return io.Copy(dst, src)
	}
	var total int64
	buf := make([]byte, 32*1024)
	for {
		if *remaining <= 0 {
			return total, errQuotaExceeded
		}
		// 单轮最多写 min(32K, remaining) 字节，超限即返回错误。
		chunk := *remaining
		if chunk > int64(len(buf)) {
			chunk = int64(len(buf))
		}
		n, rerr := src.Read(buf[:chunk])
		if n > 0 {
			wn, werr := dst.Write(buf[:n])
			total += int64(wn)
			*remaining -= int64(wn)
			if werr != nil {
				return total, werr
			}
		}
		if rerr == io.EOF {
			return total, nil
		}
		if rerr != nil {
			return total, rerr
		}
	}
}

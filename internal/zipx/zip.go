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

// maxZipExpansionRatio 限制解压压缩比，超过即按 zip 炸弹拒收。
const maxZipExpansionRatio = 100

// checkZipBomb 按归档头声明的尺寸检查压缩比是否超过上限（zip 炸弹快速路径）。
// 与最终限流（limitedCopy/MaxUnzipTotalBytes）是两道独立防线：这里只需要
// UncompressedSize/CompressedSize 两个字段，返回 nil 表示放行。
func checkZipBomb(f *zip.File) error {
	if f.CompressedSize <= 0 || f.UncompressedSize <= 0 {
		return nil
	}
	ratio := float64(f.UncompressedSize) / float64(f.CompressedSize)
	if ratio > maxZipExpansionRatio {
		return fmt.Errorf("zip entry %q has suspicious compression ratio %.1f:1 (max %d:1), possible zip bomb",
			f.Name, ratio, maxZipExpansionRatio)
	}
	return nil
}

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

// statFileWithInfo 与 statFile 相同，但可传入 walk 已给出的 FileInfo 省去重复 lstat；
// known 为 nil 时自行 os.Lstat。
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

// storeExts 是已压缩/高熵格式的扩展名集合：对这些条目用 Store 省去 deflate CPU 而体积
// 几乎不变；混合 method 的 zip 是标准格式，通用解压器均支持。
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

// zipBufSize 缓冲 zip 写出：zip.Writer 每次 Write 都直落底层，不缓冲会产生大量小 syscall。
const zipBufSize = 64 * 1024

// NewBufferedZip 把 w 包装为带 64 KiB 缓冲的 zip 写入器；返回的 flush 必须在
// zw.Close() 之后调用，以冲出 central directory 与残尾字节。
func NewBufferedZip(w io.Writer) (zw *Zip, flush func()) {
	bw := bufio.NewWriterSize(w, zipBufSize)
	zw = &Zip{Writer: zip.NewWriter(bw)}
	return zw, func() { _ = bw.Flush() }
}

// CompressToZip 将 rootDir 以 .zip 附件形式流式发送给客户端。
func CompressToZip(w http.ResponseWriter, rootDir string) {
	CompressToZipFiltered(w, rootDir, nil)
}

// CompressToZipFiltered 与 CompressToZip 相同，但可选 filter（nil 时仅排除 .ghs.yml）；
// filter 对目录返回 false 只跳过目录条目本身，其下文件仍逐个过滤。
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
		// SECURITY(L10): 与 UnzipFile 一致的 zip 炸弹防御——先按声明尺寸查压缩比，
		// 再用 limitedCopy 限制实际写出量（不信任归档头），防止存量 zip 的条目抽取
		// 通道以无上限的 inflate 拖垮 CPU/磁盘对端。
		if err := checkZipBomb(file); err != nil {
			return err
		}
		rc, er := file.Open()
		if er != nil {
			err = er
			return
		}
		defer rc.Close()
		remaining := MaxUnzipTotalBytes
		_, err = limitedCopy(w, rc, &remaining)
		if err != nil {
			return
		}
		return
	}
	return fmt.Errorf("file %s not found", strconv.Quote(path))
}

// serverStateNames 是服务器私有文件（凭据库与 FRP 密钥）。zipx 不能反向 import server，
// 此名单与 server 包的 serverStateFiles 需人工保持同步；自定义 --db 名经 RegisterServerStateName 并入。
var serverStateNames = []string{
	"gohttpserver.db",
	"gohttpserver.db-wal",
	"gohttpserver.db-shm",
	"frp-encryption.key",
	// 旧版遗留文件，不再读取但内容仍敏感。
	"auth-state.json",
	"webdav-accounts.json",
	"storage-usage.json",
}

// RegisterServerStateName 把额外文件基名（如自定义 --db 文件名）并入保护名单，须在启动阶段调用。
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

// isServerStateAlias 判断 base 是否为服务器私有文件的等价名（大小写不敏感 + 8.3 短名 +
// Windows 归一化——尾点与 `::$DATA` 变体与原文件同体），防止解压伪装名覆盖真实 DB。
func isServerStateAlias(base string) bool {
	base = util.WindowsNormaliseName(base)
	for _, n := range serverStateNames {
		if strings.EqualFold(base, n) {
			return true
		}
	}
	// 8.3 短名判定用 util.MatchesSensitiveShortName 共享实现（GOHTTP~1.DB 等）。
	return util.MatchesSensitiveShortName(base)
}

// MaxUnzipTotalBytes 限制单次解压累计实际写出量（var 以便测试与部署调优）；声明的
// UncompressedSize 可被伪造，限额必须按拷贝过程的实际写出量执行，不信任归档头。
var MaxUnzipTotalBytes int64 = 20 << 30

// UnzipFile 将 filename 解压到 dest，支持逐条目进度回调与 ctx 取消；压缩比超限按
// zip 炸弹拒收，累计实际写出量超 MaxUnzipTotalBytes 时中止（不信任声明尺寸）。
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
		if err := checkZipBomb(f); err != nil {
			return err
		}

		if err := extractZipEntry(f, dest, i+1, total, &remaining, onProgress); err != nil {
			return err
		}
	}
	return nil
}

// accessConfAliases 判断 base 是否为 .ghs.yml/.ghs.yaml 的等价名（大小写不敏感、8.3 短名、
// 尾点变体）：Windows 落盘后 .GHS.YML 就是 .ghs.yml 本身，仅上传权限的攻击者可解压植入
// 它为自己提权。server 依赖本包，判断留在此处以防 import 环。
func accessConfAliases(base string) bool {
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

// extractZipEntry 写出单个 zip 条目，独立成函数使 defer rc.Close() 逐条执行；
// remaining 为累计写出量余量，条目名指向服务器私有文件（含等价名）时跳过。
func extractZipEntry(f *zip.File, dest string, idx, total int, remaining *int64, onProgress func(idx, total int, name string)) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	filename := sanitizedName(f.Name)
	// 服务器私有文件的等价名不得落盘：覆盖 DB 后重启凭据即回退 admin/admin。
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

	// Zip Slip 防御：filepath.Clean 消不掉开头的 ".."，故按带分隔符的前缀比对（CVE-2018-1002200）。
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
	_, err = limitedCopy(outFile, rc, remaining)
	outFile.Close()
	if errors.Is(err, errQuotaExceeded) {
		return fmt.Errorf("unzip aborted: cumulative output exceeds limit (%d bytes)", MaxUnzipTotalBytes)
	}
	return err
}

// errQuotaExceeded 在累计写出量超出剩余配额时由 limitedCopy 返回。
var errQuotaExceeded = errors.New("unzip output exceeds cumulative limit")

// limitedCopy 把 src 拷到 dst，最多写出 *remaining 字节并从中扣减实际写出量。
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

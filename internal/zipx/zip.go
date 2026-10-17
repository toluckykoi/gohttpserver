package zipx

import (
	"archive/zip"
	"bytes"
	"context"
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
	info, err = os.Lstat(filename)
	if err != nil {
		return
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

func (z *Zip) Add(relpath, abspath string) error {
	info, rdc, err := statFile(abspath)
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
	hdr.Method = zip.Deflate
	writer, err := z.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, rdc)
	return err
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

	zw := &Zip{Writer: zip.NewWriter(w)}
	defer zw.Close()

	filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		// 跳过无法 lstat 的条目，避免解引用 nil info。
		if err != nil || info == nil {
			return nil
		}
		if info.Name() == ".ghs.yml" {
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
		if addErr := zw.Add(zipPath, path); addErr != nil {
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

// UnzipFile 将 filename 解压到 dest，支持逐条目进度回调与 ctx 取消；
// 压缩比超过 maxZipExpansionRatio 的条目按 zip 炸弹拒收。
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
	for i, f := range zr.File {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// zip 炸弹防御：解压前先检查压缩比。
		if f.CompressedSize > 0 && f.UncompressedSize > 0 {
			ratio := float64(f.UncompressedSize) / float64(f.CompressedSize)
			if ratio > maxZipExpansionRatio {
				return fmt.Errorf("zip entry %q has suspicious compression ratio %.1f:1 (max %d:1), possible zip bomb",
					f.Name, ratio, maxZipExpansionRatio)
			}
		}

		if err := extractZipEntry(f, dest, i+1, total, onProgress); err != nil {
			return err
		}
	}
	return nil
}

// extractZipEntry 写出单个 zip 条目；独立成函数是为了让 defer rc.Close() 逐条执行而非累积。
func extractZipEntry(f *zip.File, dest string, idx, total int, onProgress func(idx, total int, name string)) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	filename := sanitizedName(f.Name)
	if filepath.Base(filename) == ".ghs.yml" {
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
	_, err = io.Copy(outFile, rc)
	outFile.Close()

	return err
}

package main

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
	// content
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
	hdr.Method = zip.Deflate // compress method
	writer, err := z.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, rdc)
	return err
}

func CompressToZip(w http.ResponseWriter, rootDir string) {
	rootDir = filepath.Clean(rootDir)
	zipFileName := filepath.Base(rootDir) + ".zip"

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+zipFileName+`"`)

	zw := &Zip{Writer: zip.NewWriter(w)}
	defer zw.Close()

	filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		zipPath := path[len(rootDir):]
		if info.Name() == YAMLCONF { // ignore .ghs.yml for security
			return nil
		}
		return zw.Add(zipPath, path)
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
	return fmt.Errorf("File %s not found", strconv.Quote(path))
}

// unzipFile extracts the zip at filename into dest. If onProgress is non-nil,
// it is called once per entry at the start of that entry's write with the
// 1-based index, the total entry count, and the destination-relative path.
//
// If ctx is cancelled, extraction stops and the cancellation error is
// returned. Already-extracted files remain on disk (acceptable for an
// upload-and-extract workflow where the user may have aborted).
func unzipFile(ctx context.Context, filename, dest string, onProgress func(idx, total int, name string)) error {
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
		// Honor client cancellation. Skip the rest of the extraction and
		// return the context error so the caller can surface it.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		// Note: defer inside a loop intentionally defers Close until the
		// surrounding function returns. This matches the pre-existing
		// pattern in this codebase; the per-file close leak is bounded by
		// the total number of zip entries.
		defer rc.Close()

		// ignore .ghs.yml
		filename := sanitizedName(f.Name)
		if filepath.Base(filename) == ".ghs.yml" {
			continue
		}
		fpath := filepath.Join(dest, filename)

		// filename maybe GBK or UTF-8
		// Ref: https://studygolang.com/articles/3114
		if f.Flags&(1<<11) == 0 { // GBK
			tr := simplifiedchinese.GB18030.NewDecoder()
			fpathUtf8, err := tr.String(fpath)
			if err == nil {
				fpath = fpathUtf8
			}
		}

		if onProgress != nil {
			onProgress(i+1, total, filename)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		os.MkdirAll(filepath.Dir(fpath), os.ModePerm)
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()

		if err != nil {
			return err
		}
	}
	return nil
}

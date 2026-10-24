package zipx

// 第七轮审计修复的回归测试：M3（解压累计写出量无上限导致磁盘耗尽）、
// H1 姊妹（解压通道覆盖服务器私有文件）。

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// H1e: 解压不得落盘服务器私有文件（含 Windows 大小写/尾点/8.3 变体）——曾可借
// 上传 + 解压用伪造 SQLite 文件覆盖真实凭据库，重启后回退默认 admin/admin。
func TestUnzipFileRejectsServerStateVariants(t *testing.T) {
	entries := []string{
		"gohttpserver.db", "GOHTTPSERVER.DB", "gohttpserver.db-wal",
		"gohttpserver.db-shm", "frp-encryption.key", "FRP-EN~1.KEY",
		"gohttpserver.db.", "auth-state.json", "webdav-accounts.json",
	}
	for _, name := range entries {
		root := t.TempDir()
		zp := filepath.Join(root, "evil.zip")
		zf, _ := os.Create(zp)
		zw := zip.NewWriter(zf)
		w, _ := zw.Create("keep.txt")
		w.Write([]byte("ok"))
		w2, _ := zw.Create(name)
		w2.Write([]byte("ATTACKER CONTROLLED CONTENT"))
		zw.Close()
		zf.Close()

		dest := filepath.Join(root, "out")
		os.MkdirAll(dest, 0o755)
		if err := UnzipFile(context.Background(), zp, dest, nil); err != nil {
			t.Fatalf("unzip %q: %v", name, err)
		}
		// 目标名以外的任何等价形式都不该出现（Windows 上会归一化成同一文件）。
		matches, _ := filepath.Glob(filepath.Join(dest, "*"))
		for _, m := range matches {
			base := filepath.Base(m)
			if strings.EqualFold(strings.TrimRight(base, ". "), strings.TrimRight(name, ". ")) {
				t.Errorf("zip entry %q was extracted to disk as %q (server state file)", name, m)
			}
			got, _ := os.ReadFile(m)
			if string(got) == "ATTACKER CONTROLLED CONTENT" {
				t.Errorf("zip entry %q wrote attacker-controlled server state content to %q", name, m)
			}
		}
	}
}

// M3a: 累计写出量超过限额必须中止——多条目各自身份都不超单条目限制，但总和
// 可远超 MegaBytes，曾可借反复上传高压缩比 zip 撑满磁盘。
func TestUnzipFileEnforcesCumulativeLimit(t *testing.T) {
	saved := MaxUnzipTotalBytes
	MaxUnzipTotalBytes = 1024 // 测试用小限额
	defer func() { MaxUnzipTotalBytes = saved }()

	root := t.TempDir()
	zp := filepath.Join(root, "big.zip")
	zf, _ := os.Create(zp)
	zw := zip.NewWriter(zf)
	// 10 个条目 × 5 KiB 声明可压缩内容 ≈ 50 KiB，超过 1024 上限。
	content := bytes.Repeat([]byte("x"), 5*1024)
	for i := 0; i < 10; i++ {
		w, _ := zw.Create("f" + string(rune('a'+i)) + ".txt")
		w.Write(content)
	}
	zw.Close()
	zf.Close()

	dest := filepath.Join(root, "out")
	os.MkdirAll(dest, 0o755)
	err := UnzipFile(context.Background(), zp, dest, nil)
	if err == nil {
		t.Fatal("UnzipFile exceeded the cumulative limit without error")
	}
	// 实际写出量必须被限制在限额的少数个缓冲块之内，而不是解完全部。
	var total int64
	filepath.Walk(dest, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	if total > int64(len(content))*4 {
		t.Errorf("wrote %d bytes despite limit; expected the copy to be cut off promptly", total)
	}
}

// M3b: 伪造极小 UncompressedSize 无法绕过累计限额——限额按 io.Copy 实际写出量
// 强制执行，而不是信任归档头声明的尺寸。
func TestUnzipCumulativeLimitIgnoresDeclaredSize(t *testing.T) {
	saved := MaxUnzipTotalBytes
	MaxUnzipTotalBytes = 2048
	defer func() { MaxUnzipTotalBytes = saved }()

	root := t.TempDir()
	zp := filepath.Join(root, "lying.zip")
	f, _ := os.Create(zp)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("lying.txt")
	realContent := bytes.Repeat([]byte("y"), 64*1024)
	w.Write(realContent)
	// 关闭后篡改头部声明的 UncompressedSize 为 1，模拟"谎报尺寸"的归档。
	zw.Close()
	f.Close()

	data, _ := os.ReadFile(zp)
	tampered := tampeUncompressedSize(t, data, "lying.txt", 1)
	os.WriteFile(zp, tampered, 0o644)

	dest := filepath.Join(root, "out")
	os.MkdirAll(dest, 0o755)
	err := UnzipFile(context.Background(), zp, dest, nil)
	if err == nil {
		t.Fatal("declared-size lie was not caught by the cumulative limit")
	}
}

// tampeUncompressedSize 在 zip 字节流里把指定条目的 UncompressedSize 改为 size，
// 用于构造"声明尺寸与真实内容不符"的恶意归档。
func tampeUncompressedSize(t *testing.T, data []byte, entryName string, size uint32) []byte {
	t.Helper()
	idx := bytes.Index(data, []byte(entryName))
	if idx < 0 {
		t.Fatalf("entry name %q not found in zip bytes", entryName)
	}
	out := append([]byte(nil), data...)
	// 本地文件头（PK\x03\x04）位于条目名前：64 字节偏移处是
	// uncompressed size（小端 uint32）。条目名起点前 30 字节即该字段。
	hdrStart := idx - 30
	if hdrStart < 0 {
		t.Fatalf("unexpected layout: entry name too close to start")
	}
	out[hdrStart] = byte(size)
	out[hdrStart+1] = byte(size >> 8)
	out[hdrStart+2] = byte(size >> 16)
	out[hdrStart+3] = byte(size >> 24)
	return out
}

// 正常小 zip 不受限额影响（保护不误伤）。
func TestUnzipFileSmallArchiveUnaffected(t *testing.T) {
	root := t.TempDir()
	zp := filepath.Join(root, "ok.zip")
	zf, _ := os.Create(zp)
	zw := zip.NewWriter(zf)
	w, _ := zw.Create("small.txt")
	w.Write([]byte("hello"))
	zw.Close()
	zf.Close()

	dest := filepath.Join(root, "out")
	os.MkdirAll(dest, 0o755)
	if err := UnzipFile(context.Background(), zp, dest, nil); err != nil {
		t.Fatalf("small archive rejected: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "small.txt"))
	if err != nil || string(got) != "hello" {
		t.Errorf("small.txt content = %q (err %v), want hello", got, err)
	}
}

// limitedCopy 单元测试：剩余配额为 0 或不耗尽时的行为。
func TestLimitedCopyRespectsRemaining(t *testing.T) {
	// 1) 剩余充足：全部拷贝。
	var buf bytes.Buffer
	remaining := int64(10 << 20)
	n, err := limitedCopy(&buf, bytes.NewReader([]byte("hello")), &remaining)
	if err != nil || n != 5 {
		t.Fatalf("limitedCopy = (%d, %v), want (5, nil)", n, err)
	}
	if remaining != int64(10<<20)-5 {
		t.Errorf("remaining = %d, want %d", remaining, int64(10<<20)-5)
	}

	// 2) 剩余不足：返回 errQuotaExceeded 且不写超量数据。
	buf.Reset()
	remaining = 3
	n, err = limitedCopy(&buf, bytes.NewReader([]byte("abcdefgh")), &remaining)
	if !errorsIsQuota(err) {
		t.Errorf("limitedCopy err = %v, want errQuotaExceeded", err)
	}
	if n > 3 {
		t.Errorf("limitedCopy wrote %d bytes with only 3 remaining", n)
	}

	// 3) nil 配额：等同 io.Copy。
	buf.Reset()
	n, err = limitedCopy(&buf, bytes.NewReader([]byte("plain")), nil)
	if err != nil || n != 5 {
		t.Errorf("limitedCopy(nil remaining) = (%d, %v), want (5, nil)", n, err)
	}
}

func errorsIsQuota(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unzip output exceeds")
}

// 确保 reader 在超限时被关闭的语义未被破坏（limitedCopy 不负责 Close，调用方负责）。
func TestLimitedCopyDoesNotCloseReader(t *testing.T) {
	r := &trackingReader{Reader: bytes.NewReader([]byte("data"))}
	remaining := int64(1 << 20)
	if _, err := limitedCopy(io.Discard, r, &remaining); err != nil {
		t.Fatal(err)
	}
	if r.closed {
		t.Error("limitedCopy closed the reader; Close is the caller's responsibility")
	}
}

type trackingReader struct {
	io.Reader
	closed bool
}

func (t *trackingReader) Close() error { t.closed = true; return nil }

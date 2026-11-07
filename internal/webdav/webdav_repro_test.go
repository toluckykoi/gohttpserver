package webdav

// 临时诊断测试：复现「WebDAV 使用过程中本机数据丢失」的可疑机制。
// 用生产同款链路（WebdavServer.ServeHTTP + Basic Auth + quotaFileSystem + gdav.Handler）。
import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeskyblue/gohttpserver/internal/auth"
	storedb "github.com/codeskyblue/gohttpserver/internal/store"
)

func newReproServer(t *testing.T) (*WebdavServer, string, string) {
	t.Helper()
	tmp := t.TempDir()
	db, err := storedb.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	st := LoadWebdavAccountsAt(db, ":memory:")
	if err := st.SetEnabled(true); err != nil {
		t.Fatalf("enable webdav: %v", err)
	}
	// 直接注入账号，绕过 generateWebdavID 的数组越界 bug（该 bug 会让 CreateAccount 必 panic）。
	hash, err := auth.Generate("repro-pass")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	st.mu.Lock()
	st.data.Accounts = append(st.data.Accounts, WebdavAccount{
		ID: "wd_repro", Remark: "repro", Username: "admin",
		PasswordSHA: hash, RootPath: "/",
	})
	st.mu.Unlock()
	usage := LoadUsageState(db)
	srv := NewWebdavServer(tmp, st, usage)
	return srv, tmp, "admin:repro-pass"
}

func doDAV(t *testing.T, srv *WebdavServer, method, target, basic string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	req.SetBasicAuth(strings.SplitN(basic, ":", 2)[0], strings.SplitN(basic, ":", 2)[1])
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func mustPut(t *testing.T, srv *WebdavServer, basic, target, content string) {
	t.Helper()
	rec := doDAV(t, srv, "PUT", target, basic, strings.NewReader(content), nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT %s: got %d want 201, body=%s", target, rec.Code, rec.Body.String())
	}
}

func readFileRoot(t *testing.T, root, rel string) (string, error) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	return string(b), err
}

// 场景 A：大小写变更改名（Windows NTFS 常见：photo.txt → PHOTO.txt，
// 或客户端保存的名字大小写与服务器不同）。
// 预期（正确行为）：改名成功。若先 RemoveAll(dst) 再 Rename，则源文件被删除且 Rename 失败。
func TestReproMoveCaseChangeDestroysFile(t *testing.T) {
	srv, root, basic := newReproServer(t)
	mustPut(t, srv, basic, "/dav/PHOTO.TXT", "precious content")

	rec := doDAV(t, srv, "MOVE", "/dav/PHOTO.TXT", basic, nil,
		map[string]string{"Destination": "http://example.com/dav/photo.txt"})
	t.Logf("MOVE(仅大小写不同) 状态码=%d body=%s", rec.Code, rec.Body.String())

	after1, err1 := readFileRoot(t, root, "PHOTO.TXT")
	after2, err2 := readFileRoot(t, root, "photo.txt")
	t.Logf("PHOTO.TXT: content=%q err=%v; photo.txt: content=%q err=%v", after1, err1, after2, err2)

	if errors.Is(err1, os.ErrNotExist) && errors.Is(err2, os.ErrNotExist) {
		t.Error("BUG 复现：大小写改名导致文件被删除（两处都不存在）")
	}
}

// 场景 B：目录 MOVE 到自身（带尾斜杠），dst==src 的字符串比较拦不住。
func TestReproMoveDirOntoItselfTrailingSlash(t *testing.T) {
	srv, root, basic := newReproServer(t)
	rec := doDAV(t, srv, "MKCOL", "/dav/docs", basic, nil, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("MKCOL: %d", rec.Code)
	}
	mustPut(t, srv, basic, "/dav/docs/a.txt", "inside docs")

	rec = doDAV(t, srv, "MOVE", "/dav/docs", basic, nil,
		map[string]string{"Destination": "http://example.com/dav/docs/"})
	t.Logf("MOVE(dir→dir/) 状态码=%d body=%s", rec.Code, rec.Body.String())

	_, err := os.Stat(filepath.Join(root, "docs"))
	t.Logf("docs 目录：err=%v", err)
	if errors.Is(err, os.ErrNotExist) {
		t.Error("BUG 复现：目录自 MOVE（尾斜杠）导致整个目录被删除")
	}
}

// 场景 C：COPY 覆盖时目标与源是同一个文件（大小写不同）。
func TestReproCopyOverSelfDeletesSource(t *testing.T) {
	srv, root, basic := newReproServer(t)
	mustPut(t, srv, basic, "/dav/Report.txt", "the only copy")

	rec := doDAV(t, srv, "COPY", "/dav/Report.txt", basic, nil,
		map[string]string{"Destination": "http://example.com/dav/report.txt"})
	t.Logf("COPY(A.txt→a.txt) 状态码=%d body=%s", rec.Code, rec.Body.String())

	after, err := readFileRoot(t, root, "Report.txt")
	t.Logf("Report.txt: content=%q err=%v", after, err)
	if errors.Is(err, os.ErrNotExist) {
		t.Error("BUG 复现：COPY 覆盖自身（大小写）删除了唯一副本")
	}
}

// 场景 D：PUT 中途传输失败（网络中断/客户端取消），已有文件被 O_TRUNC 截断。
type failingReader struct{ n int }

func (f *failingReader) Read(p []byte) (int, error) {
	if f.n > 0 {
		f.n--
		p[0] = 'x'
		return 1, nil
	}
	return 0, errors.New("simulated network failure")
}

func TestReproFailedPutTruncatesExistingFile(t *testing.T) {
	srv, root, basic := newReproServer(t)
	mustPut(t, srv, basic, "/dav/important.txt", "original valuable content that must survive a failed upload")

	rec := doDAV(t, srv, "PUT", "/dav/important.txt", basic, &failingReader{n: 3}, nil)
	t.Logf("PUT(中断) 状态码=%d body=%s", rec.Code, rec.Body.String())

	after, err := readFileRoot(t, root, "important.txt")
	t.Logf("中断后 important.txt 内容=%q err=%v", after, err)
	if after != "original valuable content that must survive a failed upload" {
		t.Errorf("BUG 复现：PUT 失败摧毁了已有文件内容")
	}
}

// 场景 A2：大小写变更改名 + Overwrite: T（rclone 等同步客户端的默认行为）。
func TestReproMoveCaseChangeOverwriteTDestroysFile(t *testing.T) {
	srv, root, basic := newReproServer(t)
	mustPut(t, srv, basic, "/dav/PHOTO.TXT", "precious content")

	rec := doDAV(t, srv, "MOVE", "/dav/PHOTO.TXT", basic, nil,
		map[string]string{"Destination": "http://example.com/dav/photo.txt", "Overwrite": "T"})
	t.Logf("MOVE Overwrite:T(仅大小写不同) 状态码=%d body=%s", rec.Code, rec.Body.String())

	_, err1 := readFileRoot(t, root, "PHOTO.TXT")
	_, err2 := readFileRoot(t, root, "photo.txt")
	t.Logf("PHOTO.TXT err=%v; photo.txt err=%v", err1, err2)
	if errors.Is(err1, os.ErrNotExist) && errors.Is(err2, os.ErrNotExist) {
		t.Error("BUG 复现：Overwrite:T 大小写改名导致文件被删除")
	}
}

// 场景 F：目录 MOVE（Overwrite:T）到已存在目录，目标目录里有文件被其他进程占用
// （编辑器/杀软/正在上传），RemoveAll 部分删除后失败 → 目录被掏空一部分。
func TestReproMoveDirOverwritePartialDeletion(t *testing.T) {
	srv, root, basic := newReproServer(t)
	doDAV(t, srv, "MKCOL", "/dav/src", basic, nil, nil)
	doDAV(t, srv, "MKCOL", "/dav/dst", basic, nil, nil)
	mustPut(t, srv, basic, "/dav/dst/keep1.txt", "keep1")
	mustPut(t, srv, basic, "/dav/dst/keep2.txt", "keep2")

	// 模拟 dst/keep2.txt 被其他进程占用（Go 的 os.Open 不带 FILE_SHARE_DELETE，无法删除）。
	held, err := os.Open(filepath.Join(root, "dst", "keep2.txt"))
	if err != nil {
		t.Fatalf("open held: %v", err)
	}
	defer held.Close()

	rec := doDAV(t, srv, "MOVE", "/dav/src", basic, nil,
		map[string]string{"Destination": "http://example.com/dav/dst", "Overwrite": "T"})
	t.Logf("MOVE(dir Overwrite:T, dst 含占用文件) 状态码=%d body=%s", rec.Code, rec.Body.String())

	_, err1 := os.Stat(filepath.Join(root, "dst", "keep1.txt"))
	_, err2 := os.Stat(filepath.Join(root, "dst", "keep2.txt"))
	t.Logf("dst/keep1.txt err=%v; dst/keep2.txt err=%v", err1, err2)
	if errors.Is(err1, os.ErrNotExist) {
		t.Error("BUG 复现：目录 MOVE 失败但目标目录部分文件已被删除（部分删除）")
	}
}

// 场景 E（对照）：DELETE /dav/ 根目录 —— 验证库层防护是否生效。
func TestReproDeleteRootBlocked(t *testing.T) {
	srv, root, basic := newReproServer(t)
	mustPut(t, srv, basic, "/dav/keep.txt", "keep")

	rec := doDAV(t, srv, "DELETE", "/dav/", basic, nil, nil)
	t.Logf("DELETE /dav/ 状态码=%d", rec.Code)

	if _, err := os.Stat(filepath.Join(root, "keep.txt")); err != nil {
		t.Error("根目录 DELETE 未被拦住！")
	}
}

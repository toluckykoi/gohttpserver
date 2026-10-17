package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 回归测试：Windows 专属的远程 DoS——gorilla/mux 用 path.Clean 规范化请求路径（只认 "/"），
// 而 Windows 上 filepath.Clean 也把 "\" 当分隔符。POST /..%5C..%5C 因此能通过路由，
// resolvePath 解析出 Root 之上的路径，readAccessConf 递归直到进程栈溢出崩溃。
// ServeHTTP 现已直接拒绝任何含反斜杠的路径。
func TestBackslashPathsAreRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewHTTPStaticServer(root, true)
	s.Upload, s.Delete, s.Edit = true, true, true

	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		// 字面反斜杠（部分客户端原样发送）。
		for _, p := range []string{
			`/..\..\Windows\System32\drivers\etc`,
			`/sub\..\..\escape.txt`,
		} {
			req := httptest.NewRequest(method, "/", nil)
			req.URL.Path = p
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s %q: got status %d, want 400", method, p, rec.Code)
			}
		}

		// 百分号编码的反斜杠，net/http 会在我们的代码之前就解码进 URL.Path。
		// 经 httptest.NewRequest 构造请求（而不是直接赋值 URL.Path），才能走真实客户端的路径。
		req := httptest.NewRequest(method, "/..%5C..%5C", nil)
		if !strings.ContainsRune(req.URL.Path, '\\') {
			t.Fatalf("precondition failed: %%5C was not decoded, URL.Path = %q", req.URL.Path)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s /..%%5C..%%5C (decoded %q): got status %d, want 400", method, req.URL.Path, rec.Code)
		}
	}
}

// readAccessConf 对不在 Root 下的路径必须终止并返回默认配置：曾经在 filepath.Rel 永不返回 "." 时无限递归，
// 而同卷中 Root 之上的路径恰好产生这种情况。
func TestReadAccessConfAboveRootTerminates(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Dir(filepath.Clean(root))

	s := NewHTTPStaticServer(root, true)
	s.Upload = true

	ac := s.readAccessConf(parent)
	if !ac.Upload {
		t.Errorf("access conf above root = %+v, want the server defaults (Upload=true)", ac)
	}
	// 再测一个完全无法与 Root 建立相对关系的路径。
	if ac := s.readAccessConf(""); ac.Upload != true {
		t.Errorf("empty path access conf = %+v, want defaults", ac)
	}
}

// 通过 HTTP 上传（或改名）访问控制文件必须不可行：写入后处理器会立刻失效配置缓存，
// 否则仅有上传权限的用户就能为自己授予 delete/edit 权限。
func TestAccessControlFileIsNotWritable(t *testing.T) {
	if err := checkFilename(YAMLCONF); err == nil {
		t.Error("checkFilename accepted .ghs.yml")
	}
	if err := checkPathSegment(YAMLCONF); err == nil {
		t.Error("checkPathSegment accepted .ghs.yml")
	}
	if err := checkFilename("normal.txt"); err != nil {
		t.Errorf("checkFilename rejected a normal name: %v", err)
	}
}

// 删除共享根本身必须被拒绝：isUnderRoot 对相等路径返回 true，DELETE / 曾能一次清空整个共享。
func TestDeleteShareRootRefused(t *testing.T) {
	root := t.TempDir()
	victim := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewHTTPStaticServer(root, true)
	s.Delete = true

	req := httptest.NewRequest("DELETE", "/", nil)
	req.URL.Path = "/"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("DELETE /: got status %d, want 400", rec.Code)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("share contents were deleted: %v", err)
	}
}

// .ghs.yml 含 canUploadByToken / canEditByToken 接受的用户 token，绝不能经 HTTP 提供。
func TestAccessControlFileIsNotServed(t *testing.T) {
	root := t.TempDir()
	body := "users:\n  - name: alice\n    token: SECRET\n"
	if err := os.WriteFile(filepath.Join(root, YAMLCONF), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewHTTPStaticServer(root, true)
	s.Delete = true // 以前有这个权限就足以读到它

	req := httptest.NewRequest("GET", "/"+YAMLCONF, nil)
	req.URL.Path = "/" + YAMLCONF
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("GET /%s returned 200 with body %q", YAMLCONF, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "SECRET") {
		t.Fatalf("response leaked the token: %q", rec.Body.String())
	}
}

// historyDirSize 必须一次遍历填满整个子树：列出含 N 个子目录的目录，曾会触发 N 次独立全量遍历。
func TestHistoryDirSizePopulatesSubtree(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string, size int) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("a/f1.bin", 10)
	mk("a/b/f2.bin", 20)
	mk("a/b/c/f3.bin", 30)

	s := NewHTTPStaticServer(root, true)
	// 重置分片缓存
	dirInfoSize = shardedDirSize{}

	if got := s.historyDirSize("a"); got != 60 {
		t.Errorf("historyDirSize(a) = %d, want 60", got)
	}
	// b 与 b/c 必须已由那次遍历缓存。
	if size, ok := dirInfoSize.peek("a/b"); !ok || size != 50 {
		t.Errorf("a/b not populated by the walk: size=%d ok=%v, want 50", size, ok)
	}
	if size, ok := dirInfoSize.peek("a/b/c"); !ok || size != 30 {
		t.Errorf("a/b/c not populated by the walk: size=%d ok=%v, want 30", size, ok)
	}

	// invalidatePath 删除该目录及其祖先，但不影响兄弟目录。
	dirInfoSize.invalidatePath("a/b/c")
	if _, ok := dirInfoSize.peek("a/b/c"); ok {
		t.Error("invalidatePath left the entry for a/b/c")
	}
	if _, ok := dirInfoSize.peek("a"); ok {
		t.Error("invalidatePath left the ancestor entry for a")
	}
}

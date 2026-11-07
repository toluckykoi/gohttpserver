package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 第 8 轮审计回归：Windows 路径规范化（尾部点/空格、ADS 流名 ::$DATA）曾绕过
// isAccessConfName / isServerStateName 的 EqualFold+8.3 比较，使私有文件的
// 读取 / 编辑覆盖 / 删除通道全部可被变体文件名穿透。归一化统一在比较前完成，
// 因此这些用例不依赖运行平台。

const round8Token = "SUPER-SECRET-TOKEN-123"

func newRound8Server(t *testing.T) (*HTTPStaticServer, string) {
	t.Helper()
	root := t.TempDir()
	conf := "upload: true\ndelete: true\nedit: true\nusers:\n- email: a@b.c\n  token: " + round8Token + "\n"
	if err := os.WriteFile(filepath.Join(root, ".ghs.yml"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gohttpserver.db"), []byte("DB-SECRET-CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewHTTPStaticServer(root, true)
	s.Upload, s.Delete, s.Edit = true, true, true
	return s, root
}

func doRound8(s *HTTPStaticServer, method, target string, body string) *httptest.ResponseRecorder {
	t := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req.URL.Path = target
	s.ServeHTTP(t, req)
	return t
}

func TestRound8ServerStateReadBlockedViaVariants(t *testing.T) {
	s, _ := newRound8Server(t)
	for _, p := range []string{
		"/.ghs.yml.", "/.ghs.yml%20", "/.ghs.yml::$DATA",
		"/gohttpserver.db.", "/gohttpserver.db%20", "/gohttpserver.db::$DATA",
	} {
		rec := doRound8(s, "GET", p, "")
		if rec.Code == http.StatusOK {
			t.Errorf("GET %s -> %d, must be blocked", p, rec.Code)
		}
		if strings.Contains(rec.Body.String(), round8Token) || strings.Contains(rec.Body.String(), "DB-SECRET-CONTENT") {
			t.Errorf("GET %s leaked protected content", p)
		}
	}
}

func TestRound8InfoChannelBlockedViaVariants(t *testing.T) {
	s, _ := newRound8Server(t)
	for _, p := range []string{
		"/-/info/.ghs.yml.", "/-/info/.ghs.yml::$DATA",
		"/-/info/gohttpserver.db.", "/-/info/gohttpserver.db::$DATA",
		"/.ghs.yml.?op=info", "/gohttpserver.db%20?op=info",
	} {
		rec := doRound8(s, "GET", p, "")
		if rec.Code == http.StatusOK {
			t.Errorf("info %s -> %d, must be blocked", p, rec.Code)
		}
	}
}

func TestRound8EditCannotOverwriteProtectedViaTrailingDot(t *testing.T) {
	s, root := newRound8Server(t)
	// 尾点变体曾借 writeFileAtomic 的 Rename（Windows 剥尾点）覆盖真实 .ghs.yml。
	rec := doRound8(s, "PUT", "/.ghs.yml.", "upload: true\ndelete: true\nedit: true\n")
	if rec.Code != http.StatusForbidden {
		t.Errorf("PUT .ghs.yml. -> %d (%s), want 403", rec.Code, rec.Body.String())
	}
	after, err := os.ReadFile(filepath.Join(root, ".ghs.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), round8Token) {
		t.Errorf(".ghs.yml was overwritten: %q", after)
	}
}

func TestRound8EditCannotOverwriteDBViaTrailingDot(t *testing.T) {
	s, root := newRound8Server(t)
	rec := doRound8(s, "PUT", "/gohttpserver.db.", "FAKE-DB")
	if rec.Code != http.StatusForbidden {
		t.Errorf("PUT gohttpserver.db. -> %d (%s), want 403", rec.Code, rec.Body.String())
	}
	after, err := os.ReadFile(filepath.Join(root, "gohttpserver.db"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != "DB-SECRET-CONTENT" {
		t.Errorf("gohttpserver.db was overwritten: %q", after)
	}
}

func TestRound8DeleteBlockedViaVariants(t *testing.T) {
	s, root := newRound8Server(t)
	// 注意：URL.Path 是已解码形式，空格变体须用真实空格字符（真实请求中
	// "%20" 会被 net/url 解码成空格后才进入路由）。
	for _, p := range []string{"/gohttpserver.db.", "/gohttpserver.db ", "/.ghs.yml."} {
		rec := doRound8(s, "DELETE", p, "")
		if rec.Code == http.StatusOK {
			t.Errorf("DELETE %q -> %d, must be blocked", p, rec.Code)
		}
	}
	// 即便拥有 delete 权限，.ghs.yml 本体也不可删除（删除会重置目录访问配置）。
	rec := doRound8(s, "DELETE", "/.ghs.yml", "")
	if rec.Code == http.StatusOK {
		t.Errorf("DELETE /.ghs.yml -> %d, must be blocked", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "gohttpserver.db")); err != nil {
		t.Error("gohttpserver.db was deleted via trailing-dot variant")
	}
	if _, err := os.Stat(filepath.Join(root, ".ghs.yml")); err != nil {
		t.Error(".ghs.yml was deleted via trailing-dot variant")
	}
}

func TestRound8RegisteredServerStateName(t *testing.T) {
	RegisterServerStateName("round8-config.db")
	s, _ := newRound8Server(t)
	if err := os.WriteFile(filepath.Join(s.Root, "round8-config.db"), []byte("CUSTOM-DB"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/round8-config.db", "/round8-config.db.", "/ROUND8-CONFIG.DB::$DATA"} {
		rec := doRound8(s, "GET", p, "")
		if rec.Code == http.StatusOK {
			t.Errorf("GET %s -> %d, registered state name must be blocked", p, rec.Code)
		}
	}
}

func TestRound8VideoPlayerAndPlistNameOracle(t *testing.T) {
	s, _ := newRound8Server(t)
	for _, p := range []string{
		"/-/video-player/.ghs.yml", "/-/video-player/gohttpserver.db.",
		"/-/ipa/plist/.ghs.yml", "/-/ipa/plist/gohttpserver.db::$DATA",
	} {
		rec := doRound8(s, "GET", p, "")
		if rec.Code == http.StatusOK {
			t.Errorf("%s -> %d, protected-name existence oracle must be blocked", p, rec.Code)
		}
	}
}

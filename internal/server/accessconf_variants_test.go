package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// H1 回归：Windows 文件系统大小写不敏感，.ghs.yml 的全部保护必须覆盖大小写、
// 8.3 短名与尾部点变体。
func TestAccessConfNameVariants(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows 专属：大小写不敏感与 8.3 短名")
	}
	cases := map[string]bool{
		".ghs.yml":  true,
		".GHS.YML":  true,
		".Ghs.Yml":  true,
		"GHS~1.YML": true,
		// 第 8 轮审计：尾点/ADS 变体在 Windows 上与原文件同体，必须一并拦截
		//（旧断言认为 isAccessConfName 不匹配尾点变体，实测可借 ".ghs.yml."
		// 读取 token，因此行为收紧为匹配）。
		".ghs.yml.":       true,
		".ghs.yml ":       true,
		".ghs.yml::$DATA": true,
		".GHS.YML::$DATA": true,
		"keep.txt":        false,
		"ghs.yml":         false,
		"other.yml":       false,
		"data.GHS~1":      false,
		"keep.txt::$DATA": false,
	}
	for name, want := range cases {
		if got := isAccessConfName(name); got != want {
			t.Errorf("isAccessConfName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestAccessConfFileNotReadableViaVariants(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows 专属")
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".ghs.yml"),
		[]byte("users:\n- email: a@b\n  token: SUPERSECRETTOKEN123\n"), 0o644)

	s := NewHTTPStaticServer(root, true)
	s.Upload, s.Delete, s.Edit = true, true, true

	for _, p := range []string{"/.ghs.yml", "/.GHS.YML", "/GHS~1.YML"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.URL.Path = p
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Errorf("GET %s -> 200, must be blocked", p)
		}
		if strings.Contains(rec.Body.String(), "SUPERSECRETTOKEN123") {
			t.Errorf("GET %s leaked the token", p)
		}
	}
}

func TestAccessConfFileNotWritableViaVariants(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows 专属")
	}
	for _, tc := range []struct{ name, path string }{
		{"uppercase PUT", ".GHS.YML"},
		{"short-name PUT", "GHS~1.YML"},
	} {
		root := t.TempDir()
		os.WriteFile(filepath.Join(root, ".ghs.yml"), []byte("upload: true\ndelete: false\nedit: true\n"), 0o644)
		os.WriteFile(filepath.Join(root, "victim.txt"), []byte("data"), 0o644)

		s := NewHTTPStaticServer(root, true)
		s.Upload, s.Delete, s.Edit = true, true, true

		r1 := httptest.NewRequest("PUT", "/", strings.NewReader("upload: true\ndelete: true\nedit: true\n"))
		r1.URL.Path = "/" + tc.path
		rc1 := httptest.NewRecorder()
		s.ServeHTTP(rc1, r1)
		after, _ := os.ReadFile(filepath.Join(root, ".ghs.yml"))
		if strings.Contains(string(after), "delete: true") {
			t.Errorf("%s: .ghs.yml was overwritten (code=%d): %q", tc.name, rc1.Code, after)
		}

		s.invalidateAccessConf()
		r2 := httptest.NewRequest("DELETE", "/victim.txt", nil)
		r2.URL.Path = "/victim.txt"
		rc2 := httptest.NewRecorder()
		s.ServeHTTP(rc2, r2)
		if rc2.Code == http.StatusOK {
			t.Errorf("%s: delete succeeded after attempted overwrite", tc.name)
		}
	}
}

func TestUploadRejectsTrailingDotVariant(t *testing.T) {
	root := t.TempDir()
	s := NewHTTPStaticServer(root, true)
	s.Upload, s.Delete, s.Edit = true, true, true

	// multipart 上传 ".ghs.yml." 应被拒绝
	var buf strings.Builder
	buf.WriteString("--BOUNDARY\r\n")
	buf.WriteString(`Content-Disposition: form-data; name="file"; filename=".ghs.yml."` + "\r\n")
	buf.WriteString("Content-Type: application/octet-stream\r\n\r\n")
	buf.WriteString("upload: true\ndelete: true\n")
	buf.WriteString("\r\n--BOUNDARY\r\n")
	buf.WriteString(`Content-Disposition: form-data; name="filename"` + "\r\n\r\n")
	buf.WriteString(".ghs.yml.\r\n")
	buf.WriteString("--BOUNDARY--\r\n")

	req := httptest.NewRequest("POST", "/", strings.NewReader(buf.String()))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=BOUNDARY")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusForbidden {
		t.Errorf("upload '.ghs.yml.' -> %d %s, want rejection", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".ghs.yml")); err == nil {
		t.Error("'.ghs.yml.' upload created a real .ghs.yml")
	}
}

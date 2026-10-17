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

// H2 回归：CSP 不再含 'unsafe-inline' 的 script-src；nonce 按请求生成并注入内嵌页面。
func TestCSPHasNoUnsafeInlineScript(t *testing.T) {
	if strings.Contains(ContentSecurityPolicy, "script-src 'self' 'unsafe-inline'") {
		t.Error("script-src still contains 'unsafe-inline'; inline scripts are injectable via uploaded HTML")
	}
	if !strings.Contains(ContentSecurityPolicy, "'nonce-{CSP_NONCE}'") {
		t.Error("CSP must carry the per-request nonce placeholder")
	}
}

func TestSecurityHeadersInjectsNonce(t *testing.T) {
	var gotNonce string
	var gotHeader string
	h := util.SecurityHeaders(ContentSecurityPolicy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotNonce = util.CSPNonceFromContext(r.Context())
		gotHeader = w.Header().Get("Content-Security-Policy")
	}))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if gotNonce == "" {
		t.Fatal("nonce missing from request context")
	}
	want := strings.ReplaceAll(ContentSecurityPolicy, "{CSP_NONCE}", gotNonce)
	if gotHeader != want {
		t.Errorf("CSP header not substituted:\n got %s\nwant %s", gotHeader, want)
	}
	// 两个请求的 nonce 必须不同。
	got2 := ""
	h2 := util.SecurityHeaders(ContentSecurityPolicy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got2 = util.CSPNonceFromContext(r.Context())
	}))
	h2.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if got2 == "" || got2 == gotNonce {
		t.Errorf("nonce not unique per request: %q vs %q", gotNonce, got2)
	}
}

// 内嵌页面（video-player）的 script 标签必须带上 nonce 属性。
func TestRenderedPageCarriesNonce(t *testing.T) {
	root := t.TempDir()
	name := `clip.mp4`
	os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644)

	s := NewHTTPStaticServer(root, true)

	wrapped := util.SecurityHeaders(ContentSecurityPolicy)(s)
	req := httptest.NewRequest("GET", "/-/video-player/clip.mp4", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("video-player -> %d", rec.Code)
	}
	// 中间件用 r.WithContext 传递 nonce；原始 req 的 context 不变，
	// 改从响应页面里提取 nonce 属性来验证注入链路。
	body := rec.Body.String()
	idx := strings.Index(body, `nonce="`)
	if idx < 0 {
		t.Fatalf("rendered page has no nonce attribute; body:\n%s", body)
	}
	rest := body[idx+7:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatal("unterminated nonce attribute")
	}
	nonce := rest[:end]
	if !strings.Contains(body, `nonce="`+nonce+`"`) {
		t.Errorf("rendered page missing nonce attribute; body:\n%s", body)
	}
	// CSP 头也应包含同一个 nonce
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "'nonce-"+nonce+"'") {
		t.Errorf("CSP header does not carry the page nonce %q: %s", nonce, rec.Header().Get("Content-Security-Policy"))
	}
	// 模板里不得残留无 nonce 的内联 script
	if strings.Contains(body, "<script>") {
		t.Errorf("page still contains an inline <script> without nonce:\n%s", body)
	}
}

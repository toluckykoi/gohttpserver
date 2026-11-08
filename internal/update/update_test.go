package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newUpdateServer 起一个最小 update_core stub：/check 返回 resp 模板
// （download_url 自动指向本 stub 的下载端点），/download 支持单段 Range
// 续传并要求 Bearer test-token。resp 在请求到来前可由用例继续定制。
func newUpdateServer(t *testing.T, payload []byte, resp *checkResponse) *httptest.Server {
	t.Helper()
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	if resp == nil {
		resp = &checkResponse{Available: false, Reason: "up_to_date"}
	}
	if resp.Artifact != nil && resp.Artifact.SHA256 == "" {
		resp.Artifact.SHA256 = hash
		if resp.Artifact.Size == 0 {
			resp.Artifact.Size = int64(len(payload))
		}
	}

	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/apps/gohttpserver/check", func(w http.ResponseWriter, r *http.Request) {
		out := *resp
		if out.Artifact != nil {
			art := *out.Artifact
			art.URL = srv.URL + "/api/v1/artifacts/art1/download"
			out.Artifact = &art
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/api/v1/artifacts/art1/download", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		rng := r.Header.Get("Range")
		if rng != "" {
			if !strings.HasPrefix(rng, "bytes=") || !strings.HasSuffix(rng, "-") {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			offset, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(rng, "bytes="), "-"), 10, 64)
			if err != nil || offset < 0 || offset > int64(len(payload)) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			// If-Range 绑定 ETag（即 SHA-256）：不匹配时退回 200 全量。
			if r.Header.Get("If-Range") != `"`+hash+`"` {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(payload)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(payload)-1, len(payload)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[offset:])
			return
		}
		w.Header().Set("ETag", `"`+hash+`"`)
		_, _ = w.Write(payload)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func waitState(t *testing.T, svc *Service, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got := svc.Status().State; got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	st := svc.Status()
	t.Fatalf("状态未到达 %s：got %s (error=%q)", want, st.State, st.Error)
}

func newTestPayload(t *testing.T) []byte {
	t.Helper()
	return []byte("fake-new-binary-payload-0123456789")
}

func TestNormalizeVersion(t *testing.T) {
	cases := map[string]string{
		"v1.3.0":       "1.3.0",
		"V2.0.0":       "2.0.0",
		"  1.2.3  ":    "1.2.3",
		"1.2.0.dev11":  "1.2.0.dev11",
		"1.2.3+build7": "1.2.3+build7",
	}
	for in, want := range cases {
		if got := normalizeVersion(in); got != want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{"", "dev", "unknown"} {
		if versionSupported(normalizeVersion(in)) {
			t.Errorf("version %q 不应被视为可比较版本", in)
		}
	}
	if !versionSupported(normalizeVersion("v1.3.0")) {
		t.Error("v1.3.0 应被视为可比较版本")
	}
}

func TestValidateBaseURL(t *testing.T) {
	// http 与 https 均默认支持。
	for _, raw := range []string{
		"https://updates.example.com",
		"http://updates.example.com:50003/",
		"http://127.0.0.1:9000",
	} {
		if _, err := validateBaseURL(raw); err != nil {
			t.Errorf("地址 %q 应有效: %v", raw, err)
		}
	}
	for _, raw := range []string{
		"ftp://updates.example.com",        // 非 http(s)
		"https://u:p@updates.example.com",  // 带用户信息
		"https://updates.example.com/?a=b", // 带查询串
		"https://updates.example.com/sub/", // 带路径
		"not a url",                        // 解析失败
	} {
		if _, err := validateBaseURL(raw); err == nil {
			t.Errorf("地址 %q 应被拒绝", raw)
		}
	}
}

// TestCheckUpToDate 验证无更新时回到 idle、保留 reason 且不报错。
func TestCheckUpToDate(t *testing.T) {
	srv := newUpdateServer(t, nil, &checkResponse{Available: false, Reason: "up_to_date"})
	svc, err := NewService(Options{
		BaseURL: srv.URL, App: "gohttpserver", Token: "test-token",
		CurrentVersion: "v1.3.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.Check()
	st := svc.Status()
	if st.State != StateIdle {
		t.Fatalf("state = %s, want idle", st.State)
	}
	if st.Reason != "up_to_date" {
		t.Fatalf("reason = %s, want up_to_date", st.Reason)
	}
	if st.Error != "" {
		t.Fatalf("不应有错误: %s", st.Error)
	}
	if st.CurrentVersion != "1.3.0" {
		t.Fatalf("current_version = %s, want 1.3.0", st.CurrentVersion)
	}
}

// TestCheckDisabledForDevBuild dev 构建整体禁用，不发起任何请求。
func TestCheckDisabledForDevBuild(t *testing.T) {
	svc, err := NewService(Options{
		BaseURL: "http://127.0.0.1:1", App: "gohttpserver",
		CurrentVersion: "dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.Check()
	st := svc.Status()
	if st.Enabled || st.State != StateDisabled {
		t.Fatalf("dev 构建应禁用更新: %+v", st)
	}
}

// TestCheckHTTPError 验证非 200 响应的错误信息包含服务端 message。
func TestCheckHTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/apps/gohttpserver/check", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"invalid_request","message":"版本号格式不合法"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	svc, err := NewService(Options{
		BaseURL: srv.URL, App: "gohttpserver", CurrentVersion: "1.3.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.Check()
	st := svc.Status()
	if st.State != StateError {
		t.Fatalf("state = %s, want error", st.State)
	}
	if !strings.Contains(st.Error, "版本号格式不合法") {
		t.Fatalf("错误信息应包含服务端 message: %q", st.Error)
	}
}

// TestDownloadWithResume 走完整流程：check → 预置半截 .part 续传 →
// size/SHA-256 校验通过 → ready → apply 备份/替换。
func TestDownloadWithResume(t *testing.T) {
	payload := newTestPayload(t)
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	srv := newUpdateServer(t, payload, &checkResponse{
		Available: true,
		Reason:    "new_version",
		Release:   &releaseInfo{Version: "1.4.0", Notes: "修复崩溃", PublishedAt: strPtr("2026-03-01T00:00:00Z")},
		Artifact: &artifactInfo{
			Filename: "gohttpserver-1.4.0",
			Size:     int64(len(payload)),
			SHA256:   hash,
		},
	})

	svc, err := NewService(Options{
		BaseURL: srv.URL, App: "gohttpserver", Token: "test-token",
		CurrentVersion: "v1.3.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "gohttpserver-test.exe")
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc.exeOverride = exe

	// 预置半截临时文件，强制服务端走 206 续传分支。
	part, err := svc.partPath(hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(part, payload[:5], 0o600); err != nil {
		t.Fatal(err)
	}

	svc.Check()
	waitState(t, svc, StateAvailable, 2*time.Second)
	st := svc.Status()
	if st.ReleaseVersion != "1.4.0" || st.Notes != "修复崩溃" {
		t.Fatalf("release 信息不符: %+v", st)
	}
	if st.ArtifactSize != int64(len(payload)) || st.SHA256 != hash {
		t.Fatalf("artifact 信息不符: %+v", st)
	}

	if err := svc.StartDownload(); err != nil {
		t.Fatal(err)
	}
	waitState(t, svc, StateReady, 5*time.Second)
	if got := svc.Status().Downloaded; got != int64(len(payload)) {
		t.Fatalf("downloaded = %d, want %d", got, len(payload))
	}

	// 安装：备份 .bak 保留旧内容，新文件就位，.part 消失。
	if err := svc.applyTo(exe); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("安装后内容不符: %q", got)
	}
	bak, err := os.ReadFile(exe + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(bak) != "old-binary" {
		t.Fatalf("备份内容不符: %q", bak)
	}
	if _, err := os.Lstat(part); !os.IsNotExist(err) {
		t.Fatalf("临时文件应已消失: %v", err)
	}
	if st := svc.Status(); st.State != StateApplied {
		t.Fatalf("state = %s, want applied", st.State)
	}
}

// TestApplyFailsWhenPartMissing part 缺失时安装失败且当前程序不被破坏。
func TestApplyFailsWhenPartMissing(t *testing.T) {
	payload := newTestPayload(t)
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	srv := newUpdateServer(t, payload, &checkResponse{
		Available: true,
		Reason:    "new_version",
		Release:   &releaseInfo{Version: "1.4.0"},
		Artifact: &artifactInfo{
			Filename: "gohttpserver-1.4.0",
			Size:     int64(len(payload)),
			SHA256:   hash,
		},
	})

	svc, err := NewService(Options{
		BaseURL: srv.URL, App: "gohttpserver", Token: "test-token",
		CurrentVersion: "1.3.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "gohttpserver-test.exe")
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc.exeOverride = exe
	svc.Check()
	waitState(t, svc, StateAvailable, 2*time.Second)

	if err := svc.StartDownload(); err != nil {
		t.Fatal(err)
	}
	waitState(t, svc, StateReady, 5*time.Second)
	part, err := svc.partPath(hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(part); err != nil {
		t.Fatal(err)
	}

	if err := svc.Apply(); err == nil {
		t.Fatal("part 缺失时 Apply 应失败")
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-binary" {
		t.Fatalf("失败的安装不应破坏当前程序: %q", got)
	}
	if _, err := os.Lstat(exe + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("失败的安装不应留下备份文件: %v", err)
	}
}

// TestLoopbackDownloadURL 验证服务端返回回环下载地址（无端口）时：
// 校验放行 + 归一化为公共 BaseURL host（沿用 base 端口），且下载走公开地址成功。
func TestLoopbackDownloadURL(t *testing.T) {
	payload := newTestPayload(t)
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])

	// stub：check 返回 download_url 写死为 127.0.0.1 的回环地址（无端口，
	// 归一化后应改用 base host + base 端口 → 即 stub 自身）。
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/apps/gohttpserver/check", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&checkResponse{
			Available: true,
			Reason:    "new_version",
			Release:   &releaseInfo{Version: "1.4.0"},
			Artifact: &artifactInfo{
				Filename: "gohttpserver-1.4.0",
				Size:     int64(len(payload)),
				SHA256:   hash,
				URL:      "http://127.0.0.1/api/v1/artifacts/art1/download",
			},
		})
	})
	// 下载端点从 stub 的实际地址提供。
	var srv *httptest.Server
	mux.HandleFunc("/api/v1/artifacts/art1/download", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"`+hash+`"`)
		_, _ = w.Write(payload)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	svc, err := NewService(Options{
		BaseURL: srv.URL, App: "gohttpserver", Token: "test-token",
		CurrentVersion: "1.3.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "gohttpserver-test.exe")
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc.exeOverride = exe

	svc.Check()
	waitState(t, svc, StateAvailable, 2*time.Second)
	if st := svc.Status(); st.State != StateAvailable {
		t.Fatalf("回环下载地址应放行并通过校验: %+v", st)
	}

	if err := svc.StartDownload(); err != nil {
		t.Fatal(err)
	}
	waitState(t, svc, StateReady, 5*time.Second)
	if st := svc.Status(); st.State != StateReady {
		t.Fatalf("回环地址归一化后应能下载成功: state=%s err=%s", st.State, st.Error)
	}
}

// TestNormalizeDownloadURL 直接测归一化：回环地址的主机换为 base 主机，
// 端口优先保留下载地址自带的（端口映射场景），路径与查询原样保留。
func TestNormalizeDownloadURL(t *testing.T) {
	base, err := validateBaseURL("https://updates.example.com")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			"非回环地址原样返回",
			"https://updates.example.com/api/v1/pkg/download?token=x",
			"https://updates.example.com/api/v1/pkg/download?token=x",
		},
		{
			"带端口的回环地址：保留端口、主机换 base",
			"http://127.0.0.1:9999/api/v1/artifacts/1/download",
			"https://updates.example.com:9999/api/v1/artifacts/1/download",
		},
		{
			"带端口+查询的回环地址",
			"http://localhost:9000/pkg?token=t#frag",
			"https://updates.example.com:9000/pkg?token=t",
		},
		{
			"无端口的回环地址：用 base 端口（base 无端口则不写）",
			"http://127.0.0.1/api/pkg",
			"https://updates.example.com/api/pkg",
		},
		{
			"base 带端口时无端口回环地址沿用 base 端口",
			"http://localhost/api/pkg",
			"https://updates.example.com:8443/api/pkg",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b *url.URL
			if strings.Contains(tc.name, "base 带端口") {
				b = &url.URL{Scheme: "https", Host: "updates.example.com:8443"}
			} else {
				b = base
			}
			if got := normalizeDownloadURL(b, tc.raw); got != tc.want {
				t.Errorf("normalizeDownloadURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestValidateArtifact 覆盖下载元数据校验的负向路径：跨源、协议不一致、
// 带用户信息、非法 SHA-256、非法 size；以及同源/回环的放行路径。
func TestValidateArtifact(t *testing.T) {
	validSHA := strings.Repeat("ab", 32)
	base, err := validateBaseURL("https://updates.example.com")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		url     string
		sha     string
		size    int64
		wantErr bool
	}{
		{"same origin accepted", "https://updates.example.com/pkg", validSHA, 100, false},
		{"loopback accepted", "https://127.0.0.1:9/pkg", validSHA, 100, false},
		{"cross origin rejected", "https://evil.example.com/pkg", validSHA, 100, true},
		{"host with spoofed suffix rejected", "https://updates.example.com.evil.com/pkg", validSHA, 100, true},
		{"scheme mismatch rejected", "http://updates.example.com/pkg", validSHA, 100, true},
		{"user info rejected", "https://u:p@updates.example.com/pkg", validSHA, 100, true},
		{"unparsable url rejected", "://bad", validSHA, 100, true},
		{"non hex sha rejected", "https://updates.example.com/pkg", strings.Repeat("zz", 32), 100, true},
		{"short sha rejected", "https://updates.example.com/pkg", strings.Repeat("ab", 16), 100, true},
		{"empty sha rejected", "https://updates.example.com/pkg", "", 100, true},
		{"zero size rejected", "https://updates.example.com/pkg", validSHA, 0, true},
		{"negative size rejected", "https://updates.example.com/pkg", validSHA, -1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &artifactInfo{Filename: "pkg", Size: tc.size, SHA256: tc.sha, URL: tc.url}
			err := validateArtifact(base, a)
			if tc.wantErr && err == nil {
				t.Errorf("应被拒绝: %+v", a)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("应被放行: %v (artifact=%+v)", err, a)
			}
		})
	}
}

// TestVerifyFileRejectsTamperedData 验证 size + SHA-256 双重校验能发现篡改。
func TestVerifyFileRejectsTamperedData(t *testing.T) {
	payload := []byte("0123456789abcdef")
	path := filepath.Join(t.TempDir(), "pkg")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	goodSHA := hex.EncodeToString(sum[:])

	if err := verifyFile(path, int64(len(payload)), goodSHA); err != nil {
		t.Errorf("未篡改文件应通过校验: %v", err)
	}
	if err := verifyFile(path, int64(len(payload)), strings.Repeat("00", 32)); err == nil {
		t.Error("SHA-256 不匹配应报错")
	}
	if err := verifyFile(path, int64(len(payload))+1, goodSHA); err == nil {
		t.Error("size 不匹配应报错")
	}
	if err := verifyFile(filepath.Join(t.TempDir(), "missing"), 1, goodSHA); err == nil {
		t.Error("文件不存在应报错")
	}

	// 篡改内容后 size 不变，仅哈希变化 → 必须被检出。
	if err := os.WriteFile(path, []byte("0123456789abcdeF"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFile(path, int64(len(payload)), goodSHA); err == nil {
		t.Error("内容篡改应被 SHA-256 检出")
	}
}

// TestPackageTypePriority 验证包类型三级优先级。
func TestPackageTypePriority(t *testing.T) {
	svc, err := NewService(Options{
		BaseURL: "http://127.0.0.1:1", App: "gohttpserver", CurrentVersion: "1.0.0",
		PackageTypes: map[string]string{platformOS(): "from-map"},
		PackageType:  "from-single",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.packageType(); got != "from-map" {
		t.Fatalf("PackageTypes 应优先: got %q", got)
	}
	svc.opts.PackageTypes = nil
	if got := svc.packageType(); got != "from-single" {
		t.Fatalf("PackageType 应次之: got %q", got)
	}
	svc.opts.PackageType = ""
	want := "bin"
	if platformOS() == "windows" {
		want = "exe"
	}
	if got := svc.packageType(); got != want {
		t.Fatalf("内置默认: got %q, want %q", got, want)
	}
}

func strPtr(s string) *string { return &s }

package update

// 第 11 轮审计回归：H3（更新源 scheme 支持面）与 M1（版本单调性检查防降级）。

import "testing"

// H3: http 与 https 更新源均为合法配置、均可创建服务（明文 HTTP 仅告警不拦截，
// 由部署者权衡；回环/非回环一视同仁）。
func TestUpdateSourceSchemeSupported(t *testing.T) {
	newOpts := func(base string) Options {
		return Options{BaseURL: base, App: "gohttpserver", CurrentVersion: "v1.4.0"}
	}

	for _, base := range []string{
		"http://updates.example.com",  // 非回环明文 HTTP
		"http://127.0.0.1:9999",       // 回环 HTTP
		"https://updates.example.com", // HTTPS
	} {
		if _, err := NewService(newOpts(base)); err != nil {
			t.Errorf("更新源 %q 应被接受: %v", base, err)
		}
	}
}

// M1: 服务端声明"有更新"但版本严格旧于当前版本（或无法解析）时必须按无更新处理。
// 等版本（==）放行：update_core 的 Required 热修复会重发同版本构件，拒绝等版本会让
// 热修复通道永久失效；等版本不可能构成降级。
func TestCheckRejectsDowngrade(t *testing.T) {
	cases := []struct {
		name    string
		server  string
		current string
	}{
		{"严格旧版本", "1.0.0", "v1.4.0"},
		{"无法解析的版本", "not-a-version", "v1.4.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newUpdateServer(t, newTestPayload(t), &checkResponse{
				Available: true,
				Reason:    "upgrade",
				Release:   &releaseInfo{Version: tc.server},
				Artifact:  &artifactInfo{Filename: "gohttpserver.exe"},
			})
			svc, err := NewService(Options{
				BaseURL: srv.URL, App: "gohttpserver", CurrentVersion: tc.current,
			})
			if err != nil {
				t.Fatal(err)
			}
			svc.Check()
			st := svc.Status()
			if st.State != StateIdle {
				t.Fatalf("state = %s, want idle（降级/不可解析版本必须被拒绝）", st.State)
			}
			if st.ReleaseVersion != "" {
				t.Fatalf("release_version = %q, want empty", st.ReleaseVersion)
			}
			if st.Error != "" {
				t.Fatalf("不应有错误: %s", st.Error)
			}
		})
	}
}

// M1 反向用例：等版本（Required 热修复）必须被接受为可更新，而不是被当成降级拒绝。
func TestCheckAcceptsSameVersionHotfix(t *testing.T) {
	srv := newUpdateServer(t, newTestPayload(t), &checkResponse{
		Available: true,
		Reason:    "hotfix",
		Release:   &releaseInfo{Version: "1.4.0", Required: true},
		Artifact:  &artifactInfo{Filename: "gohttpserver.exe"},
	})
	svc, err := NewService(Options{
		BaseURL: srv.URL, App: "gohttpserver", CurrentVersion: "v1.4.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.Check()
	if st := svc.Status(); st.State != StateAvailable {
		t.Fatalf("state = %s, want available（等版本热修复应放行）", st.State)
	}
}

// M1 反向用例：真正的更新版本仍然被接受。
func TestCheckAcceptsNewerVersion(t *testing.T) {
	srv := newUpdateServer(t, newTestPayload(t), &checkResponse{
		Available: true,
		Reason:    "upgrade",
		Release:   &releaseInfo{Version: "1.5.0"},
		Artifact:  &artifactInfo{Filename: "gohttpserver.exe"},
	})
	svc, err := NewService(Options{
		BaseURL: srv.URL, App: "gohttpserver", CurrentVersion: "v1.4.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.Check()
	if st := svc.Status(); st.State != StateAvailable {
		t.Fatalf("state = %s, want available", st.State)
	}
}

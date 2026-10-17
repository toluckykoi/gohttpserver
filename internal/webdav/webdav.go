package webdav

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codeskyblue/gohttpserver/internal/util"
	gdav "golang.org/x/net/webdav"
)

// WebdavServer 在 golang.org/x/net/webdav 外层提供 Basic Auth、每账号 chroot 与配额限制。
type WebdavServer struct {
	root     string
	accounts *WebdavAccountState
	usage    *UsageState
	lockSys  gdav.LockSystem
	// verifyLimiter 按 IP 对 Basic Auth 失败计数并临时锁定，防暴力破解。
	verifyLimiter *authRateLimiter
}

// NewWebdavServer 构造服务器；账号状态为共享引用，每次请求惰性读取，开关/账号变更无需重启即生效。
func NewWebdavServer(root string, accounts *WebdavAccountState, usage *UsageState) *WebdavServer {
	s := &WebdavServer{
		root:          root,
		accounts:      accounts,
		usage:         usage,
		lockSys:       gdav.NewMemLS(),
		verifyLimiter: newAuthRateLimiter(5, 15*time.Minute),
	}
	// 后台清理过期的限流条目，防止 attempts 无界增长。
	s.verifyLimiter.startCleanupLoop()
	return s
}

// authRateLimiter 是轻量的按 IP 失败计数器：maxFail 次失败后锁定 lockDuration。
// 结构与 --login 的限流一致；只统计失败，成功即清零。
type authRateLimiter struct {
	mu           sync.Mutex
	attempts     map[string]*authAttempt
	maxFail      int
	lockDuration time.Duration
}

type authAttempt struct {
	count       int
	lockedUntil time.Time
}

func newAuthRateLimiter(maxFail int, lockDuration time.Duration) *authRateLimiter {
	return &authRateLimiter{
		attempts:     make(map[string]*authAttempt),
		maxFail:      maxFail,
		lockDuration: lockDuration,
	}
}

func (l *authRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attempts[ip]
	if !ok {
		return true
	}
	if !a.lockedUntil.IsZero() {
		if time.Now().Before(a.lockedUntil) {
			return false
		}
		// 锁定已过期：清零重新计数。
		delete(l.attempts, ip)
	}
	return true
}

func (l *authRateLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attempts[ip]
	if !ok {
		a = &authAttempt{}
		l.attempts[ip] = a
	}
	a.count++
	if a.count >= l.maxFail {
		a.lockedUntil = time.Now().Add(l.lockDuration)
	}
}

func (l *authRateLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

// startCleanupLoop 定期清理过期条目，防止 attempts 无界增长。
func (l *authRateLimiter) startCleanupLoop() {
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			l.mu.Lock()
			now := time.Now()
			for ip, a := range l.attempts {
				if !a.lockedUntil.IsZero() && now.After(a.lockedUntil) {
					delete(l.attempts, ip)
				}
			}
			l.mu.Unlock()
		}
	}()
}

// ServeHTTP 在底层 gdav.Handler 前做 Basic Auth 与配额检查；顶层 PROPFIND 自行处理以注入配额属性，超额 PUT 返回 507。
func (s *WebdavServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.accounts.IsEnabled() {
		http.Error(w, "WebDAV is disabled", http.StatusServiceUnavailable)
		return
	}

	// 补全 "/dav" 的末尾斜杠：GNOME/gvfs 发现阶段会探测 "/dav"，缺斜杠时底层 handler（Prefix "/dav/"）无法剥离前缀而返回 404。
	if r.URL.Path == "/dav" {
		r.URL.Path = "/dav/"
	}

	user, pass, ok := r.BasicAuth()
	if !ok || user == "" || pass == "" {
		writeBasicChallenge(w)
		http.Error(w, "WebDAV authentication required", http.StatusUnauthorized)
		return
	}
	// 暴力破解防护：与 --login 一致的按 IP 失败计数与锁定（5 次 / 15 分钟）。
	if !s.verifyLimiter.allow(util.GetClientIP(r)) {
		log.Printf("webdav: rate limited for IP %s", util.SanitizeLogInput(util.GetClientIP(r)))
		writeBasicChallenge(w)
		http.Error(w, "WebDAV authentication required", http.StatusUnauthorized)
		return
	}
	acc, ok := s.accounts.VerifyWebdav(user, pass)
	if !ok {
		s.verifyLimiter.fail(util.GetClientIP(r))
		log.Printf("webdav: auth failed for user=%q from %s (%s %s)",
			util.SanitizeLogInput(user), util.GetClientIP(r), r.Method, r.URL.Path)
		writeBasicChallenge(w)
		http.Error(w, "WebDAV authentication required", http.StatusUnauthorized)
		return
	}
	s.verifyLimiter.reset(util.GetClientIP(r))

	// 只记录写操作：只读方法在浏览时高频触发，会刷爆日志。
	switch r.Method {
	case "PUT", "DELETE", "MKCOL", "MOVE", "COPY", "PROPPATCH":
		log.Printf("webdav: %s %s by user=%q (remark=%q) from %s",
			r.Method, r.URL.Path, acc.Username, acc.Remark, util.GetClientIP(r))
	}

	// 每账号 chroot：把文件操作限制在账号的 root_path 内。
	accountRoot := filepath.Join(s.root, acc.RootPath)
	if !strings.HasSuffix(accountRoot, string(filepath.Separator)) {
		accountRoot += string(filepath.Separator)
	}

	setQuotaContext(r, quotaContext{
		accountID:  acc.ID,
		quotaBytes: acc.QuotaBytes,
		root:       accountRoot,
	})
	defer clearQuotaContext(r)

	if r.Method == "PROPFIND" && isWebdavRootPath(r.URL.Path) {
		s.handlePropfindRoot(w, r)
		return
	}

	// 用 Content-Length 预检配额；谎报长度或分块传输的客户端由 quotaFileSystem 的写入后检查兜底。
	if r.Method == "PUT" && acc.QuotaBytes > 0 {
		if err := s.preflightPut(r, acc, accountRoot); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, ErrInsufficientCapacity) {
				status = http.StatusInsufficientStorage // 507
			}
			http.Error(w, err.Error(), status)
			return
		}
	}

	// handler 为请求级，但 LockSystem 跨请求共享以维持 WebDAV 锁语义。
	inner := gdav.Dir(accountRoot)
	qfs := &quotaFileSystem{
		inner:              inner,
		state:              s.usage,
		accountID:          acc.ID,
		quotaBytes:         acc.QuotaBytes,
		root:               accountRoot,
		readOnly:           acc.ReadOnly,
		protectSystemFiles: acc.ProtectSystemFiles,
	}
	handler := &gdav.Handler{
		Prefix:     "/dav/",
		FileSystem: qfs,
		LockSystem: s.lockSys,
	}
	handler.ServeHTTP(w, r)
}

// preflightPut 用 Content-Length 做快速配额预检，写入后的记账由 FileSystem 包装层完成。
func (s *WebdavServer) preflightPut(r *http.Request, acc WebdavAccount, accountRoot string) error {
	if s.usage == nil || r.ContentLength < 0 {
		return nil
	}
	used := s.usage.Get(acc.ID)
	// 扣除旧文件大小：用 1KB 覆盖 2GB 文件时不应按 +2GB 拒绝。
	var oldSize int64
	target := filepath.Join(accountRoot, urlPathClean(r.URL.Path))
	if info, err := os.Stat(target); err == nil && info != nil {
		oldSize = info.Size()
	}
	return preflightQuota(used, acc.QuotaBytes, oldSize, r.ContentLength)
}

// writeBasicChallenge 设置质询响应头，让 Windows/Finder/RaiDrive 弹出凭据输入框而不是静默失败。
func writeBasicChallenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="gohttpserver webdav"`)
	w.Header().Set("DAV", "1, 2")
	w.Header().Set("MS-Author-Via", "DAV")
}

// urlPathClean 去掉 /dav/ 前缀并 Clean，得到供 gdav.Dir 使用的安全相对路径；返回 "." 表示 chroot 根目录本身。
func urlPathClean(p string) string {
	const prefix = "/dav/"
	switch {
	case strings.HasPrefix(p, prefix):
		p = strings.TrimPrefix(p, prefix)
	case p == "/dav":
		p = ""
	}
	if p == "" {
		return "."
	}
	return filepath.ToSlash(filepath.Clean("/" + p))
}

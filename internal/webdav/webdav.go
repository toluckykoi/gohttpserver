package webdav

import (
	"context"
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
// SECURITY: maxEntries 是 attempts 映射的硬上限——未达锁定阈值（maxFail）的 IP
// 条目曾永不被清理（清理循环只删「已锁定且锁定已过期」的），公网扫描器/IPv6
// 海量源地址下曾使该映射无界增长形成内存 DoS。装满时拒绝新 IP 而非继续增长。
type authRateLimiter struct {
	mu               sync.Mutex
	attempts         map[string]*authAttempt
	maxFail          int
	lockDuration     time.Duration
	maxEntries       int
	saturationLogged bool // 映射已满的摘要日志只打一次，腾出空间后可再打
}

type authAttempt struct {
	count       int
	lastFail    time.Time
	lockedUntil time.Time
}

// authRateLimiterMaxEntries 与 login 包的 maxLoginAttemptsEntries 保持同一量级。
const authRateLimiterMaxEntries = 10000

func newAuthRateLimiter(maxFail int, lockDuration time.Duration) *authRateLimiter {
	return &authRateLimiter{
		attempts:     make(map[string]*authAttempt),
		maxFail:      maxFail,
		lockDuration: lockDuration,
		maxEntries:   authRateLimiterMaxEntries,
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
		// 硬上限：映射满时淘汰 lastFail 最旧的条目腾位后正常记录。
		// SECURITY: 旧实现直接 return——新 IP 的失败不被记录，而 allow() 对
		// 未知 IP 恒放行，限流对新 IP 完全失效（fail-open）。淘汰最旧条目
		// 不可被攻击者定向利用（最旧的条目本就最接近过期）。
		if len(l.attempts) >= l.maxEntries {
			l.evictOldestLocked()
			l.warnFullOnceLocked()
		}
		a = &authAttempt{}
		l.attempts[ip] = a
	}
	a.count++
	a.lastFail = time.Now()
	if a.count >= l.maxFail {
		a.lockedUntil = time.Now().Add(l.lockDuration)
	}
}

// evictOldestLocked 删除 lastFail 最旧的条目；调用方必须持有 l.mu。
// 映射至多 maxEntries 条，全量扫描一次的成本可接受。
func (l *authRateLimiter) evictOldestLocked() {
	var oldestIP string
	var oldest time.Time
	first := true
	for ip, a := range l.attempts {
		if first || a.lastFail.Before(oldest) {
			oldestIP, oldest, first = ip, a.lastFail, false
		}
	}
	if !first {
		delete(l.attempts, oldestIP)
	}
}

// warnFullOnceLocked 在映射首次到达上限时打一条摘要日志，之后静默直到清理腾出空间。
// 调用方必须持有 l.mu。
func (l *authRateLimiter) warnFullOnceLocked() {
	if l.saturationLogged {
		return
	}
	l.saturationLogged = true
	log.Printf("webdav: rate limit map is full (%d entries); oldest entries are evicted to make room — "+
		"existing lockouts still apply", l.maxEntries)
}

func (l *authRateLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

// startCleanupLoop 定期清理过期条目，防止 attempts 无界增长。
// SECURITY: 不只删「已锁定且锁定已过期」——失败 1..maxFail-1 次（未达锁定阈值）
// 的条目曾永不被清理，配合公网扫描即可无界增长。这里对 lastFail 超过 2 小时的
// 条目兜底删除（与 login 包的 startCleanupLoop 同策略）。
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
					continue
				}
				if now.Sub(a.lastFail) > 2*time.Hour {
					delete(l.attempts, ip)
				}
			}
			// 腾出空间后允许再次报告饱和。
			l.saturationLogged = false
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

	// PUT 请求体传输失败（客户端中断、网络断开）发生在读取侧，OpenFile 拿不到
	// 该信息；包装请求体把失败标记注入 context，供 quotaFile.Close 决定是否
	// 提交临时文件——中断的 PUT 不再把已有文件截断。
	if r.Method == "PUT" {
		bts := &bodyTransferState{}
		r.Body = &errorTrackingBody{rc: r.Body, onErr: bts.markFailed}
		r = r.WithContext(context.WithValue(r.Context(), bodyTransferKey{}, bts))
	}

	if r.Method == "PROPFIND" && isWebdavRootPath(r.URL.Path) {
		s.handlePropfindRoot(w, r)
		return
	}

	// 用 Content-Length 预检配额；谎报长度或分块传输的客户端由 quotaFileSystem 的写入后检查兜底。
	if r.Method == "PUT" && acc.QuotaBytes > 0 {
		oldSize, targetAbs, err := s.preflightPut(r, acc, accountRoot)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, ErrInsufficientCapacity) {
				status = http.StatusInsufficientStorage // 507
			}
			http.Error(w, err.Error(), status)
			return
		}
		// 把预检时 stat 到的旧大小带入请求 context，quotaFileSystem.OpenFile
		// 对同一目标不再重复 stat（第 10 轮性能审查）。
		if targetAbs != "" {
			r = r.WithContext(context.WithValue(r.Context(), putOldSizeKey{},
				putOldSizeInfo{absPath: targetAbs, size: oldSize}))
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
	// 同文件 COPY（Windows 大小写变体）与全部 MOVE 在进入 gdav 前接管，
	// 避免其「先删除目标再改名/重建」语义销毁唯一副本或掏空目录。
	if r.Method == "COPY" {
		if qfs.handleSameFileCopy(w, r) {
			return
		}
	} else if r.Method == "MOVE" {
		if qfs.handleMove(w, r) {
			return
		}
	}
	handler := &gdav.Handler{
		Prefix:     "/dav/",
		FileSystem: qfs,
		LockSystem: s.lockSys,
	}
	handler.ServeHTTP(w, r)
}

// preflightPut 用 Content-Length 做快速配额预检，写入后的记账由 FileSystem 包装层完成。
// 返回值 (oldSize, targetAbs) 是预检时 stat 到的目标绝对路径与覆盖前大小，供
// OpenFile 复用；目标不存在或为目录（非法 PUT 目标）时不复用，返回 ""。
func (s *WebdavServer) preflightPut(r *http.Request, acc WebdavAccount, accountRoot string) (int64, string, error) {
	if s.usage == nil || r.ContentLength < 0 {
		return 0, "", nil
	}
	used := s.usage.Get(acc.ID)
	// 扣除旧文件大小：用 1KB 覆盖 2GB 文件时不应按 +2GB 拒绝。
	var oldSize int64
	injectable := false
	target := filepath.Join(accountRoot, urlPathClean(r.URL.Path))
	if info, err := os.Stat(target); err == nil && info != nil {
		oldSize = info.Size()
		injectable = !info.IsDir()
	}
	err := preflightQuota(used, acc.QuotaBytes, oldSize, r.ContentLength)
	if injectable {
		return oldSize, target, err
	}
	return 0, "", err
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

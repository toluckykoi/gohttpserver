package webdav

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/codeskyblue/gohttpserver/internal/util"
	gdav "golang.org/x/net/webdav"
)

// WebdavServer 在 golang.org/x/net/webdav 外层提供 Basic Auth、每账号 chroot 与配额限制。
type WebdavServer struct {
	root     string
	accounts *WebdavAccountState
	usage    *UsageState
	lockSys  gdav.LockSystem
}

// NewWebdavServer 构造服务器；账号状态为共享引用，每次请求惰性读取，开关/账号变更无需重启即生效。
func NewWebdavServer(root string, accounts *WebdavAccountState, usage *UsageState) *WebdavServer {
	return &WebdavServer{
		root:     root,
		accounts: accounts,
		usage:    usage,
		lockSys:  gdav.NewMemLS(),
	}
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
	acc, ok := s.accounts.VerifyWebdav(user, pass)
	if !ok {
		log.Printf("webdav: auth failed for user=%q from %s (%s %s)",
			user, util.GetClientIP(r), r.Method, r.URL.Path)
		writeBasicChallenge(w)
		http.Error(w, "WebDAV authentication required", http.StatusUnauthorized)
		return
	}

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

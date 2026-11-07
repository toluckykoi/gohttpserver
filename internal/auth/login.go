package auth

import (
	"database/sql"
	"encoding/gob"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"

	storedb "github.com/codeskyblue/gohttpserver/internal/store"
	"github.com/codeskyblue/gohttpserver/internal/util"
)

// loginProviderName 用于在会话中区分用户名密码登录与 OpenID/oauth2-proxy。
const loginProviderName = "login"

func init() {
	gob.Register(&LoginUser{})
}

// LoginUser 是内置 --login 流程登录后存入会话的用户值。
type LoginUser struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

// loginCredentials 对应 login_credentials 表的单例行（id=1）。
type loginCredentials struct {
	Username      string `json:"username"`
	PasswordSHA   string `json:"password_sha256"` // bcrypt 哈希；旧版本可能存十六进制 SHA-256
	CreatedAtUnix int64  `json:"created_at"`
	UpdatedAtUnix int64  `json:"updated_at"`
}

const (
	defaultUsername         = "admin"
	maxLoginAttemptsEntries = 10000 // loginAttempts 映射的硬上限
)

// 默认密码判定缓存（defaultPwdKnown 字段）的三态值。
const (
	defaultPwdUnknown int32 = iota // 尚未计算，需做一次 bcrypt
	defaultPwdTrue                 // 当前凭据仍是 admin/admin
	defaultPwdFalse                // 已确认不是默认密码
)

// LoginState 持有由互斥锁保护的内存登录凭据。
type LoginState struct {
	mu     sync.RWMutex
	creds  *loginCredentials
	db     *sql.DB
	dbPath string // 保留供测试重开同一数据库验证持久化

	// defaultPwdKnown 缓存 isDefaultPassword 的判定结果。handleAuthStatus 每次
	// 认证探测都调用它，旧实现每请求全价 bcrypt（~60ms）；凭据变化即失效。
	defaultPwdKnown atomic.Int32

	rateLimitMu       sync.Mutex
	loginAttempts     map[string]*loginAttempt // 键为客户端 IP
	rateLimitEnabled  bool
	maxAttempts       int
	lockoutDuration   time.Duration
	maxAttemptEntries int
}

type loginAttempt struct {
	count       int
	lastFail    time.Time
	lockedUntil time.Time
}

func LoadLoginCredentials(db *sql.DB) *LoginState {
	return LoadLoginCredentialsAt(db, "")
}

// LoadLoginCredentialsAt 记录数据库路径供测试验证持久化。
func LoadLoginCredentialsAt(db *sql.DB, path string) *LoginState {
	st := &LoginState{
		db:                db,
		dbPath:            path,
		loginAttempts:     make(map[string]*loginAttempt),
		rateLimitEnabled:  true,
		maxAttempts:       5,
		lockoutDuration:   15 * time.Minute,
		maxAttemptEntries: maxLoginAttemptsEntries,
	}
	st.startCleanupLoop()
	if db == nil {
		st.creds = defaultLoginCredentials()
		log.Println("login: database is nil; using in-memory default credentials (admin/admin)")
		return st
	}
	var c loginCredentials
	err := db.QueryRow(
		"SELECT username, password_sha256, created_at, updated_at FROM login_credentials WHERE id = 1",
	).Scan(&c.Username, &c.PasswordSHA, &c.CreatedAtUnix, &c.UpdatedAtUnix)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("login: failed to read credentials (%v); using default credentials", err)
		}
		st.creds = defaultLoginCredentials()
		log.Printf("login: no stored credentials; using in-memory default credentials (admin/admin). " +
			"Change the password via PUT /-/api/auth/password to persist a new one.")
		return st
	}
	if c.Username == "" || c.PasswordSHA == "" {
		log.Printf("login: stored credentials missing username or password; using default credentials")
		c = *defaultLoginCredentials()
	}
	st.creds = &c
	return st
}

// LoginFromDB 打开 path 处的数据库并加载凭据。
func LoginFromDB(path string) (*LoginState, error) {
	db, err := storedb.OpenDB(path)
	if err != nil {
		return nil, err
	}
	return LoadLoginCredentialsAt(db, path), nil
}

// defaultLoginCredentials 返回 admin/admin（bcrypt）。
func defaultLoginCredentials() *loginCredentials {
	now := time.Now().Unix()
	hash, err := Generate(defaultPassword)
	if err != nil {
		log.Fatalf("login: failed to generate default password hash: %v", err)
	}
	return &loginCredentials{
		Username:      defaultUsername,
		PasswordSHA:   hash,
		CreatedAtUnix: now,
		UpdatedAtUnix: now,
	}
}

func (s *LoginState) verify(username, password string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.creds == nil {
		return false
	}

	if username != s.creds.Username {
		return false
	}

	ph := NewPasswordHashFromStored(s.creds.PasswordSHA)
	return ph.Verify(password, func(newHash string) {
		go s.upgradePasswordHash(newHash)
	})
}

func (s *LoginState) upgradePasswordHash(newHash string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 再次确认仍是旧哈希（可能已被并发修改）
	if !isLegacySHA256Hash(s.creds.PasswordSHA) {
		return
	}

	s.creds.PasswordSHA = newHash
	s.creds.UpdatedAtUnix = time.Now().Unix()
	if err := s.saveLocked(); err != nil {
		log.Printf("login: failed to save upgraded password hash: %v", err)
		return
	}
	s.invalidateDefaultPasswordCache()
	log.Println("login: successfully upgraded password hash from SHA-256 to bcrypt")
}

// isDefaultPassword 判断当前凭据是否仍是 admin/admin。
// 性能（第 10 轮性能审查）：判定结果按凭据缓存，命中零 bcrypt；未命中时锁内
// 只快照用户名与哈希，bcrypt 在锁外执行，不阻塞并发登录与改密。
func (s *LoginState) isDefaultPassword() bool {
	switch s.defaultPwdKnown.Load() {
	case defaultPwdTrue:
		return true
	case defaultPwdFalse:
		return false
	}

	s.mu.RLock()
	var username, storedHash string
	if s.creds != nil {
		username, storedHash = s.creds.Username, s.creds.PasswordSHA
	}
	s.mu.RUnlock()
	if username == "" {
		return false
	}

	result := false
	if username == defaultUsername {
		ph := NewPasswordHashFromStored(storedHash)
		result = ph.IsDefaultPassword()
	}
	if result {
		s.defaultPwdKnown.Store(defaultPwdTrue)
	} else {
		s.defaultPwdKnown.Store(defaultPwdFalse)
	}
	return result
}

// invalidateDefaultPasswordCache 在凭据变化后丢弃缓存的默认密码判定。
func (s *LoginState) invalidateDefaultPasswordCache() {
	s.defaultPwdKnown.Store(defaultPwdUnknown)
}

func (s *LoginState) changePassword(old, new string) error {
	if err := ValidatePassword(new); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.creds == nil {
		return errors.New("login: no credentials loaded")
	}

	ph := NewPasswordHashFromStored(s.creds.PasswordSHA)
	if !ph.Verify(old, nil) {
		return errors.New("invalid current password")
	}

	newHash, err := Generate(new)
	if err != nil {
		return errors.New("failed to hash password")
	}

	s.creds.PasswordSHA = newHash
	s.creds.UpdatedAtUnix = time.Now().Unix()
	if err := s.saveLocked(); err != nil {
		return err
	}
	s.invalidateDefaultPasswordCache()
	return nil
}

// ChangeUsername 更新存储的用户名；oldName 用于防御会话与磁盘状态不一致时误改他人账号。
func (s *LoginState) ChangeUsername(oldName, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.creds == nil {
		return errors.New("login: no credentials loaded")
	}
	if s.creds.Username != oldName {
		return errors.New("login: username changed concurrently; refusing rename")
	}
	s.creds.Username = newName
	s.creds.UpdatedAtUnix = time.Now().Unix()
	if err := s.saveLocked(); err != nil {
		return err
	}
	s.invalidateDefaultPasswordCache()
	return nil
}

func (s *LoginState) credsUsername() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.creds == nil {
		return "admin"
	}
	return s.creds.Username
}

// saveLocked upsert 单行凭据；调用方必须持有 s.mu。
func (s *LoginState) saveLocked() error {
	if s.creds == nil {
		return errors.New("login: no credentials loaded")
	}
	_, err := s.db.Exec(
		`INSERT INTO login_credentials (id, username, password_sha256, created_at, updated_at)
		 VALUES (1, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   username = excluded.username,
		   password_sha256 = excluded.password_sha256,
		   created_at = excluded.created_at,
		   updated_at = excluded.updated_at`,
		s.creds.Username, s.creds.PasswordSHA, s.creds.CreatedAtUnix, s.creds.UpdatedAtUnix,
	)
	return err
}

// checkRateLimit 在未锁定（或锁定已过期）时返回 true。
func (s *LoginState) checkRateLimit(ip string) bool {
	if !s.rateLimitEnabled {
		return true
	}

	s.rateLimitMu.Lock()
	defer s.rateLimitMu.Unlock()

	attempt, exists := s.loginAttempts[ip]
	if !exists {
		return true
	}

	if !attempt.lockedUntil.IsZero() && time.Now().After(attempt.lockedUntil) {
		// 锁定已过期，重置计数
		delete(s.loginAttempts, ip)
		return true
	}

	if !attempt.lockedUntil.IsZero() && time.Now().Before(attempt.lockedUntil) {
		return false
	}

	return true
}

func (s *LoginState) recordFailedLogin(ip string) {
	if !s.rateLimitEnabled {
		return
	}

	s.rateLimitMu.Lock()
	defer s.rateLimitMu.Unlock()

	attempt, exists := s.loginAttempts[ip]
	if !exists {
		// 硬上限：映射满时淘汰 lastFail 最旧的条目腾位后正常记录。
		// SECURITY: 旧实现直接 return——新 IP 的失败不被记录，而 checkRateLimit
		// 对未知 IP 恒放行，登录限流对新 IP 完全失效（fail-open）。淘汰最旧
		// 条目不可被攻击者定向利用（最旧条目本就最接近 2 小时清理线）。
		if len(s.loginAttempts) >= s.maxAttemptEntries {
			s.evictOldestAttemptLocked()
			log.Printf("login: rate limit map full (%d entries), evicted oldest entry for new IP %s",
				s.maxAttemptEntries, ip)
		}
		attempt = &loginAttempt{
			count:    0,
			lastFail: time.Now(),
		}
		s.loginAttempts[ip] = attempt
	}

	attempt.count++
	attempt.lastFail = time.Now()

	if attempt.count >= s.maxAttempts {
		attempt.lockedUntil = time.Now().Add(s.lockoutDuration)
		log.Printf("login: IP %s locked out for %v after %d failed attempts",
			ip, s.lockoutDuration, attempt.count)
	}
}

func (s *LoginState) recordSuccessfulLogin(ip string) {
	if !s.rateLimitEnabled {
		return
	}

	s.rateLimitMu.Lock()
	defer s.rateLimitMu.Unlock()

	delete(s.loginAttempts, ip)
}

// evictOldestAttemptLocked 删除 lastFail 最旧的条目；调用方必须持有 rateLimitMu。
// 映射至多 maxAttemptEntries 条，全量扫描一次的成本可接受。
func (s *LoginState) evictOldestAttemptLocked() {
	var oldestIP string
	var oldest time.Time
	first := true
	for ip, attempt := range s.loginAttempts {
		if first || attempt.lastFail.Before(oldest) {
			oldestIP, oldest, first = ip, attempt.lastFail, false
		}
	}
	if !first {
		delete(s.loginAttempts, oldestIP)
	}
}

func (s *LoginState) getRemainingAttempts(ip string) int {
	if !s.rateLimitEnabled {
		return s.maxAttempts
	}

	s.rateLimitMu.Lock()
	defer s.rateLimitMu.Unlock()

	attempt, exists := s.loginAttempts[ip]
	if !exists {
		return s.maxAttempts
	}

	remaining := s.maxAttempts - attempt.count
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

// startCleanupLoop 定期清理过期条目，防止 loginAttempts 无界增长。
func (s *LoginState) startCleanupLoop() {
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s.rateLimitMu.Lock()
			now := time.Now()
			for ip, attempt := range s.loginAttempts {
				if !attempt.lockedUntil.IsZero() && now.After(attempt.lockedUntil) {
					delete(s.loginAttempts, ip)
					continue
				}
				if now.Sub(attempt.lastFail) > 2*time.Hour {
					delete(s.loginAttempts, ip)
				}
			}
			s.rateLimitMu.Unlock()
		}
	}()
}

// loginWhitelist 列出启用 --login 时免认证的路径。
var loginWhitelist = []string{
	"/-/login",
	"/-/logout",
	"/-/user",
	"/-/sysinfo",
	"/-/api/auth/status",
	"/-/api/auth/password",
	"/-/frontend/",
	"/favicon.ico",
	"/favicon.png",
	// WebDAV 使用独立的 HTTP Basic Auth，不依赖会话 cookie。
	"/dav/",
}

func isLoginWhitelisted(path string) bool {
	// "/dav" 精确匹配：部分客户端（如 GNOME/gvfs）会在探测阶段访问它。
	if path == "/dav" {
		return true
	}
	for _, p := range loginWhitelist {
		// SECURITY: 精确名一律整串比较；目录型条目（以 "/" 结尾）才按前缀匹配。
		// 旧实现无条件 HasPrefix，"/-/login" 会放行 "/-/loginX" 等任意后缀路径，
		// 使其穿透登录闸门直达文件服务。
		if path == p {
			return true
		}
		if strings.HasSuffix(p, "/") && strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// loginWantsJSON 判断是否应返回 401 JSON 而非 302 重定向。
func loginWantsJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/json") {
		return true
	}
	if accept != "" && !strings.Contains(accept, "text/html") {
		return true
	}
	if accept == "" {
		return true
	}
	return false
}

// LoginAuthMiddleware 在启用 --login 时对所有请求做会话校验。
func LoginAuthMiddleware(prefix string, state *LoginState, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkPath := strings.TrimPrefix(r.URL.Path, prefix)
		if isLoginWhitelisted(checkPath) {
			next.ServeHTTP(w, r)
			return
		}

		if UserFromLoginSession(r) != nil {
			next.ServeHTTP(w, r)
			return
		}

		// 未认证：浏览器重定向，程序化客户端返回 401 JSON。
		if loginWantsJSON(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized","login_required":true}`))
			return
		}

		nextURL := safeNextURL(r, r.URL.RequestURI())
		loginURL := "/-/login?next=" + url.QueryEscape(nextURL)
		http.Redirect(w, r, loginURL, http.StatusFound)
	})
}

// UserFromLoginSession 从会话 cookie 中取出 *LoginUser；未认证或伪造请求返回 nil。
func UserFromLoginSession(r *http.Request) *LoginUser {
	session, _ := Store.Get(r, DefaultSessionName)
	val := session.Values["user"]
	if val == nil {
		return nil
	}
	u, ok := val.(*LoginUser)
	if !ok {
		return nil
	}
	return u
}

func handleLoginUI(w http.ResponseWriter, r *http.Request) {
	if UserFromLoginSession(r) != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	data, err := FrontendFS.ReadFile("frontend/dist/index.html")
	if err != nil {
		http.Error(w, "login page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func handleLoginSubmit(state *LoginState, w http.ResponseWriter, r *http.Request) {
	// SECURITY: 先限流请求体再解析表单——登录页无需大请求，未认证客户端
	// 不应能借 multipart 解析把数据缓冲到磁盘。
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	// ParseMultipartForm 同时兼容 multipart 与 url-encoded 表单。
	if err := r.ParseMultipartForm(32 << 20); err != nil && err != http.ErrNotMultipart {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	next := safeNextURL(r, r.FormValue("next"))

	if username == "" || password == "" {
		redirectToLoginWithError(w, r, next, "missing_credentials")
		return
	}

	clientIP := util.GetClientIP(r)
	if !state.checkRateLimit(clientIP) {
		log.Printf("login: rate limit exceeded for IP %s", util.SanitizeLogInput(clientIP))
		redirectToLoginWithError(w, r, next, "rate_limit_exceeded")
		return
	}

	if !state.verify(username, password) {
		state.recordFailedLogin(clientIP)
		remaining := state.getRemainingAttempts(clientIP)
		log.Printf("login: failed login attempt from %s (remaining attempts: %d)", util.SanitizeLogInput(clientIP), remaining)
		redirectToLoginWithError(w, r, next, "invalid_credentials")
		return
	}

	state.recordSuccessfulLogin(clientIP)

	// cookie 无法解码时 Store.Get 仍返回可用的空会话，故忽略错误直接写入新会话。
	session, _ := Store.Get(r, DefaultSessionName)
	session.Values["user"] = &LoginUser{
		Name:     username,
		Provider: loginProviderName,
	}
	if err := session.Save(r, w); err != nil {
		log.Printf("login: session save failed: %v", err)
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, next, http.StatusFound)
}

func redirectToLoginWithError(w http.ResponseWriter, r *http.Request, next, code string) {
	q := url.Values{}
	q.Set("next", next)
	q.Set("error", code)
	http.Redirect(w, r, "/-/login?"+q.Encode(), http.StatusFound)
}

// handleLoginLogout 清除会话中的 user 并重定向到 next（默认 /）；无会话时为空操作。
func handleLoginLogout(w http.ResponseWriter, r *http.Request) {
	session, err := Store.Get(r, DefaultSessionName)
	if err == nil {
		delete(session.Values, "user")
		session.Options.MaxAge = -1
		_ = session.Save(r, w)
	}
	next := safeNextURL(r, r.FormValue("next"))
	http.Redirect(w, r, next, http.StatusFound)
}

// handleAuthStatus 返回登录是否启用以及当前请求是否已认证。
func handleAuthStatus(loginEnabled bool, state *LoginState) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		user := UserFromLoginSession(r)
		status := map[string]any{
			"login_enabled": loginEnabled,
		}
		if loginEnabled && user != nil {
			status["authenticated"] = true
			status["name"] = user.Name
			if state != nil && state.isDefaultPassword() {
				status["using_default_password"] = true
			}
		} else if loginEnabled {
			status["authenticated"] = false
		}
		_ = json.NewEncoder(w).Encode(status)
	})
}

// HandleChangePassword 接收 JSON {old, new}，认证通过且旧密码匹配时更新密码。
func HandleChangePassword(state *LoginState, w http.ResponseWriter, r *http.Request) {
	if UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil {
		http.Error(w, "Body too large or unreadable", http.StatusBadRequest)
		return
	}
	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if err := state.changePassword(req.Old, req.New); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"success":true}`))
}

// RegisterLoginRoutes 在去掉前缀后的子路由上注册 --login 相关端点。
// /-/login 必须先于受保护的 / 兜底路由注册。
func RegisterLoginRoutes(router *mux.Router, state *LoginState, loginEnabled bool) {
	router.HandleFunc("/-/login", handleLoginUI).Methods("GET")
	router.HandleFunc("/-/login", func(w http.ResponseWriter, r *http.Request) {
		handleLoginSubmit(state, w, r)
	}).Methods("POST")

	router.HandleFunc("/-/logout", handleLoginLogout).Methods("GET")
	router.HandleFunc("/-/api/auth/status", handleAuthStatus(loginEnabled, state).ServeHTTP).Methods("GET")
	router.HandleFunc("/-/api/auth/password", func(w http.ResponseWriter, r *http.Request) {
		HandleChangePassword(state, w, r)
	}).Methods("PUT")
}

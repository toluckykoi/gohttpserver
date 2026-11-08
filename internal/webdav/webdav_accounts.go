package webdav

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codeskyblue/gohttpserver/internal/auth"
	storedb "github.com/codeskyblue/gohttpserver/internal/store"
	"github.com/codeskyblue/gohttpserver/internal/util"
)

// WebdavAccount 表示一条 WebDAV 凭据：Username 绑定登录用户、密码独立随机，仅存哈希，明文只在创建/重置时返回一次。
type WebdavAccount struct {
	ID                 string `json:"id"`
	Remark             string `json:"remark"`
	Username           string `json:"username"`
	PasswordSHA        string `json:"password_sha256"`
	RootPath           string `json:"root_path"`
	ReadOnly           bool   `json:"readonly"`
	ProtectSystemFiles bool   `json:"protect_system_files"`
	// QuotaBytes 为该账号存储上限，0 表示不限量。
	QuotaBytes    int64 `json:"quota_bytes"`
	CreatedAtUnix int64 `json:"created_at"`
	UpdatedAtUnix int64 `json:"updated_at"`
}

// WebdavAccountsFile 是 WebDAV 账号的内存状态。
type WebdavAccountsFile struct {
	Enabled  bool            `json:"enabled"`
	Accounts []WebdavAccount `json:"accounts"`
}

// WebdavAccountState 是并发安全的内存状态。
type WebdavAccountState struct {
	mu     sync.RWMutex
	data   *WebdavAccountsFile
	db     *sql.DB
	dbPath string // 供测试重开同一 DB 验证持久化

	// 凭据缓存：(username, password) 的哈希 → 账号 ID，TTL 内命中免去每请求一次的 bcrypt。
	// 任何账号变更经 saveLocked 统一清空。
	credMu    sync.Mutex
	credCache map[string]webdavCredEntry
}

// credCacheTTL 是凭据缓存的有效期；变更路径都会立即清空缓存，TTL 只是外部改动的兜底。
const credCacheTTL = 5 * time.Minute

type webdavCredEntry struct {
	accountID string
	expiresAt time.Time
}

// credCacheKey 派生缓存键：对 (username, password) 整体做 SHA-256，避免明文密码驻留内存。
func credCacheKey(username, password string) string {
	sum := sha256.Sum256([]byte(username + "\x00" + password))
	return hex.EncodeToString(sum[:])
}

// lookupCredCache 返回缓存命中的账号 ID，过期条目顺带剔除；调用方持有 s.mu（读锁即可）。
// 账号快照由调用方按 ID 从当前列表读取，属性始终是最新值。
func (s *WebdavAccountState) lookupCredCache(username, password string) (string, bool) {
	s.credMu.Lock()
	defer s.credMu.Unlock()
	if s.credCache == nil {
		return "", false
	}
	key := credCacheKey(username, password)
	entry, ok := s.credCache[key]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		delete(s.credCache, key)
		return "", false
	}
	return entry.accountID, true
}

// storeCredCache 记录一次验证成功的凭据；只在成功后写入，错误密码无法污染缓存。
func (s *WebdavAccountState) storeCredCache(username, password, accountID string) {
	s.credMu.Lock()
	defer s.credMu.Unlock()
	if s.credCache == nil {
		s.credCache = make(map[string]webdavCredEntry)
	}
	s.credCache[credCacheKey(username, password)] = webdavCredEntry{
		accountID: accountID,
		expiresAt: time.Now().Add(credCacheTTL),
	}
}

// invalidateCredCache 清空凭据缓存；所有持久化变更都经过 saveLocked，在此统一失效即可覆盖全部变更点。
func (s *WebdavAccountState) invalidateCredCache() {
	s.credMu.Lock()
	s.credCache = nil
	s.credMu.Unlock()
}

// systemFileNames 中的文件在开启 ProtectSystemFiles 时禁止通过 WebDAV 写入或删除。
var systemFileNames = map[string]bool{
	"auth-state.json":      true,
	"webdav-accounts.json": true,
	"storage-usage.json":   true,
	"gohttpserver.db":      true,
	"gohttpserver.db-wal":  true,
	"gohttpserver.db-shm":  true,
	"frp-encryption.key":   true,
	".ghs.yml":             true,
	"favicon.ico":          true,
	"favicon.png":          true,
}

// serverStateFileNames 无论 ProtectSystemFiles 开关如何都禁止经 WebDAV 读写：
// 账号 chroot 默认就是服务根，能读出凭据库/密钥即等同泄露。
var serverStateFileNames = []string{
	"gohttpserver.db",
	"gohttpserver.db-wal",
	"gohttpserver.db-shm",
	"frp-encryption.key",
	"auth-state.json",
	"webdav-accounts.json",
	"storage-usage.json",
}

// RegisterServerStateName 把额外文件基名（如自定义 --db 的文件名）并入 serverStateFileNames，须在启动阶段调用。
func RegisterServerStateName(name string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || strings.ContainsAny(name, `/\`) {
		return
	}
	for _, n := range serverStateFileNames {
		if strings.EqualFold(n, name) {
			return
		}
	}
	serverStateFileNames = append(serverStateFileNames, name)
}

// isServerStateNameWebdav 判断 base 是否为服务器私有文件的等价名
// （Windows 名字归一化 + 大小写不敏感，尾点与 ADS 变体都拦）。
// SECURITY(H2)：必须同时识别 8.3 短名（GOHTTP~1.DB 等）——系统卷默认生成短名，
// 凭据库/密钥可经短名绕过精确名单被读出或删除；判定用 util 共享实现，与 server/zipx 对齐。
func isServerStateNameWebdav(base string) bool {
	base = util.WindowsNormaliseName(base)
	for _, n := range serverStateFileNames {
		if strings.EqualFold(base, n) {
			return true
		}
	}
	return util.MatchesSensitiveShortName(base)
}

// LoadWebdavAccounts 从 SQLite 读取 WebDAV 账号状态。
func LoadWebdavAccounts(db *sql.DB) *WebdavAccountState {
	return LoadWebdavAccountsAt(db, "")
}

// LoadWebdavAccountsAt 额外记录 DB 路径（测试会重开该库）。
func LoadWebdavAccountsAt(db *sql.DB, path string) *WebdavAccountState {
	st := &WebdavAccountState{
		db:     db,
		dbPath: path,
		data:   &WebdavAccountsFile{Enabled: false},
	}

	var enabledStr string
	err := db.QueryRow("SELECT value FROM webdav_meta WHERE key = 'enabled'").Scan(&enabledStr)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Printf("webdav: failed to read enabled flag (%v); starting with empty account list", err)
		return st
	}
	st.data.Enabled = enabledStr == "true"

	rows, err := db.Query(
		`SELECT id, remark, username, password_sha256, root_path, readonly,
		        protect_system_files, quota_bytes, created_at, updated_at
		 FROM webdav_accounts ORDER BY created_at`)
	if err != nil {
		log.Printf("webdav: failed to read accounts (%v); starting with empty account list", err)
		return st
	}
	defer rows.Close()
	for rows.Next() {
		var a WebdavAccount
		if scanErr := rows.Scan(
			&a.ID, &a.Remark, &a.Username, &a.PasswordSHA, &a.RootPath,
			&a.ReadOnly, &a.ProtectSystemFiles, &a.QuotaBytes,
			&a.CreatedAtUnix, &a.UpdatedAtUnix,
		); scanErr != nil {
			log.Printf("webdav: scan account row failed: %v", scanErr)
			continue
		}
		st.data.Accounts = append(st.data.Accounts, a)
	}
	if err := rows.Err(); err != nil {
		log.Printf("webdav: error iterating accounts: %v", err)
	}
	log.Printf("webdav: loaded %d account(s) (enabled=%v)", len(st.data.Accounts), st.data.Enabled)
	return st
}

// WebdavAccountsFromDB 打开 path 处的 DB 并加载状态（仅供测试）。
func WebdavAccountsFromDB(path string) (*WebdavAccountState, error) {
	db, err := storedb.OpenDB(path)
	if err != nil {
		return nil, err
	}
	return LoadWebdavAccountsAt(db, path), nil
}

// generateWebdavPassword 生成 10 位随机密码，字符集排除易混淆的 0/O/1/I。
func generateWebdavPassword() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	const length = 10
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(buf), nil
}

// generateWebdavID 返回 "wd_" 前缀加 6 位十六进制的标识符。
func generateWebdavID() (string, error) {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	const hexChars = "0123456789abcdef"
	result := make([]byte, 9) // "wd_" + 6 chars
	result[0] = 'w'
	result[1] = 'd'
	result[2] = '_'
	for i := 0; i < 3; i++ {
		result[3+i*2] = hexChars[b[i]>>4]
		result[4+i*2] = hexChars[b[i]&0x0f]
	}
	return string(result), nil
}

// IsEnabled 返回 WebDAV 服务是否接受连接。
func (s *WebdavAccountState) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Enabled
}

// SetEnabled 切换总开关并持久化。
func (s *WebdavAccountState) SetEnabled(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Enabled = on
	return s.saveLocked()
}

// List 返回账号列表的副本。
func (s *WebdavAccountState) List() []WebdavAccount {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]WebdavAccount, len(s.data.Accounts))
	copy(out, s.data.Accounts)
	return out
}

// CreateAccountRequest 是 POST /-/api/admin/webdav/accounts 的 JSON 请求体。
type CreateAccountRequest struct {
	Remark             string `json:"remark"`
	RootPath           string `json:"root_path"`
	ReadOnly           bool   `json:"readonly"`
	ProtectSystemFiles bool   `json:"protect_system_files"`
	// QuotaBytes 为 0 或省略表示不限量。
	QuotaBytes int64 `json:"quota_bytes"`
}

// CreateAccount 校验请求、生成随机密码、持久化账号并一次性返回明文密码。
func (s *WebdavAccountState) CreateAccount(req CreateAccountRequest, loginUsername string) (WebdavAccount, string, error) {
	if req.Remark == "" {
		return WebdavAccount{}, "", errors.New("remark is required")
	}
	// 与 UpdateAccount 对齐拒绝负数配额：负值会使所有 quota>0 判断为假，静默等效"不限量"。
	if req.QuotaBytes < 0 {
		return WebdavAccount{}, "", errors.New("quota_bytes must not be negative")
	}
	root, err := normaliseWebdavRoot(req.RootPath)
	if err != nil {
		return WebdavAccount{}, "", err
	}
	id, err := generateWebdavID()
	if err != nil {
		return WebdavAccount{}, "", fmt.Errorf("generate id: %w", err)
	}
	pw, err := generateWebdavPassword()
	if err != nil {
		return WebdavAccount{}, "", fmt.Errorf("generate password: %w", err)
	}
	hash, err := auth.Generate(pw)
	if err != nil {
		return WebdavAccount{}, "", fmt.Errorf("hash password: %w", err)
	}
	now := time.Now().Unix()
	acc := WebdavAccount{
		ID:                 id,
		Remark:             req.Remark,
		Username:           loginUsername,
		PasswordSHA:        hash,
		RootPath:           root,
		ReadOnly:           req.ReadOnly,
		ProtectSystemFiles: req.ProtectSystemFiles,
		QuotaBytes:         req.QuotaBytes,
		CreatedAtUnix:      now,
		UpdatedAtUnix:      now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Accounts = append(s.data.Accounts, acc)
	if err := s.saveLocked(); err != nil {
		// 持久化失败则回滚 append。
		s.data.Accounts = s.data.Accounts[:len(s.data.Accounts)-1]
		return WebdavAccount{}, "", err
	}
	return acc, pw, nil
}

// UpdateAccountRequest 是 PUT /-/api/admin/webdav/accounts/{id} 的 JSON 请求体；不接受 username/password（前者绑定登录用户，后者只能重置）。
type UpdateAccountRequest struct {
	Remark             *string `json:"remark,omitempty"`
	RootPath           *string `json:"root_path,omitempty"`
	ReadOnly           *bool   `json:"readonly,omitempty"`
	ProtectSystemFiles *bool   `json:"protect_system_files,omitempty"`
	// QuotaBytes 用指针支持部分更新：省略不变、0 为不限量、正数封顶，负数拒绝。
	QuotaBytes *int64 `json:"quota_bytes,omitempty"`
}

// UpdateAccount 对已有账号做部分更新，未提供的字段保持不变。
func (s *WebdavAccountState) UpdateAccount(id string, req UpdateAccountRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.data.Accounts {
		if s.data.Accounts[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return errors.New("account not found")
	}
	acc := &s.data.Accounts[idx]
	if req.Remark != nil {
		if *req.Remark == "" {
			return errors.New("remark must not be empty")
		}
		acc.Remark = *req.Remark
	}
	if req.RootPath != nil {
		root, err := normaliseWebdavRoot(*req.RootPath)
		if err != nil {
			return err
		}
		acc.RootPath = root
	}
	if req.ReadOnly != nil {
		acc.ReadOnly = *req.ReadOnly
	}
	if req.ProtectSystemFiles != nil {
		acc.ProtectSystemFiles = *req.ProtectSystemFiles
	}
	if req.QuotaBytes != nil {
		if *req.QuotaBytes < 0 {
			return errors.New("quota_bytes must not be negative")
		}
		acc.QuotaBytes = *req.QuotaBytes
	}
	acc.UpdatedAtUnix = time.Now().Unix()
	return s.saveLocked()
}

// ResetPassword 为账号生成新随机密码并返回明文。
func (s *WebdavAccountState) ResetPassword(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.data.Accounts {
		if s.data.Accounts[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", errors.New("account not found")
	}
	pw, err := generateWebdavPassword()
	if err != nil {
		return "", err
	}
	hash, err := auth.Generate(pw)
	if err != nil {
		return "", err
	}
	s.data.Accounts[idx].PasswordSHA = hash
	s.data.Accounts[idx].UpdatedAtUnix = time.Now().Unix()
	if err := s.saveLocked(); err != nil {
		return "", err
	}
	return pw, nil
}

// DeleteAccount 删除指定 ID 的账号；ID 不存在时静默成功，保证 API 幂等。
func (s *WebdavAccountState) DeleteAccount(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.data.Accounts {
		if s.data.Accounts[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	s.data.Accounts = append(s.data.Accounts[:idx], s.data.Accounts[idx+1:]...)
	return s.saveLocked()
}

// SyncUsernamesTo 把绑定 oldName 的账号改名到 newName，在登录用户改名后调用，避免 WebDAV 客户端失效。
func (s *WebdavAccountState) SyncUsernamesTo(oldName, newName string) (int, error) {
	if oldName == newName {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// 记录索引以便 saveLocked 失败时回滚。
	type rename struct {
		idx     int
		oldName string
	}
	renames := make([]rename, 0)
	for i := range s.data.Accounts {
		if s.data.Accounts[i].Username == oldName {
			renames = append(renames, rename{idx: i})
		}
	}
	if len(renames) == 0 {
		return 0, nil
	}
	now := time.Now().Unix()
	for _, r := range renames {
		s.data.Accounts[r.idx].Username = newName
		s.data.Accounts[r.idx].UpdatedAtUnix = now
	}
	if err := s.saveLocked(); err != nil {
		// 持久化失败则回滚已改的用户名。
		for _, r := range renames {
			s.data.Accounts[r.idx].Username = oldName
		}
		return 0, err
	}
	log.Printf("webdav: synced %d account(s) from username %q → %q",
		len(renames), oldName, newName)
	return len(renames), nil
}

// VerifyWebdav 按用户名查账号并校验密码；同名账号可能多个、需全部尝试，不提前返回以免泄露用户名是否存在。
// TTL 内验证成功的凭据直接缓存命中（零 bcrypt）；bcrypt 在 s.mu 之外执行，锁内只拷贝同名账号的值副本，
// 命中后按 ID 重取当前账号，属性不受锁外验证期间变更的影响。
func (s *WebdavAccountState) VerifyWebdav(username, password string) (WebdavAccount, bool) {
	s.mu.RLock()
	cachedID, hasCached := s.lookupCredCache(username, password)
	var candidates []WebdavAccount
	for _, acc := range s.data.Accounts {
		if acc.Username == username {
			candidates = append(candidates, acc) // 值拷贝，脱离锁后 bcrypt 不再触碰共享状态
		}
	}
	totalAccounts := len(s.data.Accounts)
	s.mu.RUnlock()

	if hasCached {
		if acc, ok := s.accountByID(cachedID); ok {
			return acc, true
		}
		// 缓存指向的账号已不存在：落到慢路径（理论上不可达，变更即失效）。
	}

	var matchedID string
	found := false
	for i := range candidates {
		acc := candidates[i]
		ph := auth.NewPasswordHashFromStored(acc.PasswordSHA)
		if ph.Verify(password, func(newHash string) {
			// 校验成功后异步把旧哈希升级为 bcrypt。
			go s.upgradeWebdavPasswordHash(acc.ID, newHash)
		}) {
			matchedID = acc.ID
			found = true
			break
		}
	}
	if !found {
		// 记录用户名是否命中任何账号，便于区分「用户名错误」与「密码错误」。
		log.Printf("webdav: verify failed user=%q (accounts with matching username=%d, total accounts=%d)",
			username, len(candidates), totalAccounts)
		return WebdavAccount{}, false
	}

	// 命中后重取当前账号：锁外验证期间账号可能已被管理员修改或删除。
	acc, ok := s.accountByID(matchedID)
	if !ok {
		return WebdavAccount{}, false
	}
	s.storeCredCache(username, password, matchedID)
	return acc, true
}

// accountByID 返回当前列表中指定 ID 的账号快照。
func (s *WebdavAccountState) accountByID(id string) (WebdavAccount, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, acc := range s.data.Accounts {
		if acc.ID == id {
			return acc, true
		}
	}
	return WebdavAccount{}, false
}

// upgradeWebdavPasswordHash 在登录成功后以 goroutine 将旧 SHA-256 哈希升级为 bcrypt。
func (s *WebdavAccountState) upgradeWebdavPasswordHash(accountID, newHash string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i := range s.data.Accounts {
		if s.data.Accounts[i].ID == accountID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}

	// 加锁后复查：并发下哈希可能已被其他请求升级。
	ph := auth.NewPasswordHashFromStored(s.data.Accounts[idx].PasswordSHA)
	if ph.Algorithm() != auth.HashLegacySHA256 {
		return
	}

	s.data.Accounts[idx].PasswordSHA = newHash
	s.data.Accounts[idx].UpdatedAtUnix = time.Now().Unix()
	if err := s.saveLocked(); err != nil {
		log.Printf("webdav: failed to save upgraded password hash for account %q: %v", accountID, err)
		return
	}
	log.Printf("webdav: successfully upgraded password hash from SHA-256 to bcrypt for account %q", accountID)
}

// normaliseWebdavRoot 校验并规范化 root_path：强制 "/" 前缀且不含 ".."（防止逃出 --root）。
func normaliseWebdavRoot(input string) (string, error) {
	if input == "" {
		return "/", nil
	}
	// 统一为 "/"，WebDAV URL 只使用正斜杠。
	cleaned := filepath.ToSlash(filepath.Clean(input))
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	// 拒绝任何 ".." 段（filepath.Clean 会保留前导的 ".."）。
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == ".." {
			return "", errors.New("root_path must not contain '..'")
		}
	}
	return cleaned, nil
}

// saveLocked 在单个事务中持久化全部账号状态；调用方必须持有 s.mu 写锁。
// 任何变更（增删改/改密/重命名）都走这里，凭据缓存随之整体失效。
func (s *WebdavAccountState) saveLocked() error {
	if s.data == nil {
		return errors.New("webdav: no state loaded")
	}
	s.invalidateCredCache()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	enabled := "false"
	if s.data.Enabled {
		enabled = "true"
	}
	if _, err := tx.Exec(
		`INSERT INTO webdav_meta (key, value) VALUES ('enabled', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, enabled); err != nil {
		return err
	}

	if _, err := tx.Exec("DELETE FROM webdav_accounts"); err != nil {
		return err
	}
	insert, err := tx.Prepare(
		`INSERT INTO webdav_accounts
		   (id, remark, username, password_sha256, root_path, readonly,
		    protect_system_files, quota_bytes, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insert.Close()
	for _, a := range s.data.Accounts {
		if _, err := insert.Exec(
			a.ID, a.Remark, a.Username, a.PasswordSHA, a.RootPath, a.ReadOnly,
			a.ProtectSystemFiles, a.QuotaBytes, a.CreatedAtUnix, a.UpdatedAtUnix,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// isProtectedFilename 判断文件名在开启 ProtectSystemFiles 时是否禁止写入/删除。
// 比较前先做 Windows 名字归一化，尾点与 ADS 变体落盘时与原文件同体，一并拦截；
// 8.3 短名（GOHTTP~1.DB 等，H2）与敏感扩展的短名同体，同样禁止写入/删除。
func isProtectedFilename(name string) bool {
	name = util.WindowsNormaliseName(name)
	for sysName := range systemFileNames {
		if strings.EqualFold(name, sysName) {
			return true
		}
	}
	if util.MatchesSensitiveShortName(name) {
		return true
	}
	// 点文件同样受保护。
	return strings.HasPrefix(name, ".")
}

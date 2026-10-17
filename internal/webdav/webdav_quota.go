package webdav

import (
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	storedb "github.com/codeskyblue/gohttpserver/internal/store"
	gdav "golang.org/x/net/webdav"
)

// WebDAV 配额子系统：实施每账号存储上限、经 PROPFIND（RFC 4331）上报用量、超额写入返回 507。

// ErrInsufficientCapacity 在写入会超出配额时返回。
var ErrInsufficientCapacity = errors.New("webdav: insufficient storage capacity")

// UsageState 持久化各账号已用字节数，以 SQLite 为后端。
type UsageState struct {
	mu     sync.RWMutex
	used   map[string]int64
	db     *sql.DB
	dbPath string // 供测试重开同一 DB 验证持久化
}

// LoadUsageState 读取 storage_usage 表。
func LoadUsageState(db *sql.DB) *UsageState {
	return loadUsageStateAt(db, "")
}

// loadUsageStateAt 额外记录 DB 路径（测试会重开该库）。
func loadUsageStateAt(db *sql.DB, path string) *UsageState {
	s := &UsageState{
		db:     db,
		dbPath: path,
		used:   make(map[string]int64),
	}
	rows, err := db.Query("SELECT account_id, used_bytes FROM storage_usage")
	if err != nil {
		log.Printf("webdav-quota: failed to read storage_usage (%v); starting with empty usage", err)
		return s
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var used int64
		if scanErr := rows.Scan(&id, &used); scanErr != nil {
			log.Printf("webdav-quota: scan storage_usage row failed: %v", scanErr)
			continue
		}
		s.used[id] = used
	}
	if err := rows.Err(); err != nil {
		log.Printf("webdav-quota: error iterating storage_usage: %v", err)
	}
	log.Printf("webdav-quota: loaded usage for %d account(s)", len(s.used))
	return s
}

// usageStateFromDB 打开 path 处的 DB 并加载用量表（仅供测试）。
func usageStateFromDB(path string) (*UsageState, error) {
	db, err := storedb.OpenDB(path)
	if err != nil {
		return nil, err
	}
	return loadUsageStateAt(db, path), nil
}

// Get 返回账号的缓存字节数。
func (s *UsageState) Get(accountID string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.used[accountID]
}

// IsEmpty 报告用量表是否为空（为空说明是新库，需要初始播种）。
func (s *UsageState) IsEmpty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.used) == 0
}

// addDelta 原子更新缓存字节数并持久化；负 delta 会被截断到 0。
func (s *UsageState) addDelta(accountID string, delta int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.used[accountID] + delta
	if next < 0 {
		next = 0
	}
	if delta == 0 {
		return nil
	}
	s.used[accountID] = next
	return s.saveLocked()
}

// setUsage 原子替换缓存字节数并持久化；已知绝对总量时用它代替 addDelta，避免 Get 与 addDelta 之间的 TOCTOU 窗口。
func (s *UsageState) setUsage(accountID string, total int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.used[accountID] = total
	return s.saveLocked()
}

// Recalculate 遍历 rootPath，用磁盘实际总量替换缓存；遍历出错不致命（仍采用部分和）。
func (s *UsageState) Recalculate(rootPath string, accountID string) error {
	total, walkErr := walkSize(rootPath)
	if walkErr != nil {
		log.Printf("webdav-quota: recalculate(%s, %s) walk error: %v (using partial sum %d)",
			rootPath, accountID, walkErr, total)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.used[accountID] = total
	if err := s.saveLocked(); err != nil {
		return fmt.Errorf("persist after recalculate: %w", err)
	}
	if walkErr == nil {
		log.Printf("webdav-quota: recalculated usage for %s = %d bytes", accountID, total)
	}
	return nil
}

// RecalculateAll 遍历所有账号并刷新缓存。
func (s *UsageState) RecalculateAll(accounts []WebdavAccount, root string) []map[string]any {
	out := make([]map[string]any, 0, len(accounts))
	for _, acc := range accounts {
		full := filepath.Join(root, acc.RootPath)
		total, err := walkSize(full)
		if err != nil {
			out = append(out, map[string]any{
				"id":         acc.ID,
				"ok":         false,
				"error":      err.Error(),
				"used_bytes": int64(0),
			})
			continue
		}
		_ = s.setUsage(acc.ID, total)
		out = append(out, map[string]any{
			"id":         acc.ID,
			"ok":         true,
			"used_bytes": total,
		})
	}
	return out
}

// saveLocked 在单个事务中把用量表写入 storage_usage；调用方必须持有 s.mu 写锁。
func (s *UsageState) saveLocked() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	upsert, err := tx.Prepare(
		`INSERT INTO storage_usage (account_id, used_bytes) VALUES (?, ?)
		 ON CONFLICT(account_id) DO UPDATE SET used_bytes = excluded.used_bytes`)
	if err != nil {
		return err
	}
	defer upsert.Close()
	del, err := tx.Prepare("DELETE FROM storage_usage WHERE account_id = ?")
	if err != nil {
		return err
	}
	defer del.Close()

	for k, v := range s.used {
		if v <= 0 {
			if _, err := del.Exec(k); err != nil {
				return err
			}
			continue
		}
		if _, err := upsert.Exec(k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// walkSize 累加 root 下所有普通文件的大小。
func walkSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// 根目录缺失时干净返回 0，让调用方视为「尚无上传」。
			if errors.Is(walkErr, fs.ErrNotExist) && p == root {
				return nil
			}
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		// 跳过符号链接（可能指向根外或形成环）。
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil // 跳过不可读条目而非中止
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// quotaFileSystem 包装 webdav.FileSystem 以实施每账号配额：只在字节增减的边界拦截，硬保证来自写入后校验与管理员重算。
type quotaFileSystem struct {
	inner      gdav.FileSystem
	state      *UsageState
	accountID  string
	quotaBytes int64 // 0 表示不限量
	root       string
	readOnly   bool
	// protectSystemFiles 对应账号开关：拒绝写入系统文件与点文件（见 checkWriteAllowed）。
	protectSystemFiles bool
}

// Mkdir 创建目录（目录不占配额字节）。
func (q *quotaFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	if q.readOnly {
		return errors.New("webdav: account is read-only")
	}
	if err := q.checkWriteAllowed(name); err != nil {
		return err
	}
	return q.inner.Mkdir(ctx, name, perm)
}

// OpenFile 透传内层文件系统，并包装返回的 File 以便 Close 时对账字节差；写操作一律记账（不限量账号也要在管理界面显示用量），超配额告警仅在 quotaBytes > 0 时触发。
func (q *quotaFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (gdav.File, error) {
	isWrite := flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0

	if isWrite && q.readOnly {
		return nil, errors.New("webdav: account is read-only")
	}
	if isWrite {
		if err := q.checkWriteAllowed(name); err != nil {
			return nil, err
		}
	}

	// 必须在 inner.OpenFile 之前取旧文件大小：O_TRUNC 会先截断，之后再 Stat 就拿不到原值。
	var oldSize int64
	var absPath string
	if isWrite {
		absPath, _ = q.resolveInRoot(name)
		if absPath != "" {
			if info, statErr := os.Stat(absPath); statErr == nil && info != nil {
				oldSize = info.Size()
			}
		}
	}

	inner, err := q.inner.OpenFile(ctx, name, flag, perm)
	if err != nil {
		return nil, err
	}

	if !isWrite || absPath == "" {
		// 读操作或无法解析 chroot 的写入：直接透传，不记账。
		return inner, nil
	}

	// 把配额状态快照到局部变量，避免闭包依赖 q。
	preflight := q.quotaBytes
	acct := q.accountID
	stateRef := q.state

	qf := &quotaFile{
		File:    inner,
		absPath: absPath,
		oldSize: oldSize,
		onClose: func() error {
			info, statErr := os.Stat(absPath)
			if statErr != nil {
				// 文件可能已被其他途径删除，跳过记账。
				return nil
			}
			newSize := info.Size()
			delta := newSize - oldSize
			if delta == 0 {
				return nil
			}
			// 写入后校验：即使超出配额也只记录日志并接受文件，避免拒绝已落盘的数据。
			if preflight > 0 && stateRef.Get(acct)+delta > preflight {
				log.Printf("webdav-quota: account %s overshot quota by %d bytes (post-check); accepting file",
					acct, stateRef.Get(acct)+delta-preflight)
			}
			return stateRef.addDelta(acct, delta)
		},
	}
	return qf, nil
}

// RemoveAll 删除路径并扣减其删除前的字节总量；内层删除失败则不扣减计数。
func (q *quotaFileSystem) RemoveAll(ctx context.Context, name string) error {
	if q.readOnly {
		return errors.New("webdav: account is read-only")
	}
	if err := q.checkWriteAllowed(name); err != nil {
		return err
	}
	abs, inside := q.resolveInRoot(name)
	var removed int64
	if inside {
		removed, _ = walkSize(abs) // 忽略错误：部分和聊胜于无
	} else {
		// chroot 外的路径绝不记账。
		log.Printf("webdav-quota: refusing to account out-of-chroot delete %q", name)
	}
	if err := q.inner.RemoveAll(ctx, name); err != nil {
		return err
	}
	if inside && removed > 0 {
		if err := q.state.addDelta(q.accountID, -removed); err != nil {
			log.Printf("webdav-quota: account %s RemoveAll accounting failed: %v", q.accountID, err)
		}
	}
	return nil
}

// Rename 在同一 root 内改名（大小不变，不计配额）。
func (q *quotaFileSystem) Rename(ctx context.Context, oldName, newName string) error {
	if q.readOnly {
		return errors.New("webdav: account is read-only")
	}
	// 新旧两个路径都要通过写权限检查。
	if err := q.checkWriteAllowed(oldName); err != nil {
		return err
	}
	if err := q.checkWriteAllowed(newName); err != nil {
		return err
	}
	return q.inner.Rename(ctx, oldName, newName)
}

// Stat 纯透传，配额状态不影响元数据。
func (q *quotaFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return q.inner.Stat(ctx, name)
}

// quotaFile 包装 webdav.File，让 Close 时通过 UsageState.addDelta 对账字节差。
type quotaFile struct {
	gdav.File
	absPath string
	oldSize int64
	onClose func() error
	closed  bool
}

// Close 关闭内层文件并执行延迟记账。
func (f *quotaFile) Close() error {
	err := f.File.Close()
	if f.closed {
		return err
	}
	f.closed = true
	if f.onClose != nil {
		if accErr := f.onClose(); accErr != nil && err == nil {
			err = accErr
		}
	}
	return err
}

// resolveInRoot 把 name 拼到账号 chroot 上并报告结果是否仍在其内。切勿二次解码 name：net/http 已对 r.URL.Path 做过百分号解码，再解一次会让记账解析出的路径与内层 FS 实际操作的路径不一致。
func (q *quotaFileSystem) resolveInRoot(name string) (string, bool) {
	joined := filepath.Join(q.root, filepath.FromSlash(name))
	return joined, isUnderDir(joined, q.root)
}

// isUnderDir 判断 path 是 dir 本身还是其子孙。
func isUnderDir(path, dir string) bool {
	cleanDir := filepath.Clean(dir)
	cleanPath := filepath.Clean(path)
	if cleanPath == cleanDir {
		return true
	}
	return strings.HasPrefix(cleanPath+string(filepath.Separator),
		cleanDir+string(filepath.Separator))
}

// checkWriteAllowed 实施账号写限制：.ghs.yml（HTTP 访问控制文件）永远禁止写入；开启 ProtectSystemFiles 时系统文件与点文件同样受保护。
func (q *quotaFileSystem) checkWriteAllowed(name string) error {
	base := filepath.Base(filepath.FromSlash(name))
	if base == ".ghs.yml" {
		return errors.New("webdav: .ghs.yml is protected")
	}
	if q.protectSystemFiles && isProtectedFilename(base) {
		return fmt.Errorf("webdav: %q is a protected system file", base)
	}
	return nil
}

// quotaContext 是 ServeHTTP 暂存、handlePropfindRoot 读取的每请求配额快照。
type quotaContext struct {
	accountID  string
	quotaBytes int64
	root       string // 账号 chroot 的绝对文件系统路径
}

// 用 sync.Map 是因为普通 map 并发读写不安全。
var quotaRequestContext sync.Map

// setQuotaContext 为 r 安装配额上下文；调用方必须 defer clearQuotaContext(r) 以防泄漏。
func setQuotaContext(r *http.Request, ctx quotaContext) {
	quotaRequestContext.Store(r, ctx)
}

// clearQuotaContext 移除 r 的配额上下文，可重复调用。
func clearQuotaContext(r *http.Request) {
	quotaRequestContext.Delete(r)
}

// getQuotaContext 返回 r 的配额上下文；未设置时返回零值（由调用方自行处理）。
func getQuotaContext(r *http.Request) quotaContext {
	v, ok := quotaRequestContext.Load(r)
	if !ok {
		return quotaContext{}
	}
	return v.(quotaContext)
}

// davHref 返回 PROPFIND 响应使用的规范 href，按 RFC 4918 § 8.3 始终以斜杠结尾。
func (s *WebdavServer) davHref() string {
	return "/dav/"
}

// handlePropfindRoot 处理 WebDAV 根上的 PROPFIND，返回带配额属性（RFC 4331）的 207 multistatus；忽略请求体，让发空体的客户端（Windows 资源管理器、RaiDrive）也能成功。
func (s *WebdavServer) handlePropfindRoot(w http.ResponseWriter, r *http.Request) {
	depth := parseDepthHeader(r.Header.Get("Depth"))
	if depth > 1 {
		depth = 1 // RFC 4918 禁止在未声明 children-infinity 时使用 infinity
	}

	qctx := getQuotaContext(r)

	used := int64(0)
	if s.usage != nil {
		used = s.usage.Get(qctx.accountID)
	}
	quota := qctx.quotaBytes
	root := qctx.root

	rootInfo, err := os.Stat(root)
	if err != nil {
		http.Error(w, "root not accessible", http.StatusInternalServerError)
		return
	}

	var children []childInfo
	if depth == 1 {
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			http.Error(w, "cannot list root", http.StatusInternalServerError)
			return
		}
		for _, e := range entries {
			info, infoErr := e.Info()
			if infoErr != nil {
				continue
			}
			children = append(children, childInfo{
				name:    e.Name(),
				size:    info.Size(),
				modTime: info.ModTime(),
				isDir:   e.IsDir(),
			})
		}
	}

	ms := buildMultistatusResponse(
		s.davHref(), rootInfo, used, quota, children,
	)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("DAV", "1, 2")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(ms); err != nil {
		log.Printf("webdav-quota: PROPFIND encode error: %v", err)
	}
	_, _ = io.WriteString(w, "\n")
}

// parseDepthHeader 把 Depth 头转成 int（RFC 4918：0/1/infinity）。
func parseDepthHeader(v string) int {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1":
		return 1
	case "infinity":
		return 2 // "一直向下"的哨兵值；调用方反正会截断到 1
	default:
		return 0
	}
}

// Multistatus XML 类型（RFC 4918 / RFC 4331）。
type msResponse struct {
	XMLName   xml.Name     `xml:"D:response"`
	Href      string       `xml:"D:href"`
	Propstats []msPropstat `xml:"D:propstat"`
}

type msPropstat struct {
	XMLName xml.Name `xml:"D:propstat"`
	Prop    msProp   `xml:"D:prop"`
	Status  string   `xml:"D:status"`
}

type msProp struct {
	Resourcetype     msResourcetype `xml:"D:resourcetype"`
	DisplayName      string         `xml:"D:displayname,omitempty"`
	CreationDate     string         `xml:"D:creationdate,omitempty"`
	GetLastModified  string         `xml:"D:getlastmodified,omitempty"`
	GetContentLength *int64         `xml:"D:getcontentlength,omitempty"`
	GetContentType   string         `xml:"D:getcontenttype,omitempty"`
	GetEtag          string         `xml:"D:getetag,omitempty"`
	SupportedLock    string         `xml:"D:supportedlock,omitempty"`
	QuotaUsedBytes   *int64         `xml:"D:quota-used-bytes,omitempty"`
	QuotaAvailBytes  *int64         `xml:"D:quota-available-bytes,omitempty"`
}

type msResourcetype struct {
	XMLName    xml.Name  `xml:"D:resourcetype"`
	Collection *struct{} `xml:"D:collection,omitempty"`
}

type msMultistatus struct {
	XMLName   xml.Name     `xml:"D:multistatus"`
	DAVNS     string       `xml:"xmlns:D,attr"`
	Responses []msResponse `xml:"D:response"`
}

// childInfo 是 depth-1 子项列表所需的 os.FileInfo 精简版。
type childInfo struct {
	name    string
	size    int64
	modTime time.Time
	isDir   bool
}

// buildMultistatusResponse 组装 207 响应体。
func buildMultistatusResponse(
	rootHref string,
	rootInfo os.FileInfo,
	used, quota int64,
	children []childInfo,
) msMultistatus {
	rootProps := msProp{
		Resourcetype:    msResourcetype{Collection: &struct{}{}},
		DisplayName:     rootInfo.Name(),
		CreationDate:    rootInfo.ModTime().UTC().Format(time.RFC3339),
		GetLastModified: formatHTTPDate(rootInfo.ModTime()),
		SupportedLock:   "",
	}
	if quota > 0 {
		u := used
		rootProps.QuotaUsedBytes = &u
		avail := quota - used
		if avail < 0 {
			avail = 0
		}
		rootProps.QuotaAvailBytes = &avail
	}
	rootResp := msResponse{
		Href: rootHref,
		Propstats: []msPropstat{{
			Prop:   rootProps,
			Status: "HTTP/1.1 200 OK",
		}},
	}

	responses := []msResponse{rootResp}
	for _, c := range children {
		href := strings.TrimRight(rootHref, "/") + "/" + urlPathEscape(c.name)
		props := msProp{
			Resourcetype:    msResourcetype{}, // 空表示普通文件或非集合
			DisplayName:     c.name,
			CreationDate:    c.modTime.UTC().Format(time.RFC3339),
			GetLastModified: formatHTTPDate(c.modTime),
			GetEtag:         weakEtag(c.modTime, c.size),
		}
		if !c.isDir {
			size := c.size
			props.GetContentLength = &size
		} else {
			props.Resourcetype = msResourcetype{Collection: &struct{}{}}
		}
		responses = append(responses, msResponse{
			Href: href,
			Propstats: []msPropstat{{
				Prop:   props,
				Status: "HTTP/1.1 200 OK",
			}},
		})
	}

	return msMultistatus{
		DAVNS:     "DAV:",
		Responses: responses,
	}
}

// formatHTTPDate 返回适合 DAV:getlastmodified 的 RFC 1123 日期。
func formatHTTPDate(t time.Time) string {
	return t.UTC().Format(http.TimeFormat)
}

// weakEtag 生成 "W/<unix>-<size>" 形式的弱校验标签，足够满足只需变更检测的客户端。
func weakEtag(t time.Time, size int64) string {
	return fmt.Sprintf(`W/"%x-%x"`, t.Unix(), size)
}

// isWebdavRootPath 判断 p 是否为 WebDAV 挂载根（带不带结尾斜杠均可），供 ServeHTTP 决定是否自行处理该请求。
func isWebdavRootPath(p string) bool {
	trimmed := strings.TrimRight(p, "/")
	return trimmed == "" || trimmed == "/dav"
}

// preflightQuota 在写入 contentLength 字节会超出配额时返回 ErrInsufficientCapacity；未设配额或长度未知时返回 nil。
func preflightQuota(used, quotaBytes, oldSize, contentLength int64) error {
	if quotaBytes <= 0 {
		return nil
	}
	if contentLength < 0 {
		// 长度未知，交给写入后校验兜底。
		return nil
	}
	after := used - oldSize + contentLength
	if after < 0 {
		// 文件在缩小。
		return nil
	}
	if after > quotaBytes {
		return ErrInsufficientCapacity
	}
	return nil
}

// urlPathEscape 自实现路径转义，避免净引入 net/url（纯路径测试中会形成导入环）。
func urlPathEscape(s string) string {
	const safe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.~"
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c > 0x7f || strings.IndexByte(safe, c) < 0 {
			// 两位零填充的十六进制。
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&0x0f])
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

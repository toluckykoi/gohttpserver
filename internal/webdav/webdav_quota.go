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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	storedb "github.com/codeskyblue/gohttpserver/internal/store"
	"github.com/codeskyblue/gohttpserver/internal/util"
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
	return s.persistAccountLocked(accountID)
}

// setUsage 原子替换缓存字节数并持久化；已知绝对总量时用它代替 addDelta，避免 Get 与 addDelta 之间的 TOCTOU 窗口。
func (s *UsageState) setUsage(accountID string, total int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.used[accountID] = total
	return s.persistAccountLocked(accountID)
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
	if err := s.persistAccountLocked(accountID); err != nil {
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

// persistAccountLocked 只写单个账号的用量行：每个 WebDAV PUT/COPY 的 Close 都会
// 触发一次记账，旧实现全表重写（O(账号数) 放大）且跑在唯一连接上串行化
// （第 8 轮性能审查）。用量 ≤0 时删除该行，与旧 saveLocked 的清理语义一致。
// 调用方必须持有 s.mu 写锁。
func (s *UsageState) persistAccountLocked(accountID string) error {
	v := s.used[accountID]
	if v <= 0 {
		_, err := s.db.Exec("DELETE FROM storage_usage WHERE account_id = ?", accountID)
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO storage_usage (account_id, used_bytes) VALUES (?, ?)
		 ON CONFLICT(account_id) DO UPDATE SET used_bytes = excluded.used_bytes`,
		accountID, v)
	return err
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
//
// SECURITY/可靠性（第 8 轮审计）：
//   - 读通道先过 checkReadAllowed，服务器私有文件与 .ghs.yml 以 404 形式拒绝；
//   - 整体替换型写（含 O_TRUNC/O_CREATE，即 PUT 与 COPY 覆盖）改为写同目录临时
//     文件、Close 成功后再改名替换——x/net/webdav 的 handlePut 用 O_TRUNC 直开
//     目标，上传中途断连会把已有文件截断为零字节。
func (q *quotaFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (gdav.File, error) {
	isWrite := flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0

	if isWrite && q.readOnly {
		return nil, errors.New("webdav: account is read-only")
	}
	if isWrite {
		if err := q.checkWriteAllowed(name); err != nil {
			return nil, err
		}
	} else if err := q.checkReadAllowed(name); err != nil {
		return nil, readDenied("open", name)
	}

	// 必须在 inner.OpenFile 之前取旧文件大小：O_TRUNC 会先截断，之后再 Stat 就拿不到原值。
	// SECURITY: 仅当解析结果确实落在 chroot 内才启用临时文件/记账通道——
	// absPath 直接来自文件系统路径拼接，逃逸的 name（如来自非清洗调用方的
	// "../x"）会让临时文件写到 chroot 之外。逃逸时回退到 inner.OpenFile，
	// 由 gdav.Dir 的 resolve 语义（slashClean 后拼回根内）兜底，与旧行为一致。
	var oldSize int64
	var absPath string
	if isWrite {
		if joined, inside := q.resolveInRoot(name); inside {
			absPath = joined
			// preflightPut 已对 PUT 目标 stat 过旧大小（见 putOldSizeInfo）：
			// 路径一致时直接复用，不一致（应不可达）时退回自行 stat。
			if pi, ok := ctx.Value(putOldSizeKey{}).(putOldSizeInfo); ok && pi.absPath == absPath {
				oldSize = pi.size
			} else if info, statErr := os.Stat(absPath); statErr == nil && info != nil {
				oldSize = info.Size()
			}
		}
	}

	// 原子写通道：仅对「整体替换」语义（O_TRUNC 或 O_CREATE）启用；纯追加/原位
	// 读写（WebDAV 客户端不会发出）保持直开，避免破坏 append 语义。
	var tmpPath string
	var inner gdav.File
	if isWrite && absPath != "" && flag&(os.O_TRUNC|os.O_CREATE) != 0 {
		tmp, err := os.CreateTemp(filepath.Dir(absPath), "."+filepath.Base(absPath)+".davtmp-*")
		if err != nil {
			return nil, err
		}
		tmpPath = tmp.Name()
		inner = tmp // *os.File 满足 gdav.File 全部方法
	} else {
		var err error
		inner, err = q.inner.OpenFile(ctx, name, flag, perm)
		if err != nil {
			return nil, err
		}
	}

	if !isWrite || absPath == "" {
		// 读操作或无法解析 chroot 的写入：直接透传，不记账。
		return inner, nil
	}

	// 把配额状态快照到局部变量，避免闭包依赖 q。
	preflight := q.quotaBytes
	acct := q.accountID
	stateRef := q.state

	transfer, _ := ctx.Value(bodyTransferKey{}).(*bodyTransferState)
	qf := &quotaFile{
		File:     inner,
		absPath:  absPath,
		oldSize:  oldSize,
		tmpPath:  tmpPath,
		transfer: transfer,
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

// Stat 透传元数据查询，但服务器私有文件与 .ghs.yml 以 404 形式拒绝，
// 不向客户端确认其存在。
func (q *quotaFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	if err := q.checkReadAllowed(name); err != nil {
		return nil, readDenied("stat", name)
	}
	return q.inner.Stat(ctx, name)
}

// quotaFile 包装 webdav.File，让 Close 时通过 UsageState.addDelta 对账字节差。
// tmpPath 非空表示本次写入走的是临时文件，Close 负责改名替换目标；
// writeErr 与 transfer（请求体传输失败标记，见 bodyTransferState）任一命中，
// Close 都会丢弃临时文件而非把残缺内容提交到目标路径。
type quotaFile struct {
	gdav.File
	absPath  string
	oldSize  int64
	tmpPath  string
	transfer *bodyTransferState
	onClose  func() error
	closed   bool
	writeErr error
}

// Write 转发底层写入并记录首个错误，供 Close 决定是否提交。
func (f *quotaFile) Write(p []byte) (int, error) {
	n, err := f.File.Write(p)
	if err != nil && f.writeErr == nil {
		f.writeErr = err
	}
	return n, err
}

// Close 关闭内层文件，提交或丢弃写入，最后执行延迟记账。
func (f *quotaFile) Close() error {
	err := f.File.Close()
	if f.closed {
		return err
	}
	f.closed = true

	if f.writeErr != nil || f.transfer.transferFailed() {
		// 写入失败（磁盘满）或请求体传输中断（客户端断开）：丢弃临时文件，
		// 目标文件保持原样。
		if f.tmpPath != "" {
			os.Remove(f.tmpPath)
		}
		if f.writeErr != nil {
			return f.writeErr
		}
		return errors.New("webdav: request body transfer failed")
	}
	if f.tmpPath != "" {
		if err != nil {
			os.Remove(f.tmpPath)
			return err
		}
		if rerr := os.Rename(f.tmpPath, f.absPath); rerr != nil {
			os.Remove(f.tmpPath)
			return rerr
		}
	} else if err != nil {
		return err
	}

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

// stripDavPrefix 从 URL 路径剥掉 "/dav/" 前缀，得到与 gdav.FileSystem 一致的
// 请求名；不是该前缀下的路径返回 false。
func stripDavPrefix(p string) (string, bool) {
	const prefix = "/dav/"
	switch {
	case strings.HasPrefix(p, prefix):
		return strings.TrimPrefix(p, prefix), true
	case p == "/dav":
		return "", true
	}
	return "", false
}

// bodyTransferState 跟踪 PUT 请求体的传输是否失败。失败可能发生在读取侧
// （客户端中断、网络断开），不经过 OpenFile 返回的 File 的 Write，因此由
// ServeHTTP 层包装请求体记录，Close 时据此丢弃临时文件。
type bodyTransferState struct {
	failed atomic.Bool
}

func (b *bodyTransferState) markFailed() { b.failed.Store(true) }
func (b *bodyTransferState) transferFailed() bool {
	return b != nil && b.failed.Load()
}

// bodyTransferKey 是 bodyTransferState 在请求 context 中的键。
type bodyTransferKey struct{}

// putOldSizeInfo 由 ServeHTTP 在 preflightPut 之后注入请求 context：PUT 目标的
// 绝对路径与其覆盖前大小。quotaFileSystem.OpenFile 对同一目标可复用该值，
// 省去背靠背的重复 stat（第 10 轮性能审查）；路径不一致时退回自行 stat。
type putOldSizeInfo struct {
	absPath string
	size    int64
}

type putOldSizeKey struct{}

// errorTrackingBody 包装请求体，任何非 EOF 的读取错误都回调 onErr。
type errorTrackingBody struct {
	rc    io.ReadCloser
	onErr func()
}

func (b *errorTrackingBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		b.onErr()
	}
	return n, err
}

func (b *errorTrackingBody) Close() error { return b.rc.Close() }

// handleSameFileCopy 拦截「目标与源是同一个文件」的 COPY（Windows 大小写变体
// 最常见，如 Report.txt → report.txt）。x/net/webdav 的 copyFiles 对已存在目标
// 先 RemoveAll 再截断重建——目标就是源本身时，唯一副本会先被删除（本仓库
// repro 测试实证的数据丢失）。按 RFC 4918 §9.8.4 返回 403 且不动数据。
// 其余 COPY 返回 false，交回底层 handler 按标准语义处理。
func (q *quotaFileSystem) handleSameFileCopy(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "COPY" {
		return false
	}
	src, ok := stripDavPrefix(r.URL.Path)
	if !ok {
		return false
	}
	dstURL, err := url.Parse(r.Header.Get("Destination"))
	if err != nil || dstURL.Path == "" {
		return false // 目标头非法，交给 gdav 返回 400
	}
	if dstURL.Host != "" && dstURL.Host != r.Host {
		return false // 跨主机目标，交给 gdav 按标准返回 502
	}
	dst, ok := stripDavPrefix(dstURL.Path)
	if !ok {
		return false // 跨 chroot 的目标，交给 gdav 报错
	}
	if dst == src {
		return false // gdav 已按 RFC 对 dst==src 返回 403
	}

	srcAbs, srcInside := q.resolveInRoot(src)
	dstAbs, dstInside := q.resolveInRoot(dst)
	// chroot 根（"." 等解析到根的形式）不参与同文件判定，交回 gdav 由其
	// Dir 层根守卫处理。
	if !srcInside || !dstInside ||
		filepath.Clean(srcAbs) == filepath.Clean(q.root) ||
		filepath.Clean(dstAbs) == filepath.Clean(q.root) {
		return false
	}
	dstInfo, err := os.Stat(dstAbs)
	if err != nil {
		return false // 目标不存在，属正常 COPY
	}
	srcInfo, err := os.Stat(srcAbs)
	if err != nil {
		return false // 源不存在，交给 gdav 返回 404
	}
	if !os.SameFile(srcInfo, dstInfo) {
		return false
	}
	http.Error(w, "cannot copy a resource onto itself", http.StatusForbidden)
	return true
}

// handleMove 以安全语义处理全部 MOVE：x/net/webdav 的 moveFiles 对已存在目标
// 先 RemoveAll 再 Rename——「先删后搬」中途失败（目标文件被占用、权限不足等）
// 会把目标目录掏空或销毁后又不完成搬移（repro 测试实证）。这里改为：
//  1. 同一文件（大小写改名）：直接 Rename，Windows 原生支持；
//  2. 目标存在且 Overwrite:T：先把目标**改名**挪到同目录临时名（原子、轻量），
//     搬入成功后再清理；失败则原样放回；
//  3. 目标不存在：普通 Rename，失败无副作用。
//
// 与 gdav 的偏差：不校验 Lock token（LOCK 由单管理员模型兜底）；目录与文件
// 类型互换的目标替换返回 403 而不是先删后建。返回 true 表示已完整处理请求。
func (q *quotaFileSystem) handleMove(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "MOVE" {
		return false
	}
	src, ok := stripDavPrefix(r.URL.Path)
	if !ok {
		return false
	}
	dstURL, err := url.Parse(r.Header.Get("Destination"))
	if err != nil || dstURL.Path == "" {
		return false // 目标头非法，交给 gdav 返回 400
	}
	if dstURL.Host != "" && dstURL.Host != r.Host {
		return false // 跨主机目标，交给 gdav 按标准返回 502
	}
	dst, ok := stripDavPrefix(dstURL.Path)
	if !ok {
		return false // 跨 chroot 的目标，交给 gdav 报错
	}
	if dst == src {
		http.Error(w, "source and destination are the same", http.StatusForbidden)
		return true
	}
	// 账号 chroot 根本身不可被改名或作为目标。
	if src == "" || dst == "" {
		http.Error(w, "cannot move the collection root", http.StatusForbidden)
		return true
	}
	if q.readOnly {
		http.Error(w, "webdav: account is read-only", http.StatusForbidden)
		return true
	}
	if err := q.checkWriteAllowed(src); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return true
	}
	if err := q.checkWriteAllowed(dst); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return true
	}

	srcAbs, srcInside := q.resolveInRoot(src)
	dstAbs, dstInside := q.resolveInRoot(dst)
	// SECURITY: 账号 chroot 根本身（"."、"/" 等解析到根的等价形式）不可作为
	// MOVE 的源或目标——对根执行「先挪去临时名」会在共享目录上开窗（挪走后
	// 进程崩溃即断链）。gdav 的 Dir 层自带根守卫，自管语义须做同等防御。
	if !srcInside || !dstInside ||
		filepath.Clean(srcAbs) == filepath.Clean(q.root) ||
		filepath.Clean(dstAbs) == filepath.Clean(q.root) {
		http.Error(w, "invalid move source or destination", http.StatusBadRequest)
		return true
	}
	// 不允许把目录移动进它自己的子树（否则 Rename 部分完成后形成环）。
	if filepath.Clean(dstAbs) != filepath.Clean(srcAbs) && isUnderDir(dstAbs, srcAbs) {
		http.Error(w, "cannot move a collection into itself", http.StatusConflict)
		return true
	}

	srcInfo, err := os.Stat(srcAbs)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return true
	}

	created := false
	var aside string
	dstInfo, dstErr := os.Stat(dstAbs)
	if dstErr == nil {
		if os.SameFile(srcInfo, dstInfo) {
			// 大小写改名等同一文件场景：直接改名，不做任何删除。
			if err := os.Rename(srcAbs, dstAbs); err != nil {
				log.Printf("webdav: same-file rename %q -> %q failed: %v", src, dst, err)
				http.Error(w, "move failed", http.StatusForbidden)
				return true
			}
			w.WriteHeader(http.StatusNoContent)
			return true
		}
		if r.Header.Get("Overwrite") != "T" {
			http.Error(w, "destination exists", http.StatusPreconditionFailed)
			return true
		}
		// 先把既有目标原子地挪走（而非删除），搬入失败时原样放回。
		aside, err = reserveTempName(dstAbs)
		if err != nil {
			http.Error(w, "move failed", http.StatusInternalServerError)
			return true
		}
		if err := os.Rename(dstAbs, aside); err != nil {
			os.Remove(aside)
			log.Printf("webdav: stashing existing destination %q failed: %v", dst, err)
			http.Error(w, "move failed", http.StatusForbidden)
			return true
		}
	} else {
		created = true
	}

	if err := os.Rename(srcAbs, dstAbs); err != nil {
		if aside != "" {
			// 回滚：把原目标放回原位，杜绝部分删除。回滚失败时旧目标仍在
			// aside 临时名下（数据未丢），必须留痕供人工恢复。
			if rbErr := os.Rename(aside, dstAbs); rbErr != nil {
				log.Printf("webdav: ROLLBACK FAILED for move %q -> %q: %v (old destination preserved at %q)",
					src, dst, rbErr, aside)
			}
		}
		log.Printf("webdav: move %q -> %q failed: %v", src, dst, err)
		http.Error(w, "move failed", http.StatusForbidden)
		return true
	}
	if aside != "" {
		// 被替换的旧目标此刻才真正删除；其字节数需从账号用量中扣回。
		if removed, _ := walkSize(aside); removed > 0 {
			if derr := q.state.addDelta(q.accountID, -removed); derr != nil {
				log.Printf("webdav-quota: account %s move-replace accounting failed: %v", q.accountID, derr)
			}
		}
		os.RemoveAll(aside)
	}
	if created {
		w.WriteHeader(http.StatusCreated)
		return true
	}
	w.WriteHeader(http.StatusNoContent)
	return true
}

// reserveTempName 在 target 同目录预留一个不存在的临时路径，供「先挪走目标」
// 使用。CreateTemp 再关闭并删除，留下可用的名字；命名保证不与用户文件冲突。
func reserveTempName(target string) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".davmove-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	os.Remove(name)
	return name, nil
}

// isAccessConfNameWindows 判断 base 是否为 .ghs.yml 的 Windows 等价名
// （Windows 名字归一化 + 大小写不敏感 + 8.3 短名 GHS~1.YML）。与 server 包的
// isAccessConfName 规则一致。归一化封住 ".ghs.yml."（尾点）与 ".ghs.yml::$DATA"
// （ADS）等打开时与原文件同体的变体——仅 EqualFold+8.3 曾被绕过（第 8 轮审计实证）。
func isAccessConfNameWindows(base string) bool {
	base = util.WindowsNormaliseName(base)
	if strings.EqualFold(base, ".ghs.yml") {
		return true
	}
	upper := strings.ToUpper(base)
	// 8.3 短名模式：<前缀>~<数字>.<3 位扩展名>；.ghs.yml 的短名形如 GHS~1.YML。
	idx := strings.IndexByte(upper, '~')
	if idx <= 0 || idx > 7 {
		return false
	}
	rest := upper[idx+1:]
	dot := strings.IndexByte(rest, '.')
	if dot < 1 {
		return false
	}
	digits := rest[:dot]
	if digits == "" {
		return false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false
		}
	}
	ext := rest[dot+1:]
	return ext == "YML" || ext == "YAML"
}

// checkWriteAllowed 实施账号写限制：.ghs.yml（HTTP 访问控制文件）永远禁止写入
// （Windows 上大小写不敏感且存在 8.3 短名，精确匹配曾可被 .GHS.YML / GHS~1.YML 绕过）；
// 服务器私有文件（DB/FRP 密钥）无论 ProtectSystemFiles 开关如何都禁止写入——
// 开关关闭时账号曾能直接覆盖凭据库；开启 ProtectSystemFiles 时系统文件与
// 点文件同样受保护。
func (q *quotaFileSystem) checkWriteAllowed(name string) error {
	base := filepath.Base(filepath.FromSlash(name))
	if isAccessConfNameWindows(base) {
		return errors.New("webdav: .ghs.yml is protected")
	}
	if isServerStateNameWebdav(base) {
		return fmt.Errorf("webdav: %q is a protected system file", base)
	}
	if q.protectSystemFiles && isProtectedFilename(base) {
		return fmt.Errorf("webdav: %q is a protected system file", base)
	}
	return nil
}

// checkReadAllowed 拦截经 WebDAV 读取服务器私有文件与 .ghs.yml。写保护此前
// 只覆盖写通道，GET 从未受限——chroot 为 "/" 的任意账号（含只读）都能下载
// gohttpserver.db（离线爆破哈希）、frp-encryption.key（解 FRP token）与
// .ghs.yml（拿主服务器的 upload/edit/delete token）。与开关无关地拦截。
// 调用方把错误包装成 os.ErrNotExist 形式，避免向客户端确认文件存在。
func (q *quotaFileSystem) checkReadAllowed(name string) error {
	base := filepath.Base(filepath.FromSlash(name))
	if isAccessConfNameWindows(base) || isServerStateNameWebdav(base) {
		return errProtectedRead
	}
	return nil
}

// errProtectedRead 表示目标属于服务器私有文件，禁止经 WebDAV 读取。
// 以 ErrNotExist 语义呈现，使 gdav 返回 404 而非 403，不确认文件存在。
var errProtectedRead = errors.New("webdav: protected system file")

// readDenied 返回伪装成「路径不存在」的错误，隐藏受保护文件的存在性。
func readDenied(op, name string) error {
	return &os.PathError{Op: op, Path: name, Err: fs.ErrNotExist}
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

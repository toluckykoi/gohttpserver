// Package update 实现基于 update_core 分发服务的应用自我更新：
// 检查（/check）→ 下载（Range 断点续传 + size/SHA-256 双重校验）→ 替换自身二进制。
// 协议约定见 update_core 项目 docs/integration.md；下载与校验逻辑参考其
// examples/go/main.go 参考实现。
//
// 安全约定：拒绝跟随重定向（防止 Bearer 令牌外泄）、更新服务强制 HTTPS
// （仅本机地址放行 HTTP 供联调）、download_url 必须与服务同源、令牌只从
// 配置/环境变量读取且不写入日志。
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 状态机的全部取值，前端据此切换界面。
const (
	StateDisabled    = "disabled" // 构建无有效版本号（dev 构建）等原因导致整体未启用
	StateIdle        = "idle"     // 已检查，无更新可用
	StateChecking    = "checking"
	StateAvailable   = "available" // 有更新待下载
	StateDownloading = "downloading"
	StateReady       = "ready" // 已下载并校验通过，等待安装
	StateApplied     = "applied" // 已替换磁盘上的二进制，等待重启
	StateError       = "error"
)

// Options 是更新服务的全部配置。
type Options struct {
	// BaseURL 是更新服务根地址，如 https://updates.example.com。
	BaseURL string
	// App 是软件标识（slug）。
	App string
	// Channel 是发布渠道，空值按 stable 处理。
	Channel string
	// PackageType 是全平台统一的包类型；优先级低于 PackageTypes。
	PackageType string
	// PackageTypes 按系统覆盖包类型（键：windows/linux/macos），
	// 优先级最高；两者皆空时使用内置默认（windows=exe，其余=bin）。
	PackageTypes map[string]string
	// Token 是私有软件的 Bearer 令牌，可为空（公开软件）。
	Token string
	// CurrentVersion 是构建注入的版本号（可带 v 前缀）。
	CurrentVersion string
	// Interval 是后台复查间隔；<=0 表示仅启动时检查一次。
	Interval time.Duration
	// Restart 由调用方注入，触发优雅停机并退出进程（依赖进程守护拉起新版本）。
	Restart func()
	// Logger 为空时使用 slog.Default()。
	Logger *slog.Logger
}

// Status 是暴露给前端的状态快照（扁平结构，便于消费）。
type Status struct {
	Enabled        bool       `json:"enabled"`
	State          string     `json:"state"`
	CurrentVersion string     `json:"current_version"`
	Reason         string     `json:"reason,omitempty"`
	Required       bool       `json:"required,omitempty"`
	ReleaseVersion string     `json:"release_version,omitempty"`
	Notes          string     `json:"notes,omitempty"`
	PublishedAt    string     `json:"published_at,omitempty"`
	ArtifactName   string     `json:"artifact_name,omitempty"`
	ArtifactSize   int64      `json:"artifact_size,omitempty"`
	SHA256         string     `json:"sha256,omitempty"`
	Downloaded     int64      `json:"downloaded,omitempty"`
	LastCheck      *time.Time `json:"last_check,omitempty"`
	Error          string     `json:"error,omitempty"`
}

type releaseInfo struct {
	Version     string  `json:"version"`
	Notes       string  `json:"notes"`
	Required    bool    `json:"required"`
	PublishedAt *string `json:"published_at"`
}

type artifactInfo struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	URL      string `json:"download_url"`
}

type checkResponse struct {
	Available bool          `json:"update_available"`
	Required  bool          `json:"required"`
	Reason    string        `json:"reason"`
	Release   *releaseInfo  `json:"release"`
	Artifact  *artifactInfo `json:"artifact"`
}

// Service 串起完整的更新状态机。字段并发规则：可变状态由 mu 保护；
// downloaded 用原子量以支持下载过程中的进度读取；checkMu 串行化检查动作。
type Service struct {
	opts    Options
	base    *url.URL
	version string // 归一化后的当前版本
	enabled bool

	checkClient *http.Client // 检查请求：短超时
	dlClient    *http.Client // 下载请求：长超时，支持续传

	ctx    context.Context
	cancel context.CancelFunc

	// exeOverride 仅测试用：覆盖 os.Executable() 的结果，
	// 使下载/安装逻辑可以在临时目录里演练。
	exeOverride string

	checkMu sync.Mutex // 同一时刻只允许一个检查在跑（TryLock 幂等跳过）

	mu             sync.Mutex
	state          string
	reason         string
	required       bool
	releaseVersion string
	notes          string
	publishedAt    string
	artifactName   string
	artifactSize   int64
	artifactSHA    string
	downloadURL    string
	errMsg         string
	lastCheck      *time.Time
	downloaded     atomic.Int64
}

// NewService 校验配置并构造更新服务；失败时返回错误（调用方应放弃启用而非退出进程）。
func NewService(opts Options) (*Service, error) {
	if opts.BaseURL == "" {
		return nil, errors.New("update: 更新服务地址为空")
	}
	if opts.App == "" {
		return nil, errors.New("update: 软件标识（slug）为空")
	}
	if opts.Channel == "" {
		opts.Channel = "stable"
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	base, err := validateBaseURL(opts.BaseURL)
	if err != nil {
		return nil, err
	}
	if base.Scheme == "http" {
		// 明文 HTTP 无法防中间人：SHA-256 摘要与安装包走同一信道，可被一并
		// 替换。此处只警告不拦截，由部署者自行权衡（常见于自有内网更新源）。
		opts.Logger.Warn("self-update: HTTP update source is unencrypted; "+
			"a man-in-the-middle could swap both the package and its SHA-256, "+
			"prefer HTTPS when possible",
			slog.String("base", opts.BaseURL))
	}
	version := normalizeVersion(opts.CurrentVersion)
	enabled := versionSupported(version)
	if !enabled {
		opts.Logger.Info("self-update disabled: no usable version info in this build",
			slog.String("version", opts.CurrentVersion))
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		opts:    opts,
		base:    base,
		version: version,
		enabled: enabled,
		checkClient: &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: rejectRedirect,
		},
		dlClient: &http.Client{
			// 总超时兜底防失控；Range 续传会发起多个请求，各自独立计时。
			Timeout:       time.Hour,
			CheckRedirect: rejectRedirect,
		},
		ctx:    ctx,
		cancel: cancel,
		state:  StateIdle,
	}, nil
}

func rejectRedirect(*http.Request, []*http.Request) error {
	return errors.New("update: 拒绝跟随重定向")
}

// validateBaseURL 校验地址格式干净（无用户信息/查询串/片段/多余路径），
// scheme 支持 http 与 https 两种；明文 HTTP 由调用方负责给出风险警告。
func validateBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return nil, errors.New("update: 无效的更新服务地址")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("update: 仅支持 http 或 https 地址")
	}
	return u, nil
}

// normalizeVersion 去掉构建注入版本号的 v/V 前缀，得到可上报的 SemVer。
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 0 && (v[0] == 'v' || v[0] == 'V') {
		v = v[1:]
	}
	return v
}

// versionSupported 判断该构建是否携带可比较的版本号；dev/unknown（未注入
// ldflags 的构建）无法与服务端 SemVer 比较，整体禁用更新功能。
func versionSupported(v string) bool {
	return v != "" && v != "unknown" && v != "dev"
}

// platformOS 把 Go 的 GOOS 映射为 update_core 的 os 取值。
func platformOS() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return runtime.GOOS
}

// packageType 按优先级解析当前平台应上报的包类型：
// PackageTypes[os] > PackageType > 内置默认（windows=exe，其余=bin）。
// 上报值必须与 update_core 后台登记的完全一致，否则拿不到任何包。
func (s *Service) packageType() string {
	if v := s.opts.PackageTypes[platformOS()]; v != "" {
		return v
	}
	if s.opts.PackageType != "" {
		return s.opts.PackageType
	}
	if platformOS() == "windows" {
		return "exe"
	}
	return "bin"
}

// Run 启动后台检查循环：启动 2 秒后首查（错开启动高峰），之后按 Interval
// 周期复查。阻塞直到 Close()；应在独立 goroutine 中调用。
func (s *Service) Run() {
	if !s.enabled {
		return
	}
	select {
	case <-time.After(2 * time.Second):
		s.Check()
	case <-s.ctx.Done():
		return
	}
	if s.opts.Interval <= 0 {
		return
	}
	ticker := time.NewTicker(s.opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.Check()
		}
	}
}

// Close 停止后台检查循环并取消进行中的下载。
func (s *Service) Close() { s.cancel() }

// Check 执行一次更新检查（绕过缓存），结果写入状态机。
// 已有检查在跑或正在下载时直接返回（幂等，不并发打扰）。
func (s *Service) Check() {
	if !s.enabled {
		return
	}
	if !s.checkMu.TryLock() {
		return
	}
	defer s.checkMu.Unlock()

	s.mu.Lock()
	if s.state == StateDownloading {
		s.mu.Unlock()
		return
	}
	// ready/applied 承载"已下载待安装"的成果，检查期间保持可见，
	// 完成后再按结果决定是否覆盖。
	switch s.state {
	case StateReady, StateApplied:
	default:
		s.state = StateChecking
	}
	s.mu.Unlock()

	resp, err := s.doCheck()

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.lastCheck = &now
	if err != nil {
		switch s.state {
		case StateReady, StateApplied:
			// 网络抖动不应冲掉已下载待安装的更新。
			s.opts.Logger.Warn("update: check failed, keeping downloaded update",
				slog.String("error", err.Error()))
		default:
			s.state = StateError
			s.errMsg = err.Error()
		}
		return
	}
	s.reason = resp.Reason
	if !resp.Available {
		switch s.state {
		case StateReady, StateApplied, StateDownloading:
			// 服务端已看不到该版本（如被撤回），但本地已有成品，仍允许安装。
		default:
			s.state = StateIdle
			s.clearUpdate()
		}
		return
	}
	// 有更新：仅当"检查到的是已下载的同一版本"时保留 ready/applied 状态。
	if (s.state == StateReady || s.state == StateApplied) && s.releaseVersion == resp.Release.Version {
		// keep
	} else {
		s.state = StateAvailable
		s.downloaded.Store(0)
	}
	s.required = resp.Required
	s.releaseVersion = resp.Release.Version
	s.notes = resp.Release.Notes
	if resp.Release.PublishedAt != nil {
		s.publishedAt = *resp.Release.PublishedAt
	} else {
		s.publishedAt = ""
	}
	s.artifactName = resp.Artifact.Filename
	s.artifactSize = resp.Artifact.Size
	s.artifactSHA = strings.ToLower(resp.Artifact.SHA256)
	s.downloadURL = resp.Artifact.URL
}

// clearUpdate 无更新时清空与具体版本绑定的信息，避免陈旧数据残留界面。
func (s *Service) clearUpdate() {
	s.required = false
	s.releaseVersion = ""
	s.notes = ""
	s.publishedAt = ""
	s.artifactName = ""
	s.artifactSize = 0
	s.artifactSHA = ""
	s.downloadURL = ""
}

// doCheck 发起 /check 请求并解析、校验响应。
func (s *Service) doCheck() (*checkResponse, error) {
	q := url.Values{
		"version":      {s.version},
		"channel":      {s.opts.Channel},
		"os":           {platformOS()},
		"arch":         {runtime.GOARCH},
		"package_type": {s.packageType()},
	}
	endpoint := strings.TrimRight(s.base.String(), "/") +
		"/api/v1/apps/" + url.PathEscape(s.opts.App) + "/check?" + q.Encode()

	resp, err := s.request(s.checkClient, endpoint)
	if err != nil {
		return nil, fmt.Errorf("无法连接更新服务: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("检查更新失败 HTTP %d: %s", resp.StatusCode, readErrorMessage(resp.Body))
	}
	var data checkResponse
	// 更新说明可能很长，仍远小于 1MB；限长防服务端异常响应撑爆内存。
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&data); err != nil {
		return nil, fmt.Errorf("更新服务响应解析失败: %w", err)
	}
	if data.Available {
		if data.Release == nil || data.Artifact == nil {
			return nil, errors.New("更新服务返回了不完整的更新信息")
		}
		if err := validateArtifact(s.base, data.Artifact); err != nil {
			return nil, err
		}
	}
	return &data, nil
}

// request 构造带 Bearer 令牌（如已配置）的 GET 请求。令牌不进 URL、不进日志。
func (s *Service) request(c *http.Client, address string) (*http.Response, error) {
	req, err := http.NewRequest("GET", address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gohttpserver-selfupdate")
	if s.opts.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.opts.Token)
	}
	return c.Do(req)
}

// validateArtifact 与参考实现同规则：下载地址必须同源（防止令牌被引到第三方）、
// SHA-256 必须是 32 字节 hex、大小必须为正。
func validateArtifact(base *url.URL, a *artifactInfo) error {
	d, err := url.Parse(a.URL)
	if err != nil || d.Host == "" {
		return errors.New("无效的下载地址")
	}
	if d.Scheme != base.Scheme || d.Host != base.Host || d.User != nil {
		return errors.New("下载地址与更新服务不同源，已拒绝")
	}
	digest, err := hex.DecodeString(a.SHA256)
	if err != nil || len(digest) != 32 {
		return errors.New("无效的软件包元数据（SHA-256）")
	}
	if a.Size <= 0 {
		return errors.New("无效的软件包元数据（大小）")
	}
	return nil
}

// readErrorMessage 解析 update_core 统一错误结构 {code,message,request_id}，
// 解析不出时回退为原始文本。
func readErrorMessage(r io.Reader) string {
	body, _ := io.ReadAll(io.LimitReader(r, 4096))
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &e); err == nil && e.Message != "" {
		return e.Message
	}
	if raw := strings.TrimSpace(string(body)); raw != "" {
		return raw
	}
	return "(无详情)"
}

// StartDownload 启动后台下载（幂等：已在下载或已就绪时直接返回 nil）。
func (s *Service) StartDownload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled {
		return errors.New("更新功能未启用")
	}
	switch s.state {
	case StateDownloading, StateReady:
		return nil
	case StateAvailable:
	default:
		return errors.New("当前没有可用更新")
	}
	if s.artifactSHA == "" || s.artifactSize <= 0 || s.downloadURL == "" {
		return errors.New("更新信息不完整，请重新检查更新")
	}
	s.state = StateDownloading
	s.downloaded.Store(0)
	version := s.releaseVersion
	go s.download(version)
	return nil
}

// download 按 update_core 参考实现执行带断点续传的下载：
// .part 临时文件名绑定 SHA-256（不同版本互不干扰），续传带 If-Range 校验，
// 3 次网络重试；终态以 size + SHA-256 双重校验为准。
func (s *Service) download(version string) {
	s.mu.Lock()
	size, hash, downloadURL := s.artifactSize, s.artifactSHA, s.downloadURL
	s.mu.Unlock()

	part, err := s.partPath(hash)
	if err != nil {
		s.fail("无法确定安装位置: " + err.Error())
		return
	}
	// 临时文件路径可被预置符号链接劫持（指向任意文件后被追加写入）。
	if st, err := os.Lstat(part); err == nil && st.Mode()&os.ModeSymlink != 0 {
		s.fail("更新临时文件不能是符号链接")
		return
	}

	var lastErr error
	done := false
	for attempt := 0; attempt < 3 && !done; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Second):
			case <-s.ctx.Done():
				s.fail("下载已取消")
				return
			}
		}
		if s.ctx.Err() != nil {
			s.fail("下载已取消")
			return
		}

		var offset int64
		if st, err := os.Stat(part); err == nil {
			offset = st.Size()
		}
		if offset > size {
			_ = os.Remove(part)
			s.fail("更新临时文件过大，已清除")
			return
		}
		if offset == size {
			done = true
			break
		}

		req, err := http.NewRequestWithContext(s.ctx, "GET", downloadURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "gohttpserver-selfupdate")
		if s.opts.Token != "" {
			req.Header.Set("Authorization", "Bearer "+s.opts.Token)
		}
		if offset > 0 {
			// ETag 即 SHA-256：If-Range 确保"续传的还是同一个文件"。
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
			req.Header.Set("If-Range", `"`+hash+`"`)
		}
		resp, err := s.dlClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		var mode int
		var base int64
		switch resp.StatusCode {
		case http.StatusPartialContent:
			want := fmt.Sprintf("bytes %d-%d/%d", offset, size-1, size)
			if offset <= 0 || resp.Header.Get("Content-Range") != want {
				// Content-Range 与本地偏移对不上：文件可能已变化，丢弃重来。
				resp.Body.Close()
				_ = os.Remove(part)
				s.fail("Range 响应无效，已重置下载")
				return
			}
			mode = os.O_CREATE | os.O_WRONLY | os.O_APPEND
			base = offset
		case http.StatusOK:
			mode = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
			base = 0
		case http.StatusRequestedRangeNotSatisfiable:
			resp.Body.Close()
			_ = os.Remove(part)
			lastErr = errors.New("续传范围无效，已重置")
			continue
		case http.StatusNotFound:
			resp.Body.Close()
			s.fail("安装包不存在或已被撤回，请重新检查更新")
			return
		case http.StatusTooManyRequests, http.StatusInternalServerError,
			http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			resp.Body.Close()
			lastErr = fmt.Errorf("下载失败 HTTP %d", resp.StatusCode)
			continue
		default:
			resp.Body.Close()
			s.fail(fmt.Sprintf("下载失败 HTTP %d", resp.StatusCode))
			return
		}

		f, err := os.OpenFile(part, mode, 0o600)
		if err != nil {
			resp.Body.Close()
			s.fail("无法写入更新临时文件: " + err.Error())
			return
		}
		s.downloaded.Store(base)
		n, copyErr := io.Copy(f, io.TeeReader(
			io.LimitReader(resp.Body, size-base+1),
			&progressCounter{total: &s.downloaded}))
		closeErr := f.Close()
		resp.Body.Close()
		if closeErr != nil {
			s.fail("写入更新临时文件失败: " + closeErr.Error())
			return
		}
		s.downloaded.Store(base + n)
		if copyErr == nil && n+base == size {
			done = true
			break
		}
		if n+base > size {
			s.fail("下载长度超限")
			return
		}
		if copyErr != nil {
			lastErr = copyErr
		} else {
			lastErr = errors.New("下载不完整")
		}
	}

	if !done {
		if s.ctx.Err() != nil {
			s.fail("下载已取消")
			return
		}
		s.fail(errText(lastErr))
		return
	}

	// 终校验（必做）：任何一项不符都删除重下。
	if err := verifyFile(part, size, hash); err != nil {
		_ = os.Remove(part)
		s.fail(err.Error())
		return
	}
	_ = os.Chmod(part, 0o755)
	s.mu.Lock()
	s.state = StateReady
	s.downloaded.Store(size)
	s.mu.Unlock()
	s.opts.Logger.Info("update: new package downloaded and verified",
		slog.String("version", version), slog.String("path", part))
}

// progressCounter 把已读字节数累加进原子的 downloaded 计数（配合 TeeReader）。
type progressCounter struct {
	total *atomic.Int64
}

func (p *progressCounter) Write(b []byte) (int, error) {
	p.total.Add(int64(len(b)))
	return len(b), nil
}

func errText(err error) string {
	if err == nil {
		return "下载失败"
	}
	return err.Error()
}

// fail 把状态机置为错误态并记录日志。
func (s *Service) fail(msg string) {
	s.mu.Lock()
	s.state = StateError
	s.errMsg = msg
	s.mu.Unlock()
	s.opts.Logger.Warn("update: " + msg)
}

// partPath 返回下载临时文件路径：<exe目录>/<exe名>.<sha256>.part。
// 与最终可执行文件同目录同卷，保证 apply 时 rename 原子生效。
func (s *Service) partPath(sha string) (string, error) {
	exe, err := s.currentExe()
	if err != nil {
		return "", err
	}
	dir, name := filepath.Split(exe)
	if dir == "" {
		return "", errors.New("无法确定程序所在目录")
	}
	return filepath.Join(dir, name+"."+sha+".part"), nil
}

// currentExe 返回当前可执行文件路径（测试可注入）。
func (s *Service) currentExe() (string, error) {
	if s.exeOverride != "" {
		return filepath.Abs(s.exeOverride)
	}
	return currentExecutable()
}

// currentExecutable 返回解析符号链接后的当前可执行文件绝对路径，
// 确保替换的是真实二进制而非入口符号链接。
func currentExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// verifyFile 校验文件字节数与 SHA-256 摘要（小写 hex）均与元数据一致。
func verifyFile(path string, size int64, wantSHA string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("无法读取下载文件: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return fmt.Errorf("读取下载文件失败: %w", err)
	}
	if n != size {
		return fmt.Errorf("文件大小不符: 期望 %d 字节，实际 %d 字节", size, n)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSHA {
		return errors.New("安装包校验失败（SHA-256 不符），请重新下载")
	}
	return nil
}

// Apply 把已校验的新版本二进制替换到当前可执行文件的位置：
// 先重验完整性，再 备份(.bak) → 替换 → 失败回滚。替换在程序运行中即可完成
// （Windows 也允许重命名运行中的 exe），新版本在下次启动时生效。
func (s *Service) Apply() error {
	exe, err := s.currentExe()
	if err != nil {
		return fmt.Errorf("无法定位当前程序: %w", err)
	}
	return s.applyTo(exe)
}

// applyTo 执行到指定可执行文件路径的替换，供 Apply 与单测共用。
func (s *Service) applyTo(exe string) error {
	s.mu.Lock()
	if s.state != StateReady {
		st := s.state
		s.mu.Unlock()
		return fmt.Errorf("当前状态 %s 不允许安装更新", st)
	}
	version, hash, size := s.releaseVersion, s.artifactSHA, s.artifactSize
	s.mu.Unlock()

	part, err := s.partPath(hash)
	if err != nil {
		return err
	}

	// 安装前重验：防止下载完成到安装之间文件被替换或损坏。
	if err := verifyFile(part, size, hash); err != nil {
		_ = os.Remove(part)
		s.fail(err.Error())
		return err
	}

	bak := exe + ".bak"
	_ = os.Remove(bak) // 清掉上次遗留的备份（Windows 不允许 rename 覆盖已有文件）
	if err := os.Rename(exe, bak); err != nil {
		return fmt.Errorf("备份当前程序失败（目录可能没有写权限）: %w", err)
	}
	if err := os.Rename(part, exe); err != nil {
		if rbErr := os.Rename(bak, exe); rbErr != nil {
			return fmt.Errorf("安装新版本失败且回滚失败: %v（原程序备份保留在 %s）", err, bak)
		}
		return fmt.Errorf("安装新版本失败，已回滚: %w", err)
	}

	s.mu.Lock()
	s.state = StateApplied
	s.mu.Unlock()
	s.opts.Logger.Info("update: new binary installed, restart required",
		slog.String("version", version), slog.String("backup", bak))
	return nil
}

// RequestRestart 触发调用方注入的重启回调；未注入时返回错误。
func (s *Service) RequestRestart() error {
	if s.opts.Restart == nil {
		return errors.New("重启能力不可用")
	}
	s.opts.Restart()
	return nil
}

// Status 返回当前状态快照。
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{
		Enabled:        s.enabled,
		State:          s.state,
		CurrentVersion: s.version,
		Reason:         s.reason,
		Required:       s.required,
		ReleaseVersion: s.releaseVersion,
		Notes:          s.notes,
		PublishedAt:    s.publishedAt,
		ArtifactName:   s.artifactName,
		ArtifactSize:   s.artifactSize,
		SHA256:         s.artifactSHA,
		Downloaded:     s.downloaded.Load(),
		LastCheck:      s.lastCheck,
		Error:          s.errMsg,
	}
	if !s.enabled {
		st.State = StateDisabled
	}
	return st
}

// Package update 实现基于 update_core 分发服务的应用自我更新：
// 检查（/check）→ 下载（Range 断点续传 + size/SHA-256 双重校验）→ 替换自身二进制。
// 协议约定见 update_core 项目 docs/integration.md。
// 安全约定：拒绝跟随重定向（防令牌外泄）、更新源同时支持 http/https（明文 HTTP
// 告警不拦截，由部署者权衡）、download_url 必须同源、客户端做版本单调性检查防降级
// （M1）、令牌不写入日志。
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
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/mod/semver"
)

// 状态机的全部取值，前端据此切换界面。
const (
	StateDisabled    = "disabled" // 构建无有效版本号（dev 构建）等原因导致整体未启用
	StateIdle        = "idle"     // 已检查，无更新可用
	StateChecking    = "checking"
	StateAvailable   = "available" // 有更新待下载
	StateDownloading = "downloading"
	StateReady       = "ready"   // 已下载并校验通过，等待安装
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
	// PackageType 全平台统一包类型，优先级低于 PackageTypes。
	PackageType string
	// PackageTypes 按系统覆盖包类型（键：windows/linux/macos），
	// 优先级最高；两者皆空时使用内置默认（windows=exe，其余=bin）。
	PackageTypes map[string]string
	// Token 可为空（公开软件）。
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

// Status 是暴露给前端的更新状态快照。
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

// Service 串起完整的更新状态机。
// 并发规则：可变状态由 mu 保护，downloaded 用原子量供进度读取，checkMu 串行化检查。
type Service struct {
	opts    Options
	base    *url.URL
	version string // 归一化后的当前版本
	enabled bool

	checkClient *http.Client // 检查请求：短超时
	dlClient    *http.Client // 下载请求：长超时，支持续传

	ctx    context.Context
	cancel context.CancelFunc

	// exeOverride 仅测试用：覆盖 os.Executable() 结果以便在临时目录演练。
	exeOverride string

	checkMu sync.Mutex // 同一时刻只允许一个检查在跑（TryLock 幂等跳过）

	installMu sync.Mutex // 串行化安装：并发 Apply 会互相破坏 .bak 备份与重命名序列

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
		return nil, errors.New("update: update server URL is empty")
	}
	if opts.App == "" {
		return nil, errors.New("update: app slug is empty")
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
		// 明文 HTTP 下包与 SHA-256 可被中间人一并替换；http/https 均为合法配置，
		// 这里只告警不拦截，由部署者权衡（内网/自建源常见明文）。
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
	return errors.New("update: redirects are not allowed")
}

// validateBaseURL 校验地址格式干净（无用户信息/查询串/片段/多余路径），scheme 限 http/https。
func validateBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return nil, errors.New("update: invalid update server URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("update: only http or https URLs are supported")
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

// versionSupported 判断构建是否带可比较的版本号；dev/unknown（未注入 ldflags）禁用更新。
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

// packageType 按优先级解析上报的包类型：PackageTypes[os] > PackageType > 内置默认（windows=exe，其余=bin）。
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

// Run 启动后台检查循环：启动 2 秒后首查，之后按 Interval 周期复查；阻塞直到 Close()，应在独立 goroutine 中调用。
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

// Check 执行一次更新检查（绕过缓存），结果写入状态机；已有检查或下载在跑时幂等返回。
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
	// ready/applied 承载"已下载待安装"的成果，检查期间保持可见。
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
	// SECURITY(M1): 客户端侧版本单调性检查。是否有更新由服务端决定，但把实例"更新"到
	// 含已知漏洞的旧版本是更新通道被操控（明文 HTTP/服务器失陷）后的经典降级攻击，
	// 必须拒绝：仅当服务端版本可解析且**不小于**当前版本才接受（服务端版本严格小于
	// 当前版本或不可解析时拒绝）。等版本（==）放行：update_core 的 Required 热修复
	// 会重发同版本构件，拒绝等版本等于永久关闭热修复通道；等版本不可能引入降级。
	// 当前版本不是合法 SemVer（无法比较）时跳过检查，维持原行为。
	if resp.Available && semver.IsValid("v"+s.version) {
		server := "v" + normalizeVersion(resp.Release.Version)
		if !semver.IsValid(server) || semver.Compare(server, "v"+s.version) < 0 {
			s.opts.Logger.Warn("update: server offered a non-newer or unparseable version; ignoring (possible downgrade)",
				slog.String("server", resp.Release.Version), slog.String("current", s.version))
			resp.Available = false
			resp.Reason = ""
		}
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
	s.downloadURL = normalizeDownloadURL(s.base, resp.Artifact.URL)
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
		return nil, fmt.Errorf("cannot reach update server: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("check for updates failed HTTP %d: %s", resp.StatusCode, readErrorMessage(resp.Body))
	}
	var data checkResponse
	// 更新说明可能很长，仍远小于 1MB；限长防服务端异常响应撑爆内存。
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to parse update server response: %w", err)
	}
	if data.Available {
		if data.Release == nil || data.Artifact == nil {
			return nil, errors.New("update server returned incomplete update info")
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
// SHA-256 为 32 字节 hex、大小为正；服务端开发环境可能把 download_url 生成为
// 回环地址（容器/端口映射场景），此时放行，由 normalizeDownloadURL 改写为公共地址。
func validateArtifact(base *url.URL, a *artifactInfo) error {
	d, err := url.Parse(a.URL)
	if err != nil || d.Host == "" {
		return errors.New("invalid download URL")
	}
	if d.User != nil || d.Scheme != base.Scheme ||
		(!equalsHost(d.Host, base.Host) && !isLoopbackHost(d.Host)) {
		return errors.New("download URL is not from the same origin as the update server, rejected")
	}
	digest, err := hex.DecodeString(a.SHA256)
	if err != nil || len(digest) != 32 {
		return errors.New("invalid package metadata (SHA-256)")
	}
	if a.Size <= 0 {
		return errors.New("invalid package metadata (size)")
	}
	return nil
}

// normalizeDownloadURL 把服务端返回的回环地址（127.0.0.1/localhost/[::1]）改写为
// 公共 BaseURL 地址（客户端直连回环必然失败）；其余原样返回。
// 改写的目标地址：主机名取 base 的主机名；端口**优先取下载地址自带的端口**
// （update_core 部署在容器/端口映射场景时，download_url 中的端口才是客户端应
// 访问的端口），下载地址无端口时退回 base 的端口。路径与查询原样保留。
func normalizeDownloadURL(base *url.URL, raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	if !isLoopbackHost(u.Host) {
		return raw
	}
	out := *base
	if _, port, err := net.SplitHostPort(u.Host); err == nil {
		out.Host = net.JoinHostPort(base.Hostname(), port)
	} else {
		out.Host = base.Host
	}
	out.Path = u.Path
	out.RawQuery = u.RawQuery
	out.Fragment = ""
	return out.String()
}

// equalsHost 大小写不敏感地比较 host。
func equalsHost(a, b string) bool {
	return strings.EqualFold(a, b)
}

// isLoopbackHost 判断 URL host（可含端口/brackets）是否为回环地址。
func isLoopbackHost(host string) bool {
	u, err := url.Parse("//" + host)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if h == "localhost" || h == "127.0.0.1" || h == "::1" {
		return true
	}
	// 127.0.0.0/8 段（含 127.0.0.2 等）。
	return strings.HasPrefix(h, "127.")
}

// readErrorMessage 解析 update_core 统一错误结构 {code,message,request_id}，失败时回退原文。
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
	return "(no details)"
}

// StartDownload 启动后台下载（幂等：已在下载或已就绪时直接返回 nil）。
func (s *Service) StartDownload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled {
		return errors.New("update feature is not enabled")
	}
	switch s.state {
	case StateDownloading, StateReady:
		return nil
	case StateAvailable:
	default:
		return errors.New("no update available")
	}
	if s.artifactSHA == "" || s.artifactSize <= 0 || s.downloadURL == "" {
		return errors.New("update info is incomplete, please check for updates again")
	}
	s.state = StateDownloading
	s.downloaded.Store(0)
	version := s.releaseVersion
	go s.download(version)
	return nil
}

// download 按 update_core 参考实现执行断点续传下载：.part 文件名绑定 SHA-256，
// 续传带 If-Range 校验，3 次重试；终态以 size + SHA-256 双重校验为准。
func (s *Service) download(version string) {
	s.mu.Lock()
	size, hash, downloadURL := s.artifactSize, s.artifactSHA, s.downloadURL
	s.mu.Unlock()

	part, err := s.partPath(hash)
	if err != nil {
		s.fail("cannot determine install location: " + err.Error())
		return
	}

	var lastErr error
	done := false
	for attempt := 0; attempt < 3 && !done; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Second):
			case <-s.ctx.Done():
				s.fail("download cancelled")
				return
			}
		}
		if s.ctx.Err() != nil {
			s.fail("download cancelled")
			return
		}

		var offset int64
		if st, err := os.Stat(part); err == nil {
			offset = st.Size()
		}
		if offset > size {
			_ = os.Remove(part)
			s.fail("update temp file too large, cleared")
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
				s.fail("invalid Range response, download reset")
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
			lastErr = errors.New("invalid resume range, reset")
			continue
		case http.StatusNotFound:
			resp.Body.Close()
			s.fail("package no longer exists or was retracted, please check for updates again")
			return
		case http.StatusTooManyRequests, http.StatusInternalServerError,
			http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			resp.Body.Close()
			lastErr = fmt.Errorf("download failed HTTP %d", resp.StatusCode)
			continue
		default:
			resp.Body.Close()
			s.fail(fmt.Sprintf("download failed HTTP %d", resp.StatusCode))
			return
		}

		// 临时文件路径可被预置符号链接劫持（指向任意文件后被追加写入）。
		// 必须在每次 OpenFile 前重查：与之前任何检查之间都存在 TOCTOU 窗口。
		if st, err := os.Lstat(part); err == nil && st.Mode()&os.ModeSymlink != 0 {
			resp.Body.Close()
			s.fail("update temp file cannot be a symlink")
			return
		}
		f, err := os.OpenFile(part, mode, 0o600)
		if err != nil {
			resp.Body.Close()
			s.fail("cannot write update temp file: " + err.Error())
			return
		}
		s.downloaded.Store(base)
		n, copyErr := io.Copy(f, io.TeeReader(
			io.LimitReader(resp.Body, size-base+1),
			&progressCounter{total: &s.downloaded}))
		closeErr := f.Close()
		resp.Body.Close()
		if closeErr != nil {
			s.fail("failed to write update temp file: " + closeErr.Error())
			return
		}
		s.downloaded.Store(base + n)
		if copyErr == nil && n+base == size {
			done = true
			break
		}
		if n+base > size {
			s.fail("download length exceeded")
			return
		}
		if copyErr != nil {
			lastErr = copyErr
		} else {
			lastErr = errors.New("download incomplete")
		}
	}

	if !done {
		if s.ctx.Err() != nil {
			s.fail("download cancelled")
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
		return "download failed"
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

// partPath 返回下载临时文件路径 <exe目录>/<exe名>.<sha256>.part，与最终文件同目录同卷，保证 rename 原子生效。
func (s *Service) partPath(sha string) (string, error) {
	exe, err := s.currentExe()
	if err != nil {
		return "", err
	}
	dir, name := filepath.Split(exe)
	if dir == "" {
		return "", errors.New("cannot determine executable directory")
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

// currentExecutable 返回解析符号链接后的可执行文件绝对路径，确保替换真实二进制。
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
		return fmt.Errorf("cannot read downloaded file: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return fmt.Errorf("failed to read downloaded file: %w", err)
	}
	if n != size {
		return fmt.Errorf("file size mismatch: expected %d bytes, got %d", size, n)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSHA {
		return errors.New("package checksum mismatch (SHA-256), please download again")
	}
	return nil
}

// Apply 把已校验的新版本二进制替换到当前可执行文件位置：重验 → 备份(.bak) → 替换 → 失败回滚。
// 替换在运行中即可完成（Windows 允许重命名运行中的 exe），下次启动生效。
func (s *Service) Apply() error {
	exe, err := s.currentExe()
	if err != nil {
		return fmt.Errorf("cannot locate current executable: %w", err)
	}
	return s.applyTo(exe)
}

// applyTo 执行到指定可执行文件路径的替换，供 Apply 与单测共用。
func (s *Service) applyTo(exe string) error {
	// 串行化整个"校验→备份→替换→回滚"序列：状态检查与文件操作非原子，
	// 并发 Apply 会在第二次调用中误删仅有的 .bak 备份并让重命名互相踩踏。
	s.installMu.Lock()
	defer s.installMu.Unlock()

	s.mu.Lock()
	if s.state != StateReady {
		st := s.state
		s.mu.Unlock()
		return fmt.Errorf("cannot install update in state %s", st)
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
		return fmt.Errorf("failed to back up current binary (directory may not be writable): %w", err)
	}
	if err := os.Rename(part, exe); err != nil {
		if rbErr := os.Rename(bak, exe); rbErr != nil {
			return fmt.Errorf("install failed and rollback failed: %v (backup kept at %s)", err, bak)
		}
		return fmt.Errorf("install failed, rolled back: %w", err)
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
		return errors.New("restart is not available")
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

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/alecthomas/kingpin"
	"github.com/go-yaml/yaml"
	"github.com/goji/httpauth"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	proxyproto "github.com/pires/go-proxyproto"

	"github.com/codeskyblue/gohttpserver/internal/admin"
	"github.com/codeskyblue/gohttpserver/internal/auth"
	"github.com/codeskyblue/gohttpserver/internal/frp"
	"github.com/codeskyblue/gohttpserver/internal/server"
	"github.com/codeskyblue/gohttpserver/internal/store"
	"github.com/codeskyblue/gohttpserver/internal/update"
	"github.com/codeskyblue/gohttpserver/internal/util"
	"github.com/codeskyblue/gohttpserver/internal/webdav"
	"github.com/codeskyblue/gohttpserver/internal/zipx"
	"github.com/gorilla/securecookie"
)

type Configure struct {
	Conf            *os.File `yaml:"-"`
	Addr            string   `yaml:"addr"`
	Port            int      `yaml:"port"`
	Root            string   `yaml:"root"`
	Prefix          string   `yaml:"prefix"`
	HTTPAuth        string   `yaml:"httpauth"`
	Cert            string   `yaml:"cert"`
	Key             string   `yaml:"key"`
	Theme           string   `yaml:"theme"`
	XHeaders        bool     `yaml:"xheaders"`
	Upload          bool     `yaml:"upload"`
	Delete          bool     `yaml:"delete"`
	Edit            bool     `yaml:"edit"`
	PlistProxy      string   `yaml:"plistproxy"`
	Title           string   `yaml:"title"`
	Debug           bool     `yaml:"debug"`
	GoogleTrackerID string   `yaml:"google-tracker-id"`
	Auth            struct {
		Type   string   `yaml:"type"`
		OpenID string   `yaml:"openid"`
		HTTP   []string `yaml:"http"`
		ID     string   `yaml:"id"`     // for oauth2
		Secret string   `yaml:"secret"` // for oauth2
	} `yaml:"auth"`
	DeepPathMaxDepth  int           `yaml:"deep-path-max-depth"`
	NoIndex           bool          `yaml:"no-index"`
	CORSOrigins       []string      `yaml:"cors-origins"`
	AllowPublicOauth2 bool          `yaml:"allow-public-oauth2"`
	Login             bool          `yaml:"login"`
	DB                string        `yaml:"db"`
	SessionTTL        time.Duration `yaml:"session-ttl"`
	FRP               struct {
		Enabled    bool   `yaml:"enabled"`
		ServerAddr string `yaml:"serverAddr"`
		ServerPort int    `yaml:"serverPort"`
		AuthToken  string `yaml:"authToken"`
		ProxyName  string `yaml:"proxyName"`
		ProxyType  string `yaml:"proxyType"`
		LocalIP    string `yaml:"localIP"`
		LocalPort  int    `yaml:"localPort"`
		RemotePort int    `yaml:"remotePort"`
	} `yaml:"frp"`
	LogLevel  string `yaml:"log-level"`  // debug, info, warn, error
	LogFormat string `yaml:"log-format"` // json, text
}

var (
	defaultPlistProxy = "https://plistproxy.herokuapp.com/plist"
	defaultOpenID     = "https://login.netease.com/openid"
	gcfg              = Configure{}

	VERSION   = "unknown"
	BUILDTIME = "unknown time"
	GITCOMMIT = "unknown git commit"
	SITE      = "https://github.com/codeskyblue/gohttpserver"
)

// updateServerConfig 是内置的自我更新配置：更新服务器信息直接写入代码，
// 不再通过配置文件或命令行参数设置。私有软件的 Bearer 令牌仍可通过
// GHS_UPDATE_TOKEN 环境变量注入。BaseURL 置空可整体禁用更新功能。
var updateServerConfig = update.Options{
	BaseURL: "http://update.toluckykoi.com:50003/",
	App:     "gohttpserver",
	Channel: "stable",
	// package_type 上报值必须与 update_core 后台登记的完全一致，否则拿不到任何包。
	PackageTypes: map[string]string{
		"windows": "exe",
		"linux":   "bin",
		"macos":   "bin",
	},
	Interval: 12 * time.Hour, // 后台复查间隔；0 = 仅启动时检查
}

func versionMessage() string {
	t := template.Must(template.New("version").Parse(`GoHTTPServer
  Version:        {{.Version}}
  Go version:     {{.GoVersion}}
  OS/Arch:        {{.OSArch}}
  Git commit:     {{.GitCommit}}
  Built:          {{.Built}}
  Site:           {{.Site}}`))
	buf := bytes.NewBuffer(nil)
	t.Execute(buf, map[string]any{
		"Version":   VERSION,
		"GoVersion": runtime.Version(),
		"OSArch":    runtime.GOOS + "/" + runtime.GOARCH,
		"GitCommit": GITCOMMIT,
		"Built":     BUILDTIME,
		"Site":      SITE,
	})
	return buf.String()
}

func parseFlags() error {
	gcfg.Root = "./"
	gcfg.Port = 8000
	gcfg.Addr = ""
	gcfg.Theme = "black"
	gcfg.PlistProxy = defaultPlistProxy
	gcfg.Auth.OpenID = defaultOpenID
	gcfg.GoogleTrackerID = ""
	gcfg.Title = "Go HTTP File Server"
	gcfg.DeepPathMaxDepth = 5
	gcfg.NoIndex = false

	kingpin.HelpFlag.Short('h')
	kingpin.Version(versionMessage())
	kingpin.Flag("conf", "config file path, yaml format").FileVar(&gcfg.Conf)
	kingpin.Flag("root", "root directory, default ./").Short('r').StringVar(&gcfg.Root)
	kingpin.Flag("prefix", "url prefix, eg /foo").StringVar(&gcfg.Prefix)
	kingpin.Flag("port", "listen port, default 8000").IntVar(&gcfg.Port)
	kingpin.Flag("addr", "listen address, eg 127.0.0.1:8000").Short('a').StringVar(&gcfg.Addr)
	kingpin.Flag("cert", "tls cert.pem path").StringVar(&gcfg.Cert)
	kingpin.Flag("key", "tls key.pem path").StringVar(&gcfg.Key)
	kingpin.Flag("auth-type", "Auth type <http|openid>").StringVar(&gcfg.Auth.Type)
	kingpin.Flag("auth-http", "HTTP basic auth (ex: user:pass)").StringsVar(&gcfg.Auth.HTTP)
	kingpin.Flag("auth-openid", "OpenID auth identity url").StringVar(&gcfg.Auth.OpenID)
	kingpin.Flag("theme", "web theme, one of <black|green>").StringVar(&gcfg.Theme)
	kingpin.Flag("upload", "enable upload support").BoolVar(&gcfg.Upload)
	kingpin.Flag("delete", "enable delete support").BoolVar(&gcfg.Delete)
	kingpin.Flag("edit", "enable file edit support").BoolVar(&gcfg.Edit)
	kingpin.Flag("xheaders", "used when behide nginx").BoolVar(&gcfg.XHeaders)
	kingpin.Flag("debug", "enable debug mode").BoolVar(&gcfg.Debug)
	kingpin.Flag("plistproxy", "plist proxy when server is not https").Short('p').StringVar(&gcfg.PlistProxy)
	kingpin.Flag("title", "server title").StringVar(&gcfg.Title)
	kingpin.Flag("google-tracker-id", "set to empty to disable it").StringVar(&gcfg.GoogleTrackerID)
	kingpin.Flag("deep-path-max-depth", "set to -1 to not combine dirs").IntVar(&gcfg.DeepPathMaxDepth)
	kingpin.Flag("no-index", "disable indexing").BoolVar(&gcfg.NoIndex)
	kingpin.Flag("cors-origins", "allowed CORS origins (repeatable; empty = no CORS, '*' = allow all [insecure])").StringsVar(&gcfg.CORSOrigins)
	kingpin.Flag("allow-public-oauth2", "allow oauth2-proxy mode on a non-loopback bind (insecure: only use behind a proxy that strips X-Auth-Request-*)").BoolVar(&gcfg.AllowPublicOauth2)
	kingpin.Flag("login", "enable username/password login gate (independent of --auth-type). When enabled, upload/delete/edit are automatically enabled too. Default credentials: admin/admin.").BoolVar(&gcfg.Login)
	kingpin.Flag("db", "path to the SQLite database file (default: ./gohttpserver.db in the working directory). Only used when --login is enabled.").StringVar(&gcfg.DB)
	kingpin.Flag("session-ttl", "how long a logged-in session stays valid before requiring re-login (default 12h). Set to 0 for browser-session cookies (expire when the browser closes).").Default("12h").DurationVar(&gcfg.SessionTTL)
	kingpin.Flag("log-level", "log level (debug, info, warn, error)").Default("info").StringVar(&gcfg.LogLevel)
	kingpin.Flag("log-format", "log output format (json, text)").Default("json").StringVar(&gcfg.LogFormat)

	kingpin.Parse() // 先解析一次，取出 conf 文件路径

	if gcfg.Conf != nil {
		defer func() {
			kingpin.Parse() // 再解析一次，命令行优先级高于配置文件
		}()
		ymlData, err := os.ReadFile(gcfg.Conf.Name())
		if err != nil {
			return err
		}
		return yaml.Unmarshal(ymlData, &gcfg)
	}
	return nil
}

func fixPrefix(prefix string) string {
	prefix = regexp.MustCompile(`/*$`).ReplaceAllString(prefix, "")
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	if prefix == "/" {
		prefix = ""
	}
	return prefix
}

// cors 应用 --cors-origins 配置的策略：未设置=仅同源，'*'=允许所有（不安全），
// 否则只回显列出的 origin。OPTIONS 预检直接应答，不经过认证链。
func cors(allowedOrigins []string) func(http.Handler) http.Handler {
	allowAll := false
	allowSet := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
		}
		allowSet[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				switch {
				case allowAll:
					w.Header().Set("Access-Control-Allow-Origin", "*")
				case allowSet[origin]:
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
				}
			}
			if allowAll || allowSet[origin] {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				// 只允许应用实际用到的头，不使用 "*"
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Token, Authorization")
				w.Header().Set("Access-Control-Max-Age", "600")
			}
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func multiBasicAuth(auths []string) func(http.Handler) http.Handler {
	userPassMap := make(map[string]string)
	for _, auth := range auths {
		userpass := strings.SplitN(auth, ":", 2)
		if len(userpass) == 2 {
			userPassMap[userpass[0]] = userpass[1]
		}
	}
	return httpauth.BasicAuth(httpauth.AuthOptions{
		Realm: "Restricted",
		AuthFunc: func(user, pass string, request *http.Request) bool {
			password, ok := userPassMap[user]
			if !ok {
				return false
			}
			givenPass := sha256.Sum256([]byte(pass))
			requiredPass := sha256.Sum256([]byte(password))
			return subtle.ConstantTimeCompare(givenPass[:], requiredPass[:]) == 1
		},
	})
}

func main() {
	if err := parseFlags(); err != nil {
		util.InitLogger(&slog.HandlerOptions{Level: slog.LevelInfo})
		slog.Error("parse flags failed", util.ErrorAttr(err))
		os.Exit(1)
	}

	util.InitLoggerWithConfig(util.LoggerConfig{
		Level:  util.LogLevel(gcfg.LogLevel),
		Format: util.LogFormat(gcfg.LogFormat),
	})

	// --login 隐含上传/删除/编辑权限，且解析完 flag 才生效，因此
	// `--login --no-upload` 仍然是可写的：只读认证服务请改用其它 auth-type。
	if gcfg.Login {
		gcfg.Upload = true
		gcfg.Delete = true
		gcfg.Edit = true
	}
	if gcfg.Debug {
		debugCfg := gcfg
		debugCfg.Auth.HTTP = nil
		debugCfg.Auth.Secret = ""
		data, _ := yaml.Marshal(debugCfg)
		slog.Debug("config (secrets redacted)", slog.String("config", string(data)))
	}

	gcfg.Prefix = fixPrefix(gcfg.Prefix)
	if gcfg.Prefix != "" {
		slog.Info("url prefix configured", slog.String("prefix", gcfg.Prefix))
	}

	ss := server.NewHTTPStaticServer(gcfg.Root, gcfg.NoIndex)
	ss.Assets = FrontendAssets
	ss.Prefix = gcfg.Prefix
	ss.Theme = gcfg.Theme
	ss.Title = gcfg.Title
	ss.GoogleTrackerID = gcfg.GoogleTrackerID
	ss.Upload = gcfg.Upload
	ss.Delete = gcfg.Delete
	ss.Edit = gcfg.Edit
	ss.AuthType = gcfg.Auth.Type
	ss.DeepPathMaxDepth = gcfg.DeepPathMaxDepth

	if gcfg.PlistProxy != "" {
		u, err := url.Parse(gcfg.PlistProxy)
		if err != nil {
			slog.Error("invalid plistproxy url", util.ErrorAttr(err))
			os.Exit(1)
		}
		u.Scheme = "https"
		ss.PlistProxy = u.String()
	}
	if ss.PlistProxy != "" {
		slog.Info("plistproxy configured", slog.String("url", ss.PlistProxy))
	}

	var hdlr http.Handler = ss

	// CORS 由 --cors-origins 显式开启，未设置则仅同源
	hdlr = cors(gcfg.CORSOrigins)(hdlr)

	if gcfg.XHeaders {
		hdlr = handlers.ProxyHeaders(hdlr)
		// 仅在 --xheaders 时信任代理转发头，否则伪造 X-Forwarded-For 可绕过登录限流
		util.SetTrustProxyHeaders(true)
	}

	mainRouter := mux.NewRouter()

	mainRouter.Use(util.RequestIDMiddleware)

	// CSRF 纵深防御：拒绝带跨站 Origin 的 POST/PUT/DELETE/PATCH（浏览器跨站
	// 写请求必带 Origin，原生客户端不带）。挂在 mainRouter 上以覆盖登录、
	// 管理 API、WebDAV 与文件处理全部通道。
	mainRouter.Use(util.OriginCheckMiddleware)

	mainRouter.Use(util.LoggingMiddleware(util.Logger()))

	mainRouter.HandleFunc("/healthz", util.HealthCheckHandler(VERSION)).Methods("GET")
	mainRouter.HandleFunc("/-/healthz", util.HealthCheckHandler(VERSION)).Methods("GET")

	// 指标路径注册在 prefix 之前，抓取端不受 --prefix 影响
	mainRouter.HandleFunc("/metrics", util.MetricsHandler(VERSION)).Methods("GET", "HEAD")
	mainRouter.HandleFunc("/-/metrics", util.MetricsHandler(VERSION)).Methods("GET", "HEAD")

	router := mainRouter
	if gcfg.Prefix != "" {
		router = mainRouter.PathPrefix(gcfg.Prefix).Subrouter()
		mainRouter.Handle(gcfg.Prefix, hdlr)
		mainRouter.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, gcfg.Prefix, http.StatusTemporaryRedirect)
		})
	}

	if gcfg.Addr == "" {
		gcfg.Addr = fmt.Sprintf(":%d", gcfg.Port)
	}
	if !strings.Contains(gcfg.Addr, ":") {
		gcfg.Addr = ":" + gcfg.Addr
	}

	// 为 FRP 自动配置解析本地地址：绑定 0.0.0.0/:: 时用 util.GetLocalIP() 取真实
	// 局域网 IP；绑定具体 IP 时仅在它属于 10/8 或 192.168/16 时采用，否则回退到
	// 自动探测或 127.0.0.1，避免管理面板展示不可达/非局域网的 IP。
	localIP := "127.0.0.1"
	localPort := gcfg.Port
	if gcfg.Addr != "" {
		if host, portStr, err := net.SplitHostPort(gcfg.Addr); err == nil {
			if host == "" || host == "0.0.0.0" || host == "::" {
				if lanIP := util.GetLocalIP(); lanIP != "" {
					localIP = lanIP
				}
			} else if parsed := net.ParseIP(host); parsed != nil && util.IsLANAddress(parsed) {
				localIP = host
			} else if lanIP := util.GetLocalIP(); lanIP != "" {
				localIP = lanIP
			}
			if p, err := strconv.Atoi(portStr); err == nil {
				localPort = p
			}
		}
	}

	// DB 放在工作目录而非 gcfg.Root：gcfg.Root 会被 HTTP 直接对外服务，
	// 若把 DB 放进去，一次 GET /gohttpserver.db 就能拿到密码哈希。
	dbPath := gcfg.DB
	if dbPath == "" {
		dbPath = "gohttpserver.db"
	}
	// 自定义 --db 的文件名不在内置保护名单内：若它恰好落在对外服务的 root 下
	//（例如 --db ./data/config.db），一次 GET 就能拿到凭据库。动态注册进
	// server / zipx / webdav 三处保护名单，使其与默认 gohttpserver.db 受
	// 同样的名字变体（大小写/尾点/ADS/8.3）拦截。
	if gcfg.DB != "" {
		base := filepath.Base(filepath.ToSlash(filepath.Clean(dbPath)))
		server.RegisterServerStateName(base)
		zipx.RegisterServerStateName(base)
		webdav.RegisterServerStateName(base)
	}
	db, err := store.OpenDB(dbPath)
	if err != nil {
		slog.Error("open database failed", util.ErrorAttr(err), slog.String("path", dbPath))
		os.Exit(1)
	}

	frpClient := frp.NewFRPClient(db)

	dbFRPCfg, dbFRPEnabled, err := frpClient.LoadFromDB()
	if err != nil {
		slog.Error("FRP load config from DB failed", util.ErrorAttr(err))
	}

	// 用监听地址补齐 LocalIP/LocalPort

	frpLocalIP := gcfg.FRP.LocalIP
	if frpLocalIP == "" || frpLocalIP == "127.0.0.1" {
		frpLocalIP = localIP
	}
	frpLocalPort := gcfg.FRP.LocalPort
	if frpLocalPort == 0 {
		frpLocalPort = localPort
	}

	// 配置文件优先于数据库

	var frpCfg *frp.FRPConfig
	var frpEnabled bool

	if gcfg.FRP.Enabled || gcfg.FRP.ServerAddr != "" {
		frpCfg = &frp.FRPConfig{
			ServerAddr: gcfg.FRP.ServerAddr,
			ServerPort: gcfg.FRP.ServerPort,
			AuthToken:  gcfg.FRP.AuthToken,
			ProxyName:  gcfg.FRP.ProxyName,
			ProxyType:  gcfg.FRP.ProxyType,
			LocalIP:    frpLocalIP,
			LocalPort:  frpLocalPort,
			RemotePort: gcfg.FRP.RemotePort,
		}
		frpEnabled = gcfg.FRP.Enabled
	} else if dbFRPCfg != nil {
		frpCfg = dbFRPCfg
		frpEnabled = dbFRPEnabled
		// LocalIP/LocalPort 可在管理面板编辑，只在 DB 中尚无值时补齐，避免覆盖手动设置
		if frpCfg.LocalIP == "" || frpCfg.LocalIP == "127.0.0.1" {
			frpCfg.LocalIP = localIP
		}
		if frpCfg.LocalPort == 0 {
			frpCfg.LocalPort = localPort
		}
	}

	// SECURITY: oauth2-proxy 的鉴权完全依赖可被任意客户端伪造的 X-Auth-Request-*
	// 头，下方 loopback 绑定守卫只覆盖直连面；FRP 隧道会把本服务整体发布到公网，
	// 隧道侧请求同样能伪造这些头直接获得会话身份。两者同时启用等于把服务裸奔
	// 到公网，拒绝启动并要求运维显式择一（或改用 --login）。
	if frpEnabled && frpCfg != nil && gcfg.Auth.Type == "oauth2-proxy" {
		slog.Error("refusing to start: FRP tunnel together with auth-type=oauth2-proxy is insecure",
			slog.String("reason", "clients reaching the service through the FRP tunnel can forge X-Auth-Request-* headers and gain a session; use --login instead or disable one of the two"))
		os.Exit(1)
	}

	if frpCfg != nil {
		if err := frpClient.UpdateConfig(frpCfg); err != nil {
			slog.Error("FRP config error", util.ErrorAttr(err))
		}
		if frpEnabled {
			if err := frpClient.SetEnabled(true); err != nil {
				slog.Error("FRP start error", util.ErrorAttr(err))
			}
		}
	}

	// 登录闸门优先于其它 auth-type：在 HTTP Basic / OpenID / oauth2-proxy 之前
	// 安装，使其同样覆盖这些被包装的 handler。
	// 自我更新的"立即重启"经 restartCh 通知主 select 循环优雅停机退出；
	// updSvc 的后台检查循环在 shutdownMgr 就绪后才启动（见下方 RegisterCleanup）。
	restartCh := make(chan struct{}, 1)
	var updSvc *update.Service
	var loginStateObj *auth.LoginState
	if gcfg.Login {
		auth.FrontendFS = frontendDistFS
		loginStateObj = auth.LoadLoginCredentials(db)
		// 登录路由必须在下方 catch-all PathPrefix("/").Handler(hdlr) 之前注册，
		// 否则 mux 不会把 /-/login 交给它
		auth.RegisterLoginRoutes(router, loginStateObj, true)
		hdlr = auth.LoginAuthMiddleware(gcfg.Prefix, loginStateObj, hdlr)

		// WebDAV 与管理 API 都依赖 loginStateObj，共用同一个 SQLite 句柄
		webdavState := webdav.LoadWebdavAccounts(db)
		usageStateObj := webdav.LoadUsageState(db)

		// 仅当用量表为空（全新 DB 或迁移）时才从磁盘播种，管理接口可强制刷新
		if usageStateObj.IsEmpty() {
			for _, acc := range webdavState.List() {
				full := filepath.Join(gcfg.Root, acc.RootPath)
				if err := usageStateObj.Recalculate(full, acc.ID); err != nil {
					slog.Error("webdav quota startup recalculate failed", util.ErrorAttr(err), slog.String("account_id", acc.ID))
				}
			}
		}

		webdavSrv := webdav.NewWebdavServer(gcfg.Root, webdavState, usageStateObj)
		// 挂到子路由上，让 PROPFIND/PUT/DELETE 等动词共用同一 handler；
		// PathPrefix 已匹配开头 "/dav/"，尾部的通配是隐式的。
		router.PathPrefix("/dav/").Handler(webdavSrv)
		// gvfs 在发现阶段会不带尾斜杠探测该 collection
		router.Handle("/dav", webdavSrv)
		// 管理 API 位于登录闸门之后，未认证请求会重定向到 /-/login
		admin.RegisterRoutes(router, &admin.API{
			Login:     loginStateObj,
			Webdav:    webdavState,
			Usage:     usageStateObj,
			Root:      gcfg.Root,
			LocalAddr: gcfg.Addr,
			FRP:       frpClient,
			Version:   VERSION,
		})

		// 自我更新与 admin API 同级保护：仅 --login 模式注册（下载并替换
		// 二进制是特权操作，不能暴露给匿名访客）。更新服务器信息内置在
		// 代码中（updateServerConfig）；私有软件令牌支持 GHS_UPDATE_TOKEN
		// 环境变量注入。
		if updateServerConfig.BaseURL != "" {
			opts := updateServerConfig // 拷贝一份，避免共享可变的 map/字段
			opts.CurrentVersion = VERSION
			opts.Restart = func() {
				select {
				case restartCh <- struct{}{}:
				default:
				}
			}
			if envToken := os.Getenv("GHS_UPDATE_TOKEN"); envToken != "" {
				opts.Token = envToken
			}
			svc, err := update.NewService(opts)
			if err != nil {
				// 配置非法只禁用更新功能，不影响文件服务正常启动。
				slog.Error("update service disabled", util.ErrorAttr(err))
			} else {
				update.RegisterRoutes(router, svc)
				updSvc = svc
			}
		}
	} else {
		// SECURITY: --login 关闭时不注册管理 API。它不在 loginWhitelist 中，
		// 用 nil Login 注册后，OpenID / oauth2-proxy 会话能满足
		// UserFromLoginSession 并在 api.Login 为 nil 时 panic；
		// 不注册同时也消除了可发现的 401 攻击面。
	}

	var userHandlerRegistered bool
	switch gcfg.Auth.Type {
	case "http":
		hdlr = multiBasicAuth(gcfg.Auth.HTTP)(hdlr)
	case "openid":
		auth.HandleOpenID(router, gcfg.Auth.OpenID, false) // FIXME(ssx): set secure default to false
		userHandlerRegistered = true
		// case "github":
		// 	handleOAuth2ID(router, gcfg.Auth.Type, gcfg.Auth.ID, gcfg.Auth.Secret) // FIXME(ssx): set secure default to false
	case "oauth2-proxy":
		// 先把代理注入的身份头写入 session 再做下游鉴权，否则 .ghs.yml 的
		// 按用户规则永远不匹配（canDelete/canUpload/canEdit 只看 session）
		hdlr = auth.Oauth2ProxyMiddleware(hdlr)
		auth.HandleOauth2(router)
		userHandlerRegistered = true
	}

	// 没有认证模块注册 /-/user 时的默认实现：从 session 读取 LoginUser 返回，
	// 使前端 loadUser() 拿到非 null 用户，避免把已认证状态覆盖成 null 并退回登录页。
	if !userHandlerRegistered {
		router.HandleFunc("/-/user", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			if u := auth.UserFromLoginSession(r); u != nil {
				data, _ := json.Marshal(map[string]any{
					"name":     u.Name,
					"provider": u.Provider,
				})
				w.Write(data)
				return
			}
			w.Write([]byte("null"))
		}).Methods("GET")
	}

	// 内嵌 SPA 走 util.StaticAssets，为带哈希的资源提供 gzip 与 immutable 缓存
	router.PathPrefix("/-/frontend/").Handler(util.StaticAssets(
		http.StripPrefix(gcfg.Prefix+"/-/frontend/", http.FileServer(FrontendAssets))))

	router.HandleFunc("/-/sysinfo", func(w http.ResponseWriter, r *http.Request) {
		data, _ := json.Marshal(map[string]any{
			"version": VERSION,
			// 前端据此提前禁用更新入口，避免向未注册的更新 API 发请求
			// （未注册时 POST 会落到文件上传 handler，返回难懂的 400）。
			"update_enabled": updSvc != nil,
		})
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		w.Write(data)
	})
	router.PathPrefix("/").Handler(hdlr)

	if gcfg.Addr == "" {
		gcfg.Addr = fmt.Sprintf(":%d", gcfg.Port)
	}
	if !strings.Contains(gcfg.Addr, ":") {
		gcfg.Addr = ":" + gcfg.Addr
	}
	_, port, _ := net.SplitHostPort(gcfg.Addr)

	// oauth2-proxy 模式依赖可被任意客户端伪造的 X-Auth-Request-* 头；除非
	// 用 --allow-public-oauth2 显式确认风险，否则拒绝在非 loopback 上启动。
	if gcfg.Auth.Type == "oauth2-proxy" && !gcfg.AllowPublicOauth2 && !auth.IsLoopbackAddr(gcfg.Addr) {
		slog.Error("refusing to start: oauth2-proxy on non-loopback is insecure",
			slog.String("addr", gcfg.Addr),
			slog.String("port", port),
			slog.String("reason", "clients can forge X-Auth-Request-* headers; bind to loopback or use --allow-public-oauth2 only behind a header-stripping proxy"))
		os.Exit(1)
	}
	slog.Info("server listening",
		slog.String("addr", gcfg.Addr),
		slog.String("local_ip", util.GetLocalIP()),
		slog.String("port", port),
	)

	srv := &http.Server{
		// SecurityHeaders 包住整个 router（而不只是文件 handler），
		// 使登录页、管理 API、WebDAV、/metrics 与内嵌 SPA 都带上安全头
		Handler: util.SecurityHeaders(server.ContentSecurityPolicy)(mainRouter),
		Addr:    gcfg.Addr,
		// ReadHeaderTimeout 缓解 Slowloris：客户端须在此窗口内发完请求头。
		// 不设 Read/WriteTimeout 是因为它们会限制大文件上传下载的耗时
		// （body 的 I/O 已由 handler 层限制兜底）；IdleTimeout 回收空闲 keep-alive。
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// --session-ttl 控制登录态有效期；gorilla/sessions 中 MaxAge=0 表示
	// 浏览器会话 cookie（关闭即失效），--session-ttl=0 语义相同。未设置
	// GHS_SESSION_KEY 时每次重启都会生成新的签名密钥，使既有 cookie 全部失效。
	if sessionKey := os.Getenv("GHS_SESSION_KEY"); sessionKey != "" {
		auth.Store.Codecs = securecookie.CodecsFromPairs([]byte(sessionKey))
		slog.Info("using GHS_SESSION_KEY from environment for session signing")
		// securecookie 对任意长度 key 都接受，但 HMAC-SHA256 的安全性依赖密钥熵：
		// 短 key 可被离线爆破，伪造登录 cookie。小于 32 字节给出明确警告。
		if len(sessionKey) < 32 {
			slog.Warn("GHS_SESSION_KEY is shorter than 32 bytes; session cookies are brute-forceable. "+
				"Use at least 32 random bytes, e.g. head -c 32 /dev/urandom | base64",
				slog.Int("length", len(sessionKey)))
		}
	} else {
		slog.Warn("GHS_SESSION_KEY not set; sessions will be invalidated on restart")
	}
	if gcfg.SessionTTL > 0 {
		auth.Store.Options.MaxAge = int(gcfg.SessionTTL.Seconds())
	} else {
		auth.Store.Options.MaxAge = 0
	}
	slog.Info("session cookie ttl configured", slog.String("ttl", gcfg.SessionTTL.String()))

	// 走 TLS 时把 session cookie 标记为 Secure，防止浏览器在明文 HTTP 上发送它。
	// SECURITY: 反代终止 TLS（--xheaders）时本进程看到的是明文 HTTP，浏览器侧
	// 仍是 https——同样必须标记 Secure，否则 cookie 会在降级连接上外泄。
	// 若部署为「反代 HTTPS + 本机 HTTP」却用明文浏览，运维需自行清除该标记。
	if gcfg.Key != "" && gcfg.Cert != "" {
		auth.Store.Options.Secure = true
		slog.Info("session cookies marked Secure (direct TLS)")
	} else if gcfg.XHeaders {
		auth.Store.Options.Secure = true
		slog.Info("session cookies marked Secure (TLS terminated upstream; --xheaders)",
			slog.String("hint", "if you serve plain HTTP through the proxy, set --session-ttl accordingly"))
	}

	ln, err := net.Listen("tcp", gcfg.Addr)
	if err != nil {
		slog.Error("listen failed", util.ErrorAttr(err), slog.String("addr", gcfg.Addr))
		os.Exit(1)
	}

	// 用 proxyproto 包装 listener 以支持 HAProxy PROXY protocol，
	// 使 FRP 开启 ProxyProtocol 时能传入真实客户端 IP。
	//
	// SECURITY: 只接受来自 loopback 或私网地址的 PROXY protocol 头（FRP/HAProxy
	// 所在位置）。若无条件接受，直连暴露时攻击者可伪造该头伪造源 IP；
	// 公网连接一律按普通方式处理，不解析 PROXY protocol。
	proxyLn := &proxyproto.Listener{
		Listener: ln,
		Policy: func(upstream net.Addr) (proxyproto.Policy, error) {
			host, _, _ := net.SplitHostPort(upstream.String())
			ip := net.ParseIP(host)
			if ip != nil && (ip.IsLoopback() || util.IsPrivateIP(ip)) {
				return proxyproto.USE, nil
			}
			return proxyproto.IGNORE, nil
		},
		ReadHeaderTimeout: 5 * time.Second,
	}

	// 在后台 goroutine 中 Serve，主 goroutine 只等待「Serve 致命错误」或
	// 「终止信号」二者其一；http.Server.Shutdown 会关闭其 listener，无需另调 Close。
	serveErr := make(chan error, 1)
	if gcfg.Key != "" && gcfg.Cert != "" {
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		tlsLn := tls.NewListener(proxyLn, tlsConfig)
		go func() { serveErr <- srv.Serve(tlsLn) }()
	} else {
		go func() { serveErr <- srv.Serve(proxyLn) }()
	}

	// 优雅退出：先排空在途请求再释放数据库。cleanup 按 LIFO 执行且只在 HTTP
	// 服务停止后进行，保证事务中的请求全程持有可用的 DB 句柄。
	shutdownMgr := util.NewShutdownManager(srv, util.Logger(), 30*time.Second)
	shutdownMgr.RegisterCleanup(func() error {
		slog.Info("closing database")
		return db.Close()
	})
	if updSvc != nil {
		// cleanup 按 LIFO 执行：更新循环先于 DB 关闭被取消。
		shutdownMgr.RegisterCleanup(func() error {
			updSvc.Close()
			return nil
		})
		go updSvc.Run()
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	select {
	case err := <-serveErr:
		// Serve 返回即服务已停止；除正常 Shutdown 外的错误都视为致命，
		// 但仍先跑 cleanup，确保退出前释放 DB 句柄与 listener。
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped unexpectedly", util.ErrorAttr(err))
			if shutErr := shutdownMgr.Shutdown(); shutErr != nil {
				slog.Error("cleanup after server failure reported errors", util.ErrorAttr(shutErr))
			}
			os.Exit(1)
		}
	case <-restartCh:
		// 更新服务的"立即重启"：与信号退出同一套优雅停机流程，
		// 退出后由 systemd(Restart=always) 等守护进程以新版本拉起。
		slog.Info("restart requested (update applied); shutting down")
		if err := shutdownMgr.Shutdown(); err != nil {
			slog.Error("graceful shutdown did not complete cleanly", util.ErrorAttr(err))
		}
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped unexpectedly during shutdown", util.ErrorAttr(err))
		}
		os.Exit(0)
	case <-signalCtx.Done():
		slog.Info("shutdown signal received")
		if err := shutdownMgr.Shutdown(); err != nil {
			slog.Error("graceful shutdown did not complete cleanly", util.ErrorAttr(err))
		}
		// 等 Serve 观察到 listener 已关闭，避免进程退出与在途 goroutine 竞态
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped unexpectedly during shutdown", util.ErrorAttr(err))
			os.Exit(1)
		}
	}
}

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
	"github.com/codeskyblue/gohttpserver/internal/util"
	"github.com/codeskyblue/gohttpserver/internal/webdav"
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
		Type   string   `yaml:"type"` // openid|http|github
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
	// initial default conf
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

	kingpin.Parse() // first parse conf

	if gcfg.Conf != nil {
		defer func() {
			kingpin.Parse() // command line priority high than conf
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

// cors returns a middleware that applies the configured CORS policy.
// Previously this unconditionally sent Access-Control-Allow-Origin: *,
// which let any website issue cross-origin requests with a stolen token
// (Allow-Headers: * let X-Token through). Now the policy is opt-in:
//   - --cors-origins not set → no CORS headers (same-origin only)
//   - --cors-origins '*'     → allow all (the old behaviour; insecure,
//     only useful for public read-only servers)
//   - --cors-origins https://a.com --cors-origins https://b.com
//     → allow only those origins
//
// Only the matching origin is echoed back; credentials are not allowed
// when the origin is "*". Preflight OPTIONS is answered without calling
// the auth chain.
func cors(allowedOrigins []string) func(http.Handler) http.Handler {
	// Normalise once for O(1) lookup.
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
				// Limit headers to what the app actually uses instead of "*".
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
	// Initialize structured logger early
	util.InitLogger(&slog.HandlerOptions{
		Level: slog.LevelInfo,
	})

	if err := parseFlags(); err != nil {
		slog.Error("parse flags failed", util.ErrorAttr(err))
		os.Exit(1)
	}
	// --login implies upload/delete/edit: an authenticated operator
	// should be able to manage files without separately passing those
	// flags. This runs after flag parsing, so passing --no-upload
	// alongside --login will NOT disable upload — use --auth-type http
	// or another auth mechanism instead of --login if you need a
	// read-only authenticated server.
	if gcfg.Login {
		gcfg.Upload = true
		gcfg.Delete = true
		gcfg.Edit = true
	}
	if gcfg.Debug {
		// Print the config for debugging, but redact secrets — the
		// marshal below used to dump HTTP basic-auth passwords and the
		// OAuth2 client secret to stdout, which then flowed into log
		// collectors. We shallow-copy the config and blank the
		// sensitive fields before printing.
		debugCfg := gcfg
		debugCfg.Auth.HTTP = nil
		debugCfg.Auth.Secret = ""
		data, _ := yaml.Marshal(debugCfg)
		slog.Debug("config (secrets redacted)", slog.String("config", string(data)))
	}

	// make sure prefix matches: ^/.*[^/]$
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

	// NOTE: there used to be a second logger here
	// (accesslog.NewLoggingHandler). It logged every request a second time and
	// re-parsed the private-IP CIDR list on the hot path. The structured
	// middleware below now emits one line per request, including the
	// LOCAL/FRP access_type classification.

	// CORS — opt-in via --cors-origins. Empty = same-origin only.
	hdlr = cors(gcfg.CORSOrigins)(hdlr)

	if gcfg.XHeaders {
		hdlr = handlers.ProxyHeaders(hdlr)
		// Only trust proxy-forwarded IP headers when --xheaders is
		// enabled. This prevents attackers from forging X-Forwarded-For
		// to bypass login rate limiting when the server is directly
		// exposed.
		util.SetTrustProxyHeaders(true)
	}

	mainRouter := mux.NewRouter()

	// Request ID middleware - adds X-Request-ID header and context
	mainRouter.Use(util.RequestIDMiddleware)

	// Structured logging middleware
	mainRouter.Use(util.LoggingMiddleware(util.Logger()))

	// Health check endpoint (before prefix routing)
	mainRouter.HandleFunc("/healthz", util.HealthCheckHandler(VERSION)).Methods("GET")
	mainRouter.HandleFunc("/-/healthz", util.HealthCheckHandler(VERSION)).Methods("GET")

	// Prometheus-format metrics (before prefix routing so scrapers can
	// hit a stable path regardless of --prefix).
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

	// Normalize address early for FRP auto-configuration
	if gcfg.Addr == "" {
		gcfg.Addr = fmt.Sprintf(":%d", gcfg.Port)
	}
	if !strings.Contains(gcfg.Addr, ":") {
		gcfg.Addr = ":" + gcfg.Addr
	}

	// Parse local address for FRP auto-configuration
	// When binding to 0.0.0.0 or ::, use util.GetLocalIP() to get the real LAN IP
	// When binding to a specific IP, use that IP
	localIP := "127.0.0.1"
	localPort := gcfg.Port
	if gcfg.Addr != "" {
		if host, portStr, err := net.SplitHostPort(gcfg.Addr); err == nil {
			// If binding to all interfaces, get the real LAN IP
			if host == "" || host == "0.0.0.0" || host == "::" {
				if lanIP := util.GetLocalIP(); lanIP != "" {
					localIP = lanIP
				}
			} else {
				// Binding to a specific IP, use it directly
				localIP = host
			}
			if p, err := strconv.Atoi(portStr); err == nil {
				localPort = p
			}
		}
	}

	// Open database for FRP config persistence (and login/webdav if enabled)
	// Keep the database in the CURRENT WORKING DIRECTORY, not under
	// gcfg.Root. gcfg.Root is what gohttpserver serves over HTTP —
	// putting gohttpserver.db there would let any config mistake (a
	// misordered middleware, a future route that bypasses the gate,
	// a --prefix edge case) expose the password hash via a simple
	// GET /gohttpserver.db. The working directory is the conventional
	// spot for server-side state and is never served by the file
	// handler.
	dbPath := gcfg.DB
	if dbPath == "" {
		dbPath = "gohttpserver.db"
	}
	db, err := store.OpenDB(dbPath)
	if err != nil {
		slog.Error("open database failed", util.ErrorAttr(err), slog.String("path", dbPath))
		os.Exit(1)
	}

	// FRP client initialization with database for config persistence
	frpClient := frp.NewFRPClient(db)

	// Try to load FRP config from database first
	dbFRPCfg, dbFRPEnabled, err := frpClient.LoadFromDB()
	if err != nil {
		slog.Error("FRP load config from DB failed", util.ErrorAttr(err))
	}

	// Auto-fill LocalIP and LocalPort from program's listening address
	frpLocalIP := gcfg.FRP.LocalIP
	if frpLocalIP == "" || frpLocalIP == "127.0.0.1" {
		frpLocalIP = localIP
	}
	frpLocalPort := gcfg.FRP.LocalPort
	if frpLocalPort == 0 {
		frpLocalPort = localPort
	}

	// Determine which config to use: config file takes precedence over database
	var frpCfg *frp.FRPConfig
	var frpEnabled bool

	if gcfg.FRP.Enabled || gcfg.FRP.ServerAddr != "" {
		// Config file has FRP settings, use them
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
		// No config file settings, use database config
		frpCfg = dbFRPCfg
		frpEnabled = dbFRPEnabled
	}

	// Apply FRP config if available
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

	// --login gate. When enabled, an independent username/password login
	// flow shadows every other auth-type. Must be installed BEFORE the
	// HTTP Basic / OpenID / oauth2-proxy switch so the gate covers those
	// wrapped handlers too (a config like `--auth-type http --login`
	// would otherwise only HTTP-basic-gate on top of an un-gated server).
	var loginStateObj *auth.LoginState
	if gcfg.Login {
		auth.FrontendFS = frontendDistFS
		loginStateObj = auth.LoadLoginCredentials(db)
		// Routes must be registered BEFORE the catch-all router.PathPrefix("/").Handler(hdlr)
		// below so mux routes /-/login specifically. Middleware is added to
		// hdlr so the gate wraps everything (file handler, APIs) except
		// the white-listed paths.
		auth.RegisterLoginRoutes(router, loginStateObj, true)
		hdlr = auth.LoginAuthMiddleware(gcfg.Prefix, loginStateObj, hdlr)

		// WebDAV server + admin API. Both depend on loginStateObj:
		//   - webdav account usernames are bound to the login user
		//   - admin API handlers re-authenticate against loginStateObj
		//     for sensitive operations (e.g. username change).
		// All three subsystems (login, webdav, usage) share the same
		// SQLite handle — the database is the single source of truth
		// for server-side state when --login is enabled.
		webdavState := webdav.LoadWebdavAccounts(db)
		usageStateObj := webdav.LoadUsageState(db)

		// Startup recalculation: if the storage_usage table is empty but
		// accounts exist (fresh DB with seeded accounts, or migration
		// from a pre-quota build), seed the table by walking each
		// account's chroot. Otherwise trust the cache — the operator
		// can force a refresh via the admin endpoint.
		if usageStateObj.IsEmpty() {
			for _, acc := range webdavState.List() {
				full := filepath.Join(gcfg.Root, acc.RootPath)
				if err := usageStateObj.Recalculate(full, acc.ID); err != nil {
					slog.Error("webdav quota startup recalculate failed", util.ErrorAttr(err), slog.String("account_id", acc.ID))
				}
			}
		}

		webdavSrv := webdav.NewWebdavServer(gcfg.Root, webdavState, usageStateObj)
		// /dav/ is mounted on a subrouter so PROPFIND/PUT/DELETE/etc.
		// all funnel through the same handler. PathPrefix matches the
		// leading "/dav/" and the trailing wildcard is implicit.
		router.PathPrefix("/dav/").Handler(webdavSrv)
		// Some clients (notably GNOME/gvfs on Linux) probe the collection
		// without a trailing slash ("/dav") during discovery. Route that
		// exact path to the same handler so it doesn't fall through to the
		// login gate and get rejected with Unauthorized.
		router.Handle("/dav", webdavSrv)
		// Admin API sits behind the login gate (it's not in
		// loginWhitelist), so an unauthenticated request is redirected
		// to /-/login rather than reaching the handlers.
		admin.RegisterRoutes(router, &admin.API{
			Login:     loginStateObj,
			Webdav:    webdavState,
			Usage:     usageStateObj,
			Root:      gcfg.Root,
			LocalAddr: gcfg.Addr,
			FRP:       frpClient,
			Version:   VERSION,
		})
	} else {
		// SECURITY FIX: When --login is disabled, do NOT register the
		// admin API routes. Previously the routes were registered with
		// a nil Login field, and every handler checked
		// `UserFromLoginSession(r) == nil` and returned 401. While not
		// directly exploitable (all handlers reject unauthenticated
		// requests), the routes were still discoverable (returns 401
		// instead of 404), exposing the API surface. Worse, in OpenID
		// or oauth2-proxy mode a valid session would satisfy the
		// `UserFromLoginSession(r)` check — the admin handlers would
		// proceed despite api.Login being nil, causing nil-pointer
		// panics on api.Login.ChangeUsername() etc. Not registering
		// the routes at all is the clean fix.
	}

	// HTTP Basic Authentication
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
		// Wrap the handler chain so the proxy-injected identity headers
		// are persisted into the session before downstream authorisation
		// checks read it. Without this the per-user rules in .ghs.yml
		// never matched, because canDelete/canUpload/canEdit only look
		// at the session.
		hdlr = auth.Oauth2ProxyMiddleware(hdlr)
		auth.HandleOauth2(router)
		userHandlerRegistered = true
	}

	// 如果没有通过认证模块注册 /-/user 路由，则添加默认实现。
	// 当 --login 启用时，从 session 中读取 LoginUser 并返回其信息，
	// 这样前端 loadUser() 能拿到非 null 的用户对象，不会把已经认证
	// 的状态又覆盖成 null 而错误地回到登录页。
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

	// The embedded SPA bundle is served through util.StaticAssets so the
	// hashed assets get gzip + immutable caching (without it the browser had
	// neither a compressed body nor any validator to revalidate with).
	router.PathPrefix("/-/frontend/").Handler(util.StaticAssets(
		http.StripPrefix(gcfg.Prefix+"/-/frontend/", http.FileServer(FrontendAssets))))

	router.HandleFunc("/-/sysinfo", func(w http.ResponseWriter, r *http.Request) {
		data, _ := json.Marshal(map[string]any{
			"version": VERSION,
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

	// oauth2-proxy mode trusts X-Auth-Request-* headers, which any
	// client can forge if the server is reachable without going through
	// the proxy. Refuse to start on a non-loopback bind unless the
	// operator explicitly acknowledges the risk with --allow-public-oauth2.
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
		// SecurityHeaders wraps the whole router (not just the file handler)
		// so the login page, the admin API, WebDAV, /metrics and the embedded
		// SPA assets all carry CSP / nosniff / X-Frame-Options too.
		Handler: util.SecurityHeaders(server.ContentSecurityPolicy)(mainRouter),
		Addr:    gcfg.Addr,
		// ReadHeaderTimeout mitigates Slowloris: a client must send the
		// full request headers within this window or the connection is
		// dropped. We don't set ReadTimeout/WriteTimeout because they
		// would cap large file uploads/downloads (the body I/O is
		// bounded by the handler-level limits already). IdleTimeout
		// reaps keep-alive connections that have no active request.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Session cookie lifetime: --session-ttl controls how long a logged-in
	// user stays signed in before the cookie expires and re-login is
	// required. gorilla/sessions treats MaxAge=0 as a session-only cookie
	// (dropped when the browser closes); we honour --session-ttl=0 the
	// same way. Cookies persist across server restarts as long as the
	// operator set GHS_SESSION_KEY (otherwise each restart mints a new
	// signing key, invalidating all existing cookies).
	if sessionKey := os.Getenv("GHS_SESSION_KEY"); sessionKey != "" {
		auth.Store.Codecs = securecookie.CodecsFromPairs([]byte(sessionKey))
		slog.Info("using GHS_SESSION_KEY from environment for session signing")
	} else {
		slog.Warn("GHS_SESSION_KEY not set; sessions will be invalidated on restart")
	}
	if gcfg.SessionTTL > 0 {
		auth.Store.Options.MaxAge = int(gcfg.SessionTTL.Seconds())
	} else {
		auth.Store.Options.MaxAge = 0
	}
	slog.Info("session cookie ttl configured", slog.String("ttl", gcfg.SessionTTL.String()))

	// When serving over TLS, mark the session cookie Secure so browsers
	// never send it over a plain-HTTP connection (downgrade sniffing).
	if gcfg.Key != "" && gcfg.Cert != "" {
		auth.Store.Options.Secure = true
	}

	// Create listener and wrap with proxyproto for FRP real IP support
	ln, err := net.Listen("tcp", gcfg.Addr)
	if err != nil {
		slog.Error("listen failed", util.ErrorAttr(err), slog.String("addr", gcfg.Addr))
		os.Exit(1)
	}

	// Wrap listener with proxyproto to support HAProxy PROXY protocol
	// This allows FRP to pass real client IP when ProxyProtocol is enabled.
	//
	// SECURITY FIX: Previously the Policy unconditionally returned USE,
	// meaning ANY connection's PROXY protocol header was accepted. If the
	// server is directly exposed (not behind HAProxy/FRP), an attacker can
	// forge the PROXY protocol header to spoof their source IP. Now we
	// only accept PROXY protocol from loopback or private IPs (where
	// FRP/HAProxy would be running). Public connections are handled
	// normally without PROXY protocol parsing.
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

	// Serve in a background goroutine so the main goroutine can react to
	// whichever happens first: a fatal Serve error, or a termination
	// signal. http.Server.Shutdown closes the listener it was given, so
	// there is no separate Close() to run here.
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

	// Graceful shutdown: drain in-flight requests, then release the
	// database. Cleanup runs in LIFO order and only after the HTTP
	// server has stopped, so a request mid-transaction still has a live
	// DB handle while it finishes.
	shutdownMgr := util.NewShutdownManager(srv, util.Logger(), 30*time.Second)
	shutdownMgr.RegisterCleanup(func() error {
		slog.Info("closing database")
		return db.Close()
	})

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	select {
	case err := <-serveErr:
		// Serve only returns once the server has stopped. Anything other
		// than a clean Shutdown is fatal; still run cleanup so the DB
		// handle and listeners are released before we exit.
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped unexpectedly", util.ErrorAttr(err))
			if shutErr := shutdownMgr.Shutdown(); shutErr != nil {
				slog.Error("cleanup after server failure reported errors", util.ErrorAttr(shutErr))
			}
			os.Exit(1)
		}
	case <-signalCtx.Done():
		slog.Info("shutdown signal received")
		if err := shutdownMgr.Shutdown(); err != nil {
			slog.Error("graceful shutdown did not complete cleanly", util.ErrorAttr(err))
		}
		// Wait for Serve to observe the closed listener so we don't race
		// process exit against in-flight goroutines.
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped unexpectedly during shutdown", util.ErrorAttr(err))
			os.Exit(1)
		}
	}
}

package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/alecthomas/kingpin"
	accesslog "github.com/codeskyblue/go-accesslog"
	"github.com/go-yaml/yaml"
	"github.com/goji/httpauth"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
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
	DeepPathMaxDepth int  `yaml:"deep-path-max-depth"`
	NoIndex          bool `yaml:"no-index"`
	CORSOrigins      []string `yaml:"cors-origins"`
	AllowPublicOauth2 bool   `yaml:"allow-public-oauth2"`
	Login            bool   `yaml:"login"`
	DB               string `yaml:"db"`
	SessionTTL       time.Duration `yaml:"session-ttl"`
}

type httpLogger struct{}

func (l httpLogger) Log(record accesslog.LogRecord) {
	log.Printf("%s - %s %d %s", record.Ip, record.Method, record.Status, record.Uri)
}

var (
	defaultPlistProxy = "https://plistproxy.herokuapp.com/plist"
	defaultOpenID     = "https://login.netease.com/openid"
	gcfg              = Configure{}
	logger            = httpLogger{}

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
//                              only useful for public read-only servers)
//   - --cors-origins https://a.com --cors-origins https://b.com
//                            → allow only those origins
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
	if err := parseFlags(); err != nil {
		log.Fatal(err)
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
		fmt.Printf("--- config (secrets redacted) ---\n%s\n", string(data))
	}
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	// make sure prefix matches: ^/.*[^/]$
	gcfg.Prefix = fixPrefix(gcfg.Prefix)
	if gcfg.Prefix != "" {
		log.Printf("url prefix: %s", gcfg.Prefix)
	}

	ss := NewHTTPStaticServer(gcfg.Root, gcfg.NoIndex)
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
			log.Fatal(err)
		}
		u.Scheme = "https"
		ss.PlistProxy = u.String()
	}
	if ss.PlistProxy != "" {
		log.Printf("plistproxy: %s", strconv.Quote(ss.PlistProxy))
	}

	var hdlr http.Handler = ss

	hdlr = accesslog.NewLoggingHandler(hdlr, logger)

	// CORS — opt-in via --cors-origins. Empty = same-origin only.
	hdlr = cors(gcfg.CORSOrigins)(hdlr)

	if gcfg.XHeaders {
		hdlr = handlers.ProxyHeaders(hdlr)
	}

	mainRouter := mux.NewRouter()
	router := mainRouter
	if gcfg.Prefix != "" {
		router = mainRouter.PathPrefix(gcfg.Prefix).Subrouter()
		mainRouter.Handle(gcfg.Prefix, hdlr)
		mainRouter.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, gcfg.Prefix, http.StatusTemporaryRedirect)
		})
	}

	// --login gate. When enabled, an independent username/password login
	// flow shadows every other auth-type. Must be installed BEFORE the
	// HTTP Basic / OpenID / oauth2-proxy switch so the gate covers those
	// wrapped handlers too (a config like `--auth-type http --login`
	// would otherwise only HTTP-basic-gate on top of an un-gated server).
	var loginStateObj *loginState
	if gcfg.Login {
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
		db, err := openDB(dbPath)
		if err != nil {
			log.Fatalf("login: open database %q: %v", dbPath, err)
		}
		defer db.Close()

		loginStateObj = loadLoginCredentials(db)
		// Routes must be registered BEFORE the catch-all router.PathPrefix("/").Handler(hdlr)
		// below so mux routes /-/login specifically. Middleware is added to
		// hdlr so the gate wraps everything (file handler, APIs) except
		// the white-listed paths.
		registerLoginRoutes(router, loginStateObj, true)
		hdlr = loginAuthMiddleware(loginStateObj, hdlr)

		// WebDAV server + admin API. Both depend on loginStateObj:
		//   - webdav account usernames are bound to the login user
		//   - admin API handlers re-authenticate against loginStateObj
		//     for sensitive operations (e.g. username change).
		// All three subsystems (login, webdav, usage) share the same
		// SQLite handle — the database is the single source of truth
		// for server-side state when --login is enabled.
		webdavState := loadWebdavAccounts(db)
		usageStateObj := loadUsageState(db)

		// Startup recalculation: if the storage_usage table is empty but
		// accounts exist (fresh DB with seeded accounts, or migration
		// from a pre-quota build), seed the table by walking each
		// account's chroot. Otherwise trust the cache — the operator
		// can force a refresh via the admin endpoint.
		if usageStateObj.isEmpty() {
			for _, acc := range webdavState.list() {
				full := filepath.Join(gcfg.Root, acc.RootPath)
				if err := usageStateObj.recalculate(full, acc.ID); err != nil {
					log.Printf("webdav-quota: startup recalculate for %s failed: %v", acc.ID, err)
				}
			}
		}

		webdavSrv := newWebdavServer(gcfg.Root, webdavState, usageStateObj)
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
		registerAdminRoutes(router, &adminAPI{
			login:  loginStateObj,
			webdav: webdavState,
			usage:  usageStateObj,
			root:   gcfg.Root,
		})
	}

	// HTTP Basic Authentication
	var userHandlerRegistered bool
	switch gcfg.Auth.Type {
	case "http":
		hdlr = multiBasicAuth(gcfg.Auth.HTTP)(hdlr)
	case "openid":
		handleOpenID(router, gcfg.Auth.OpenID, false) // FIXME(ssx): set secure default to false
		userHandlerRegistered = true
		// case "github":
		// 	handleOAuth2ID(router, gcfg.Auth.Type, gcfg.Auth.ID, gcfg.Auth.Secret) // FIXME(ssx): set secure default to false
	case "oauth2-proxy":
		// Wrap the handler chain so the proxy-injected identity headers
		// are persisted into the session before downstream authorisation
		// checks read it. Without this the per-user rules in .ghs.yml
		// never matched, because canDelete/canUpload/canEdit only look
		// at the session.
		hdlr = oauth2ProxyMiddleware(hdlr)
		handleOauth2(router)
		userHandlerRegistered = true
	}

	// 如果没有通过认证模块注册 /-/user 路由，则添加默认实现。
	// 当 --login 启用时，从 session 中读取 LoginUser 并返回其信息，
	// 这样前端 loadUser() 能拿到非 null 的用户对象，不会把已经认证
	// 的状态又覆盖成 null 而错误地回到登录页。
	if !userHandlerRegistered {
		router.HandleFunc("/-/user", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			if u := userFromLoginSession(r); u != nil {
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

	router.PathPrefix("/-/frontend/").Handler(http.StripPrefix(gcfg.Prefix+"/-/frontend/", http.FileServer(FrontendAssets)))
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
	if gcfg.Auth.Type == "oauth2-proxy" && !gcfg.AllowPublicOauth2 && !isLoopbackAddr(gcfg.Addr) {
		log.Fatalf("Refusing to start: auth-type=oauth2-proxy on non-loopback %s is insecure " +
			"(clients can forge X-Auth-Request-* headers). Bind to 127.0.0.1:%s or use --allow-public-oauth2 " +
			"only if a proxy in front strips those headers.", gcfg.Addr, port)
	}
	log.Printf("listening on %s, local address http://%s:%s\n", strconv.Quote(gcfg.Addr), getLocalIP(), port)

	srv := &http.Server{
		Handler: mainRouter,
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
	if gcfg.SessionTTL > 0 {
		store.Options.MaxAge = int(gcfg.SessionTTL.Seconds())
	} else {
		store.Options.MaxAge = 0
	}
	log.Printf("session cookie ttl: %s", gcfg.SessionTTL)

	// When serving over TLS, mark the session cookie Secure so browsers
	// never send it over a plain-HTTP connection (downgrade sniffing).
	if gcfg.Key != "" && gcfg.Cert != "" {
		store.Options.Secure = true
	}

	var err error
	if gcfg.Key != "" && gcfg.Cert != "" {
		err = srv.ListenAndServeTLS(gcfg.Cert, gcfg.Key)
	} else {
		err = srv.ListenAndServe()
	}
	log.Fatal(err)
}

package auth

import (
	"crypto/rand"
	"encoding/gob"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	openid "github.com/codeskyblue/openid-go"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
)

var (
	nonceStore         = openid.NewSimpleNonceStore()
	discoveryCache     = openid.NewSimpleDiscoveryCache()
	Store              *sessions.CookieStore
	DefaultSessionName = "ghs-session"
)

// sessionKeyLen 是未设置 GHS_SESSION_KEY 时的随机会话密钥长度（32 字节 = 256 位，适配 HMAC-SHA256）。
const sessionKeyLen = 32

func init() {
	if key := os.Getenv("GHS_SESSION_KEY"); key != "" {
		Store = sessions.NewCookieStore([]byte(key))
	} else {
		buf := make([]byte, sessionKeyLen)
		if _, err := rand.Read(buf); err != nil {
			log.Fatalf("failed to generate session key: %v", err)
		}
		log.Println("WARNING: GHS_SESSION_KEY not set; using a random session key. " +
			"Sessions will not survive a restart. Set GHS_SESSION_KEY for production.")
		Store = sessions.NewCookieStore(buf)
	}
	// Secure=false 以兼容明文 HTTP 本地开发（gorilla v1.4.0 默认 true 会破坏 HTTP），
	// TLS 启用时由 main() 置为 true；Path="/" 保证登录后重定向仍带着 cookie。
	Store.Options.HttpOnly = true
	Store.Options.SameSite = http.SameSiteLaxMode
	Store.Options.Secure = false
	Store.Options.Path = "/"
}

type UserInfo struct {
	Id       string `json:"id"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	NickName string `json:"nickName"`
}

type M map[string]any

func init() {
	gob.Register(&UserInfo{})
	gob.Register(&M{})
}

func requestScheme(r *http.Request) string {
	if r.URL.Scheme != "" {
		return r.URL.Scheme
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// safeNextURL 校验用户提供的 next 跳转目标，仅允许同源或相对 URL（封堵开放重定向）；
// 拒绝反斜杠，因为浏览器会把 "/\\evil.com" 归一化为 "//evil.com"。
func safeNextURL(r *http.Request, next string) string {
	if next == "" {
		return "/"
	}
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
		if strings.Contains(next, "\\") {
			return "/"
		}
		return next
	}
	if u, err := url.Parse(next); err == nil && u.IsAbs() {
		if u.Host == r.Host {
			return next
		}
	}
	return "/"
}

func HandleOpenID(router *mux.Router, loginUrl string, secure bool) {
	router.HandleFunc("/-/login", func(w http.ResponseWriter, r *http.Request) {
		nextUrl := r.FormValue("next")
		referer := r.Referer()
		if nextUrl == "" && strings.Contains(referer, "://"+r.Host) {
			nextUrl = referer
		}
		nextUrl = safeNextURL(r, nextUrl)
		scheme := requestScheme(r)
		log.Println("Scheme:", scheme)
		callbackURL := scheme + "://" + r.Host + "/-/openidcallback?next=" + url.QueryEscape(nextUrl)
		if u, err := openid.RedirectURL(loginUrl, callbackURL, ""); err == nil {
			http.Redirect(w, r, u, 303)
		} else {
			log.Println("Should not got error here:", err)
		}
	}).Methods("GET")

	router.HandleFunc("/-/openidcallback", func(w http.ResponseWriter, r *http.Request) {
		scheme := requestScheme(r)
		id, err := openid.Verify(scheme+"://"+r.Host+r.URL.String(), discoveryCache, nonceStore)
		if err != nil {
			io.WriteString(w, "Authentication check failed.")
			return
		}
		session, err := Store.Get(r, DefaultSessionName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		user := &UserInfo{
			Id:       id,
			Email:    r.FormValue("openid.sreg.email"),
			Name:     r.FormValue("openid.sreg.fullname"),
			NickName: r.FormValue("openid.sreg.nickname"),
		}
		session.Values["user"] = user
		if err := session.Save(r, w); err != nil {
			log.Println("session save error:", err)
		}

		nextUrl := safeNextURL(r, r.FormValue("next"))
		http.Redirect(w, r, nextUrl, 302)
	}).Methods("GET")

	router.HandleFunc("/-/user", func(w http.ResponseWriter, r *http.Request) {
		session, err := Store.Get(r, DefaultSessionName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		val := session.Values["user"]
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		data, _ := json.Marshal(val)
		w.Write(data)
	}).Methods("GET")

	router.HandleFunc("/-/logout", func(w http.ResponseWriter, r *http.Request) {
		session, err := Store.Get(r, DefaultSessionName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		delete(session.Values, "user")
		session.Options.MaxAge = -1
		nextUrl := r.FormValue("next")
		if nextUrl == "" {
			nextUrl = r.Referer()
		}
		nextUrl = safeNextURL(r, nextUrl)
		_ = session.Save(r, w)
		http.Redirect(w, r, nextUrl, 302)
	}).Methods("GET")
}

package main

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
	store              *sessions.CookieStore
	defaultSessionName = "ghs-session"
)

// sessionKeyLen is the length (in bytes) of the random session signing
// key generated when GHS_SESSION_KEY is not provided. 32 bytes = 256 bits,
// the recommended minimum for HMAC-SHA256 cookie signing.
const sessionKeyLen = 32

func init() {
	// Session key precedence: GHS_SESSION_KEY env var → random key.
	// Previously this was a hardcoded literal in the source, which let
	// anyone reading the repo forge valid session cookies. A random key
	// survives across restarts only within a single process, so logged-in
	// users will be signed out on server restart — that's the trade-off
	// for not shipping a secret in the binary. Operators who need sticky
	// sessions across restarts should set GHS_SESSION_KEY explicitly.
	if key := os.Getenv("GHS_SESSION_KEY"); key != "" {
		store = sessions.NewCookieStore([]byte(key))
	} else {
		buf := make([]byte, sessionKeyLen)
		if _, err := rand.Read(buf); err != nil {
			log.Fatalf("failed to generate session key: %v", err)
		}
		log.Println("WARNING: GHS_SESSION_KEY not set; using a random session key. " +
			"Sessions will not survive a restart. Set GHS_SESSION_KEY for production.")
		store = sessions.NewCookieStore(buf)
	}
	// Harden the session cookie by default: HttpOnly prevents JS from
	// reading it (mitigates XSS token theft), SameSite=Lax blocks most
	// CSRF. gorilla/sessions v1.4.0 changed NewCookieStore to default
	// Secure=true and SameSite=None — that breaks plain-HTTP local dev
	// (the browser refuses to store a Secure cookie over HTTP, so the
	// session is silently lost on every login). We explicitly reset
	// Secure=false here; main() flips it back to true when TLS is on.
	// Path="/" ensures the cookie is sent on all requests, not just
	// the login path — without this, some browsers won't send the
	// cookie after the post-login redirect, causing a login loop.
	store.Options.HttpOnly = true
	store.Options.SameSite = http.SameSiteLaxMode
	store.Options.Secure = false
	store.Options.Path = "/"
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

// requestScheme returns the scheme of the incoming request. It prefers
// r.URL.Scheme (set by some reverse proxies), falls back to https when
// r.TLS is non-nil, and otherwise returns http. Extracted so the login
// redirect and the callback verify use the same scheme — previously
// login inferred scheme from r.URL.Scheme while verify hardcoded "http://",
// which broke HTTPS deployments.
func requestScheme(r *http.Request) string {
	if r.URL.Scheme != "" {
		return r.URL.Scheme
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// safeNextURL validates a user-supplied "next" redirect target. Only
// same-origin or relative URLs are allowed; anything else falls back to
// "/". This closes an open-redirect hole where a crafted next= could
// send users to an attacker-controlled site after login.
//
// Backslash handling: browsers normalise "/\\evil.com" to "//evil.com"
// (treating backslash as a path separator), so a value like "/\\evil.com"
// would bypass a naive "starts with / but not //" check. We reject any
// relative path whose second character is a backslash to close that.
func safeNextURL(r *http.Request, next string) string {
	if next == "" {
		return "/"
	}
	// Relative paths (starting with "/" but not "//") are always safe.
	// Also reject "/\" and "/\\": browsers treat these as protocol-relative.
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
		if len(next) >= 2 && next[1] == '\\' {
			return "/"
		}
		return next
	}
	// Absolute URLs must match the request host.
	if u, err := url.Parse(next); err == nil && u.IsAbs() {
		if u.Host == r.Host {
			return next
		}
	}
	return "/"
}

func handleOpenID(router *mux.Router, loginUrl string, secure bool) {
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
		// Build the verify URL from the actual request scheme so HTTPS
		// deployments match what the OpenID provider redirected to.
		scheme := requestScheme(r)
		id, err := openid.Verify(scheme+"://"+r.Host+r.URL.String(), discoveryCache, nonceStore)
		if err != nil {
			io.WriteString(w, "Authentication check failed.")
			return
		}
		session, err := store.Get(r, defaultSessionName)
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
		session, err := store.Get(r, defaultSessionName)
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
		session, err := store.Get(r, defaultSessionName)
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
		// Same open-redirect guard as /-/login and /-/openidcallback:
		// without this, /-/logout?next=https://evil.com would send users
		// to an attacker-controlled site right after they sign out.
		nextUrl = safeNextURL(r, nextUrl)
		_ = session.Save(r, w)
		http.Redirect(w, r, nextUrl, 302)
	}).Methods("GET")
}

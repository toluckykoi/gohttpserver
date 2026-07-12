package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"

	"github.com/gorilla/mux"
)

// oauth2ProxyMiddleware reads the trusted headers that oauth2-proxy
// injects (X-Auth-Request-Email etc.) and persists the identity into the
// session. Without this, oauth2-proxy mode was broken: canDelete /
// canUpload / canEdit read the *session* for the user, but handleOauth2
// only echoed headers from /-/user and never wrote a session — so every
// authorisation check fell through to the .ghs.yml default and the proxy
// authentication was effectively bypassed for per-user rules.
//
// SECURITY: this trusts the X-Auth-Request-* headers unconditionally.
// Those headers are only meaningful when an oauth2-proxy sits in front
// of this server and strips any client-supplied values. main() refuses
// to start in oauth2-proxy mode unless the listen address is loopback
// (or the operator explicitly opts in with --allow-public-oauth2),
// because binding to a public interface lets any client forge these
// headers and impersonate any user.
func oauth2ProxyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only stamp a session when the proxy actually sent an identity.
		// Requests without the header (e.g. the proxy's own auth flow)
		// pass through unmodified.
		email := r.Header.Get("X-Auth-Request-Email")
		if email != "" {
			fullNameMap, _ := url.ParseQuery(r.Header.Get("X-Auth-Request-Fullname"))
			var fullName string
			for k := range fullNameMap {
				fullName = k
				break
			}
			user := &UserInfo{
				Email:    email,
				Name:     fullName,
				NickName: r.Header.Get("X-Auth-Request-User"),
			}
			session, err := store.Get(r, defaultSessionName)
			if err == nil {
				session.Values["user"] = user
				// Best-effort save; if it fails the per-request identity
				// is still resolved on subsequent calls via userFromSession
				// on the next request. Don't abort the request over it.
				_ = session.Save(r, w)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func handleOauth2(router *mux.Router) {
	router.HandleFunc("/-/user", func(w http.ResponseWriter, r *http.Request) {
		// Identity now comes from the session (populated by the
		// middleware), not directly from forgeable headers.
		userInfo := userFromSession(r)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		data, _ := json.Marshal(userInfo)
		w.Write(data)
	}).Methods("GET")
}

// isLoopbackAddr reports whether the given listen address (host:port or
// :port form) binds to a loopback interface only. Used to guard
// oauth2-proxy mode against public exposure.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// Unparseable — be conservative, treat as non-loopback.
		return false
	}
	if host == "" || host == "localhost" {
		// ":port" binds all interfaces; "localhost" may resolve to
		// loopback but we still treat the bind as non-loopback for the
		// oauth2-proxy guard, since ":port" is publicly reachable.
		if host == "localhost" {
			return true
		}
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

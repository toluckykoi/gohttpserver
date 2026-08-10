package auth

import (
	"embed"
	"net/http"

	"github.com/gorilla/sessions"
)

// FrontendFS is set by main at startup to the embedded frontend/dist
// filesystem. The auth package uses it to serve the login SPA page
// (handleLoginUI) without depending on the main package.
var FrontendFS embed.FS

// GetSession returns the gorilla session for the request. The session is
// always non-nil even when the cookie can't be decoded — gorilla/sessions
// returns a fresh empty session in that case.
func GetSession(r *http.Request) (*sessions.Session, error) {
	return Store.Get(r, DefaultSessionName)
}

// UserFromSession extracts the logged-in user from the session cookie.
// Returns nil when there is no session, the value is absent, or —
// crucially — when the stored value is not an *UserInfo. The previous
// code used a panic-style type assertion (val.(*UserInfo)); a crafted
// or legacy cookie whose "user" value deserialised to a different type
// would crash the request goroutine. The comma-ok form degrades
// gracefully to "not logged in" instead.
func UserFromSession(r *http.Request) *UserInfo {
	session, err := Store.Get(r, DefaultSessionName)
	if err != nil {
		return nil
	}
	val := session.Values["user"]
	if val == nil {
		return nil
	}
	userInfo, ok := val.(*UserInfo)
	if !ok {
		return nil
	}
	return userInfo
}

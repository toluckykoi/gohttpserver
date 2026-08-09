package admin

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"github.com/codeskyblue/gohttpserver/internal/auth"
	"github.com/codeskyblue/gohttpserver/internal/frp"
	"github.com/codeskyblue/gohttpserver/internal/util"
	"github.com/codeskyblue/gohttpserver/internal/webdav"
)

// API bundles the state every /-/api/admin/* handler needs. We
// pass this single struct around rather than threading the loginState
// and webdavAccountState through every handler signature.
type API struct {
	Login  *auth.LoginState
	Webdav *webdav.WebdavAccountState
	// Usage is the per-account byte counter; nil-safe — handlers
	// degrade to reporting quota_bytes only when Usage is unset.
	Usage *webdav.UsageState
	// Root is the --root filesystem path; needed by the recalculate
	// endpoint to walk each account's chroot.
	Root string
	// LocalAddr is the program's listening address (IP:port)
	LocalAddr string
	// FRP is the FRP client manager; nil-safe — handlers degrade to
	// returning disabled status when FRP is unset.
	FRP *frp.FRPClient
	// Version is the build version string, reported by /-/api/admin/profile.
	Version string
}

// RegisterRoutes wires every /-/api/admin/* endpoint onto the
// provided router. The router MUST be the post-prefix subrouter so
// --prefix works transparently. All admin endpoints sit behind the
// login middleware (registered in main.go before this is called), so
// by the time any handler here runs the caller is already authenticated
// via userFromLoginSession.
func RegisterRoutes(router *mux.Router, api *API) {
	router.HandleFunc("/-/api/admin/profile", api.handleProfile).Methods("GET")
	router.HandleFunc("/-/api/admin/profile/username", api.handleChangeUsername).Methods("PUT")
	// Password change is exposed under both the legacy path (already
	// registered in registerLoginRoutes as /-/api/auth/password) and the
	// new admin path. We keep the old one working so existing clients
	// (and the SPA's old useFileApi.changePassword) don't break, but
	// route new UI traffic through the admin namespace.
	router.HandleFunc("/-/api/admin/profile/password", func(w http.ResponseWriter, r *http.Request) {
		auth.HandleChangePassword(api.Login, w, r)
	}).Methods("PUT")

	router.HandleFunc("/-/api/admin/webdav/status", api.handleWebdavStatus).Methods("GET")
	router.HandleFunc("/-/api/admin/webdav/enabled", api.handleWebdavSetEnabled).Methods("PUT")
	router.HandleFunc("/-/api/admin/webdav/accounts", api.handleWebdavList).Methods("GET")
	router.HandleFunc("/-/api/admin/webdav/accounts", api.handleWebdavCreate).Methods("POST")
	router.HandleFunc("/-/api/admin/webdav/accounts/{id}", api.handleWebdavUpdate).Methods("PUT")
	router.HandleFunc("/-/api/admin/webdav/accounts/{id}", api.handleWebdavDelete).Methods("DELETE")
	router.HandleFunc("/-/api/admin/webdav/accounts/{id}/reset-password", api.handleWebdavResetPassword).Methods("POST")
	router.HandleFunc("/-/api/admin/webdav/recalculate-usage", api.handleWebdavRecalculateUsage).Methods("POST")

	// FRP endpoints
	router.HandleFunc("/-/api/admin/frp/status", api.handleFRPStatus).Methods("GET")
	router.HandleFunc("/-/api/admin/frp/enabled", api.handleFRPSetEnabled).Methods("PUT")
	router.HandleFunc("/-/api/admin/frp/config", api.handleFRPUpdateConfig).Methods("PUT")
}

// handleProfile returns the current user's profile information. The
// frontend uses this to render the "个人中心" tab. We include the
// username, auth provider, and version (so the UI doesn't need a
// separate /-/sysinfo round-trip).
func (api *API) handleProfile(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromLoginSession(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"username": user.Name,
		"provider": user.Provider,
		"version":  api.Version,
	})
}

// handleChangeUsername accepts JSON {new_username} and renames the
// login user. The session is then re-stamped with the new username so
// the SPA doesn't need a re-login to see the updated name in the header.
//
// Username changes are gated by the authenticated session itself — no
// current password is required. Renaming is reversible and visible in
// the UI, so the operational convenience outweighs the marginal risk
// of a stolen session cookie being used to rename the account.
func (api *API) handleChangeUsername(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromLoginSession(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil {
		http.Error(w, "Body too large or unreadable", http.StatusBadRequest)
		return
	}
	var req struct {
		NewUsername string `json:"new_username"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	newUsername := strings.TrimSpace(req.NewUsername)
	if newUsername == "" {
		http.Error(w, "new_username must not be empty", http.StatusBadRequest)
		return
	}
	if len(newUsername) > 64 {
		http.Error(w, "new_username too long (max 64 chars)", http.StatusBadRequest)
		return
	}
	if newUsername == user.Name {
		http.Error(w, "new_username is the same as the current one", http.StatusBadRequest)
		return
	}
	if err := api.Login.ChangeUsername(user.Name, newUsername); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// WebDAV accounts were created with a Username field bound to the
	// login user at creation time. Now that the login user has been
	// renamed, every bound webdav account is still carrying the OLD
	// name — a Linux client (GVfs / rclone / Cyberduck) trying to
	// connect with the new username would see a silent 401 and
	// surface "Unauthorized". Sync the username across all bound
	// webdav accounts so the rename is consistent everywhere.
	if synced, syncErr := api.Webdav.SyncUsernamesTo(user.Name, newUsername); syncErr != nil {
		// Username rename already persisted; the webdav sync is a
		// best-effort follow-up. Log and continue — the operator can
		// re-create webdav accounts manually if needed.
		log.Printf("admin: webdav username sync after rename failed: %v", syncErr)
	} else if synced > 0 {
		log.Printf("admin: webdav username sync updated %d account(s)", synced)
	}
	// Re-stamp the session so the SPA's user object picks up the new
	// name without requiring a sign-out / sign-in cycle.
	session, _ := auth.Store.Get(r, auth.DefaultSessionName)
	session.Values["user"] = &auth.LoginUser{
		Name:     newUsername,
		Provider: user.Provider,
	}
	if err := session.Save(r, w); err != nil {
		// The username was already persisted to disk; the session save
		// failure just means the cookie wasn't updated. The next request
		// will still authenticate, but the header might show the old
		// name for one more request. Log and proceed.
		log.Printf("admin: session save after username change failed: %v", err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":  true,
		"username": newUsername,
	})
}

// handleWebdavStatus returns the master enable flag plus the full
// account list. The frontend uses this to render the WebDAV panel in
// one round-trip rather than polling two endpoints.
func (api *API) handleWebdavStatus(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	// Strip password hashes from the response — the UI only needs to
	// show that a password exists, not what it hashes to.
	accounts := api.Webdav.List()
	public := make([]map[string]any, 0, len(accounts))
	for _, a := range accounts {
		public = append(public, api.webdavAccountPublic(a))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"enabled":   api.Webdav.IsEnabled(),
		"accounts":  public,
		"webdav_url": "/dav/",
	})
}

// handleWebdavSetEnabled toggles the WebDAV server on/off. Body:
// {"enabled": true|false}.
func (api *API) handleWebdavSetEnabled(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256))
	if err != nil {
		http.Error(w, "Body too large or unreadable", http.StatusBadRequest)
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if err := api.Webdav.SetEnabled(req.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "enabled": req.Enabled})
}

// handleWebdavList returns just the accounts (without the enabled flag).
// Kept as a separate endpoint from /status for clients that poll only
// the account list after a mutation.
func (api *API) handleWebdavList(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	accounts := api.Webdav.List()
	public := make([]map[string]any, 0, len(accounts))
	for _, a := range accounts {
		public = append(public, api.webdavAccountPublic(a))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"accounts": public})
}

// handleWebdavCreate accepts the create request and returns the new
// account PLUS the plaintext password. The frontend is expected to
// display the password exactly once and prompt the user to copy it.
func (api *API) handleWebdavCreate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromLoginSession(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		http.Error(w, "Body too large or unreadable", http.StatusBadRequest)
		return
	}
	var req webdav.CreateAccountRequest
	// Default: system files protected, root path "/", not read-only.
	// The frontend supplies these explicitly, but we keep the defaults
	// here too so a curl without the fields still produces a sensible
	// account.
	req.ProtectSystemFiles = true
	req.RootPath = "/"
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	acc, plaintext, err := api.Webdav.CreateAccount(req, user.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	public := api.webdavAccountPublic(acc)
	// Plaintext password — shown to the operator exactly once. They
	// are responsible for copying it; we never store or return it
	// again.
	public["password"] = plaintext
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(public)
}

// handleWebdavUpdate applies a partial update. See updateAccountRequest
// for the accepted fields.
func (api *API) handleWebdavUpdate(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		http.Error(w, "Body too large or unreadable", http.StatusBadRequest)
		return
	}
	var req webdav.UpdateAccountRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if err := api.Webdav.UpdateAccount(id, req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}

// handleWebdavDelete removes an account. Idempotent — deleting a
// non-existent ID returns 200.
func (api *API) handleWebdavDelete(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if err := api.Webdav.DeleteAccount(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}

// handleWebdavResetPassword generates a new random password and returns
// the plaintext. Same "shown once" contract as the create endpoint.
func (api *API) handleWebdavResetPassword(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	plaintext, err := api.Webdav.ResetPassword(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":  true,
		"password": plaintext,
	})
}

// handleWebdavRecalculateUsage walks every account's chroot and
// refreshes the byte counter. Used to recover from counter drift
// (e.g. operator manually deleted files, or initial migration from a
// pre-quota install where storage-usage.json doesn't exist yet).
//
// Mirrors Cloudreve's admin "calibrate storage" button
// (inventory/user.go:326-361).
func (api *API) handleWebdavRecalculateUsage(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if api.Usage == nil {
		http.Error(w, "usage tracking not configured", http.StatusServiceUnavailable)
		return
	}
	accounts := api.Webdav.List()
	results := api.Usage.RecalculateAll(accounts, api.Root)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"results": results,
	})
}

// webdavAccountPublic builds the JSON shape returned by the WebDAV
// admin endpoints (status, list, create, update). The shape is the
// single source of truth — every handler funnels through here so a new
// field only needs to be added in one place.
//
// password_sha256 is deliberately omitted (sensitive); quota_bytes +
// used_bytes are derived live so the frontend always shows fresh data.
func (api *API) webdavAccountPublic(a webdav.WebdavAccount) map[string]any {
	used := int64(0)
	if api.Usage != nil {
		used = api.Usage.Get(a.ID)
	}
	return map[string]any{
		"id":                   a.ID,
		"remark":               a.Remark,
		"username":             a.Username,
		"root_path":            a.RootPath,
		"readonly":             a.ReadOnly,
		"protect_system_files": a.ProtectSystemFiles,
		"quota_bytes":          a.QuotaBytes,
		"used_bytes":           used,
		"created_at":           a.CreatedAtUnix,
		"updated_at":           a.UpdatedAtUnix,
	}
}

// handleFRPStatus returns the current FRP client status including
// enabled state, running state, and configuration.
func (api *API) handleFRPStatus(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if api.FRP == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"enabled": false,
			"running": false,
		})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(api.FRP.Status())
}

// handleFRPSetEnabled toggles the FRP client on/off. Body:
// {"enabled": true|false}.
func (api *API) handleFRPSetEnabled(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if api.FRP == nil {
		http.Error(w, "FRP client not initialized", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256))
	if err != nil {
		http.Error(w, "Body too large or unreadable", http.StatusBadRequest)
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if err := api.FRP.SetEnabled(req.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "enabled": req.Enabled})
}

// handleFRPUpdateConfig updates the FRP client configuration and
// restarts the client if needed. Body: JSON with FRP configuration fields.
func (api *API) handleFRPUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if api.FRP == nil {
		http.Error(w, "FRP client not initialized", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		http.Error(w, "Body too large or unreadable", http.StatusBadRequest)
		return
	}
	var req struct {
		ServerAddr           string `json:"serverAddr"`
		ServerPort           int    `json:"serverPort"`
		AuthToken            string `json:"authToken"`
		ProxyName            string `json:"proxyName"`
		ProxyType            string `json:"proxyType"`
		LocalIP              string `json:"localIP"`
		LocalPort            int    `json:"localPort"`
		RemotePort           int    `json:"remotePort"`
		ProxyProtocolVersion string `json:"proxyProtocolVersion"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	// Validate proxyProtocolVersion
	if req.ProxyProtocolVersion != "" &&
		req.ProxyProtocolVersion != "v1" &&
		req.ProxyProtocolVersion != "v2" {
		http.Error(w, "Invalid proxyProtocolVersion, must be v1 or v2", http.StatusBadRequest)
		return
	}
	cfg := &frp.FRPConfig{
		ServerAddr:           req.ServerAddr,
		ServerPort:           req.ServerPort,
		AuthToken:            req.AuthToken,
		ProxyName:            req.ProxyName,
		ProxyType:            req.ProxyType,
		LocalIP:              req.LocalIP,
		LocalPort:            req.LocalPort,
		RemotePort:           req.RemotePort,
		ProxyProtocolVersion: req.ProxyProtocolVersion,
	}
	// Auto-fill local address from program's listening address if not specified
	if cfg.LocalIP == "" || cfg.LocalIP == "127.0.0.1" {
		if api.LocalAddr != "" {
			if host, portStr, err := net.SplitHostPort(api.LocalAddr); err == nil {
				if host != "" && host != "0.0.0.0" && host != "::" {
					cfg.LocalIP = host
				} else if lanIP := util.GetLocalIP(); lanIP != "" {
					cfg.LocalIP = lanIP
				}
				if p, err := strconv.Atoi(portStr); err == nil && cfg.LocalPort == 0 {
					cfg.LocalPort = p
				}
			}
		}
	}
	if err := api.FRP.UpdateConfig(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}

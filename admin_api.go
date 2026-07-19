package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

// adminAPI bundles the state every /-/api/admin/* handler needs. We
// pass this single struct around rather than threading the loginState
// and webdavAccountState through every handler signature.
type adminAPI struct {
	login  *loginState
	webdav *webdavAccountState
	// usage is the per-account byte counter; nil-safe — handlers
	// degrade to reporting quota_bytes only when usage is unset.
	usage *usageState
	// root is the --root filesystem path; needed by the recalculate
	// endpoint to walk each account's chroot.
	root string
}

// registerAdminRoutes wires every /-/api/admin/* endpoint onto the
// provided router. The router MUST be the post-prefix subrouter so
// --prefix works transparently. All admin endpoints sit behind the
// login middleware (registered in main.go before this is called), so
// by the time any handler here runs the caller is already authenticated
// via userFromLoginSession.
func registerAdminRoutes(router *mux.Router, api *adminAPI) {
	router.HandleFunc("/-/api/admin/profile", api.handleProfile).Methods("GET")
	router.HandleFunc("/-/api/admin/profile/username", api.handleChangeUsername).Methods("PUT")
	// Password change is exposed under both the legacy path (already
	// registered in registerLoginRoutes as /-/api/auth/password) and the
	// new admin path. We keep the old one working so existing clients
	// (and the SPA's old useFileApi.changePassword) don't break, but
	// route new UI traffic through the admin namespace.
	router.HandleFunc("/-/api/admin/profile/password", func(w http.ResponseWriter, r *http.Request) {
		handleChangePassword(api.login, w, r)
	}).Methods("PUT")

	router.HandleFunc("/-/api/admin/webdav/status", api.handleWebdavStatus).Methods("GET")
	router.HandleFunc("/-/api/admin/webdav/enabled", api.handleWebdavSetEnabled).Methods("PUT")
	router.HandleFunc("/-/api/admin/webdav/accounts", api.handleWebdavList).Methods("GET")
	router.HandleFunc("/-/api/admin/webdav/accounts", api.handleWebdavCreate).Methods("POST")
	router.HandleFunc("/-/api/admin/webdav/accounts/{id}", api.handleWebdavUpdate).Methods("PUT")
	router.HandleFunc("/-/api/admin/webdav/accounts/{id}", api.handleWebdavDelete).Methods("DELETE")
	router.HandleFunc("/-/api/admin/webdav/accounts/{id}/reset-password", api.handleWebdavResetPassword).Methods("POST")
	router.HandleFunc("/-/api/admin/webdav/recalculate-usage", api.handleWebdavRecalculateUsage).Methods("POST")
}

// handleProfile returns the current user's profile information. The
// frontend uses this to render the "个人中心" tab. We include the
// username, auth provider, and version (so the UI doesn't need a
// separate /-/sysinfo round-trip).
func (api *adminAPI) handleProfile(w http.ResponseWriter, r *http.Request) {
	user := userFromLoginSession(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"username": user.Name,
		"provider": user.Provider,
		"version":  VERSION,
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
func (api *adminAPI) handleChangeUsername(w http.ResponseWriter, r *http.Request) {
	user := userFromLoginSession(r)
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
	if err := api.login.changeUsername(user.Name, newUsername); err != nil {
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
	if synced, syncErr := api.webdav.syncUsernamesTo(user.Name, newUsername); syncErr != nil {
		// Username rename already persisted; the webdav sync is a
		// best-effort follow-up. Log and continue — the operator can
		// re-create webdav accounts manually if needed.
		log.Printf("admin: webdav username sync after rename failed: %v", syncErr)
	} else if synced > 0 {
		log.Printf("admin: webdav username sync updated %d account(s)", synced)
	}
	// Re-stamp the session so the SPA's user object picks up the new
	// name without requiring a sign-out / sign-in cycle.
	session, _ := store.Get(r, defaultSessionName)
	session.Values["user"] = &LoginUser{
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
func (api *adminAPI) handleWebdavStatus(w http.ResponseWriter, r *http.Request) {
	if userFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	// Strip password hashes from the response — the UI only needs to
	// show that a password exists, not what it hashes to.
	accounts := api.webdav.list()
	public := make([]map[string]any, 0, len(accounts))
	for _, a := range accounts {
		public = append(public, api.webdavAccountPublic(a))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"enabled":   api.webdav.isEnabled(),
		"accounts":  public,
		"webdav_url": "/dav/",
	})
}

// handleWebdavSetEnabled toggles the WebDAV server on/off. Body:
// {"enabled": true|false}.
func (api *adminAPI) handleWebdavSetEnabled(w http.ResponseWriter, r *http.Request) {
	if userFromLoginSession(r) == nil {
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
	if err := api.webdav.setEnabled(req.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "enabled": req.Enabled})
}

// handleWebdavList returns just the accounts (without the enabled flag).
// Kept as a separate endpoint from /status for clients that poll only
// the account list after a mutation.
func (api *adminAPI) handleWebdavList(w http.ResponseWriter, r *http.Request) {
	if userFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	accounts := api.webdav.list()
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
func (api *adminAPI) handleWebdavCreate(w http.ResponseWriter, r *http.Request) {
	user := userFromLoginSession(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		http.Error(w, "Body too large or unreadable", http.StatusBadRequest)
		return
	}
	var req createAccountRequest
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
	acc, plaintext, err := api.webdav.createAccount(req, user.Name)
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
func (api *adminAPI) handleWebdavUpdate(w http.ResponseWriter, r *http.Request) {
	if userFromLoginSession(r) == nil {
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
	var req updateAccountRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if err := api.webdav.updateAccount(id, req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}

// handleWebdavDelete removes an account. Idempotent — deleting a
// non-existent ID returns 200.
func (api *adminAPI) handleWebdavDelete(w http.ResponseWriter, r *http.Request) {
	if userFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if err := api.webdav.deleteAccount(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}

// handleWebdavResetPassword generates a new random password and returns
// the plaintext. Same "shown once" contract as the create endpoint.
func (api *adminAPI) handleWebdavResetPassword(w http.ResponseWriter, r *http.Request) {
	if userFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	plaintext, err := api.webdav.resetPassword(id)
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
func (api *adminAPI) handleWebdavRecalculateUsage(w http.ResponseWriter, r *http.Request) {
	if userFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if api.usage == nil {
		http.Error(w, "usage tracking not configured", http.StatusServiceUnavailable)
		return
	}
	accounts := api.webdav.list()
	results := api.usage.recalculateAll(accounts, api.root)
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
func (api *adminAPI) webdavAccountPublic(a webdavAccount) map[string]any {
	used := int64(0)
	if api.usage != nil {
		used = api.usage.get(a.ID)
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

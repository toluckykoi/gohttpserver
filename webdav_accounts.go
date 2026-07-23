package main

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// webdavAccount is a single WebDAV credential entry. The username is
// always the current login user's name (bound at creation); the password
// is an independent random 10-char string so a leaked WebDAV password
// doesn't compromise the login password.
//
// The on-disk representation never stores the plaintext password — only
// the SHA-256 hash. Plaintext is returned to the caller exactly once, at
// creation / reset time, and is never recoverable afterwards.
type webdavAccount struct {
	ID                 string `json:"id"`
	Remark             string `json:"remark"`
	Username           string `json:"username"`
	PasswordSHA        string `json:"password_sha256"`
	RootPath           string `json:"root_path"`
	ReadOnly           bool   `json:"readonly"`
	ProtectSystemFiles bool   `json:"protect_system_files"`
	// QuotaBytes is the per-account storage cap in bytes. Zero means
	// unlimited (no quota enforced, no quota reported via PROPFIND).
	// Old on-disk records without this field decode as 0, which is the
	// safest default for a quota field on legacy deployments.
	QuotaBytes   int64 `json:"quota_bytes"`
	CreatedAtUnix int64 `json:"created_at"`
	UpdatedAtUnix int64 `json:"updated_at"`
}

// webdavAccountsFile is the in-memory shape of the WebDAV account state.
// The top-level "enabled" flag lets the operator turn the whole WebDAV
// server off without deleting configured accounts. It is persisted to the
// webdav_meta table (key="enabled") and the webdav_accounts table.
type webdavAccountsFile struct {
	Enabled  bool            `json:"enabled"`
	Accounts []webdavAccount `json:"accounts"`
}

// webdavAccountState is the in-memory state, guarded by a RWMutex.
// Persistence is backed by SQLite (webdav_accounts + webdav_meta tables),
// replacing the previous webdav-accounts.json file. Snapshot reads under
// the read lock, transactional writes under the write lock.
type webdavAccountState struct {
	mu     sync.RWMutex
	data   *webdavAccountsFile
	db     *sql.DB
	dbPath string // kept so tests can reopen the same DB to verify persistence
}

// systemFileNames are the filenames protected by ProtectSystemFiles.
// Anything matching this set is refused for write/delete operations
// through the WebDAV handler. Hidden files (starting with ".") are
// also protected, matching the file manager's default behaviour.
var systemFileNames = map[string]bool{
	"auth-state.json":      true,
	"webdav-accounts.json": true,
	"storage-usage.json":   true,
	"gohttpserver.db":      true,
	"gohttpserver.db-wal":  true,
	"gohttpserver.db-shm":  true,
	".ghs.yml":             true,
	"favicon.ico":          true,
	"favicon.png":          true,
}

// loadWebdavAccounts reads the WebDAV account state from SQLite (or returns
// an empty, disabled state if nothing is stored yet). Errors during read
// are logged but non-fatal — the server falls back to an empty state
// rather than refusing to start.
func loadWebdavAccounts(db *sql.DB) *webdavAccountState {
	return loadWebdavAccountsAt(db, "")
}

// loadWebdavAccountsAt is loadWebdavAccounts with the database path
// recorded on the returned state. Tests use the path to reopen the same
// DB to verify persistence. Production callers (main.go) use the
// parameter-less variant and ignore the empty path.
func loadWebdavAccountsAt(db *sql.DB, path string) *webdavAccountState {
	st := &webdavAccountState{
		db:     db,
		dbPath: path,
		data:   &webdavAccountsFile{Enabled: false},
	}

	// Read the enabled flag from webdav_meta.
	var enabledStr string
	err := db.QueryRow("SELECT value FROM webdav_meta WHERE key = 'enabled'").Scan(&enabledStr)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Printf("webdav: failed to read enabled flag (%v); starting with empty account list", err)
		return st
	}
	st.data.Enabled = enabledStr == "true"

	rows, err := db.Query(
		`SELECT id, remark, username, password_sha256, root_path, readonly,
		        protect_system_files, quota_bytes, created_at, updated_at
		 FROM webdav_accounts ORDER BY created_at`)
	if err != nil {
		log.Printf("webdav: failed to read accounts (%v); starting with empty account list", err)
		return st
	}
	defer rows.Close()
	for rows.Next() {
		var a webdavAccount
		if scanErr := rows.Scan(
			&a.ID, &a.Remark, &a.Username, &a.PasswordSHA, &a.RootPath,
			&a.ReadOnly, &a.ProtectSystemFiles, &a.QuotaBytes,
			&a.CreatedAtUnix, &a.UpdatedAtUnix,
		); scanErr != nil {
			log.Printf("webdav: scan account row failed: %v", scanErr)
			continue
		}
		st.data.Accounts = append(st.data.Accounts, a)
	}
	log.Printf("webdav: loaded %d account(s) (enabled=%v)", len(st.data.Accounts), st.data.Enabled)
	return st
}

// webdavAccountsFromDB opens the database at path and loads the WebDAV
// account state. Test-only convenience for re-opening the same DB to
// verify persistence. Cleanup of the new handle is the caller's
// responsibility (close the state.db or register a t.Cleanup).
func webdavAccountsFromDB(path string) (*webdavAccountState, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, err
	}
	return loadWebdavAccountsAt(db, path), nil
}

// generateWebdavPassword returns a random 10-character alphanumeric
// password. The alphabet is intentionally limited to unambiguous
// characters (no 0/O/1/I) so the password can be transcribed by hand
// if a client UI doesn't allow copy-paste.
func generateWebdavPassword() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	const length = 10
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(buf), nil
}

// generateWebdavID returns a unique-enough identifier for a new account.
// 6 hex chars from crypto/rand (24 bits of entropy) prefixed with "wd_".
func generateWebdavID() (string, error) {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "wd_" + hex.EncodeToString(b[:]), nil
}

// isEnabled returns whether the WebDAV server should accept connections.
func (s *webdavAccountState) isEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Enabled
}

// setEnabled flips the master switch and persists to disk.
func (s *webdavAccountState) setEnabled(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Enabled = on
	return s.saveLocked()
}

// list returns a copy of all accounts.
func (s *webdavAccountState) list() []webdavAccount {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]webdavAccount, len(s.data.Accounts))
	copy(out, s.data.Accounts)
	return out
}

// createAccountRequest is the JSON body accepted by POST
// /-/api/admin/webdav/accounts. Password is NOT accepted — the server
// generates it and returns the plaintext in the response.
type createAccountRequest struct {
	Remark             string `json:"remark"`
	RootPath           string `json:"root_path"`
	ReadOnly           bool   `json:"readonly"`
	ProtectSystemFiles bool   `json:"protect_system_files"`
	// QuotaBytes in bytes; 0 (or omitted) means unlimited.
	QuotaBytes int64 `json:"quota_bytes"`
}

// createAccount validates the request, generates a random password, and
// persists the new entry. Returns the created account plus the plaintext
// password — the caller returns the plaintext in the HTTP response
// exactly once.
func (s *webdavAccountState) createAccount(req createAccountRequest, loginUsername string) (webdavAccount, string, error) {
	if req.Remark == "" {
		return webdavAccount{}, "", errors.New("remark is required")
	}
	root, err := normaliseWebdavRoot(req.RootPath)
	if err != nil {
		return webdavAccount{}, "", err
	}
	id, err := generateWebdavID()
	if err != nil {
		return webdavAccount{}, "", fmt.Errorf("generate id: %w", err)
	}
	pw, err := generateWebdavPassword()
	if err != nil {
		return webdavAccount{}, "", fmt.Errorf("generate password: %w", err)
	}
	now := time.Now().Unix()
	acc := webdavAccount{
		ID:                 id,
		Remark:             req.Remark,
		Username:           loginUsername,
		PasswordSHA:        sha256Hex(pw),
		RootPath:           root,
		ReadOnly:           req.ReadOnly,
		ProtectSystemFiles: req.ProtectSystemFiles,
		QuotaBytes:         req.QuotaBytes,
		CreatedAtUnix:      now,
		UpdatedAtUnix:      now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Accounts = append(s.data.Accounts, acc)
	if err := s.saveLocked(); err != nil {
		// Roll back the in-memory append so list() doesn't return an
		// account whose persistence failed.
		s.data.Accounts = s.data.Accounts[:len(s.data.Accounts)-1]
		return webdavAccount{}, "", err
	}
	return acc, pw, nil
}

// updateAccountRequest is the JSON body for PUT
// /-/api/admin/webdav/accounts/{id}. Password and Username are
// intentionally NOT accepted: username is bound to the login user, and
// password rotation goes through the dedicated reset endpoint.
type updateAccountRequest struct {
	Remark             *string `json:"remark,omitempty"`
	RootPath           *string `json:"root_path,omitempty"`
	ReadOnly           *bool   `json:"readonly,omitempty"`
	ProtectSystemFiles *bool   `json:"protect_system_files,omitempty"`
	// QuotaBytes uses a pointer for partial-update semantics: omit the
	// field to leave unchanged, set to 0 to make the account unlimited,
	// set to a positive value to apply a cap. Negative values are rejected.
	QuotaBytes *int64 `json:"quota_bytes,omitempty"`
}

// updateAccount applies a partial update to an existing account. Fields
// not present in the request are left untouched.
func (s *webdavAccountState) updateAccount(id string, req updateAccountRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.data.Accounts {
		if s.data.Accounts[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return errors.New("account not found")
	}
	acc := &s.data.Accounts[idx]
	if req.Remark != nil {
		if *req.Remark == "" {
			return errors.New("remark must not be empty")
		}
		acc.Remark = *req.Remark
	}
	if req.RootPath != nil {
		root, err := normaliseWebdavRoot(*req.RootPath)
		if err != nil {
			return err
		}
		acc.RootPath = root
	}
	if req.ReadOnly != nil {
		acc.ReadOnly = *req.ReadOnly
	}
	if req.ProtectSystemFiles != nil {
		acc.ProtectSystemFiles = *req.ProtectSystemFiles
	}
	if req.QuotaBytes != nil {
		if *req.QuotaBytes < 0 {
			return errors.New("quota_bytes must not be negative")
		}
		acc.QuotaBytes = *req.QuotaBytes
	}
	acc.UpdatedAtUnix = time.Now().Unix()
	return s.saveLocked()
}

// resetPassword generates a new random password for the account and
// returns the plaintext.
func (s *webdavAccountState) resetPassword(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.data.Accounts {
		if s.data.Accounts[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", errors.New("account not found")
	}
	pw, err := generateWebdavPassword()
	if err != nil {
		return "", err
	}
	s.data.Accounts[idx].PasswordSHA = sha256Hex(pw)
	s.data.Accounts[idx].UpdatedAtUnix = time.Now().Unix()
	if err := s.saveLocked(); err != nil {
		return "", err
	}
	return pw, nil
}

// deleteAccount removes the account with the given ID. Deleting a
// non-existent ID is a no-op so the API can be idempotent.
func (s *webdavAccountState) deleteAccount(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.data.Accounts {
		if s.data.Accounts[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	s.data.Accounts = append(s.data.Accounts[:idx], s.data.Accounts[idx+1:]...)
	return s.saveLocked()
}

// syncUsernamesTo renames every webdav account bound to oldName to
// newName. Returns the number of accounts that were updated.
//
// This closes a footgun: webdav accounts are bound to the LOGIN
// username at creation time, but the admin API lets the operator
// rename the login user via /-/api/admin/profile/username. Without
// this sync the webdav accounts keep the old username, so a Linux
// GVfs / rclone / Cyberduck client trying to connect with the new
// username gets a silent 401 ("Unauthorized") with no indication
// that the username needs to be the OLD login name.
//
// Called from admin_api.handleChangeUsername after the rename
// succeeds, so webdav-bound clients immediately work with the new
// name. No-op when nothing matches — leaves un-bound accounts alone.
func (s *webdavAccountState) syncUsernamesTo(oldName, newName string) (int, error) {
	if oldName == newName {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Snapshot the indices we'll change so we can roll back cleanly
	// if saveLocked fails.
	type rename struct {
		idx     int
		oldName string
	}
	renames := make([]rename, 0)
	for i := range s.data.Accounts {
		if s.data.Accounts[i].Username == oldName {
			renames = append(renames, rename{idx: i})
		}
	}
	if len(renames) == 0 {
		return 0, nil
	}
	now := time.Now().Unix()
	for _, r := range renames {
		s.data.Accounts[r.idx].Username = newName
		s.data.Accounts[r.idx].UpdatedAtUnix = now
	}
	if err := s.saveLocked(); err != nil {
		// Roll back to the pre-rename state so list() doesn't return
		// accounts that the disk copy no longer matches.
		for _, r := range renames {
			s.data.Accounts[r.idx].Username = oldName
		}
		return 0, err
	}
	log.Printf("webdav: synced %d account(s) from username %q → %q",
		len(renames), oldName, newName)
	return len(renames), nil
}

// verifyWebdav looks up an account by username and checks the password.
// Returns the matching account (copied, safe for caller to mutate) and
// true on success, or zero-value + false on any failure. Constant-time
// comparison is used for the password hash.
//
// Multiple accounts may share the same username (the admin can create
// several accounts bound to "admin" with different root_path / readonly
// flags). We iterate ALL accounts with a matching username and return
// the first whose password hash matches. Without this, if account A
// (created earlier) appears before account B in the list and the user
// tries to log in with B's password, the early-return-on-mismatch
// behaviour would reject the login even though B's password is correct.
// We also avoid early return on hash mismatch so the timing doesn't
// leak which username exists (a non-match walks the whole list).
func (s *webdavAccountState) verifyWebdav(username, password string) (webdavAccount, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	givenHash := sha256Hex(password)
	var matched webdavAccount
	found := false
	userMatched := 0
	for _, acc := range s.data.Accounts {
		if acc.Username != username {
			continue
		}
		userMatched++
		if subtle.ConstantTimeCompare([]byte(givenHash), []byte(acc.PasswordSHA)) == 1 {
			matched = acc
			found = true
			break
		}
	}
	if !found {
		// Distinguish "username doesn't match any account" from
		// "username matches but password is wrong" — this is the key
		// signal for diagnosing login failures. The two cases require
		// completely different operator fixes:
		//   - userMatched == 0 → user is typing the wrong username
		//     (e.g. using the "remark" field, or a stale username
		//     from before a rename).
		//   - userMatched > 0  → username is correct but the password
		//     is wrong (typo, stale clipboard, reset was done but
		//     client cached the old password).
		log.Printf("webdav: verify failed user=%q (accounts with matching username=%d, total accounts=%d)",
			username, userMatched, len(s.data.Accounts))
	}
	return matched, found
}

// normaliseWebdavRoot validates and canonicalises a WebDAV account's
// root_path. The result is always "/"-prefixed and never contains ".."
// segments (which would let the account escape --root via path
// traversal in the WebDAV handler).
func normaliseWebdavRoot(input string) (string, error) {
	if input == "" {
		return "/", nil
	}
	// Force forward slashes regardless of platform — WebDAV URLs always
	// use "/", and the file handler later translates back to the OS
	// separator via filepath.FromSlash.
	cleaned := filepath.ToSlash(filepath.Clean(input))
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	// filepath.Clean collapses ".." where it can, but a leading ".." or
	// a "/../foo" pattern still survives. Reject any segment equal to
	// ".." to be safe.
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == ".." {
			return "", errors.New("root_path must not contain '..'")
		}
	}
	return cleaned, nil
}

// saveLocked persists the full account state to SQLite in one transaction:
// the enabled flag into webdav_meta and the account list into
// webdav_accounts (delete-all + reinsert, mirroring the previous
// whole-file-rewrite semantics). The transaction gives atomicity — a
// failed commit rolls back cleanly. Caller must hold s.mu in write mode.
func (s *webdavAccountState) saveLocked() error {
	if s.data == nil {
		return errors.New("webdav: no state loaded")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	enabled := "false"
	if s.data.Enabled {
		enabled = "true"
	}
	if _, err := tx.Exec(
		`INSERT INTO webdav_meta (key, value) VALUES ('enabled', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, enabled); err != nil {
		return err
	}

	if _, err := tx.Exec("DELETE FROM webdav_accounts"); err != nil {
		return err
	}
	insert, err := tx.Prepare(
		`INSERT INTO webdav_accounts
		   (id, remark, username, password_sha256, root_path, readonly,
		    protect_system_files, quota_bytes, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insert.Close()
	for _, a := range s.data.Accounts {
		if _, err := insert.Exec(
			a.ID, a.Remark, a.Username, a.PasswordSHA, a.RootPath, a.ReadOnly,
			a.ProtectSystemFiles, a.QuotaBytes, a.CreatedAtUnix, a.UpdatedAtUnix,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// isProtectedFilename returns true if the given basename should be
// protected from write/delete operations through WebDAV when the
// account has ProtectSystemFiles enabled.
func isProtectedFilename(name string) bool {
	if systemFileNames[name] {
		return true
	}
	// Hide any dotfile — matches the file manager's default behaviour
	// (showHidden is false by default) and prevents accidentally
	// exposing or clobbering configuration.
	return strings.HasPrefix(name, ".")
}

package main

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// ─────────────────────────────────────────────────────────────────────────────
// SQLite-backed persistence for the --login subsystem.
//
// When --login is enabled, gohttpserver stores its server-side state in a
// single SQLite database file (default: gohttpserver.db in the working
// directory). This replaces the previous scattered JSON files
// (auth-state.json, webdav-accounts.json, storage-usage.json).
//
// The DB lives in the working directory, NOT under --root, so it is never
// served over HTTP — the same reasoning that kept the JSON files out of the
// served root.
//
// modernc.org/sqlite is a pure-Go driver (no CGO), which keeps the project's
// CGO_ENABLED=0 cross-compilation (.goreleaser.yml / build.sh) working.
// ─────────────────────────────────────────────────────────────────────────────

// schemaSQL creates every table used by the --login subsystem. All statements
// are IF NOT EXISTS so opening an existing database is a no-op.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS login_credentials (
	id              INTEGER PRIMARY KEY CHECK (id = 1),
	username        TEXT    NOT NULL,
	password_sha256 TEXT    NOT NULL,
	created_at      INTEGER NOT NULL,
	updated_at      INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS webdav_meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS webdav_accounts (
	id                   TEXT PRIMARY KEY,
	remark               TEXT    NOT NULL,
	username             TEXT    NOT NULL,
	password_sha256      TEXT    NOT NULL,
	root_path            TEXT    NOT NULL,
	readonly             INTEGER NOT NULL,
	protect_system_files INTEGER NOT NULL,
	quota_bytes          INTEGER NOT NULL,
	created_at           INTEGER NOT NULL,
	updated_at           INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS storage_usage (
	account_id TEXT PRIMARY KEY,
	used_bytes INTEGER NOT NULL
);
`

// openDB opens (creating if needed) the SQLite database at path and ensures
// the schema exists. WAL mode gives better concurrency for the read-heavy
// verify path; a single open connection sidesteps "database is locked"
// contention on writes (write volume here is tiny — a password change or a
// quota delta, never a hot loop).
func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	// Pure-Go sqlite serializes writes per connection; capping the pool at 1
	// avoids SQLITE_BUSY under concurrent saves from the login / webdav /
	// usage states, all of which share this handle.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign_keys: %w", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return db, nil
}

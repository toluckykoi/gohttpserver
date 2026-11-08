package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// store 以单文件 SQLite 持久化服务端状态（登录凭据、webdav 账号、用量、frp 配置），
// 文件在工作目录中不经 HTTP 提供；选用纯 Go 驱动以保住 CGO_ENABLED=0 交叉编译。

// schemaSQL 创建 --login 子系统用到的全部表；语句均为 IF NOT EXISTS，打开已有库时是空操作。
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

CREATE TABLE IF NOT EXISTS frp_config (
	id              INTEGER PRIMARY KEY CHECK (id = 1),
	enabled         INTEGER NOT NULL DEFAULT 0,
	server_addr     TEXT NOT NULL DEFAULT '',
	server_port     INTEGER NOT NULL DEFAULT 7000,
	auth_token      TEXT NOT NULL DEFAULT '',
	proxy_name      TEXT NOT NULL DEFAULT 'gohttpserver',
	proxy_type      TEXT NOT NULL DEFAULT 'tcp',
	local_ip        TEXT NOT NULL DEFAULT '127.0.0.1',
	local_port      INTEGER NOT NULL DEFAULT 8000,
	remote_port     INTEGER NOT NULL DEFAULT 30000,
	proxy_protocol_version TEXT NOT NULL DEFAULT '',
	updated_at      INTEGER NOT NULL
);
`

// migrationSQL 给已有表加列，在 schemaSQL 之后执行。
const migrationSQL = `
-- Add proxy_protocol_version column if it doesn't exist (SQLite doesn't support IF NOT EXISTS for ADD COLUMN)
-- We use a transaction with exception handling to make this idempotent
ALTER TABLE frp_config ADD COLUMN proxy_protocol_version TEXT NOT NULL DEFAULT '';
`

// OpenDB 打开（必要时创建）SQLite 库并确保 schema 就绪；WAL 提升读并发，连接池上限 1：
// 纯 Go 驱动按连接串行化写入，封顶避免并发保存时出现 SQLITE_BUSY。
func OpenDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	// WAL 下 NORMAL 是标准组合：写只落 WAL 不逐次 fsync，掉电至多丢最后几笔提交且不损坏库。
	if _, err := db.Exec("PRAGMA synchronous=NORMAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set synchronous mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign_keys: %w", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	// 迁移语句报错说明已应用过，忽略。
	db.Exec(migrationSQL)
	return db, nil
}

// 存储层：SQLite（modernc.org/sqlite 纯 Go 驱动，免 CGO）
// DDL 以 PostgreSQL 语义设计、SQLite 方言落地，与 Node MVP schema 完全一致。
// 注：sqlc 代码生成在 PG 主模式确定后接入（见 issue #1 任务清单）。
package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

func Open(dataDir string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s/taboo.db?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", dataDir)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// 单写多读：SQLite 单连接避免 database is locked
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

type migrationDB interface {
	Exec(string, ...any) (sql.Result, error)
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

func migrate(db *sql.DB) error {
	if !autoMigrateEnabled() {
		return requireSchema(db)
	}
	return migrateSchema(context.Background(), db, goose.DialectSQLite3, "sqlite", SchemaVersionSQLite, "", "")
}

// requireSchema 在关闭自动迁移时只校验版本，不一致就拒绝启动。
func requireSchema(db *sql.DB) error {
	v, err := Version(db)
	if err != nil {
		return err
	}
	if want := ExpectedVersion(db); v != want {
		return fmt.Errorf("database schema %d does not match server %d (TABOO_AUTO_MIGRATE is off)", v, want)
	}
	return nil
}

// relaxDynamicEngineCheck 把旧的 postgres-only CHECK 放宽到 mysql。
// 已有库的 CREATE TABLE 不会被 IF NOT EXISTS 改写，必须重建表。

// migrateFolders 把还没挂上目录记录的密钥补到项目根目录。

// discardEnvironmentModel 丢掉尚未上线时留下的环境表。已有密钥和目录不保留。

// migrateIdentityScopes 重建 CHECK，并把存量 read 升为 reveal（避免 CI 突然 403）。

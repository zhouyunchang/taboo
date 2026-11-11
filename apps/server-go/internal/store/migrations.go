// 数据库结构迁移：使用 goose（github.com/pressly/goose/v3）执行版本化脚本。
//
// 脚本是唯一的结构来源，按方言分开：
//
//	schema/pg/00001_account.sql … 00013_runtime_grants.sql
//	schema/sqlite/00001_account.sql … 00010_device.sql
//
// 启动时按顺序执行尚未运行过的脚本，版本表 goose_db_version 记录进度；
// 多副本同时启动由 goose 的会话锁（PostgreSQL advisory lock）串行化。
//
// 历史上的自研迁移器没有版本记录，只有 schema_version（PostgreSQL）/
// PRAGMA user_version（SQLite）里的终值。旧库首次用 goose 打开时按该终值基线化：
// 把 1..N 标记为已执行，不重放任何脚本（重放会 DROP 数据）。
package store

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"github.com/pressly/goose/v3/lock"
)

//go:embed schema/pg/*.sql
var pgMigrations embed.FS

//go:embed schema/sqlite/*.sql
var sqliteMigrations embed.FS

const (
	// SchemaVersionPostgres / SchemaVersionSQLite 是当前二进制期望的迁移版本，
	// 等于该方言最后一个脚本的编号。
	SchemaVersionPostgres = 13
	SchemaVersionSQLite   = 10

	// migrationsTable 是 goose 的版本表名。
	migrationsTable = "goose_db_version"

	// legacySchemaVersion 是自研迁移器时代的终值，用来识别需要基线化的旧库。
	legacySchemaVersion = 23
)

// ExpectedVersion 返回该数据库连接应达到的迁移版本。
func ExpectedVersion(db *sql.DB) int {
	if IsPostgres(db) {
		return SchemaVersionPostgres
	}
	return SchemaVersionSQLite
}

// newMigrationStore 是 goose 的版本表访问器（按方言选 SQL）。
func newMigrationStore(dialect goose.Dialect) (database.Store, error) {
	return database.NewStore(dialect, migrationsTable)
}

// Version 返回数据库当前已应用的迁移版本（无记录时为 0）。
func Version(db *sql.DB) (int, error) {
	dialect := goose.DialectSQLite3
	if IsPostgres(db) {
		dialect = goose.DialectPostgres
	}
	st, err := newMigrationStore(dialect)
	if err != nil {
		return 0, err
	}
	ok, err := hasTable(db, migrationsTable)
	if err != nil || !ok {
		return 0, err
	}
	v, err := st.GetLatestVersion(context.Background(), db)
	if errors.Is(err, database.ErrVersionNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return int(v), nil
}

// migrateSchema 把数据库结构迁移到本二进制所需的版本。
//
// owner/role 只用于 PostgreSQL 脚本里的部署占位符；SQLite 传空。
func migrateSchema(ctx context.Context, db *sql.DB, dialect goose.Dialect, root string, expected int, owner, role string) error {
	sub, err := fs.Sub(schemaSource(dialect, root, owner, role), ".")
	if err != nil {
		return err
	}
	st, err := newMigrationStore(dialect)
	if err != nil {
		return err
	}
	exists, err := hasTable(db, migrationsTable)
	if err != nil {
		return err
	}
	if !exists {
		if err := st.CreateVersionTable(ctx, db); err != nil {
			return err
		}
		// goose 用版本 0 代表"版本表已就绪"；缺了它，Provider 会以为表不存在而重复创建。
		if err := st.Insert(ctx, db, database.InsertRequest{Version: 0}); err != nil {
			return err
		}
	}
	latest, err := st.GetLatestVersion(ctx, db)
	if errors.Is(err, database.ErrVersionNotFound) {
		if err := st.Insert(ctx, db, database.InsertRequest{Version: 0}); err != nil {
			return err
		}
		latest, err = 0, nil
	}
	if err != nil {
		return err
	}
	if latest == 0 {
		if err := baseline(ctx, db, st, dialect, expected); err != nil {
			return err
		}
	} else if int(latest) > expected {
		// 库比二进制新：拒绝启动，不做降级尝试。
		return fmt.Errorf("database schema %d is newer than server %d", latest, expected)
	}

	opts := []goose.ProviderOption{
		goose.WithLogger(goose.NopLogger()),
	}
	if dialect == goose.DialectPostgres {
		// 会话锁：多副本同时启动时只有一个实例真正执行迁移。
		locker, err := lock.NewPostgresSessionLocker()
		if err != nil {
			return err
		}
		opts = append(opts, goose.WithSessionLocker(locker))
	}
	provider, err := goose.NewProvider(dialect, db, sub, opts...)
	if err != nil {
		return err
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return err
	}
	for _, r := range results {
		slog.Info("schema migrated", "source", r.Source.Path, "duration", r.Duration.String())
	}
	return nil
}

// baseline 处理"旧迁移器建好的库"：结构已经是终态，只补版本记录。
//
// 只支持从旧终值（legacySchemaVersion）迁移；更早的中间版本结构不同，
// 逐版本补 DDL 的风险大于收益，直接报错让人工处理。
func baseline(ctx context.Context, db *sql.DB, st database.Store, dialect goose.Dialect, expected int) error {
	legacy, err := legacyVersion(db, dialect)
	if err != nil {
		return err
	}
	if legacy == 0 {
		return nil // 空库：让 goose 从头执行
	}
	if legacy != legacySchemaVersion {
		hint := "re-create the database or migrate it with an older release first"
		if dialect == goose.DialectSQLite3 {
			hint = "delete the local database file (development data) or migrate it with an older release first"
		}
		return fmt.Errorf("database was created by the legacy migrator at version %d, which cannot be baselined automatically (supported: 0 or %d): %s", legacy, legacySchemaVersion, hint)
	}
	for v := int64(1); v <= int64(expected); v++ {
		if err := st.Insert(ctx, db, database.InsertRequest{Version: v}); err != nil {
			return fmt.Errorf("baseline migration %d: %w", v, err)
		}
	}
	slog.Info("schema baselined from legacy migrator", "legacy_version", legacy, "migrations", expected)
	return nil
}

// legacyVersion 读取旧迁移器留下的版本标记。
func legacyVersion(db *sql.DB, dialect goose.Dialect) (int, error) {
	if dialect == goose.DialectPostgres {
		ok, err := hasTable(db, "schema_version")
		if err != nil || !ok {
			return 0, err
		}
		var v int
		if err := db.QueryRow(`SELECT version FROM schema_version WHERE singleton=true`).Scan(&v); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, nil
			}
			return 0, err
		}
		return v, nil
	}
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

// hasTable 报告当前库里是否存在某张表（两种方言）。
func hasTable(db *sql.DB, name string) (bool, error) {
	q := `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`
	if IsPostgres(db) {
		q = `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=?`
	}
	var n int
	if err := db.QueryRow(q, name).Scan(&n); err != nil {
		return false, err
	}
	return n == 1, nil
}

// schemaSource 返回要交给 goose 的文件系统。PostgreSQL 脚本里带部署占位符
// （{owner} 控制登录、{role} 运行角色），在内存里替换后再交给 goose，
// 避免把角色名写死在仓库里。
func schemaSource(dialect goose.Dialect, root, owner, role string) fs.FS {
	var base embed.FS
	if dialect == goose.DialectPostgres {
		base = pgMigrations
	} else {
		base = sqliteMigrations
	}
	dir := "schema/" + root
	files := map[string][]byte{}
	entries, err := fs.ReadDir(base, dir)
	if err != nil {
		return base // 让 goose 报出同样的错误
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		body, err := fs.ReadFile(base, dir+"/"+e.Name())
		if err != nil {
			continue
		}
		if owner != "" || role != "" {
			body = []byte(strings.NewReplacer("{owner}", owner, "{role}", role).Replace(string(body)))
		}
		files[e.Name()] = body
	}
	return memFS(files)
}

// autoMigrateEnabled 允许部署时关闭启动迁移（例如由独立迁移 Job 负责）。
// 默认开启：新环境只要一个二进制就能起来。
func autoMigrateEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("TABOO_AUTO_MIGRATE"))) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

// memFS 是把内存里的文件当成 fs.FS：只读、扁平，够 goose 使用。
// 实现 ReadDirFS，因为 goose 用 fs.WalkDir 收集迁移脚本。
type memFS map[string][]byte

func (m memFS) sortedNames() []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (m memFS) Open(name string) (fs.File, error) {
	if name == "." || name == "" {
		return &memDir{names: m.sortedNames()}, nil
	}
	b, ok := m[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &memFile{name: name, r: bytes.NewReader(b), size: int64(len(b))}, nil
}

func (m memFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "." && name != "" {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	names := m.sortedNames()
	entries := make([]fs.DirEntry, 0, len(names))
	for _, n := range names {
		entries = append(entries, fs.FileInfoToDirEntry(memFileInfo{name: n, size: int64(len(m[n]))}))
	}
	return entries, nil
}

type memDir struct{ names []string }

func (d *memDir) Stat() (fs.FileInfo, error) { return memFileInfo{name: ".", isDir: true}, nil }
func (d *memDir) Read([]byte) (int, error)   { return 0, io.EOF }
func (d *memDir) Close() error               { return nil }
func (d *memDir) ReadDir(n int) ([]fs.DirEntry, error) {
	entries := make([]fs.DirEntry, 0, len(d.names))
	for _, name := range d.names {
		entries = append(entries, fs.FileInfoToDirEntry(memFileInfo{name: name}))
	}
	return entries, nil
}

type memFile struct {
	name string
	r    *bytes.Reader
	size int64
}

func (f *memFile) Stat() (fs.FileInfo, error) { return memFileInfo{name: f.name, size: f.size}, nil }
func (f *memFile) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if errors.Is(err, io.EOF) && n > 0 {
		return n, nil
	}
	return n, err
}
func (f *memFile) Close() error { return nil }

type memFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (i memFileInfo) Name() string       { return i.name }
func (i memFileInfo) Size() int64        { return i.size }
func (i memFileInfo) Mode() fs.FileMode  { return 0o444 }
func (i memFileInfo) ModTime() time.Time { return time.Time{} }
func (i memFileInfo) IsDir() bool        { return i.isDir }
func (i memFileInfo) Sys() any           { return nil }

// PendingMigrations 返回尚未应用的脚本名，供运维命令展示（只读）。
func PendingMigrations(db *sql.DB) ([]string, error) {
	applied, err := Version(db)
	if err != nil {
		return nil, err
	}
	dialect, dir := goose.DialectSQLite3, "schema/sqlite"
	if IsPostgres(db) {
		dialect, dir = goose.DialectPostgres, "schema/pg"
	}
	base := sqliteMigrations
	if dialect == goose.DialectPostgres {
		base = pgMigrations
	}
	entries, err := base.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var pending []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		v, convErr := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0])
		if convErr != nil {
			return nil, fmt.Errorf("migration %s: name must start with a version number", e.Name())
		}
		if v > applied {
			pending = append(pending, e.Name())
		}
	}
	return pending, nil
}

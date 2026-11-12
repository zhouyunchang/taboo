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
// 迁移只向前：库里的版本高于本二进制时拒绝启动，不做降级。
// 不支持从早期自研迁移器的库升级——结构变更直接改脚本历史（项目未发布 1.0）。
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
	SchemaVersionPostgres = 15
	SchemaVersionSQLite   = 11

	// migrationsTable 是 goose 的版本表名。
	migrationsTable = "goose_db_version"
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
	if int(latest) > expected {
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

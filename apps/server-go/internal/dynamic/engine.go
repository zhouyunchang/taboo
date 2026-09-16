// 动态密钥引擎（M4 #9）—— SecretEngine 插件点（设计文档 §11.3：v1 不做大而全，但留好接口）
// PostgreSQL 实现：CREATE USER ... VALID UNTIL + 最小只读授权；到期 DROP USER 回收。
// Mock 实现：供冒烟/CI（TABOO_DYNAMIC_ENGINE=mock），记录 Grant/Revoke 调用。
package dynamic

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// Engine 动态密钥引擎：为每个 lease 在目标库创建/回收短期账号
type Engine interface {
	Type() string
	// Grant 创建（或续期）账号：username/password 由 taboo 生成，ttl 决定 VALID UNTIL
	Grant(ctx context.Context, username, password string, ttl time.Duration) error
	// Revoke 回收账号：终止连接 → 回收权限 → DROP USER
	Revoke(ctx context.Context, username string) error
}

// EngineFactory 由连接串构造引擎（postgres: postgres://user:pass@host:5432/db?sslmode=disable）
type EngineFactory func(connString string) (Engine, error)

// Factory 可替换（mock 注入）；默认 PostgreSQL
var Factory EngineFactory = PostgresFactory

func PostgresFactory(connString string) (Engine, error) {
	db, err := sql.Open("postgres", connString)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	db.SetConnMaxLifetime(time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	return &Postgres{db: db, database: databaseName(connString)}, nil
}

// databaseName 从连接串取库名（默认 postgres）
func databaseName(conn string) string {
	// postgres://user:pass@host:5432/dbname?...
	rest := conn
	if i := strings.LastIndex(rest, "/"); i >= 0 {
		rest = rest[i+1:]
	} else {
		return "postgres"
	}
	if i := strings.Index(rest, "?"); i >= 0 {
		rest = rest[:i]
	}
	if rest == "" {
		return "postgres"
	}
	return rest
}

// Postgres 最小只读授权：CONNECT + USAGE + SELECT（含未来表）
type Postgres struct {
	db       *sql.DB
	database string
}

func (p *Postgres) Type() string { return "postgres" }

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func quoteLit(s string) string { return `'` + strings.ReplaceAll(s, `'`, `''`) + `'` }

func (p *Postgres) Grant(ctx context.Context, username, password string, ttl time.Duration) error {
	validUntil := time.Now().Add(ttl).UTC().Format("2006-01-02 15:04:05")
	stmts := []string{
		fmt.Sprintf(`CREATE USER %s WITH PASSWORD %s VALID UNTIL %s`, quoteIdent(username), quoteLit(password), quoteLit(validUntil)),
		fmt.Sprintf(`GRANT CONNECT ON DATABASE %s TO %s`, quoteIdent(p.database), quoteIdent(username)),
		fmt.Sprintf(`GRANT USAGE ON SCHEMA public TO %s`, quoteIdent(username)),
		fmt.Sprintf(`GRANT SELECT ON ALL TABLES IN SCHEMA public TO %s`, quoteIdent(username)),
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO %s`, quoteIdent(username)),
	}
	for _, s := range stmts {
		if _, err := p.db.ExecContext(ctx, s); err != nil {
			// CREATE USER 已存在（续期语义）：改密码 + 延长 VALID UNTIL
			if strings.Contains(err.Error(), "already exists") {
				alt := fmt.Sprintf(`ALTER USER %s WITH PASSWORD %s VALID UNTIL %s`, quoteIdent(username), quoteLit(password), quoteLit(validUntil))
				if _, err2 := p.db.ExecContext(ctx, alt); err2 != nil {
					return fmt.Errorf("alter user: %w", err2)
				}
				continue
			}
			return fmt.Errorf("grant stmt %q: %w", s, err)
		}
	}
	return nil
}

func (p *Postgres) Revoke(ctx context.Context, username string) error {
	u := quoteIdent(username)
	pre := []string{
		// 终止现有连接
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usename = $1 AND pid <> pg_backend_pid()`,
		fmt.Sprintf(`REVOKE ALL PRIVILEGES ON DATABASE %s FROM %s`, quoteIdent(p.database), u),
		fmt.Sprintf(`REVOKE ALL PRIVILEGES ON SCHEMA public FROM %s`, u),
		fmt.Sprintf(`REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM %s`, u),
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM %s`, u),
	}
	for i, s := range pre {
		var err error
		if i == 0 {
			_, err = p.db.ExecContext(ctx, s, username) // 仅终止连接语句带 $1 占位
		} else {
			_, err = p.db.ExecContext(ctx, s)
		}
		if err != nil && !strings.Contains(err.Error(), "does not exist") {
			return fmt.Errorf("revoke pre %q: %w", s, err)
		}
	}
	if _, err := p.db.ExecContext(ctx, fmt.Sprintf(`DROP USER IF EXISTS %s`, u)); err != nil {
		return fmt.Errorf("drop user: %w", err)
	}
	return nil
}

// ---------- Mock（冒烟/CI） ----------

// MockEngine 记录调用，不触达真实数据库
type MockEngine struct {
	Granted      map[string]int // username → Grant 次数
	Revoked      map[string]bool
	LastGrantTTL time.Duration
}

func NewMockEngine() *MockEngine {
	return &MockEngine{Granted: map[string]int{}, Revoked: map[string]bool{}}
}

func (m *MockEngine) Type() string { return "postgres(mock)" }

func (m *MockEngine) Grant(_ context.Context, username, password string, ttl time.Duration) error {
	m.Granted[username]++
	m.LastGrantTTL = ttl
	delete(m.Revoked, username)
	return nil
}

func (m *MockEngine) Revoke(_ context.Context, username string) error {
	m.Revoked[username] = true
	return nil
}

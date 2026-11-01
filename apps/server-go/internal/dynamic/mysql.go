package dynamic

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// MySQL 短期只读账号。PASSWORD EXPIRE 的粒度是天，精确到期由 taboo 的 lease worker DROP USER。
type MySQL struct {
	db       *sql.DB
	database string
}

func MySQLFactory(connString string) (Engine, error) {
	dsn, database, err := mysqlDSN(connString)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	db.SetConnMaxLifetime(time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, errors.New("database connection failed")
	}
	return &MySQL{db: db, database: database}, nil
}

func (m *MySQL) Close() error { return m.db.Close() }

func (m *MySQL) Type() string { return "mysql" }

func mysqlQuoteIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

func mysqlUser(username string) string {
	return mysqlQuoteIdent(username) + "@" + mysqlQuoteIdent("%")
}

func expireDays(ttl time.Duration) int {
	if ttl < time.Second {
		ttl = time.Second
	}
	days := int((ttl + 24*time.Hour - 1) / (24 * time.Hour))
	if days < 1 {
		days = 1
	}
	if days > 365 {
		days = 365
	}
	return days
}

func (m *MySQL) Grant(ctx context.Context, username, password string, ttl time.Duration) error {
	days := expireDays(ttl)
	user := mysqlUser(username)
	create := fmt.Sprintf("CREATE USER %s IDENTIFIED BY ? PASSWORD EXPIRE INTERVAL %d DAY", user, days)
	if _, err := m.db.ExecContext(ctx, create, password); err != nil {
		var me *mysql.MySQLError
		if !errors.As(err, &me) || me.Number != 1396 {
			if !strings.Contains(strings.ToLower(err.Error()), "already exists") {
				return errors.New("database grant failed")
			}
		}
		alter := fmt.Sprintf("ALTER USER %s IDENTIFIED BY ? PASSWORD EXPIRE INTERVAL %d DAY", user, days)
		if _, err2 := m.db.ExecContext(ctx, alter, password); err2 != nil {
			return errors.New("database role update failed")
		}
	}
	grant := fmt.Sprintf("GRANT SELECT ON %s.* TO %s", mysqlQuoteIdent(m.database), user)
	if _, err := m.db.ExecContext(ctx, grant); err != nil {
		return errors.New("database grant failed")
	}
	return nil
}

func (m *MySQL) Revoke(ctx context.Context, username string) error {
	// 先锁账号再 KILL 会话，避免 KILL 与 DROP 之间重新登录；DROP USER 不会断开已有会话。
	_, _ = m.db.ExecContext(ctx, fmt.Sprintf("ALTER USER IF EXISTS %s ACCOUNT LOCK", mysqlUser(username)))
	rows, err := m.db.QueryContext(ctx, `SELECT ID FROM information_schema.PROCESSLIST WHERE USER = ?`, username)
	if err == nil {
		var ids []int64
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			_, _ = m.db.ExecContext(ctx, fmt.Sprintf("KILL %d", id))
		}
	}
	if _, err = m.db.ExecContext(ctx, fmt.Sprintf("DROP USER IF EXISTS %s", mysqlUser(username))); err != nil {
		return errors.New("database role deletion failed")
	}
	return nil
}

// mysqlDSN 接受 mysql://user:pass@host:3306/db 或 go-sql-driver 的 DSN。
func mysqlDSN(conn string) (string, string, error) {
	conn = strings.TrimSpace(conn)
	if conn == "" {
		return "", "", errors.New("empty mysql connection")
	}
	cfg := mysql.NewConfig()
	cfg.InterpolateParams = true
	cfg.ParseTime = true
	cfg.Params = map[string]string{"charset": "utf8mb4"}
	cfg.Collation = "utf8mb4_general_ci"
	if strings.Contains(conn, "://") {
		u, err := url.Parse(conn)
		if err != nil || u.Scheme != "mysql" || u.User == nil {
			return "", "", errors.New("invalid mysql url")
		}
		cfg.User = u.User.Username()
		cfg.Passwd, _ = u.User.Password()
		cfg.Net = "tcp"
		host := u.Hostname()
		port := u.Port()
		if port == "" {
			port = "3306"
		}
		if host == "" {
			return "", "", errors.New("invalid mysql url")
		}
		cfg.Addr = net.JoinHostPort(host, port)
		cfg.DBName = strings.Trim(strings.TrimPrefix(u.Path, "/"), "/")
		if cfg.DBName == "" {
			return "", "", errors.New("mysql database required")
		}
		q := u.Query()
		if strings.EqualFold(q.Get("multiStatements"), "true") {
			return "", "", errors.New("multiStatements not allowed")
		}
		if tlsMode := q.Get("tls"); tlsMode != "" {
			cfg.TLSConfig = tlsMode
		}
		return hardenMySQL(cfg), cfg.DBName, nil
	}
	parsed, err := mysql.ParseDSN(conn)
	if err != nil || parsed.DBName == "" {
		return "", "", errors.New("invalid mysql dsn")
	}
	if parsed.MultiStatements {
		return "", "", errors.New("multiStatements not allowed")
	}
	if parsed.Net != "tcp" || parsed.Addr == "" {
		return "", "", errors.New("mysql dsn must use tcp(host:port)")
	}
	// 只拷贝白名单字段：丢弃 allowAllFiles（LOAD DATA LOCAL 读服务器本地文件）、
	// allowCleartextPasswords/allowOldPasswords、任意会话参数等危险开关。
	cfg.User, cfg.Passwd, cfg.Net, cfg.Addr, cfg.DBName = parsed.User, parsed.Passwd, "tcp", parsed.Addr, parsed.DBName
	cfg.TLSConfig = parsed.TLSConfig
	return hardenMySQL(cfg), cfg.DBName, nil
}

func hardenMySQL(cfg *mysql.Config) string {
	cfg.AllowAllFiles = false
	cfg.AllowCleartextPasswords = false
	cfg.AllowOldPasswords = false
	cfg.AllowFallbackToPlaintext = false
	cfg.MultiStatements = false
	cfg.InterpolateParams = true
	return cfg.FormatDSN()
}

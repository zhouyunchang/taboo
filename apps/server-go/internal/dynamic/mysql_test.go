package dynamic

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestMySQLDSN(t *testing.T) {
	dsn, db, err := mysqlDSN("mysql://app:p%40ss@db.internal:3307/leased?tls=skip-verify")
	if err != nil || db != "leased" || !strings.Contains(dsn, "@tcp(db.internal:3307)/leased") || !strings.Contains(dsn, "tls=skip-verify") {
		t.Fatalf("dsn=%s db=%s err=%v", dsn, db, err)
	}
	if _, _, err = mysqlDSN("mysql://app@db/leased?multiStatements=true"); err == nil {
		t.Fatal("multiStatements accepted")
	}
	if _, _, err = mysqlDSN("postgres://a:b@h/db"); err == nil {
		t.Fatal("postgres url accepted as mysql")
	}
}

func TestMySQLGrantAndRevoke(t *testing.T) {
	dsn := os.Getenv("TABOO_TEST_MYSQL_DSN")
	var cleanup func()
	if dsn == "" {
		dsn, cleanup = startMySQL(t)
		defer cleanup()
	}
	engine, err := MySQLFactory(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c, ok := engine.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	})
	if engine.Type() != "mysql" {
		t.Fatal(engine.Type())
	}
	ctx := context.Background()
	user := "taboo_" + strings.ReplaceAll(t.Name(), "/", "_")
	if len(user) > 32 {
		user = user[:32]
	}
	user = strings.ToLower(user)
	pass := "p@ss'word\\x"
	if err = engine.Grant(ctx, user, pass, 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	if err = engine.Grant(ctx, user, pass, 3*time.Hour); err != nil {
		t.Fatal("renew", err)
	}
	leased, err := openLeased(dsn, user, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer leased.Close()
	var one int
	if err = leased.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil || one != 1 {
		t.Fatal(err)
	}
	if _, err = leased.ExecContext(ctx, `CREATE TABLE should_fail(id INT)`); err == nil {
		t.Fatal("lease could create a table")
	}
	if err = engine.Revoke(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err = engine.Revoke(ctx, user); err != nil {
		t.Fatal("second revoke", err)
	}
	if err = leased.PingContext(ctx); err == nil {
		t.Fatal("revoked user still connected")
	}
}

func openLeased(adminDSN, user, pass string) (*sql.DB, error) {
	dsn, database, err := mysqlDSN(adminDSN)
	if err != nil {
		return nil, err
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	cfg.User = user
	cfg.Passwd = pass
	cfg.DBName = database
	cfg.InterpolateParams = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func startMySQL(t *testing.T) (string, func()) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is required for the MySQL engine test")
	}
	name := fmt.Sprintf("taboo-mysql-%d", time.Now().UnixNano())
	cmd := exec.Command("docker", "run", "-d", "--name", name,
		"-e", "MYSQL_ROOT_PASSWORD=fixture-root",
		"-e", "MYSQL_DATABASE=leasedb",
		"-p", "127.0.0.1::3306",
		"mysql:8.4")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v %s", err, out)
	}
	cleanup := func() {
		_ = exec.Command("docker", "rm", "-f", name).Run()
	}
	portOut, err := exec.Command("docker", "port", name, "3306").CombinedOutput()
	if err != nil {
		cleanup()
		t.Fatalf("docker port: %v %s", err, portOut)
	}
	portLine := strings.TrimSpace(string(portOut))
	if i := strings.LastIndex(portLine, ":"); i >= 0 {
		portLine = portLine[i+1:]
	}
	deadline := time.Now().Add(90 * time.Second)
	var ready bool
	for time.Now().Before(deadline) {
		ping := exec.Command("docker", "exec", name, "mysqladmin", "--protocol=tcp", "-uroot", "-pfixture-root", "ping")
		if err = ping.Run(); err == nil {
			ready = true
			break
		}
		time.Sleep(time.Second)
	}
	if !ready {
		logs, _ := exec.Command("docker", "logs", "--tail", "40", name).CombinedOutput()
		cleanup()
		t.Fatalf("mysql did not become ready\n%s", logs)
	}
	return fmt.Sprintf("mysql://root:fixture-root@127.0.0.1:%s/leasedb", portLine), cleanup
}

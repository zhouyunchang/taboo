// 配置加载 —— 环境变量契约与 Node MVP 完全一致
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL         string
	DatabaseRuntimeRole string
	PublicOrigin        string // Exact browser origin for OIDC callbacks.
	IssuerAllowlist     []string
	SessionProfile      string // Explicit BOUNDED profile; no implicit online assurance.
	Port                string // TABOO_PORT / PORT，默认 7100
	DataDir             string // TABOO_DATA_DIR，默认 <exe>/../../data（开发期）或 cwd/data
	MasterKey           string // TABOO_MASTER_KEY（hex/base64 32B）；空则读/生成 data/master.key
	JWTSecret           string // TABOO_JWT_SECRET；空则随机（重启会话失效，并打印告警）
	CORS                string // TABOO_CORS_ORIGIN，默认 *
	WebDist             string // TABOO_WEB_DIST，默认 <module>/web（embed 优先）
	LoginRate           int    // TABOO_LOGIN_RATE_LIMIT，每 IP 每窗口登录类请求上限，默认 5
	DisableRegister     bool   // TABOO_DISABLE_REGISTER：关闭邮箱注册（Keycloak 等外部用户源）
	DisablePassword     bool   // TABOO_DISABLE_PASSWORD：关闭密码登录，仅外部 Realm
}

func Load() *Config {
	if os.Getenv("TABOO_DATABASE_URL") != "" && (os.Getenv("TABOO_MASTER_KEY") == "" || os.Getenv("TABOO_JWT_SECRET") == "") {
		log.Fatal("PostgreSQL requires explicit TABOO_MASTER_KEY and TABOO_JWT_SECRET shared by every instance")
	}

	c := &Config{
		DatabaseURL:         os.Getenv("TABOO_DATABASE_URL"),
		DatabaseRuntimeRole: os.Getenv("TABOO_DATABASE_RUNTIME_ROLE"),
		IssuerAllowlist:     strings.Split(os.Getenv("TABOO_ISSUER_ALLOWLIST")+","+os.Getenv("TABOO_OIDC_ISSUER_ALLOWLIST"), ","),
		SessionProfile:      os.Getenv("TABOO_OIDC_SESSION_PROFILE"),
		PublicOrigin:        os.Getenv("TABOO_PUBLIC_ORIGIN"),
		Port:                getenv("TABOO_PORT", getenv("PORT", "7100")),
		DataDir:             getenv("TABOO_DATA_DIR", "data"),
		CORS:                getenv("TABOO_CORS_ORIGIN", "*"),
		LoginRate:           5,
	}
	if v := os.Getenv("TABOO_LOGIN_RATE_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.LoginRate = n
		}
	}
	c.DisableRegister = envBool("TABOO_DISABLE_REGISTER")
	c.DisablePassword = envBool("TABOO_DISABLE_PASSWORD")
	if _, err := os.Stat(c.DataDir); os.IsNotExist(err) {
		_ = os.MkdirAll(c.DataDir, 0o700)
	}
	abs, err := filepath.Abs(c.DataDir)
	if err == nil {
		c.DataDir = abs
	}

	c.MasterKey = os.Getenv("TABOO_MASTER_KEY")
	if c.MasterKey == "" {
		key, err := loadOrCreateMasterKey(c.DataDir)
		if err != nil {
			log.Fatal(err)
		}
		c.MasterKey = key
	}

	c.JWTSecret = os.Getenv("TABOO_JWT_SECRET")
	if c.JWTSecret == "" {
		buf := make([]byte, 32)
		_, _ = rand.Read(buf)
		c.JWTSecret = hex.EncodeToString(buf)
		log.Println("[taboo] TABOO_JWT_SECRET not set, using ephemeral secret (sessions reset on restart). Set it for production.")
	}
	return c
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envBool(k string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(k))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// Never replace root material when an existing database may depend on it.
func loadOrCreateMasterKey(dir string) (string, error) {
	keyFile := filepath.Join(dir, "master.key")
	raw, err := os.ReadFile(keyFile)
	if err == nil {
		return string(raw), nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("original master key cannot be read")
	}
	if _, err := os.Stat(filepath.Join(dir, "taboo.db")); err == nil || !os.IsNotExist(err) {
		return "", fmt.Errorf("existing database requires its original master key; restore it or set TABOO_MASTER_KEY")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	raw = []byte(hex.EncodeToString(buf))
	f, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, err = f.Write(raw)
	closed := f.Close()
	if err != nil {
		return "", err
	}
	if closed != nil {
		return "", closed
	}
	return string(raw), nil
}

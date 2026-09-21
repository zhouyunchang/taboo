// 配置加载 —— 环境变量契约与 Node MVP 完全一致
package config

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Port            string // TABOO_PORT / PORT，默认 7100
	DataDir         string // TABOO_DATA_DIR，默认 <exe>/../../data（开发期）或 cwd/data
	MasterKey       string // TABOO_MASTER_KEY（hex/base64 32B）；空则读/生成 data/master.key
	JWTSecret       string // TABOO_JWT_SECRET；空则随机（重启会话失效，并打印告警）
	CORS            string // TABOO_CORS_ORIGIN，默认 *
	WebDist         string // TABOO_WEB_DIST，默认 <module>/web（embed 优先）
	LoginRate       int    // TABOO_LOGIN_RATE_LIMIT，每 IP 每窗口登录类请求上限，默认 5
	DisableRegister bool   // TABOO_DISABLE_REGISTER：关闭邮箱注册（Keycloak 等外部用户源）
	DisablePassword bool   // TABOO_DISABLE_PASSWORD：关闭密码登录，仅外部 Realm
}

func Load() *Config {
	c := &Config{
		Port:      getenv("TABOO_PORT", getenv("PORT", "7100")),
		DataDir:   getenv("TABOO_DATA_DIR", "data"),
		CORS:      getenv("TABOO_CORS_ORIGIN", "*"),
		LoginRate: 5,
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
		keyFile := filepath.Join(c.DataDir, "master.key")
		raw, err := os.ReadFile(keyFile)
		if err != nil {
			buf := make([]byte, 32)
			if _, err := rand.Read(buf); err != nil {
				log.Fatalf("generate master key: %v", err)
			}
			raw = []byte(hex.EncodeToString(buf))
			if err := os.WriteFile(keyFile, raw, 0o600); err != nil {
				log.Fatalf("write master key: %v", err)
			}
			log.Printf("[taboo] master key generated at %s (keep it safe, back it up)", keyFile)
		}
		c.MasterKey = string(raw)
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

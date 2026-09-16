// 信封加密（Envelope Encryption）+ 密码哈希 + JWT —— 对齐设计文档 §3.2 / §8
//
//	Root Key (KEK, 主密钥)        文件/环境变量/KMS（KMS 适配层后续迭代）
//	  └─ Org Data Key (DEK)        加密后存 DB，惰性解密入内存
//	     └─ Secret Value           AES-256-GCM，nonce 12B 随机绝不复用
//
// 密码哈希：Argon2id（m=64MB, t=3, p=4）；保留 scrypt 校验路径，
// 登录成功后透明重哈希完成迁移（issue #2 验收项）。
// JWT：HS256，TTL 15min。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/scrypt"
)

const (
	KeyLen   = 32
	NonceLen = 12
)

// ---------- Root Key ----------

func ParseMasterKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := hex.DecodeString(s); err == nil && len(b) == KeyLen {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == KeyLen {
		return b, nil
	}
	return nil, errors.New("master key must be 32 bytes (hex or base64)")
}

// ---------- AES-256-GCM 原语（KEK 包 DEK、DEK 包明文共用） ----------

// Encrypt 输出: nonce(12) | tag(16) | ciphertext，base64 编码
func Encrypt(key []byte, plaintext string) (string, error) {
	nonce := make([]byte, NonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	buf := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	out := append(append([]byte{}, nonce...), buf...)
	return base64.StdEncoding.EncodeToString(out), nil
}

func Decrypt(key []byte, payload string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(raw) < NonceLen+16 {
		return "", errors.New("invalid ciphertext")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	pt, err := gcm.Open(nil, raw[:NonceLen], raw[NonceLen:], nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// ---------- 组织 DEK 惰性缓存 ----------

type DEKCache struct {
	mu   sync.Mutex
	deks map[string][]byte // orgID -> DEK(32B)
}

func NewDEKCache() *DEKCache { return &DEKCache{deks: map[string][]byte{}} }

func (c *DEKCache) Get(masterKey []byte, orgID, dekEncrypted string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if dek, ok := c.deks[orgID]; ok {
		return dek, nil
	}
	hexDEK, err := Decrypt(masterKey, dekEncrypted)
	if err != nil {
		return nil, fmt.Errorf("unwrap org DEK: %w", err)
	}
	dek, err := hex.DecodeString(hexDEK)
	if err != nil || len(dek) != KeyLen {
		return nil, errors.New("invalid org DEK")
	}
	c.deks[orgID] = dek
	return dek, nil
}

// ---------- 密码哈希：Argon2id（新）+ scrypt（旧校验/迁移） ----------

type Params struct {
	Memory  uint32
	Time    uint32
	Threads uint8
	SaltLen int
	KeyLen  uint32
}

var Argon2Params = Params{Memory: 64 * 1024, Time: 3, Threads: 4, SaltLen: 16, KeyLen: 32}

func HashPassword(password string) (string, error) {
	salt := make([]byte, Argon2Params.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt,
		Argon2Params.Time, Argon2Params.Memory, Argon2Params.Threads, Argon2Params.KeyLen)
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		Argon2Params.Memory, Argon2Params.Time, Argon2Params.Threads,
		hex.EncodeToString(salt), hex.EncodeToString(hash)), nil
}

// CheckPassword 返回密码是否正确、以及存储哈希是否为旧 scrypt（需要重哈希）
func CheckPassword(password, stored string) (ok bool, needsRehash bool, err error) {
	parts := strings.Split(stored, "$")
	if len(parts) < 2 {
		return false, false, errors.New("malformed password hash")
	}
	switch parts[0] {
	case "argon2id":
		// argon2id$v=19$m=65536,t=3,p=4$salt$hash
		if len(parts) != 6 {
			return false, false, errors.New("malformed argon2id hash")
		}
		var m, t uint32
		var p uint8
		if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
			return false, false, err
		}
		salt, err := hex.DecodeString(parts[4])
		if err != nil {
			return false, false, err
		}
		want, err := hex.DecodeString(parts[5])
		if err != nil {
			return false, false, err
		}
		hash := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
		return subtle.ConstantTimeCompare(hash, want) == 1, false, nil
	case "scrypt":
		// scrypt$N$r$p$salt$hash（Node MVP 遗留格式）
		if len(parts) != 6 {
			return false, false, errors.New("malformed scrypt hash")
		}
		var N int
		var r, p int
		if _, err := fmt.Sscanf(parts[1], "%d", &N); err != nil {
			return false, false, err
		}
		if _, err := fmt.Sscanf(parts[2], "%d", &r); err != nil {
			return false, false, err
		}
		if _, err := fmt.Sscanf(parts[3], "%d", &p); err != nil {
			return false, false, err
		}
		salt, err := hex.DecodeString(parts[4])
		if err != nil {
			return false, false, err
		}
		want, err := hex.DecodeString(parts[5])
		if err != nil {
			return false, false, err
		}
		hash, err := scrypt.Key([]byte(password), salt, N, r, p, len(want))
		if err != nil {
			return false, false, err
		}
		return subtle.ConstantTimeCompare(hash, want) == 1, true, nil
	default:
		return false, false, errors.New("unknown hash algorithm")
	}
}

// ---------- JWT (HS256) ----------

func b64u(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func SignJWT(subject, email, secret string, ttl time.Duration) (string, error) {
	header := b64u([]byte(`{"alg":"HS256","typ":"JWT"}`))
	now := time.Now()
	body := b64u([]byte(fmt.Sprintf(`{"sub":%q,"email":%q,"iat":%d,"exp":%d}`,
		subject, email, now.Unix(), now.Add(ttl).Unix())))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(header + "." + body))
	return header + "." + body + "." + b64u(mac.Sum(nil)), nil
}

func VerifyJWT(token, secret string) (sub string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("malformed token")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || subtle.ConstantTimeCompare(mac.Sum(nil), sig) != 1 {
		return "", errors.New("bad signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var claims struct {
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", err
	}
	if claims.Exp < time.Now().Unix() {
		return "", errors.New("token expired")
	}
	return claims.Sub, nil
}

func SHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

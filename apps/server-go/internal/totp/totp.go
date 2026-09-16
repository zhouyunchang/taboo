// TOTP 算法（RFC 6238）：HMAC-SHA1、30s 步长、6 位动态口令
// 不引入第三方 otp 库，stdlib 实现；服务端校验窗口 ±1 步并做防重放（last_step 单调递增）
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	StepSeconds = int64(30)
	Digits      = 6
	SecretBytes = 20 // 160bit，RFC 4226 推荐
)

// GenerateSecret 生成 base32（无填充、大写）随机密钥
func GenerateSecret() (string, error) {
	b := make([]byte, SecretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// Code 计算指定步的 TOTP 码
func Code(secret string, step int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	return fmt.Sprintf("%0*d", Digits, bin%1_000_000), nil
}

// NowStep 当前时间步
func NowStep() int64 { return time.Now().Unix() / StepSeconds }

// Verify 在 ±1 窗口内校验 code；返回成功的时间步（用于防重放：必须 > lastStep）
func Verify(secret, code string, lastStep int64) (int64, bool) {
	now := NowStep()
	for _, s := range []int64{now - 1, now, now + 1} {
		if s <= lastStep {
			continue
		}
		c, err := Code(secret, s)
		if err != nil || c != code {
			continue
		}
		return s, true
	}
	return 0, false
}

// OTPAuthURL 标准迁移 URI（authenticator 扫码 / 手动录入）
func OTPAuthURL(issuer, account, secret string) string {
	u := url.URL{Scheme: "otpauth", Host: "totp", Path: "/" + issuer + ":" + account}
	q := u.Query()
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", strconv.Itoa(Digits))
	q.Set("period", strconv.FormatInt(StepSeconds, 10))
	u.RawQuery = q.Encode()
	return u.String()
}

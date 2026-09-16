// TOTP 2FA 服务（M2 #4）—— 自助开启/关闭、登录二次验证、恢复码
//
//	POST /auth/totp/setup    （登录态）生成密钥 + otpauth URL + 二维码 PNG
//	POST /auth/totp/verify   （登录态）校验并激活，签发恢复码（仅展示一次）
//	POST /auth/totp/disable  （登录态 + 密码确认）关闭并清除密钥与恢复码
//	POST /auth/totp/login    （公开，限流）challenge + TOTP 码/恢复码 → 换正式 token
//
// 安全要点：secret 以 Master Key 加密落库；防重放（totp_last_step 单调递增）；
// 恢复码仅存 sha256 哈希、用后即废；challenge JWT 独立 typ=totp、TTL 5min、一次性语义（用完即过期由 exp 保证）。
package totp

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/skip2/go-qrcode"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

const (
	Issuer        = "taboo"
	challengeTTL  = 5 * time.Minute
	recoveryCount = 10
)

type Service struct {
	DB        *sql.DB
	MasterKey []byte
	JWTSecret string
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

// PublicRoutes 公开路由（/auth/totp/login，挂载点由 server 包限流）
func (s *Service) PublicRoutes(r chi.Router) {
	r.Post("/auth/totp/login", s.Login2FA)
}

// Routes 登录态路由（/auth/totp/*）
func (s *Service) Routes(r chi.Router) {
	r.Route("/auth/totp", func(r chi.Router) {
		r.Post("/setup", s.Setup)
		r.Post("/verify", s.VerifyActivate)
		r.Post("/disable", s.Disable)
	})
}

func (s *Service) audit(u *auth.Actor, action, resource string, r *http.Request) {
	var orgID string
	if err := s.DB.QueryRow(`SELECT org_id FROM org_members WHERE user_id = ? LIMIT 1`, u.ID).Scan(&orgID); err == nil {
		auth.Audit(s.DB, orgID, u, action, resource, nil, clientIP(r))
	}
}

// Setup POST /auth/totp/setup —— 生成待激活密钥（旧 pending 直接覆盖）
func (s *Service) Setup(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	secret, err := GenerateSecret()
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	enc, err := tc.Encrypt(s.MasterKey, secret)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if _, err := s.DB.Exec(`UPDATE users SET totp_pending_enc = ? WHERE id = ?`, enc, u.ID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	otpauth := OTPAuthURL(Issuer, u.Email, secret)
	png, err := qrcode.Encode(otpauth, qrcode.Medium, 200)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	s.audit(u, "auth.totp.setup", "user/"+u.Email, r)
	writeJSON(w, 200, map[string]any{
		"secret":      secret,
		"otpauth_url": otpauth,
		"qr_png":      "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
	})
}

// VerifyActivate POST /auth/totp/verify {code} —— 校验 pending 密钥并激活，签发恢复码
func (s *Service) VerifyActivate(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	var b struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || strings.TrimSpace(b.Code) == "" {
		writeErr(w, apperr.InvalidInput)
		return
	}
	var enc string
	if err := s.DB.QueryRow(`SELECT totp_pending_enc FROM users WHERE id = ?`, u.ID).Scan(&enc); err != nil || enc == "" {
		writeErr(w, apperr.New(400, "NO_PENDING", "run setup first"))
		return
	}
	secret, err := tc.Decrypt(s.MasterKey, enc)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	step, ok := Verify(secret, strings.TrimSpace(b.Code), 0)
	if !ok {
		s.audit(u, "auth.totp.verify_fail", "user/"+u.Email, r)
		writeErr(w, apperr.New(401, "BAD_CODE", "invalid totp code"))
		return
	}
	codes, err := s.issueRecoveryCodes(u.ID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	// pending → active；激活即消费的码记入 last_step，杜绝同窗口复用
	if _, err := s.DB.Exec(`UPDATE users SET totp_secret_enc = totp_pending_enc, totp_pending_enc = NULL,
		totp_enabled = 1, totp_last_step = ? WHERE id = ?`, step, u.ID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	s.audit(u, "auth.totp.enable", "user/"+u.Email, r)
	writeJSON(w, 200, map[string]any{"enabled": true, "recovery_codes": codes})
}

// Disable POST /auth/totp/disable {password} —— 密码确认后关闭
func (s *Service) Disable(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	var b struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Password == "" {
		writeErr(w, apperr.InvalidInput)
		return
	}
	var hash string
	if err := s.DB.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, u.ID).Scan(&hash); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	ok, _, _ := tc.CheckPassword(b.Password, hash)
	if !ok {
		s.audit(u, "auth.totp.disable_fail", "user/"+u.Email, r)
		writeErr(w, apperr.BadCredentials)
		return
	}
	if _, err := s.DB.Exec(`UPDATE users SET totp_secret_enc = NULL, totp_pending_enc = NULL,
		totp_enabled = 0, totp_last_step = 0 WHERE id = ?`, u.ID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	_, _ = s.DB.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, u.ID)
	s.audit(u, "auth.totp.disable", "user/"+u.Email, r)
	writeJSON(w, 200, map[string]any{"enabled": false})
}

// Login2FA POST /auth/totp/login {challenge, code} —— 2FA 第二步换正式 token
// code 为 6 位 TOTP 动态码，或形如 xxxx-xxxx-xxxx 的恢复码（用后即废）
func (s *Service) Login2FA(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Challenge == "" || strings.TrimSpace(b.Code) == "" {
		writeErr(w, apperr.InvalidInput)
		return
	}
	claims, err := tc.VerifyJWT(b.Challenge, s.JWTSecret)
	if err != nil || claims["typ"] != "totp" {
		writeErr(w, apperr.New(401, "BAD_CHALLENGE", "invalid or expired challenge"))
		return
	}
	userID, _ := claims["sub"].(string)
	var email, name, enc string
	var enabled int
	var lastStep int64
	if err := s.DB.QueryRow(`SELECT email, name, totp_secret_enc, totp_enabled, totp_last_step
		FROM users WHERE id = ?`, userID).Scan(&email, &name, &enc, &enabled, &lastStep); err != nil || enabled != 1 {
		writeErr(w, apperr.New(401, "BAD_CHALLENGE", "2fa not enabled"))
		return
	}
	code := strings.TrimSpace(b.Code)
	if strings.Contains(code, "-") {
		if !s.consumeRecoveryCode(userID, code) {
			writeErr(w, apperr.New(401, "BAD_CODE", "invalid recovery code"))
			return
		}
	} else {
		secret, err := tc.Decrypt(s.MasterKey, enc)
		if err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
		step, ok := Verify(secret, code, lastStep)
		if !ok {
			writeErr(w, apperr.New(401, "BAD_CODE", "invalid totp code"))
			return
		}
		if _, err := s.DB.Exec(`UPDATE users SET totp_last_step = ? WHERE id = ?`, step, userID); err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
	}
	u := &auth.Actor{ID: userID, Email: email, Name: name, Kind: auth.KindUser}
	s.audit(u, "auth.login", "user/"+email, r)
	tk, err := auth.IssueTokens(s.DB, s.JWTSecret, u)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	writeJSON(w, 200, map[string]any{"user": u, "tokens": tk})
}

// issueRecoveryCodes 重新生成 10 个恢复码（xxxx-xxxx-xxxx），仅返回明文这一次
func (s *Service) issueRecoveryCodes(userID string) ([]string, error) {
	if _, err := s.DB.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	codes := make([]string, 0, recoveryCount)
	for i := 0; i < recoveryCount; i++ {
		var raw [9]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, err
		}
		c := strings.Join([]string{
			base32HexLower(raw[0:4]), base32HexLower(raw[4:6]), base32HexLower(raw[6:9]),
		}, "-")
		if _, err := s.DB.Exec(`INSERT INTO recovery_codes (id, user_id, code_hash) VALUES (?, ?, ?)`,
			tc.NewID(), userID, tc.SHA256(c)); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, nil
}

// consumeRecoveryCode 原子核销恢复码（UPDATE ... WHERE used_at IS NULL 防并发复用）
func (s *Service) consumeRecoveryCode(userID, code string) bool {
	res, err := s.DB.Exec(`UPDATE recovery_codes SET used_at = datetime('now')
		WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`, userID, tc.SHA256(code))
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

// base32HexLower 小写 base32（ Crockford 风格字母表，避免 0/O、1/I 混淆）
func base32HexLower(b []byte) string {
	const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"
	out := make([]byte, 0, len(b)*8/5+1)
	var buf uint32
	var nbits uint
	for _, x := range b {
		buf = buf<<8 | uint32(x)
		nbits += 8
		for nbits >= 5 {
			out = append(out, alphabet[(buf>>(nbits-5))&0x1f])
			nbits -= 5
		}
	}
	if nbits > 0 {
		out = append(out, alphabet[(buf<<(5-nbits))&0x1f])
	}
	return string(out)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	return r.RemoteAddr
}

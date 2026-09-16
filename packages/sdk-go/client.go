// taboo Go SDK —— 客户端、错误模型、认证（M3 #8）
package taboo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ApiError 服务端错误（统一 {code, message, details} 契约）
type ApiError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func (e *ApiError) Error() string { return fmt.Sprintf("taboo %d %s: %s", e.Status, e.Code, e.Message) }

// Client taboo API 客户端（非并发安全修改配置；Get/Post 等并发安全）
type Client struct {
	BaseURL    string
	HTTPClient *http.Client

	mu        sync.Mutex
	token     string
	refresh   string
	identity  *IdentityCredentials
	onRefresh func(token, refresh string) // 旋转回调（落盘持久化用）
}

// IdentityCredentials 机器身份凭证（临期自动重换）
type IdentityCredentials struct {
	ClientID     string
	ClientSecret string
	ExpiresAt    time.Time
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// WithToken 静态 token（如已持有的 JWT）
func (c *Client) WithToken(token string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
	return c
}

// WithUserTokens 用户 token 对；refresh 临期自动旋转
func (c *Client) WithUserTokens(access, refresh string, expiresIn time.Duration, onRefresh func(access, refresh string)) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = access
	c.refresh = refresh
	c.onRefresh = onRefresh
	return c
}

// WithIdentity 机器身份：每次调用前检查临期，自动用 client_credentials 重换
func (c *Client) WithIdentity(clientID, clientSecret string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.identity = &IdentityCredentials{ClientID: clientID, ClientSecret: clientSecret}
	return c
}

// ensureToken 返回可用 token；身份凭证临期（<30s）时自动重换
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.identity != nil && time.Until(c.identity.ExpiresAt) < 30*time.Second {
		p := fmt.Sprintf("%s/api/v1/identities/token", c.BaseURL)
		body, _ := json.Marshal(map[string]string{"client_id": c.identity.ClientID, "client_secret": c.identity.ClientSecret})
		req, _ := http.NewRequestWithContext(ctx, "POST", p, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := c.HTTPClient.Do(req)
		if err != nil {
			return "", err
		}
		defer res.Body.Close()
		var d struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int    `json:"expires_in"`
		}
		if err := json.NewDecoder(res.Body).Decode(&d); err != nil {
			return "", err
		}
		if res.StatusCode != 200 {
			return "", fmt.Errorf("identity token exchange failed: %d", res.StatusCode)
		}
		c.token = d.AccessToken
		c.identity.ExpiresAt = time.Now().Add(time.Duration(d.ExpiresIn) * time.Second)
	}
	return c.token, nil
}

func (c *Client) do(ctx context.Context, method, p string, body, out any) error {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return err
	}
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+p, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
	 ae := &ApiError{Status: res.StatusCode}
		_ = json.NewDecoder(res.Body).Decode(ae)
		if ae.Code == "" {
			ae.Code = "HTTP_" + fmt.Sprint(res.StatusCode)
		}
		return ae
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}
	if s, ok := out.(*string); ok {
		b, err := io.ReadAll(res.Body)
		*s = string(b)
		return err
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// q 组装 query string（跳过空值）
func q(v url.Values) string {
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

func vset(pairs ...string) url.Values {
	v := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			v.Set(pairs[i], pairs[i+1])
		}
	}
	return v
}

// ---------- 认证 ----------

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type TokenPair struct {
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	TokenType string `json:"tokenType"`
	ExpiresIn int    `json:"expiresIn"`
}

type AuthResponse struct {
	User   *User      `json:"user"`
	Tokens *TokenPair `json:"tokens"`
	// TOTP 2FA：密码通过但未二次验证
	TotpRequired bool   `json:"totp_required"`
	Challenge    string `json:"challenge"`
}

// Login 用户登录（未开 2FA 直接返回 tokens；已开启则返回 challenge，随后调 TotpLogin）
func (c *Client) Login(ctx context.Context, email, password string) (*AuthResponse, error) {
	var out AuthResponse
	err := c.do(ctx, "POST", "/api/v1/auth/login", map[string]string{"email": email, "password": password}, &out)
	return &out, err
}

// TotpLogin 2FA 第二步：challenge + 动态码/恢复码 → 正式 token
func (c *Client) TotpLogin(ctx context.Context, challenge, code string) (*AuthResponse, error) {
	var out AuthResponse
	err := c.do(ctx, "POST", "/api/v1/auth/totp/login", map[string]string{"challenge": challenge, "code": code}, &out)
	return &out, err
}

// Register 注册（自动创建个人组织 + 默认项目 + 三环境）
func (c *Client) Register(ctx context.Context, email, password, name string) (*AuthResponse, error) {
	var out AuthResponse
	err := c.do(ctx, "POST", "/api/v1/auth/register", map[string]string{"email": email, "password": password, "name": name}, &out)
	return &out, err
}

// Refresh 旋转 refresh token（旧 refresh 立即失效，需持久化新对）
func (c *Client) Refresh(ctx context.Context, refresh string) (*TokenPair, error) {
	var out TokenPair
	if err := c.do(ctx, "POST", "/api/v1/auth/refresh", map[string]string{"refresh": refresh}, &out); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.token, c.refresh = out.Access, out.Refresh
	if c.onRefresh != nil {
		c.onRefresh(out.Access, out.Refresh)
	}
	c.mu.Unlock()
	return &out, nil
}

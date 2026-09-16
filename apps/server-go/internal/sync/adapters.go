// Secret Sync 平台适配器（M5 #11）：目标平台接口 + GitHub Actions / Vercel / Cloudflare 实现
// 所有适配器均为「单密钥推送」语义：Push(ctx, cfg, key, value)。
// 平台访问令牌由调用方从 config_enc 解密后传入 cfg，适配器本身不落库。
package sync

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/box"
)

// Adapter 平台适配器接口（internal/sync，设计文档 §3.3）
type Adapter interface {
	// Push 推送单个密钥；cfg 为目标配置（含平台令牌）
	Push(ctx context.Context, cfg map[string]string, secretKey, value string) error
	// Ping 校验配置可用（可选，Create 时调用）
	Ping(ctx context.Context, cfg map[string]string) error
}

// Adapters 平台注册表
var Adapters = map[string]Adapter{
	"github":     GitHubAdapter{},
	"vercel":     VercelAdapter{},
	"cloudflare": CloudflareAdapter{},
}

// 每个平台必需的配置键（校验用；其余键透传）
var requiredKeys = map[string][]string{
	"github":     {"token", "owner", "repo"},
	"vercel":     {"token", "project_id"},
	"cloudflare": {"token", "account_id", "script_name"},
}

func httpDo(ctx context.Context, method, u string, headers map[string]string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	return res.StatusCode, b, nil
}

func fail(code int, body []byte, what string) error {
	msg := strings.TrimSpace(string(body))
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return fmt.Errorf("%s: HTTP %d: %s", what, code, msg)
}

// ---------- GitHub Actions Secrets ----------

// GitHubAdapter 支持 repo 级与 environment 级（cfg["environment"] 非空时）
type GitHubAdapter struct{}

func (GitHubAdapter) auth(cfg map[string]string) map[string]string {
	return map[string]string{
		"Authorization":        "Bearer " + cfg["token"],
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	}
}

func (GitHubAdapter) base(cfg map[string]string) string {
	b := "https://api.github.com/repos/" + url.PathEscape(cfg["owner"]) + "/" + url.PathEscape(cfg["repo"])
	if env := cfg["environment"]; env != "" {
		b += "/environments/" + url.PathEscape(env)
	}
	return b
}

// githubSecretsPublicKey 获取仓库/环境加密公钥
func (g GitHubAdapter) publicKey(ctx context.Context, cfg map[string]string) (keyID, b64key string, err error) {
	code, body, err := httpDo(ctx, "GET", g.base(cfg)+"/actions/secrets/public-key", g.auth(cfg), nil)
	if err != nil {
		return "", "", err
	}
	if code/100 != 2 {
		return "", "", fail(code, body, "github get public key")
	}
	var out struct {
		KeyID string `json:"key_id"`
		Key   string `json:"key"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", err
	}
	return out.KeyID, out.Key, nil
}

// sealGitHub libsodium sealed box：X25519 + XSalsa20-Poly1305（golang.org/x/crypto/nacl/box）
func sealGitHub(b64Pub, plaintext string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64Pub)
	if err != nil || len(raw) != 32 {
		return "", errors.New("invalid github public key")
	}
	var pub [32]byte
	copy(pub[:], raw)
	sealed, err := box.SealAnonymous(nil, []byte(plaintext), &pub, nil)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (g GitHubAdapter) Push(ctx context.Context, cfg map[string]string, secretKey, value string) error {
	keyID, b64key, err := g.publicKey(ctx, cfg)
	if err != nil {
		return err
	}
	enc, err := sealGitHub(b64key, value)
	if err != nil {
		return err
	}
	code, body, err := httpDo(ctx, "PUT", g.base(cfg)+"/actions/secrets/"+url.PathEscape(secretKey),
		g.auth(cfg), map[string]string{"encrypted_value": enc, "key_id": keyID})
	if err != nil {
		return err
	}
	if code/100 != 2 {
		return fail(code, body, "github put secret")
	}
	return nil
}

func (g GitHubAdapter) Ping(ctx context.Context, cfg map[string]string) error {
	_, _, err := g.publicKey(ctx, cfg)
	return err
}

// ---------- Vercel ----------

// VercelAdapter project env，按 target 区分 production/preview/development（默认 production）
type VercelAdapter struct{}

func (VercelAdapter) base(cfg map[string]string) string {
	b := "https://api.vercel.com/v9/projects/" + url.PathEscape(cfg["project_id"])
	if team := cfg["team_id"]; team != "" {
		b += "?teamId=" + url.QueryEscape(team)
	}
	return b
}

func (VercelAdapter) targets(cfg map[string]string) []string {
	t := cfg["targets"]
	if t == "" {
		return []string{"production"}
	}
	out := []string{}
	for _, x := range strings.Split(t, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	if len(out) == 0 {
		return []string{"production"}
	}
	return out
}

func (v VercelAdapter) Push(ctx context.Context, cfg map[string]string, secretKey, value string) error {
	h := map[string]string{"Authorization": "Bearer " + cfg["token"]}
	base := v.base(cfg)

	// Vercel 同名 env 会冲突：先按 key 查旧值并删除，再创建（幂等 upsert）
	code, body, err := httpDo(ctx, "GET", base+"/env?key="+url.QueryEscape(secretKey), h, nil)
	if err != nil {
		return err
	}
	if code/100 == 2 {
		var list struct {
			Env []struct {
				ID     string `json:"id"`
				Target string `json:"target"`
			} `json:"env"`
		}
		if err := json.Unmarshal(body, &list); err == nil {
			for _, e := range list.Env {
				_, _, _ = httpDo(ctx, "DELETE", base+"/env/"+url.PathEscape(e.ID), h, nil)
			}
		}
	}
	code, body, err = httpDo(ctx, "POST", base+"/env", h, map[string]any{
		"key":    secretKey,
		"value":  value,
		"type":   "encrypted",
		"target": v.targets(cfg),
	})
	if err != nil {
		return err
	}
	if code/100 != 2 {
		return fail(code, body, "vercel create env")
	}
	return nil
}

func (v VercelAdapter) Ping(ctx context.Context, cfg map[string]string) error {
	h := map[string]string{"Authorization": "Bearer " + cfg["token"]}
	code, body, err := httpDo(ctx, "GET", v.base(cfg), h, nil)
	if err != nil {
		return err
	}
	if code/100 != 2 {
		return fail(code, body, "vercel get project")
	}
	return nil
}

// ---------- Cloudflare Workers ----------

// CloudflareAdapter Workers secrets 绑定
type CloudflareAdapter struct{}

func (CloudflareAdapter) Push(ctx context.Context, cfg map[string]string, secretKey, value string) error {
	u := "https://api.cloudflare.com/client/v4/accounts/" + url.PathEscape(cfg["account_id"]) +
		"/workers/scripts/" + url.PathEscape(cfg["script_name"]) + "/secrets"
	h := map[string]string{"Authorization": "Bearer " + cfg["token"]}
	code, body, err := httpDo(ctx, "PUT", u, h, map[string]string{
		"name": secretKey, "text": value, "type": "secret_text",
	})
	if err != nil {
		return err
	}
	if code/100 != 2 {
		return fail(code, body, "cloudflare put secret")
	}
	return nil
}

func (c CloudflareAdapter) Ping(ctx context.Context, cfg map[string]string) error {
	u := "https://api.cloudflare.com/client/v4/accounts/" + url.PathEscape(cfg["account_id"]) +
		"/workers/scripts/" + url.PathEscape(cfg["script_name"])
	h := map[string]string{"Authorization": "Bearer " + cfg["token"]}
	code, body, err := httpDo(ctx, "GET", u, h, nil)
	if err != nil {
		return err
	}
	if code/100 != 2 {
		return fail(code, body, "cloudflare get script")
	}
	return nil
}

// 最小示例：登录 → 取默认项目 → 写入/读取/导出密钥（M3 #8）
// 运行：cd packages/sdk-go && go run ./examples/basic http://localhost:7100 you@example.com password123
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	taboo "github.com/zhouyunchang/taboo/packages/sdk-go"
)

func main() {
	base := os.Args[1]
	email, password := os.Args[2], os.Args[3]

	c := taboo.New(base)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var auth *taboo.AuthResponse
	auth, err := c.Register(ctx, email, password, "sdk-example")
	if err != nil {
		var ae *taboo.ApiError
		if errors.As(err, &ae) && ae.Status == 409 {
			auth, err = c.Login(ctx, email, password) // 已注册则直接登录
		}
	}
	if err != nil {
		log.Fatalf("auth: %v", err)
	}
	if auth.TotpRequired {
		log.Fatalf("该账号已开启 2FA，请用 TotpLogin 完成二次验证")
	}
	c.WithUserTokens(auth.Tokens.Access, auth.Tokens.Refresh, time.Duration(auth.Tokens.ExpiresIn)*time.Second, nil)

	_, orgs, totp, err := c.Me(ctx)
	if err != nil {
		log.Fatalf("me: %v", err)
	}
	fmt.Printf("login ok: %s, 2fa=%v\n", auth.User.Email, totp)

	proj, err := c.DefaultProject(ctx, orgs[0].Slug)
	if err != nil {
		log.Fatalf("project: %v", err)
	}

	if _, err := c.CreateFolder(ctx, proj.ID, "dev", "/demo/"); err != nil {
		log.Fatalf("folder: %v", err)
	}
	v, err := c.UpsertSecret(ctx, proj.ID, "dev", "/demo/", "HELLO", "world", "sdk example", []string{"sdk"})
	if err != nil {
		log.Fatalf("upsert: %v", err)
	}
	fmt.Printf("upserted HELLO v%d in /demo/\n", v)

	sv, err := c.GetSecret(ctx, proj.ID, "dev", "/demo/", "HELLO")
	if err != nil {
		log.Fatalf("get: %v", err)
	}
	fmt.Printf("reveal HELLO=%s (v%d)\n", sv.Value, sv.Version)

	envText, err := c.ExportSecrets(ctx, proj.ID, "dev")
	if err != nil {
		log.Fatalf("export: %v", err)
	}
	fmt.Printf("export ok (%d bytes)\n", len(envText))
}

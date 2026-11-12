# taboo Go SDK

`packages/sdk-go` 是 REST 的手写薄封装，模块路径 `github.com/zhouyunchang/taboo/packages/sdk-go`。

覆盖登录、TOTP 第二步、refresh 旋转、机器身份临期换票、OAuth client credentials、文件夹、密钥 CRUD / reveal / 指定版本 / 回滚 / 导出 / 删除，以及 Phase 4 的引用展开、`.env` 导入和扫描报告。

```go
client := taboo.New("https://taboo.example")
client.WithIdentity(clientID, clientSecret)
value, err := client.GetSecret(ctx, projectID, "/", "DB_URL", taboo.GetOptions{Resolve: true})
```

`Resolve: true` 才展开 `${env:KEY}` 和 `${project:env:KEY}`。缺省返回库里的原文。导入用 `ImportEnv`，`preview=true` 不写库。扫描报告只用 `ScanFinding` 的规则、相对路径、行号和 64 位 sha256 指纹。

示例见 `examples/basic`。消费者契约是 `consumer_test.go`，由 `scripts/test-consumers.sh` 连同 Node、Python SDK 一起对真实服务执行。

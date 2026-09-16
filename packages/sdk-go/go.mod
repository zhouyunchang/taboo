// taboo Go SDK —— 原生手写薄封装（M3 #8，设计文档 §11.4：薄封装 REST 契约，不做大而全抽象）
// 覆盖：用户登录/refresh 旋转、机器身份 token 换取（临期自动重换）、secrets CRUD / reveal / versions / rollback / export。
module github.com/zhouyunchang/taboo/packages/sdk-go

go 1.23

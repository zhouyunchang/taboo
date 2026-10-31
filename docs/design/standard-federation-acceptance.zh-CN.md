# 标准身份联邦 P0 验收记录

日期：2026-09-27。适用：Go 服务、schema 15、SQLite 单实例及 PostgreSQL 18。对应批准计划 KTI-00 至 KTI-11；本记录汇总当前结果，早期 M0/存储/治理记录中的“剩余工作”属于历史检查点。

## 工作包交付

| 工作包 | 交付与证据 |
|---|---|
| KTI-00 | 标准协议、能力与来源边界；公开 OpenAPI；Keycloak 26.6.3 固定镜像和无专有 Claim 夹具；架构基准更新 |
| KTI-01 | SQLite 事务迁移及旧版本升级；PG 18 非特权 control/runtime 分离、FORCE RLS、复合归属约束、请求上下文隔离 |
| KTI-02 | Code/S256/state/nonce、严格 issuer/audience/type/签名、BFF cookie/CSRF；bounded/online 会话、刷新串行化、退出、外部登录策略 |
| KTI-03 | 精确且有来源的角色映射；六项独立秘密操作；认证强度、会话绝对截止；本地撤权；防最后外部 owner 锁出 |
| KTI-04 | KEK/DEK 与版本化 AAD；有界缓存；旧格式兼容/离线转换；expectedVersion/CAS、同键幂等收据 |
| KTI-05 | 治理与秘密事务审计；持久 outbox/消费者落库确认；有序同步、投递去重；租约/fencing、死信、动态账号回收与审计导出恢复 |
| KTI-06 | RFC 8414/OIDC 发现；严格 RFC 9068 JWT 和 RFC 7662 内省；精确 workload 绑定及项目环境/action/scope；本地机器兼容 |
| KTI-07 | 通用连接、成员动作、登录策略、会话策略、工作负载治理；组织切换需验证；现有审计/同步/动态治理适配；native Modal、窄屏、键盘、多标签及失联重试 |
| KTI-08 | 实际 SDK、CLI、MCP 标准 OAuth 与本地流程；配置隔离、内存缓存、拒绝错误目标/凭据/scope；SDK/CLI/UI 幂等与版本前置条件 |
| KTI-09 | verify/envelopes/import-sqlite；计数/关联/密文与审计链核验；双身份证明替换；旧公开版本容器升级和原根材料恢复 |
| KTI-10 | Go race/vet、前端类型/构建、Keycloak、PG、真实消费者及包装 CI job；全部使用公共依赖和临时凭据，无闭源服务前置 |
| KTI-11 | Docker 同时交付 server/migrate；PG Compose、Helm 外部 Secret/身份配置、多副本存储限制；快速开始、停写升级与回退手册 |

## 验证及范围

- SQLite Go race 套件；PG 18 race 套件包含连接隔离、并发事务、迁移、实际数据库动态账号创建/续期/回收和 SDK/CLI/MCP 子进程。
- 真实 Keycloak：人员登录、client credentials、refresh、禁用用户对旧 Token 的内省语义。独立故障夹具覆盖错误 issuer/audience/type/state/nonce、强认证、重放、并发刷新、提交失败和故障拒绝。
- 7 个真实浏览器场景：登录/CSRF/组织隔离/退出、多标签身份变化、通用连接与键盘焦点、停用连接、503 重试、开发者显式动作、双身份替换、owner 治理与“服务器已写入但响应丢失”的同键重试。390px 窄屏实测；原生 dialog 和表格滚动区域；严格静态 UI 审核无未解决项。
- 真实消费者分别覆盖严格 JWT、opaque 内省、本地身份。SDK 和 CLI 同请求重放不新增版本；错误版本拒绝；MCP 默认不返回明文。
- 构建公开 Docker 镜像后，全新安装、停机备份恢复、重启后原版本解密通过；错误或缺失 root 拒绝，未生成替代 root。固定公开基线 `d5724d32f4d5828ee89734be6e6223a992ec74b8` 建立数据后升级到 schema 15，秘密和版本保持。PG Compose 新建/写入/重启/解密通过。
- 干净 checkout（只复制已提交文件）重新安装依赖、生成契约无漂移、类型检查、全部 Go 模块 race/vet、二进制构建、14 项旧模式冒烟和真实消费者均通过。已修复原有未跟踪 embed 目录导致首次编译失败的问题。
- GoReleaser 2.18.0 配置检查与不发布快照通过；8 个 server/migrate 二进制、4 个跨平台压缩包包含恢复手册。专用发布镜像通过安装/重启/恢复测试。未执行远端上传、SBOM 生成或 OIDC 签名；公开 CI 已加入配置与快照检查，tag 流水线安装 syft 并分别签署校验和/镜像 digest。
- Helm lint 与 SQLite/PG/可选联邦配置渲染通过，拒绝 SQLite 多副本、缺少 DSN Secret、PG 多副本未共享导出卷。此项是模板验收，没有创建生产 Kubernetes 工作负载。

复现命令见 [发行运行手册](../runbooks/standard-federation-release.zh-CN.md)。GitHub Actions 定义已包含这些独立必需 job；本记录不把本地通过称作远端 CI 成功。远端 push、PR、发布 tag 与真实租户切换分别记录。

## 明确边界

- Identity 仅是可选标准提供方，本次未修改或依赖闭源工程。不能据此宣称该闭源部署已接通，或所有 OIDC/OAuth 产品都互操作。
- 单用户保留一个外部身份关联，替换需要旧/新双重证明；不支持按邮箱合并。严格外部组织始终需要可用外部 owner。
- JWT/bounded 的上游撤销有明确有效期窗口；本地绑定或成员撤销在下一次授权检查生效。已通过检查的在途操作可能完成。
- 外部 OAuth 工作负载开放秘密/目录/环境/导出协议；动态数据库账号仍由原本地机器身份协议申请，外部 owner 可治理和回收。
- P1 SCIM、AuthZEN、Back-Channel Logout、个人 CLI 浏览器登录与生产投运不在本次 P0 范围。没有执行生产数据迁移或流量切换。
- 第三方人员登录已定为 GitHub OAuth，仅记录需求，本次不实现、不验收。

# 人员准入、动作授权与组织登录策略

> 本文件保留分阶段实施检查点；其中的剩余工作已由后续提交推进，当前范围见 [P0 验收记录](standard-federation-acceptance.zh-CN.md)。

Schema 13 在原有本地角色模型上添加标准联邦人员的显式资源动作授权。OIDC 不负责创建业务组织或定义业务权限；身份连接完成认证后仍须本地成员准入。此文描述已经实现的人员治理接口；浏览器强认证和 ONLINE_REQUIRED 会话仍需后续会话策略工作完成后登记证据。

## 权限来源

外部人员必须有组织成员关系。组织角色和项目 restricted/grant 继续决定项目可见范围及管理权限。项目、环境内的 `read`、`write`、`reveal`、`export`、`rollback`、`delete` 还必须分别有 `member_action_grants` 记录。角色 Claim 映射本身不创建这些动作授权；developer/admin 不意味着可以读取秘密明文。项目路由入口要求至少一个环境的 read 授权，具体操作再次检查目标环境动作。默认 JIT viewer 没有秘密动作权限，需 owner 明确授权。

组织 owner 用 `GET/PUT /api/v1/orgs/{slug}/members/{userId}/actions` 查看或原子替换动作矩阵。空数组明确撤销全部动作；拒绝重复项及跨组织项目/环境。读取 metadata 不包含秘密值；写入、明文、导出、回滚、删除彼此独立。权限更改后下一次 API 重新查询，无 ALLOW 缓存；已开始执行的操作不保证被中途撤回。

本地用户和本地机器旧角色/scope 行为保持兼容，不因接入外部 IdP 隐式提高或减少旧权限。迁移为外部身份时逐项登记新授权。工作负载也有独立 rollback/delete grant；schema 12 的已有 grant 数据在 schema 13 升级时原样保留，不扩张已有 write grant。

成员本地变更清除连接对角色的所有权，下一次 Claim 同步不会覆盖本地指定角色。成员删除保留 federation admission tombstone，JIT 不会自动重新加入。成员添加的邮箱入口只查询已存在的本地 active 账号，不做外部身份邮箱合并。PostgreSQL 的精确本地账号查询由 owner 检查后调用受限目录函数；其余租户请求仍使用 RLS 连接。

成员增加、删除、角色和动作授权、邀请创建及接受与审计同事务；失败不留下已经生效的权限。角色降级和删除持有组织行锁，避免并发移除最后一个 owner。EXTERNAL_REQUIRED 组织还必须保留已通过启用连接准入的外部 owner。

## 组织级登录方式

`GET/PUT /api/v1/orgs/{slug}/security` 提供 `LOCAL_OR_EXTERNAL` 和 `EXTERNAL_REQUIRED`。默认保留已有本地访问。启用 EXTERNAL_REQUIRED 必须由当前已通过本组织 OIDC 会话登录的 owner 操作，避免仅凭一个本地会话就切断管理路径。

策略检查在路由处理之前，覆盖项目、组织治理和邀请目标。已发本地用户 Token 也不能访问该组织；本地 `/me` 隐藏该组织。独立本地账号仍可使用其他允许本地登录的组织。机器凭据策略独立，不因人员登录策略变更被隐式撤销。

外部连接停用或故障不会启动本地密码回退。需要恢复本地登录时，由可用的外部 owner 修改策略；全部上游不可用时使用部署者控制的离线数据库恢复流程，在停写、备份、审计恢复记录和根材料恢复保障下进行，不提供匿名应急 API。

## 页面与验证

秘密列表返回服务端 `permissions`，包括空列表；每条 metadata 返回 canReveal/canWrite/canDelete/canRollback，历史响应返回 canRollback。页面按钮使用这些结果，不把 write 推导为删除或回滚。明文在组织/环境/版本切换、权限重新加载和窗口失焦时清除。

当前证据：SQLite/PostgreSQL 同一人员授权、强制外部访问、审计故障回滚、并发 owner 测试；schema 12→13 保留工作负载升级测试；真实 Keycloak 浏览器测试以 owner 显式授予 dev 动作后验证读写、reveal 和历史回滚。完整治理表单、全部高级业务路径与会话保证仍在本期剩余工作中，不能由这些接口测试推断已完成。

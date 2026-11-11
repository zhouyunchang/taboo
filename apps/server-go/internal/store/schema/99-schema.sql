-- schema 版本：单一真源。
-- 迁移只允许前进一步；v23 是当前结构（文件夹与密钥按项目归属）。
CREATE TABLE IF NOT EXISTS schema_version (
  singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
  version   BIGINT NOT NULL
);
INSERT INTO schema_version (singleton, version) VALUES (TRUE, {{version}}) ON CONFLICT (singleton) DO UPDATE SET version = excluded.version;

-- 运行角色（{role}）：只读 schema、可读写本租户数据，不能改版本表。
-- 这句在最后执行，保证上面新建的表和序列都已经存在。
GRANT USAGE ON SCHEMA public TO {role};
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO {role};
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO {role};
REVOKE INSERT, UPDATE, DELETE ON schema_version FROM {role};

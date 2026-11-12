-- 运行角色授权：只读 schema、可读写本租户数据，不能改迁移表。
-- +goose Up

GRANT USAGE ON SCHEMA public TO {role};
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO {role};
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO {role};
-- 迁移表只归控制登录所有：运行角色不得读写迁移历史。
REVOKE ALL ON goose_db_version FROM {role};

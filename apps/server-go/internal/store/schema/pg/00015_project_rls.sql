-- 项目内三张表（folders / secrets / secret_versions）的租户策略。
-- 00012 从旧基线重建时漏掉了这三张表；它们没有 org_id，只能沿 project/secret
-- 链回到 projects 判定租户。没有 FORCE RLS 时，运行角色可以跨项目读写密钥。
-- +goose Up

ALTER TABLE folders ENABLE ROW LEVEL SECURITY;
ALTER TABLE folders FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON folders;
CREATE POLICY tenant_scope ON folders FOR ALL
  USING (((CURRENT_USER = '{owner}') OR (project_id IN (
    SELECT id FROM projects WHERE org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)
  ))))
  WITH CHECK (((CURRENT_USER = '{owner}') OR (project_id IN (
    SELECT id FROM projects WHERE org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)
  ))));

ALTER TABLE secrets ENABLE ROW LEVEL SECURITY;
ALTER TABLE secrets FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON secrets;
CREATE POLICY tenant_scope ON secrets FOR ALL
  USING (((CURRENT_USER = '{owner}') OR (project_id IN (
    SELECT id FROM projects WHERE org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)
  ))))
  WITH CHECK (((CURRENT_USER = '{owner}') OR (project_id IN (
    SELECT id FROM projects WHERE org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)
  ))));

ALTER TABLE secret_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE secret_versions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON secret_versions;
CREATE POLICY tenant_scope ON secret_versions FOR ALL
  USING (((CURRENT_USER = '{owner}') OR (secret_id IN (SELECT id FROM secrets))))
  WITH CHECK (((CURRENT_USER = '{owner}') OR (secret_id IN (SELECT id FROM secrets))));

GRANT SELECT, INSERT, UPDATE, DELETE ON folders, secrets, secret_versions TO {role};

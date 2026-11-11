-- 行级安全：控制登录（{owner}）可跨租户，运行角色（{role}）只在当前租户内可见。
-- 策略谓词里的 taboo.org_id / taboo.user_id 由连接建立时注入（见 store/postgres.go）。

-- access_requests
ALTER TABLE access_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE access_requests FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON access_requests;
CREATE POLICY tenant_scope ON access_requests FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON access_requests TO {role};

-- approval_requests
ALTER TABLE approval_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE approval_requests FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON approval_requests;
CREATE POLICY tenant_scope ON approval_requests FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON approval_requests TO {role};

-- audit_checkpoints
ALTER TABLE audit_checkpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_checkpoints FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON audit_checkpoints;
CREATE POLICY tenant_scope ON audit_checkpoints FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON audit_checkpoints TO {role};

-- audit_exports
ALTER TABLE audit_exports ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_exports FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON audit_exports;
CREATE POLICY tenant_scope ON audit_exports FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON audit_exports TO {role};

-- audit_heads
ALTER TABLE audit_heads ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_heads FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON audit_heads;
CREATE POLICY tenant_scope ON audit_heads FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON audit_heads TO {role};

-- audit_logs
ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON audit_logs;
CREATE POLICY tenant_scope ON audit_logs FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON audit_logs TO {role};

-- browser_credentials
ALTER TABLE browser_credentials ENABLE ROW LEVEL SECURITY;
ALTER TABLE browser_credentials FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON browser_credentials;
CREATE POLICY tenant_scope ON browser_credentials FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON browser_credentials TO {role};

-- browser_sessions
ALTER TABLE browser_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE browser_sessions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON browser_sessions;
CREATE POLICY tenant_scope ON browser_sessions FOR ALL USING (((CURRENT_USER = '{owner}') OR (user_id = NULLIF(current_setting('taboo.user_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (user_id = NULLIF(current_setting('taboo.user_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON browser_sessions TO {role};

-- cli_auth_codes
ALTER TABLE cli_auth_codes ENABLE ROW LEVEL SECURITY;
ALTER TABLE cli_auth_codes FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON cli_auth_codes;
CREATE POLICY tenant_scope ON cli_auth_codes FOR ALL USING ((CURRENT_USER = '{owner}')) WITH CHECK ((CURRENT_USER = '{owner}'));
GRANT SELECT, INSERT, UPDATE, DELETE ON cli_auth_codes TO {role};

-- cluster_leases
ALTER TABLE cluster_leases ENABLE ROW LEVEL SECURITY;
ALTER TABLE cluster_leases FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON cluster_leases;
CREATE POLICY tenant_scope ON cluster_leases FOR ALL USING ((CURRENT_USER = '{owner}')) WITH CHECK ((CURRENT_USER = '{owner}'));
GRANT SELECT, INSERT, UPDATE, DELETE ON cluster_leases TO {role};

-- device_authorizations
ALTER TABLE device_authorizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE device_authorizations FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON device_authorizations;
CREATE POLICY tenant_scope ON device_authorizations FOR ALL USING ((CURRENT_USER = '{owner}')) WITH CHECK ((CURRENT_USER = '{owner}'));
GRANT SELECT, INSERT, UPDATE, DELETE ON device_authorizations TO {role};

-- dynamic_engines
ALTER TABLE dynamic_engines ENABLE ROW LEVEL SECURITY;
ALTER TABLE dynamic_engines FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON dynamic_engines;
CREATE POLICY tenant_scope ON dynamic_engines FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON dynamic_engines TO {role};

-- federation_admissions
ALTER TABLE federation_admissions ENABLE ROW LEVEL SECURITY;
ALTER TABLE federation_admissions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON federation_admissions;
CREATE POLICY tenant_scope ON federation_admissions FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON federation_admissions TO {role};

-- logout_replays
ALTER TABLE logout_replays ENABLE ROW LEVEL SECURITY;
ALTER TABLE logout_replays FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON logout_replays;
CREATE POLICY tenant_scope ON logout_replays FOR ALL USING ((CURRENT_USER = '{owner}')) WITH CHECK ((CURRENT_USER = '{owner}'));
GRANT SELECT, INSERT, UPDATE, DELETE ON logout_replays TO {role};

-- machine_identities
ALTER TABLE machine_identities ENABLE ROW LEVEL SECURITY;
ALTER TABLE machine_identities FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON machine_identities;
CREATE POLICY tenant_scope ON machine_identities FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON machine_identities TO {role};

-- member_action_grants
ALTER TABLE member_action_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE member_action_grants FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON member_action_grants;
CREATE POLICY tenant_scope ON member_action_grants FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON member_action_grants TO {role};

-- mutation_receipts
ALTER TABLE mutation_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE mutation_receipts FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON mutation_receipts;
CREATE POLICY tenant_scope ON mutation_receipts FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON mutation_receipts TO {role};

-- oauth_trusts
ALTER TABLE oauth_trusts ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_trusts FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON oauth_trusts;
CREATE POLICY tenant_scope ON oauth_trusts FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON oauth_trusts TO {role};

-- oidc_login_requests
ALTER TABLE oidc_login_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE oidc_login_requests FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON oidc_login_requests;
CREATE POLICY tenant_scope ON oidc_login_requests FOR ALL USING (((CURRENT_USER = '{owner}') OR false)) WITH CHECK (((CURRENT_USER = '{owner}') OR false));
GRANT SELECT, INSERT, UPDATE, DELETE ON oidc_login_requests TO {role};

-- oidc_providers
ALTER TABLE oidc_providers ENABLE ROW LEVEL SECURITY;
ALTER TABLE oidc_providers FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON oidc_providers;
CREATE POLICY tenant_scope ON oidc_providers FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON oidc_providers TO {role};

-- org_invites
ALTER TABLE org_invites ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_invites FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON org_invites;
CREATE POLICY tenant_scope ON org_invites FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON org_invites TO {role};

-- org_members
ALTER TABLE org_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_members FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON org_members;
CREATE POLICY tenant_scope ON org_members FOR ALL USING (((CURRENT_USER = '{owner}') OR ((org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)) OR ((NULLIF(current_setting('taboo.org_id'::text, true), ''::text) IS NULL) AND (user_id = NULLIF(current_setting('taboo.user_id'::text, true), ''::text)))))) WITH CHECK (((CURRENT_USER = '{owner}') OR ((org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)) OR ((NULLIF(current_setting('taboo.org_id'::text, true), ''::text) IS NULL) AND (user_id = NULLIF(current_setting('taboo.user_id'::text, true), ''::text))))));
GRANT SELECT, INSERT, UPDATE, DELETE ON org_members TO {role};

-- outbox_events
ALTER TABLE outbox_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbox_events FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON outbox_events;
CREATE POLICY tenant_scope ON outbox_events FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON outbox_events TO {role};

-- project_grants
ALTER TABLE project_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_grants FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON project_grants;
CREATE POLICY tenant_scope ON project_grants FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON project_grants TO {role};

-- projects
ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE projects FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON projects;
CREATE POLICY tenant_scope ON projects FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON projects TO {role};

-- provider_security
ALTER TABLE provider_security ENABLE ROW LEVEL SECURITY;
ALTER TABLE provider_security FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON provider_security;
CREATE POLICY tenant_scope ON provider_security FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON provider_security TO {role};

-- rate_counters
ALTER TABLE rate_counters ENABLE ROW LEVEL SECURITY;
ALTER TABLE rate_counters FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON rate_counters;
CREATE POLICY tenant_scope ON rate_counters FOR ALL USING ((CURRENT_USER = '{owner}')) WITH CHECK ((CURRENT_USER = '{owner}'));
GRANT SELECT, INSERT, UPDATE, DELETE ON rate_counters TO {role};

-- recovery_codes
ALTER TABLE recovery_codes ENABLE ROW LEVEL SECURITY;
ALTER TABLE recovery_codes FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON recovery_codes;
CREATE POLICY tenant_scope ON recovery_codes FOR ALL USING (((CURRENT_USER = '{owner}') OR (user_id = NULLIF(current_setting('taboo.user_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (user_id = NULLIF(current_setting('taboo.user_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON recovery_codes TO {role};

-- refresh_tokens
ALTER TABLE refresh_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE refresh_tokens FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON refresh_tokens;
CREATE POLICY tenant_scope ON refresh_tokens FOR ALL USING (((CURRENT_USER = '{owner}') OR (user_id = NULLIF(current_setting('taboo.user_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (user_id = NULLIF(current_setting('taboo.user_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON refresh_tokens TO {role};

-- rotation_policies
ALTER TABLE rotation_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE rotation_policies FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON rotation_policies;
CREATE POLICY tenant_scope ON rotation_policies FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON rotation_policies TO {role};

-- scan_reports
ALTER TABLE scan_reports ENABLE ROW LEVEL SECURITY;
ALTER TABLE scan_reports FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON scan_reports;
CREATE POLICY tenant_scope ON scan_reports FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON scan_reports TO {role};

-- scim_group_members
ALTER TABLE scim_group_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE scim_group_members FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON scim_group_members;
CREATE POLICY tenant_scope ON scim_group_members FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON scim_group_members TO {role};

-- scim_groups
ALTER TABLE scim_groups ENABLE ROW LEVEL SECURITY;
ALTER TABLE scim_groups FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON scim_groups;
CREATE POLICY tenant_scope ON scim_groups FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON scim_groups TO {role};

-- scim_tokens
ALTER TABLE scim_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE scim_tokens FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON scim_tokens;
CREATE POLICY tenant_scope ON scim_tokens FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON scim_tokens TO {role};

-- scim_users
ALTER TABLE scim_users ENABLE ROW LEVEL SECURITY;
ALTER TABLE scim_users FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON scim_users;
CREATE POLICY tenant_scope ON scim_users FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON scim_users TO {role};

-- sync_runs
ALTER TABLE sync_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_runs FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON sync_runs;
CREATE POLICY tenant_scope ON sync_runs FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON sync_runs TO {role};

-- sync_targets
ALTER TABLE sync_targets ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_targets FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON sync_targets;
CREATE POLICY tenant_scope ON sync_targets FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON sync_targets TO {role};

-- webhook_deliveries
ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_deliveries FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON webhook_deliveries;
CREATE POLICY tenant_scope ON webhook_deliveries FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON webhook_deliveries TO {role};

-- webhooks
ALTER TABLE webhooks ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhooks FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON webhooks;
CREATE POLICY tenant_scope ON webhooks FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON webhooks TO {role};

-- workload_bindings
ALTER TABLE workload_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE workload_bindings FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON workload_bindings;
CREATE POLICY tenant_scope ON workload_bindings FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON workload_bindings TO {role};

-- workload_grants
ALTER TABLE workload_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE workload_grants FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_scope ON workload_grants;
CREATE POLICY tenant_scope ON workload_grants FOR ALL USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text)))) WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON workload_grants TO {role};


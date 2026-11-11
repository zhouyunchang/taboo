-- 行级不可变与完整性约束。先 DROP 再建，保证脚本可重复执行。
-- +goose Up

-- {owner} / {role} 由迁移按部署配置替换。

-- dynamic_engines
CREATE TRIGGER immutable_dynamic_engines BEFORE UPDATE ON dynamic_engines FOR EACH ROW EXECUTE FUNCTION immutable_ownership('org_id');

-- folders
CREATE TRIGGER immutable_folders BEFORE UPDATE ON folders FOR EACH ROW EXECUTE FUNCTION immutable_ownership('project_id');

-- identity_scopes
CREATE TRIGGER integrity_identity_scopes BEFORE INSERT OR UPDATE ON identity_scopes FOR EACH ROW EXECUTE FUNCTION check_identity_scope();

-- machine_identities
CREATE TRIGGER immutable_machine_auth BEFORE UPDATE ON machine_identities FOR EACH ROW EXECUTE FUNCTION immutable_ownership('auth_type');
CREATE TRIGGER immutable_machine_identities BEFORE UPDATE ON machine_identities FOR EACH ROW EXECUTE FUNCTION immutable_ownership('org_id');

-- oauth_trusts
CREATE TRIGGER immutable_oauth_trusts BEFORE UPDATE ON oauth_trusts FOR EACH ROW EXECUTE FUNCTION immutable_ownership('org_id');

-- projects
CREATE TRIGGER immutable_projects BEFORE UPDATE ON projects FOR EACH ROW EXECUTE FUNCTION immutable_ownership('org_id');

-- secrets
CREATE TRIGGER immutable_secrets BEFORE UPDATE ON secrets FOR EACH ROW EXECUTE FUNCTION immutable_ownership('project_id');

-- sync_targets
CREATE TRIGGER immutable_sync_targets BEFORE UPDATE ON sync_targets FOR EACH ROW EXECUTE FUNCTION immutable_ownership('org_id');

-- webhooks
CREATE TRIGGER immutable_webhooks BEFORE UPDATE ON webhooks FOR EACH ROW EXECUTE FUNCTION immutable_ownership('org_id');

-- workload_bindings
CREATE TRIGGER immutable_workload_binding_trust BEFORE UPDATE ON workload_bindings FOR EACH ROW EXECUTE FUNCTION immutable_ownership('trust_id');
CREATE TRIGGER immutable_workload_bindings BEFORE UPDATE ON workload_bindings FOR EACH ROW EXECUTE FUNCTION immutable_ownership('org_id');
CREATE TRIGGER integrity_workload_identity BEFORE INSERT OR UPDATE ON workload_bindings FOR EACH ROW EXECUTE FUNCTION check_workload_identity();

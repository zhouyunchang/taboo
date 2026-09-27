CREATE TABLE IF NOT EXISTS org_security (
 org_id TEXT PRIMARY KEY REFERENCES orgs(id) ON DELETE CASCADE,
 login_policy TEXT NOT NULL DEFAULT 'LOCAL_OR_EXTERNAL' CHECK(login_policy IN ('LOCAL_OR_EXTERNAL','EXTERNAL_REQUIRED'))
);
CREATE TABLE IF NOT EXISTS member_action_grants (
 org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 env_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
 action TEXT NOT NULL CHECK(action IN ('read','write','reveal','export','rollback','delete')),
 PRIMARY KEY(org_id,user_id,project_id,env_id,action),
 FOREIGN KEY(org_id,user_id) REFERENCES org_members(org_id,user_id) ON DELETE CASCADE
);

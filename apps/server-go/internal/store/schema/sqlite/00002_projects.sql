-- +goose Up

CREATE TABLE projects (
  id         TEXT PRIMARY KEY,
  org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  slug       TEXT NOT NULL,
  approval_required INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (org_id, slug)
);

CREATE TABLE folders (
  id         TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  parent_id  TEXT REFERENCES folders(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  path       TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (project_id, path)
);

CREATE TABLE secrets (
  id             TEXT PRIMARY KEY,
  project_id     TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  folder         TEXT NOT NULL DEFAULT '/',
  folder_id      TEXT REFERENCES folders(id),
  key            TEXT NOT NULL,
  comment        TEXT NOT NULL DEFAULT '',
  tags           TEXT NOT NULL DEFAULT '[]',
  latest_version INTEGER NOT NULL DEFAULT 0,
  created_at     TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at     TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (project_id, folder, key)
);

CREATE TABLE secret_versions (
  id         TEXT PRIMARY KEY,
  secret_id  TEXT NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
  version    INTEGER NOT NULL,
  ciphertext TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (secret_id, version)
);

CREATE TABLE org_invites (
  id          TEXT PRIMARY KEY,
  org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  email       TEXT NOT NULL,
  role        TEXT NOT NULL CHECK (role IN ('owner','admin','developer','viewer')),
  token_hash  TEXT NOT NULL UNIQUE,
  expires_at  INTEGER NOT NULL,
  created_by  TEXT NOT NULL,
  created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE project_grants (
  org_id     TEXT NOT NULL,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       TEXT NOT NULL CHECK (role IN ('owner','admin','developer','viewer')),
  PRIMARY KEY (project_id, user_id)
);

CREATE TABLE org_security (
 org_id TEXT PRIMARY KEY REFERENCES orgs(id) ON DELETE CASCADE,
 login_policy TEXT NOT NULL DEFAULT 'LOCAL_OR_EXTERNAL' CHECK(login_policy IN ('LOCAL_OR_EXTERNAL','EXTERNAL_REQUIRED'))
);

CREATE TABLE member_action_grants (
 org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 action TEXT NOT NULL CHECK(action IN ('read','write','reveal','export','rollback','delete')),
 PRIMARY KEY(org_id,user_id,project_id,action),
 FOREIGN KEY(org_id,user_id) REFERENCES org_members(org_id,user_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX idx_secrets_project_folder_key ON secrets(project_id, folder_id, key);

CREATE TRIGGER integrity_folders_INSERT BEFORE INSERT ON folders WHEN NOT (NEW.parent_id IS NULL OR EXISTS(SELECT 1 FROM folders f WHERE f.id=NEW.parent_id AND f.project_id=NEW.project_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_folders_UPDATE BEFORE UPDATE ON folders WHEN NOT (NEW.parent_id IS NULL OR EXISTS(SELECT 1 FROM folders f WHERE f.id=NEW.parent_id AND f.project_id=NEW.project_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_project_grants_INSERT BEFORE INSERT ON project_grants WHEN NOT (EXISTS(SELECT 1 FROM projects p JOIN org_members m ON m.org_id=p.org_id WHERE p.id=NEW.project_id AND p.org_id=NEW.org_id AND m.user_id=NEW.user_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_project_grants_UPDATE BEFORE UPDATE ON project_grants WHEN NOT (EXISTS(SELECT 1 FROM projects p JOIN org_members m ON m.org_id=p.org_id WHERE p.id=NEW.project_id AND p.org_id=NEW.org_id AND m.user_id=NEW.user_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_member_action_grants_INSERT BEFORE INSERT ON member_action_grants WHEN NOT (EXISTS(SELECT 1 FROM projects p WHERE p.id=NEW.project_id AND p.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_member_action_grants_UPDATE BEFORE UPDATE ON member_action_grants WHEN NOT (EXISTS(SELECT 1 FROM projects p WHERE p.id=NEW.project_id AND p.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_secrets_INSERT BEFORE INSERT ON secrets WHEN NOT (EXISTS(SELECT 1 FROM folders f WHERE f.id=NEW.folder_id AND f.project_id=NEW.project_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_secrets_UPDATE BEFORE UPDATE ON secrets WHEN NOT (EXISTS(SELECT 1 FROM folders f WHERE f.id=NEW.folder_id AND f.project_id=NEW.project_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER immutable_folders BEFORE UPDATE ON folders WHEN NEW.id<>OLD.id OR NEW.project_id<>OLD.project_id BEGIN SELECT RAISE(ABORT,'resource ownership is immutable'); END;

CREATE TRIGGER immutable_secrets BEFORE UPDATE ON secrets WHEN NEW.id<>OLD.id OR NEW.project_id<>OLD.project_id BEGIN SELECT RAISE(ABORT,'resource ownership is immutable'); END;

CREATE TRIGGER immutable_projects BEFORE UPDATE ON projects WHEN NEW.id<>OLD.id OR NEW.org_id<>OLD.org_id BEGIN SELECT RAISE(ABORT,'resource ownership is immutable'); END;

CREATE INDEX idx_folders_project_path ON folders(project_id, path);

CREATE INDEX idx_secrets_project_key ON secrets(project_id, folder, key);

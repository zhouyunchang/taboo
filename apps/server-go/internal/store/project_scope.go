package store

// AttachToProjects 给目录和密钥补上 project_id。密文上下文仍使用原来的环境 id，避免已有密钥无法解密。
func AttachToProjects(db migrationDB) error {
	if err := addColumn(db, `ALTER TABLE folders ADD COLUMN project_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := addColumn(db, `ALTER TABLE secrets ADD COLUMN project_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE folders SET project_id=(SELECT project_id FROM environments e WHERE e.id=folders.env_id) WHERE project_id=''`); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE secrets SET project_id=(SELECT project_id FROM environments e WHERE e.id=secrets.env_id) WHERE project_id=''`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_folders_project_path ON folders(project_id, path)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_secrets_project_key ON secrets(project_id, folder, key)`); err != nil {
		return err
	}
	return nil
}

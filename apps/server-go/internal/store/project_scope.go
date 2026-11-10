package store

// AttachToProjects 确保目录和密钥按项目索引。
func AttachToProjects(db migrationDB) error {
	if err := addColumn(db, `ALTER TABLE folders ADD COLUMN project_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := addColumn(db, `ALTER TABLE secrets ADD COLUMN project_id TEXT NOT NULL DEFAULT ''`); err != nil {
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

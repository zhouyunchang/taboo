package store

// CollapseEnvironments 只保留项目审批开关。环境模型已经删除。
func CollapseEnvironments(db migrationDB) error {
	return addColumn(db, `ALTER TABLE projects ADD COLUMN approval_required INTEGER NOT NULL DEFAULT 0`)
}

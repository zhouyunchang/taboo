// SQLite 侧的结构补丁：与 PostgreSQL 的 schema/ 目录同源、同分域，
// 只保留 SQLite 方言需要的片段（表创建与幂等索引）。
// 主结构仍在 store.go 的 migrateSchema 里，与 PG 的 10~60 号脚本一一对应。
package store

import _ "embed"

//go:embed schema/sqlite/20-projects-governance.sql
var sqliteGovernance string

//go:embed schema/sqlite/70-federation-workloads.sql
var sqliteWorkloads string

//go:embed schema/sqlite/70-federation-browser.sql
var sqliteBrowserSecurity string

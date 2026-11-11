// PostgreSQL 结构定义：按领域拆分的 DDL，按文件名顺序执行。
// 真实来源是 schema/*.sql；这里只负责嵌入与执行。
package store

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed schema/10-account.sql
var sql10 string

//go:embed schema/20-projects.sql
var sql20 string

//go:embed schema/30-identity.sql
var sql30 string

//go:embed schema/40-audit.sql
var sql40 string

//go:embed schema/50-integration.sql
var sql50 string

//go:embed schema/60-runtime.sql
var sql60 string

//go:embed schema/70-federation.sql
var sql70 string

//go:embed schema/80-scim.sql
var sql80 string

//go:embed schema/90-device.sql
var sql90 string

//go:embed schema/92-functions.sql
var sql92 string

//go:embed schema/94-triggers.sql
var sql94 string

//go:embed schema/95-rls.sql
var sql95 string

//go:embed schema/99-schema.sql
var sql99 string

// schemaOrder 是脚本执行顺序：表 → 函数 → 触发器 → 策略 → 版本。
var schemaOrder = []schemaScript{
	{name: "10-account.sql", body: sql10},
	{name: "20-projects.sql", body: sql20},
	{name: "30-identity.sql", body: sql30},
	{name: "40-audit.sql", body: sql40},
	{name: "50-integration.sql", body: sql50},
	{name: "60-runtime.sql", body: sql60},
	{name: "70-federation.sql", body: sql70},
	{name: "80-scim.sql", body: sql80},
	{name: "90-device.sql", body: sql90},
	{name: "92-functions.sql", body: sql92},
	{name: "94-triggers.sql", body: sql94},
	{name: "95-rls.sql", body: sql95},
	{name: "99-schema.sql", body: sql99},
}

type schemaScript struct{ name, body string }

// render substitutes the deployment-specific identifiers (control login and
// runtime role) and the schema version into a script.
func (s schemaScript) render(owner, role string, version int) string {
	r := strings.NewReplacer("{owner}", owner, "{role}", role, "{{version}}", fmt.Sprint(version))
	return r.Replace(s.body)
}

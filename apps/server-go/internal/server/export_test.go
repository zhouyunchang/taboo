package server

import (
	"strings"
	"testing"
)

func TestExportScopesToFolder(t *testing.T) {
	f := phase4Ready(t)
	put := func(path, body string) {
		t.Helper()
		checkStatus(t, f.localRequest("POST", path, body, ""), 200)
	}
	createFolder := func(path string) {
		t.Helper()
		checkStatus(t, f.localRequest("POST", "/api/v1/projects/project-a/folders", `{"path":"`+path+`"}`, ""), 201)
	}
	export := func(path string) string {
		t.Helper()
		w := f.localRequest("GET", path, "", "")
		checkStatus(t, w, 200)
		return w.Body.String()
	}

	put("/api/v1/projects/project-a/secrets", `{"key":"ROOT","value":"root-value"}`)
	createFolder("/team/")
	put("/api/v1/projects/project-a/secrets?path=/team/", `{"key":"TEAM","value":"team-value"}`)
	createFolder("/team/sub/")
	put("/api/v1/projects/project-a/secrets?path=/team/sub/", `{"key":"SUB","value":"sub-value"}`)

	// 不带 path 的项目级导出保持整项目，兼容 SDK 与旧调用方。
	all := export("/api/v1/projects/project-a/export")
	for _, want := range []string{"ROOT=root-value\n", "TEAM=team-value\n", "SUB=sub-value\n"} {
		if !strings.Contains(all, want) {
			t.Fatalf("project export missing %q: %q", want, all)
		}
	}

	// 指定目录只导出该目录，不带根目录和子目录。
	if got := export("/api/v1/projects/project-a/export?path=/team/"); got != "TEAM=team-value\n" {
		t.Fatalf("folder export = %q", got)
	}
	// CLI 走的 locator 路径必须与 ?path= 一致。
	if got := export("/api/v1/access?locator=a/default/team&export=1"); got != "TEAM=team-value\n" {
		t.Fatalf("locator folder export = %q", got)
	}
	// 根 locator 只导出根目录。
	if got := export("/api/v1/access?locator=a/default&export=1"); got != "ROOT=root-value\n" {
		t.Fatalf("locator root export = %q", got)
	}

	// recursive=1 包含子目录（locator 与旧 project 路径都要支持）。
	if got := export("/api/v1/access?locator=a/default/team&export=1&recursive=1"); got != "SUB=sub-value\nTEAM=team-value\n" {
		t.Fatalf("recursive folder export = %q", got)
	}
	if got := export("/api/v1/projects/project-a/export?path=/team/&recursive=1"); got != "SUB=sub-value\nTEAM=team-value\n" {
		t.Fatalf("recursive project export = %q", got)
	}
	// 根目录的递归等价于整项目。
	rootRecursive := export("/api/v1/access?locator=a/default&export=1&recursive=1")
	for _, want := range []string{"ROOT=root-value\n", "TEAM=team-value\n", "SUB=sub-value\n"} {
		if !strings.Contains(rootRecursive, want) {
			t.Fatalf("root recursive export missing %q: %q", want, rootRecursive)
		}
	}

	// 不存在的目录返回 404，不能退回整项目，也不能静默给空 .env。
	checkStatus(t, f.localRequest("GET", "/api/v1/access?locator=a/default/missing&export=1", "", ""), 404)
	checkStatus(t, f.localRequest("GET", "/api/v1/projects/project-a/export?path=/missing/", "", ""), 404)

	// 审计必须能看出导出的是哪个目录。
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='secrets.export' AND resource LIKE '%/folder/team%'`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("folder export audit missing: n=%d err=%v", n, err)
	}
}

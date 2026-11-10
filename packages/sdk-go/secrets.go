// taboo Go SDK —— secrets / folders / 组织查询（M3 #8）
package taboo

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// ---------- 实体 ----------

type OrgMembership struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Role string `json:"role"`
}

type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	CreatedAt string `json:"created_at"`
}

type Environment struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	SortOrder int    `json:"sort_order"`
}

type Folder struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
}

type SecretMeta struct {
	ID          string   `json:"id"`
	Folder      string   `json:"folder"`
	Key         string   `json:"key"`
	Comment     string   `json:"comment"`
	Tags        []string `json:"tags"`
	Version     int      `json:"version"`
	UpdatedAt   string   `json:"updated_at"`
	CanReveal   bool     `json:"canReveal"`
	CanWrite    bool     `json:"canWrite"`
	CanDelete   bool     `json:"canDelete"`
	CanRollback bool     `json:"canRollback"`
}

type SecretValue struct {
	SecretMeta
	Value string `json:"value"`
}

type SecretVersion struct {
	Version   int    `json:"version"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
}

// ---------- 组织 / 项目 / 环境 ----------

func (c *Client) Me(ctx context.Context) (*User, []OrgMembership, bool, error) {
	var out struct {
		User        *User           `json:"user"`
		Orgs        []OrgMembership `json:"orgs"`
		TotpEnabled bool            `json:"totp_enabled"`
	}
	if err := c.do(ctx, "GET", "/api/v1/me", nil, &out); err != nil {
		return nil, nil, false, err
	}
	return out.User, out.Orgs, out.TotpEnabled, nil
}

func (c *Client) Projects(ctx context.Context, orgSlug string) ([]Project, error) {
	var out struct {
		Projects []Project `json:"projects"`
	}
	err := c.do(ctx, "GET", "/api/v1/orgs/"+url.PathEscape(orgSlug)+"/projects", nil, &out)
	return out.Projects, err
}

func (c *Client) Environments(ctx context.Context, projectID string) ([]Environment, error) {
	var out struct {
		Environments []Environment `json:"environments"`
	}
	err := c.do(ctx, "GET", "/api/v1/projects/"+url.PathEscape(projectID)+"/environments", nil, &out)
	return out.Environments, err
}

// ---------- 文件夹 ----------

func (c *Client) ListFolders(ctx context.Context, projectID, env string) ([]Folder, error) {
	var out struct {
		Folders []Folder `json:"folders"`
	}
	err := c.do(ctx, "GET", "/api/v1/projects/"+url.PathEscape(projectID)+"/folders"+q(vset("env", env)), nil, &out)
	return out.Folders, err
}

func (c *Client) CreateFolder(ctx context.Context, projectID, env, path string) (*Folder, error) {
	var out Folder
	err := c.do(ctx, "POST", "/api/v1/projects/"+url.PathEscape(projectID)+"/folders"+q(vset("env", env)),
		map[string]string{"path": path}, &out)
	return &out, err
}

// ---------- 密钥 ----------

// ListSecrets ?path= 过滤（空 = 整个环境）
func (c *Client) ListSecrets(ctx context.Context, projectID, env, folderPath string) ([]SecretMeta, error) {
	var out struct {
		Secrets []SecretMeta `json:"secrets"`
	}
	p := "/api/v1/projects/" + url.PathEscape(projectID) + "/secrets" + q(vset("env", env, "path", folderPath))
	err := c.do(ctx, "GET", p, nil, &out)
	return out.Secrets, err
}

// UpsertSecret 创建/更新（产生新版本）；同 key 不同 folderPath 互不冲突
func (c *Client) UpsertSecret(ctx context.Context, projectID, env, folderPath, key, value, comment string, tags []string, options ...MutationOptions) (int, error) {
	var out struct {
		Version int `json:"version"`
	}
	p := "/api/v1/projects/" + url.PathEscape(projectID) + "/secrets" + q(vset("env", env, "path", folderPath))
	opt := firstMutationOption(options)
	body := map[string]any{"key": key, "value": value, "comment": comment, "tags": tags}
	if opt.ExpectedVersion != nil {
		body["expectedVersion"] = *opt.ExpectedVersion
	}
	err := c.doMutation(ctx, "POST", p, body, &out, opt)
	return out.Version, err
}

// GetOptions 控制读取时是否展开 ${env:KEY} / ${project:env:KEY}。
type GetOptions struct {
	Resolve bool
}

func resolvePair(options []GetOptions) string {
	if len(options) > 0 && options[0].Resolve {
		return "true"
	}
	return ""
}

// GetByLocator 按 归属/项目/目录 读取密钥。密钥名不在定位符里。
func (c *Client) GetByLocator(ctx context.Context, locator, key string) (*SecretValue, error) {
	var out SecretValue
	err := c.do(ctx, "GET", "/api/v1/access?locator="+url.QueryEscape(locator)+"&key="+url.QueryEscape(key), nil, &out)
	return &out, err
}

// PutByLocator 按定位符写入密钥。
func (c *Client) PutByLocator(ctx context.Context, locator, key, value string, options ...MutationOptions) (int, error) {
	var out struct {
		Version int `json:"version"`
	}
	opt := firstMutationOption(options)
	body := map[string]any{"key": key, "value": value}
	if opt.ExpectedVersion != nil {
		body["expectedVersion"] = *opt.ExpectedVersion
	}
	err := c.doMutation(ctx, "PUT", "/api/v1/access?locator="+url.QueryEscape(locator), body, &out, opt)
	return out.Version, err
}

// ListByLocator 列出定位符目录下的密钥名。
func (c *Client) ListByLocator(ctx context.Context, locator string) ([]SecretMeta, error) {
	var out struct {
		Secrets []SecretMeta `json:"secrets"`
	}
	err := c.do(ctx, "GET", "/api/v1/access?locator="+url.QueryEscape(locator), nil, &out)
	return out.Secrets, err
}

// GetSecret 读取明文（reveal 权限，记审计）。Resolve 为 true 时展开引用。
func (c *Client) GetSecret(ctx context.Context, projectID, env, folderPath, key string, options ...GetOptions) (*SecretValue, error) {
	var out SecretValue
	p := "/api/v1/projects/" + url.PathEscape(projectID) + "/secrets/" + url.PathEscape(key) + q(vset("env", env, "path", folderPath, "resolve", resolvePair(options)))
	err := c.do(ctx, "GET", p, nil, &out)
	return &out, err
}

// RevealSecretVersion 读取指定历史版本的明文。
func (c *Client) RevealSecretVersion(ctx context.Context, projectID, env, folderPath, key string, version int, options ...GetOptions) (*SecretValue, error) {
	var out SecretValue
	p := "/api/v1/projects/" + url.PathEscape(projectID) + "/secrets/" + url.PathEscape(key) + "/versions/" + strconv.Itoa(version) + q(vset("env", env, "path", folderPath, "resolve", resolvePair(options)))
	err := c.do(ctx, "GET", p, nil, &out)
	return &out, err
}

func (c *Client) SecretVersions(ctx context.Context, projectID, env, folderPath, key string) ([]SecretVersion, int, error) {
	var out struct {
		Versions []SecretVersion `json:"versions"`
		Latest   int             `json:"latest"`
	}
	p := "/api/v1/projects/" + url.PathEscape(projectID) + "/secrets/" + url.PathEscape(key) + "/versions" + q(vset("env", env, "path", folderPath))
	err := c.do(ctx, "GET", p, nil, &out)
	return out.Versions, out.Latest, err
}

// RollbackSecret 回滚到历史版本（以旧值产生新版本）
func (c *Client) RollbackSecret(ctx context.Context, projectID, env, folderPath, key string, version int, options ...MutationOptions) (int, error) {
	var out struct {
		Version int `json:"version"`
	}
	p := "/api/v1/projects/" + url.PathEscape(projectID) + "/secrets/" + url.PathEscape(key) + "/rollback" + q(vset("env", env, "path", folderPath))
	opt := firstMutationOption(options)
	body := map[string]int{"version": version}
	if opt.ExpectedVersion != nil {
		body["expectedVersion"] = *opt.ExpectedVersion
	}
	err := c.doMutation(ctx, "POST", p, body, &out, opt)
	return out.Version, err
}

// ExportSecrets 导出 .env 格式文本。Resolve 为 true 时展开引用。
func (c *Client) ExportSecrets(ctx context.Context, projectID, env string, options ...GetOptions) (string, error) {
	var out string
	p := "/api/v1/projects/" + url.PathEscape(projectID) + "/export" + q(vset("env", env, "resolve", resolvePair(options)))
	err := c.do(ctx, "GET", p, nil, &out)
	return out, err
}

type ImportEntry struct {
	Key     string `json:"key"`
	Action  string `json:"action"`
	Version int    `json:"version"`
}

type ImportResult struct {
	Preview   bool          `json:"preview"`
	Strategy  string        `json:"strategy"`
	Created   int           `json:"created"`
	Updated   int           `json:"updated"`
	Skipped   int           `json:"skipped"`
	Conflicts []string      `json:"conflicts"`
	Entries   []ImportEntry `json:"entries"`
}

// ImportEnv 导入 .env。preview 不写入；已有 key 需要 strategy 为 skip 或 overwrite。
func (c *Client) ImportEnv(ctx context.Context, projectID, env, folderPath, content, strategy string, preview bool, options ...MutationOptions) (*ImportResult, error) {
	var out ImportResult
	p := "/api/v1/projects/" + url.PathEscape(projectID) + "/import" + q(vset("env", env, "path", folderPath))
	body := map[string]any{"content": content, "preview": preview}
	if strategy != "" {
		body["strategy"] = strategy
	}
	err := c.doMutation(ctx, "POST", p, body, &out, firstMutationOption(options))
	return &out, err
}

type ScanFinding struct {
	RuleID      string `json:"rule_id"`
	Level       string `json:"level"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Match       string `json:"match,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Alg         string `json:"alg,omitempty"`
}

type ScanReport struct {
	ID            string        `json:"id"`
	Source        string        `json:"source"`
	ActorName     string        `json:"actor_name"`
	FindingCount  int           `json:"finding_count"`
	CriticalCount int           `json:"critical_count"`
	WarningCount  int           `json:"warning_count"`
	CreatedAt     string        `json:"created_at"`
	Findings      []ScanFinding `json:"findings"`
}

func (c *Client) CreateScanReport(ctx context.Context, orgSlug, source string, findings []ScanFinding) (*ScanReport, error) {
	var out ScanReport
	if findings == nil {
		findings = []ScanFinding{}
	}
	err := c.do(ctx, "POST", "/api/v1/orgs/"+url.PathEscape(orgSlug)+"/scan-reports", map[string]any{"source": source, "findings": findings}, &out)
	return &out, err
}

func (c *Client) ListScanReports(ctx context.Context, orgSlug string) ([]ScanReport, error) {
	var out struct {
		Reports []ScanReport `json:"reports"`
	}
	err := c.do(ctx, "GET", "/api/v1/orgs/"+url.PathEscape(orgSlug)+"/scan-reports", nil, &out)
	return out.Reports, err
}

func (c *Client) GetScanReport(ctx context.Context, orgSlug, id string) (*ScanReport, error) {
	var out ScanReport
	err := c.do(ctx, "GET", "/api/v1/orgs/"+url.PathEscape(orgSlug)+"/scan-reports/"+url.PathEscape(id), nil, &out)
	return &out, err
}

// DefaultProject 取组织内 default 项目（无则第一个），免手工传 projectID
func (c *Client) DefaultProject(ctx context.Context, orgSlug string) (*Project, error) {
	ps, err := c.Projects(ctx, orgSlug)
	if err != nil {
		return nil, err
	}
	for i := range ps {
		if ps[i].Slug == "default" {
			return &ps[i], nil
		}
	}
	if len(ps) == 0 {
		return nil, fmt.Errorf("no project in org %s", orgSlug)
	}
	return &ps[0], nil
}

func firstMutationOption(options []MutationOptions) MutationOptions {
	if len(options) > 0 {
		return options[0]
	}
	return MutationOptions{}
}

func (c *Client) DeleteSecret(ctx context.Context, projectID, env, folderPath, key string, options ...MutationOptions) error {
	opt := firstMutationOption(options)
	query := vset("env", env, "path", folderPath)
	if opt.ExpectedVersion != nil {
		query.Set("expectedVersion", strconv.Itoa(*opt.ExpectedVersion))
	}
	p := "/api/v1/projects/" + url.PathEscape(projectID) + "/secrets/" + url.PathEscape(key) + q(query)
	return c.doMutation(ctx, "DELETE", p, nil, nil, opt)
}

// taboo Go SDK —— secrets / folders / 组织查询（M3 #8）
package taboo

import (
	"context"
	"fmt"
	"net/url"
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
	ID        string   `json:"id"`
	Folder    string   `json:"folder"`
	Key       string   `json:"key"`
	Comment   string   `json:"comment"`
	Tags      []string `json:"tags"`
	Version   int      `json:"version"`
	UpdatedAt string   `json:"updated_at"`
	CanReveal bool     `json:"canReveal"`
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
func (c *Client) UpsertSecret(ctx context.Context, projectID, env, folderPath, key, value, comment string, tags []string) (int, error) {
	var out struct {
		Version int `json:"version"`
	}
	p := "/api/v1/projects/" + url.PathEscape(projectID) + "/secrets" + q(vset("env", env, "path", folderPath))
	err := c.do(ctx, "POST", p, map[string]any{"key": key, "value": value, "comment": comment, "tags": tags}, &out)
	return out.Version, err
}

// GetSecret 读取明文（reveal 权限，记审计）
func (c *Client) GetSecret(ctx context.Context, projectID, env, folderPath, key string) (*SecretValue, error) {
	var out SecretValue
	p := "/api/v1/projects/" + url.PathEscape(projectID) + "/secrets/" + url.PathEscape(key) + q(vset("env", env, "path", folderPath))
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
func (c *Client) RollbackSecret(ctx context.Context, projectID, env, folderPath, key string, version int) (int, error) {
	var out struct {
		Version int `json:"version"`
	}
	p := "/api/v1/projects/" + url.PathEscape(projectID) + "/secrets/" + url.PathEscape(key) + "/rollback" + q(vset("env", env, "path", folderPath))
	err := c.do(ctx, "POST", p, map[string]int{"version": version}, &out)
	return out.Version, err
}

// ExportSecrets 导出 .env 格式文本
func (c *Client) ExportSecrets(ctx context.Context, projectID, env string) (string, error) {
	var out string
	p := "/api/v1/projects/" + url.PathEscape(projectID) + "/export" + q(vset("env", env))
	err := c.do(ctx, "GET", p, nil, &out)
	return out, err
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

import { useCallback, useEffect, useState } from 'react';
import { api, ApiError } from '../api';
import type { User, Org, Project, Env, SecretMeta, SecretValue, Version, AuditLog } from '../api';

interface Props {
  user: User;
  orgs: Org[];
  onOrgChange: (orgs: Org[]) => void;
  onLogout: () => void;
}

export default function Workspace({ user, orgs, onOrgChange, onLogout }: Props) {
  const org = orgs[0];
  const [projects, setProjects] = useState<Project[]>([]);
  const [project, setProject] = useState<Project | null>(null);
  const [envs, setEnvs] = useState<Env[]>([]);
  const [env, setEnv] = useState('dev');
  const [tab, setTab] = useState<'secrets' | 'audit'>('secrets');
  const [error, setError] = useState('');

  const loadProjects = useCallback(async () => {
    if (!org) return;
    const d = await api.get<{ projects: Project[] }>(`/api/v1/orgs/${org.slug}/projects`);
    setProjects(d.projects);
    setProject((p) => p ?? d.projects[0] ?? null);
  }, [org?.slug]);

  useEffect(() => { loadProjects().catch((e) => setError(String(e))); }, [loadProjects]);

  useEffect(() => {
    if (!project) return;
    api.get<{ environments: Env[] }>(`/api/v1/projects/${project.id}/environments`)
      .then((d) => setEnvs(d.environments))
      .catch((e) => setError(String(e)));
  }, [project?.id]);

  async function createProject() {
    const name = window.prompt('新项目名称？');
    if (!name) return;
    try {
      await api.post(`/api/v1/orgs/${org.slug}/projects`, { name });
      await loadProjects();
    } catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
  }

  if (!org) return <div className="center-screen">没有组织</div>;

  return (
    <div className="layout">
      <header className="topbar">
        <div className="brand small">
          <span className="brand-mark">禁</span>
          <strong>taboo · 禁制</strong>
        </div>
        <span className="muted">{org.name} <em>({org.role})</em></span>
        <div className="spacer" />
        <span>{user.name || user.email}</span>
        <button className="ghost" onClick={onLogout}>退出</button>
      </header>

      <div className="body">
        <aside className="sidebar">
          <div className="sidebar-title">
            项目
            <button className="ghost" onClick={createProject} title="新建项目">＋</button>
          </div>
          {projects.map((p) => (
            <div key={p.id} className={`nav-item ${project?.id === p.id ? 'active' : ''}`} onClick={() => { setProject(p); setTab('secrets'); }}>
              {p.name}
            </div>
          ))}
          {error && <div className="error">{error}</div>}
        </aside>

        <main className="main">
          <div className="tabs">
            {envs.map((e) => (
              <button key={e.id} className={`tab ${env === e.slug ? 'active' : ''}`} onClick={() => { setEnv(e.slug); setTab('secrets'); }}>
                {e.name}
              </button>
            ))}
            <div className="spacer" />
            <button className={`tab ${tab === 'audit' ? 'active' : ''}`} onClick={() => setTab('audit')}>审计日志</button>
          </div>
          {tab === 'secrets'
            ? project && <SecretsPanel key={`${project.id}:${env}`} projectId={project.id} env={env} projectSlug={project.slug} />
            : <AuditPanel orgSlug={org.slug} />}
        </main>
      </div>
    </div>
  );
}

function SecretsPanel({ projectId, env, projectSlug }: { projectId: string; env: string; projectSlug: string }) {
  const [secrets, setSecrets] = useState<SecretMeta[]>([]);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<string | null>(null); // key 或 '__new__'
  const [detail, setDetail] = useState<string | null>(null);

  const load = useCallback(() => {
    api.get<{ secrets: SecretMeta[] }>(`/api/v1/projects/${projectId}/secrets?env=${env}`)
      .then((d) => setSecrets(d.secrets))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  }, [projectId, env]);

  useEffect(load, [load]);

  return (
    <div className="panel">
      <div className="panel-head">
        <h2>{projectSlug} / {env}</h2>
        <button onClick={() => setEditing('__new__')}>＋ 新建密钥</button>
      </div>
      {error && <div className="error">{error}</div>}
      <table className="table">
        <thead>
          <tr><th>Key</th><th>值</th><th>标签</th><th>版本</th><th>更新时间</th><th /></tr>
        </thead>
        <tbody>
          {secrets.map((s) => (
            <SecretRow key={s.id} s={s} projectId={projectId} env={env}
              onChanged={load} onEdit={() => setEditing(s.key)} onDetail={() => setDetail(s.key)} />
          ))}
          {secrets.length === 0 && <tr><td colSpan={6} className="muted center">该环境还没有密钥</td></tr>}
        </tbody>
      </table>
      {editing && (
        <SecretEditor projectId={projectId} env={env} secretKey={editing === '__new__' ? '' : editing}
          onClose={() => setEditing(null)} onSaved={() => { setEditing(null); load(); }} />
      )}
      {detail && (
        <DetailDrawer projectId={projectId} env={env} secretKey={detail}
          onClose={() => setDetail(null)} />
      )}
    </div>
  );
}

function SecretRow({ s, projectId, env, onChanged, onEdit, onDetail }: {
  s: SecretMeta; projectId: string; env: string;
  onChanged: () => void; onEdit: () => void; onDetail: () => void;
}) {
  const [value, setValue] = useState<string | null>(null);
  const [err, setErr] = useState('');

  async function reveal() {
    if (value !== null) { setValue(null); return; }
    try {
      const d = await api.get<SecretValue>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(s.key)}?env=${env}`);
      setValue(d.value);
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
  }

  async function copy() {
    try {
      const d = value ?? await api.get<SecretValue>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(s.key)}?env=${env}`).then((x) => x.value);
      await navigator.clipboard.writeText(d);
      setTimeout(() => navigator.clipboard.writeText('').catch(() => {}), 20_000); // 20s 自动清空剪贴板
    } catch { setErr('复制失败'); }
  }

  return (
    <tr>
      <td className="mono key" onClick={onDetail} title="查看详情/版本">{s.key}</td>
      <td className="mono">
        {value === null ? '••••••••' : value}
        {err && <span className="error"> {err}</span>}
      </td>
      <td>{s.tags.map((t) => <span key={t} className="tag">{t}</span>)}</td>
      <td>v{s.version}</td>
      <td className="muted">{s.updated_at}</td>
      <td className="actions">
        {s.canReveal && <button className="ghost" onClick={reveal}>{value === null ? '显示' : '隐藏'}</button>}
        {s.canReveal && <button className="ghost" onClick={copy}>复制</button>}
        <button className="ghost" onClick={onEdit}>编辑</button>
      </td>
    </tr>
  );
}

function SecretEditor({ projectId, env, secretKey, onClose, onSaved }: {
  projectId: string; env: string; secretKey: string; onClose: () => void; onSaved: () => void;
}) {
  const [key, setKey] = useState(secretKey);
  const [value, setValue] = useState('');
  const [comment, setComment] = useState('');
  const [tags, setTags] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr('');
    try {
      await api.post(`/api/v1/projects/${projectId}/secrets?env=${env}`, {
        key, value, comment,
        tags: tags.split(',').map((t) => t.trim()).filter(Boolean),
      });
      onSaved();
    } catch (e2) { setErr(e2 instanceof ApiError ? e2.message : String(e2)); }
    finally { setBusy(false); }
  }

  return (
    <div className="drawer" onClick={onClose}>
      <form className="drawer-inner" onClick={(e) => e.stopPropagation()} onSubmit={save}>
        <h3>{secretKey ? `更新密钥 — ${secretKey}` : '新建密钥'}</h3>
        <label>Key<input value={key} onChange={(e) => setKey(e.target.value)} required disabled={!!secretKey} /></label>
        <label>值<textarea value={value} onChange={(e) => setValue(e.target.value)} required rows={4} /></label>
        <label>备注<input value={comment} onChange={(e) => setComment(e.target.value)} /></label>
        <label>标签（逗号分隔）<input value={tags} onChange={(e) => setTags(e.target.value)} placeholder="db, prod" /></label>
        {err && <div className="error">{err}</div>}
        <div className="drawer-actions">
          <button type="button" className="ghost" onClick={onClose}>取消</button>
          <button type="submit" disabled={busy}>{busy ? '保存中…' : '保存（产生新版本）'}</button>
        </div>
      </form>
    </div>
  );
}

function DetailDrawer({ projectId, env, secretKey, onClose }: {
  projectId: string; env: string; secretKey: string; onClose: () => void;
}) {
  const [versions, setVersions] = useState<Version[]>([]);
  const [latest, setLatest] = useState(0);
  const [err, setErr] = useState('');

  const load = useCallback(() => {
    api.get<{ versions: Version[]; latest: number }>(
      `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}/versions?env=${env}`)
      .then((d) => { setVersions(d.versions); setLatest(d.latest); })
      .catch((e) => setErr(e instanceof ApiError ? e.message : String(e)));
  }, [projectId, env, secretKey]);
  useEffect(load, [load]);

  async function rollback(v: number) {
    if (!window.confirm(`回滚 ${secretKey} 到 v${v}？（将产生新版本）`)) return;
    try {
      await api.post(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}/rollback?env=${env}`, { version: v });
      load();
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
  }

  return (
    <div className="drawer" onClick={onClose}>
      <div className="drawer-inner" onClick={(e) => e.stopPropagation()}>
        <h3>{secretKey} — 版本历史</h3>
        {err && <div className="error">{err}</div>}
        <table className="table">
          <thead><tr><th>版本</th><th>创建人</th><th>时间</th><th /></tr></thead>
          <tbody>
            {versions.map((v) => (
              <tr key={v.version} className={v.version === latest ? 'latest' : ''}>
                <td>v{v.version}{v.version === latest && '（当前）'}</td>
                <td className="muted">{v.created_by.slice(0, 8)}</td>
                <td className="muted">{v.created_at}</td>
                <td>{v.version !== latest && <button className="ghost" onClick={() => rollback(v.version)}>回滚到此</button>}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <div className="drawer-actions"><button className="ghost" onClick={onClose}>关闭</button></div>
      </div>
    </div>
  );
}

function AuditPanel({ orgSlug }: { orgSlug: string }) {
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [filter, setFilter] = useState('');

  useEffect(() => {
    api.get<{ logs: AuditLog[] }>(`/api/v1/orgs/${orgSlug}/audit?limit=200`)
      .then((d) => setLogs(d.logs))
      .catch(() => {});
  }, [orgSlug]);

  const shown = logs.filter((l) =>
    !filter || l.action.includes(filter) || l.resource.includes(filter) || l.actor_name.includes(filter));

  return (
    <div className="panel">
      <div className="panel-head">
        <h2>审计日志</h2>
        <input placeholder="筛选 actor / action / resource" value={filter} onChange={(e) => setFilter(e.target.value)} />
      </div>
      <table className="table">
        <thead><tr><th>时间</th><th>操作者</th><th>动作</th><th>资源</th><th>IP</th></tr></thead>
        <tbody>
          {shown.map((l) => (
            <tr key={l.id}>
              <td className="muted nowrap">{l.created_at}</td>
              <td>{l.actor_name}</td>
              <td><span className="tag">{l.action}</span></td>
              <td className="mono">{l.resource}</td>
              <td className="muted">{l.ip}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

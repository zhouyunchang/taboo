import { useCallback, useEffect, useState } from 'react';
import { api, ApiError } from '../api';
import type { User, Org, Project, Env, Folder, SecretMeta, SecretValue, Version, AuditLog, Identity, CreatedIdentity, IdentityScope, DynamicEngine, DynamicLease } from '../api';
import SettingsPanel from './Settings';

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
  const [tab, setTab] = useState<'secrets' | 'audit' | 'identities' | 'dynamic' | 'settings'>('secrets');
  const [show2FA, setShow2FA] = useState(false);
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
        <button className="ghost" onClick={() => setShow2FA(true)}>两步验证</button>
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
            <button className={`tab ${tab === 'identities' ? 'active' : ''}`} onClick={() => setTab('identities')}>机器身份</button>
            <button className={`tab ${tab === 'dynamic' ? 'active' : ''}`} onClick={() => setTab('dynamic')}>动态密钥</button>
            <button className={`tab ${tab === 'audit' ? 'active' : ''}`} onClick={() => setTab('audit')}>审计日志</button>
            <button className={`tab ${tab === 'settings' ? 'active' : ''}`} onClick={() => setTab('settings')}>组织设置</button>
          </div>
          {tab === 'secrets'
            ? project && <SecretsPanel key={`${project.id}:${env}`} projectId={project.id} env={env} projectSlug={project.slug} />
            : tab === 'identities'
              ? <IdentitiesPanel orgSlug={org.slug} project={project} envs={envs} />
              : tab === 'dynamic'
                ? project && <DynamicPanel key={project.id} projectId={project.id} projectSlug={project.slug} />
                : tab === 'settings'
                  ? <SettingsPanel orgSlug={org.slug} role={org.role} />
                  : <AuditPanel orgSlug={org.slug} />}
        </main>
      </div>
      {show2FA && <TotpPanel onClose={() => setShow2FA(false)} />}
    </div>
  );
}

// 两步验证（TOTP 2FA）自助管理：开启（扫码+激活）、恢复码、关闭
function TotpPanel({ onClose }: { onClose: () => void }) {
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [setup, setSetup] = useState<{ secret: string; otpauth_url: string; qr_png: string } | null>(null);
  const [codes, setCodes] = useState<string[] | null>(null);
  const [code, setCode] = useState('');
  const [password, setPassword] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api.get<{ totp_enabled: boolean }>('/api/v1/me')
      .then((d) => setEnabled(d.totp_enabled))
      .catch((e) => setErr(e instanceof ApiError ? e.message : String(e)));
  }, []);

  async function startSetup() {
    setErr(''); setBusy(true);
    try {
      const d = await api.post<{ secret: string; otpauth_url: string; qr_png: string }>('/api/v1/auth/totp/setup');
      setSetup(d); setCodes(null);
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
    finally { setBusy(false); }
  }

  async function verify(e: React.FormEvent) {
    e.preventDefault();
    setErr(''); setBusy(true);
    try {
      const d = await api.post<{ enabled: boolean; recovery_codes: string[] }>('/api/v1/auth/totp/verify', { code: code.trim() });
      setEnabled(d.enabled); setCodes(d.recovery_codes); setSetup(null); setCode('');
    } catch (e2) { setErr(e2 instanceof ApiError ? e2.message : String(e2)); }
    finally { setBusy(false); }
  }

  async function disable(e: React.FormEvent) {
    e.preventDefault();
    if (!window.confirm('关闭两步验证？恢复码将一并作废。')) return;
    setErr(''); setBusy(true);
    try {
      await api.post('/api/v1/auth/totp/disable', { password });
      setEnabled(false); setPassword('');
    } catch (e2) { setErr(e2 instanceof ApiError ? e2.message : String(e2)); }
    finally { setBusy(false); }
  }

  return (
    <div className="drawer" onClick={onClose}>
      <div className="drawer-inner" onClick={(e) => e.stopPropagation()}>
        <h3>两步验证（TOTP）</h3>
        {err && <div className="error">{err}</div>}
        {enabled === null && <p className="muted">加载中…</p>}

        {enabled === false && !setup && !codes && (
          <>
            <p className="muted">开启后，登录除密码外还需验证器动态码（兼容 Google Authenticator / 1Password 等）。</p>
            <button onClick={startSetup} disabled={busy}>{busy ? '生成中…' : '开始设置'}</button>
          </>
        )}

        {enabled === false && setup && (
          <form onSubmit={verify} className="totp-setup">
            <p>用验证器扫描下方二维码，或手动录入密钥：</p>
            <img className="qr" src={setup.qr_png} alt="TOTP QR Code" />
            <label>密钥（base32）<input readOnly value={setup.secret} onFocus={(e) => e.target.select()} /></label>
            <label>验证器上的 6 位码<input value={code} onChange={(e) => setCode(e.target.value)} required pattern="\d{6}" placeholder="123456" autoFocus /></label>
            <div className="drawer-actions">
              <button type="button" className="ghost" onClick={() => setSetup(null)}>取消</button>
              <button type="submit" disabled={busy}>{busy ? '激活中…' : '验证并开启'}</button>
            </div>
          </form>
        )}

        {codes && (
          <div className="card once-secret">
            <h3>已开启！恢复码仅此一次展示，请立即离线保存</h3>
            <ol className="recovery-codes mono">
              {codes.map((c) => <li key={c}>{c}</li>)}
            </ol>
            <button className="ghost" onClick={() => setCodes(null)}>我已保存</button>
          </div>
        )}

        {enabled === true && !codes && (
          <form onSubmit={disable}>
            <p><span className="tag ok">已开启</span> <span className="muted">登录需密码 + 动态码。</span></p>
            <label>输入密码确认关闭<input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required /></label>
            <div className="drawer-actions">
              <button type="button" className="ghost" onClick={onClose}>关闭</button>
              <button type="submit" disabled={busy}>关闭两步验证</button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}

function SecretsPanel({ projectId, env, projectSlug }: { projectId: string; env: string; projectSlug: string }) {
  const [folders, setFolders] = useState<Folder[]>([]);
  const [secrets, setSecrets] = useState<SecretMeta[]>([]);
  const [cur, setCur] = useState('/'); // 当前文件夹路径（物化路径 /a/b/）
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<string | null>(null); // key 或 '__new__'
  const [detail, setDetail] = useState<SecretMeta | null>(null);

  const loadFolders = useCallback(() => {
    api.get<{ folders: Folder[] }>(`/api/v1/projects/${projectId}/folders?env=${env}`)
      .then((d) => setFolders(d.folders))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  }, [projectId, env]);

  const load = useCallback(() => {
    api.get<{ secrets: SecretMeta[] }>(`/api/v1/projects/${projectId}/secrets?env=${env}&path=${encodeURIComponent(cur)}`)
      .then((d) => setSecrets(d.secrets))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  }, [projectId, env, cur]);

  useEffect(load, [load]);
  useEffect(loadFolders, [loadFolders]);

  async function createFolder() {
    const name = window.prompt(`在当前文件夹 ${cur} 下新建子文件夹名称？`);
    if (!name) return;
    const clean = name.trim().replace(/^\/+|\/+$/g, '');
    if (!clean) return;
    try {
      await api.post(`/api/v1/projects/${projectId}/folders?env=${env}`, { path: cur + clean + '/' });
      loadFolders();
    } catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
  }

  async function deleteFolder() {
    if (cur === '/') return;
    if (!window.confirm(`删除空文件夹 ${cur}？（仅空文件夹可删除）`)) return;
    try {
      await api.del(`/api/v1/projects/${projectId}/folders?env=${env}&path=${encodeURIComponent(cur)}`);
      setCur(parentOf(cur));
      loadFolders();
    } catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
  }

  const crumbs = cur === '/' ? [] : cur.replace(/^\/+|\/+$/g, '').split('/');

  return (
    <div className="panel">
      <div className="panel-head">
        <h2>{projectSlug} / {env}</h2>
        <button className="ghost" onClick={createFolder}>＋ 新建文件夹</button>
        {cur !== '/' && <button className="ghost" onClick={deleteFolder}>删除此空文件夹</button>}
        <button onClick={() => setEditing('__new__')}>＋ 新建密钥</button>
      </div>
      <div className="crumbs mono">
        <span className={`crumb ${cur === '/' ? 'active' : ''}`} onClick={() => setCur('/')}>根目录</span>
        {crumbs.map((c, i) => {
          const p = '/' + crumbs.slice(0, i + 1).join('/') + '/';
          return <span key={p}><span className="muted"> / </span>
            <span className={`crumb ${cur === p ? 'active' : ''}`} onClick={() => setCur(p)}>{c}</span></span>;
        })}
      </div>
      {error && <div className="error">{error}</div>}
      <div className="split">
        <aside className="folder-tree">
          <FolderTree folders={folders} cur={cur} onSelect={setCur} />
        </aside>
        <div className="grow">
          <table className="table">
            <thead>
              <tr><th>Key</th><th>值</th><th>标签</th><th>版本</th><th>更新时间</th><th /></tr>
            </thead>
            <tbody>
              {secrets.map((s) => (
                <SecretRow key={s.id} s={s} projectId={projectId} env={env}
                  onChanged={load} onEdit={() => setEditing(s.key)} onDetail={() => setDetail(s)} />
              ))}
              {secrets.length === 0 && <tr><td colSpan={6} className="muted center">此文件夹还没有密钥</td></tr>}
            </tbody>
          </table>
        </div>
      </div>
      {editing && (
        <SecretEditor projectId={projectId} env={env} path={cur} secretKey={editing === '__new__' ? '' : editing}
          onClose={() => setEditing(null)} onSaved={() => { setEditing(null); load(); }} />
      )}
      {detail && (
        <DetailDrawer projectId={projectId} env={env} meta={detail}
          onClose={() => setDetail(null)} />
      )}
    </div>
  );
}

function parentOf(path: string) {
  const t = path.replace(/^\/+|\/+$/g, '');
  const i = t.lastIndexOf('/');
  return i < 0 ? '/' : '/' + t.slice(0, i) + '/';
}

// 平铺 folders → 按 parent_id 嵌套的树（后端已保证根 '/' 存在）
function FolderTree({ folders, cur, onSelect }: { folders: Folder[]; cur: string; onSelect: (p: string) => void }) {
  const byParent = new Map<string, Folder[]>();
  for (const f of folders) {
    const list = byParent.get(f.parent_id) ?? [];
    list.push(f);
    byParent.set(f.parent_id, list);
  }
  const root = folders.find((f) => f.path === '/');
  if (!root) return <div className="muted">无文件夹</div>;
  return (
    <ul className="tree">
      <TreeNode f={root} byParent={byParent} cur={cur} onSelect={onSelect} />
    </ul>
  );
}

function TreeNode({ f, byParent, cur, onSelect }: {
  f: Folder; byParent: Map<string, Folder[]>; cur: string; onSelect: (p: string) => void;
}) {
  const [open, setOpen] = useState(true);
  const kids = byParent.get(f.id) ?? [];
  return (
    <li>
      <div className={`tree-item ${cur === f.path ? 'active' : ''}`} onClick={() => onSelect(f.path)}>
        {kids.length > 0
          ? <span className="tree-toggle" onClick={(e) => { e.stopPropagation(); setOpen(!open); }}>{open ? '▾' : '▸'}</span>
          : <span className="tree-toggle" />}
        📁 {f.name}
      </div>
      {open && kids.length > 0 && (
        <ul className="tree">
          {kids.map((k) => <TreeNode key={k.id} f={k} byParent={byParent} cur={cur} onSelect={onSelect} />)}
        </ul>
      )}
    </li>
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
      const d = await api.get<SecretValue>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(s.key)}?env=${env}&path=${encodeURIComponent(s.folder)}`);
      setValue(d.value);
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
  }

  async function copy() {
    try {
      const d = value ?? await api.get<SecretValue>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(s.key)}?env=${env}&path=${encodeURIComponent(s.folder)}`).then((x) => x.value);
      await navigator.clipboard.writeText(d);
      setTimeout(() => navigator.clipboard.writeText('').catch(() => {}), 20_000); // 20s 自动清空剪贴板
    } catch { setErr('复制失败'); }
  }

  async function remove() {
    if (!window.confirm(`删除密钥 ${s.key}？（版本历史一并删除，不可恢复）`)) return;
    try {
      await api.del(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(s.key)}?env=${env}&path=${encodeURIComponent(s.folder)}`);
      onChanged();
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
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
        <button className="ghost" onClick={remove}>删除</button>
      </td>
    </tr>
  );
}

function SecretEditor({ projectId, env, path, secretKey, onClose, onSaved }: {
  projectId: string; env: string; path: string; secretKey: string; onClose: () => void; onSaved: () => void;
}) {
  const [key, setKey] = useState(secretKey);
  const [value, setValue] = useState('');
  const [comment, setComment] = useState('');
  const [tags, setTags] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (key.includes('/') || key.trim() !== key || key === '.' || key === '..') {
      setErr('Key 不能包含 /（用文件夹分层），且不能是 . / .. 或首尾空白');
      return;
    }
    setBusy(true);
    setErr('');
    try {
      await api.post(`/api/v1/projects/${projectId}/secrets?env=${env}&path=${encodeURIComponent(path)}`, {
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
        <h3>{secretKey ? `更新密钥 — ${secretKey}` : `新建密钥 — ${path}`}</h3>
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

function DetailDrawer({ projectId, env, meta, onClose }: {
  projectId: string; env: string; meta: SecretMeta; onClose: () => void;
}) {
  const secretKey = meta.key;
  const folder = meta.folder;
  const [versions, setVersions] = useState<Version[]>([]);
  const [latest, setLatest] = useState(0);
  const [err, setErr] = useState('');

  const load = useCallback(() => {
    api.get<{ versions: Version[]; latest: number }>(
      `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}/versions?env=${env}&path=${encodeURIComponent(folder)}`)
      .then((d) => { setVersions(d.versions); setLatest(d.latest); })
      .catch((e) => setErr(e instanceof ApiError ? e.message : String(e)));
  }, [projectId, env, secretKey, folder]);
  useEffect(load, [load]);

  async function rollback(v: number) {
    if (!window.confirm(`回滚 ${secretKey} 到 v${v}？（将产生新版本）`)) return;
    try {
      await api.post(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}/rollback?env=${env}&path=${encodeURIComponent(folder)}`, { version: v });
      load();
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
  }

  return (
    <div className="drawer" onClick={onClose}>
      <div className="drawer-inner" onClick={(e) => e.stopPropagation()}>
        <h3>{secretKey} — 版本历史 <span className="muted mono">{folder}</span></h3>
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

  // 导出带鉴权：fetch blob → 触发浏览器下载（window.open 不带 Authorization）
  async function download(format: string) {
    const res = await fetch(`/api/v1/orgs/${orgSlug}/audit/export?format=${format}`, {
      headers: { Authorization: `Bearer ${localStorage.getItem('taboo.access') ?? ''}` },
    });
    if (!res.ok) { alert('导出失败'); return; }
    const blob = await res.blob();
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `taboo-audit.${format}`;
    a.click();
    URL.revokeObjectURL(a.href);
  }

  return (
    <div className="panel">
      <div className="panel-head">
        <h2>审计日志</h2>
        <input placeholder="筛选 actor / action / resource" value={filter} onChange={(e) => setFilter(e.target.value)} />
        <button className="ghost" onClick={() => download('csv')}>导出 CSV</button>
        <button className="ghost" onClick={() => download('jsonl')}>导出 JSONL</button>
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


function IdentitiesPanel({ orgSlug, project, envs }: { orgSlug: string; project: Project | null; envs: Env[] }) {
  const [identities, setIdentities] = useState<Identity[]>([]);
  const [error, setError] = useState('');
  const [showNew, setShowNew] = useState(false);
  const [once, setOnce] = useState<CreatedIdentity | null>(null);

  const load = useCallback(() => {
    api.get<{ identities: Identity[] }>(`/api/v1/orgs/${orgSlug}/identities`)
      .then((d) => setIdentities(d.identities))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  }, [orgSlug]);
  useEffect(load, [load]);

  async function revoke(id: string, name: string) {
    if (!window.confirm(`吊销机器身份「${name}」？其 token 将立即失效。`)) return;
    try {
      await api.post(`/api/v1/orgs/${orgSlug}/identities/${id}/revoke`);
      load();
    } catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
  }

  return (
    <div className="panel">
      <div className="panel-head">
        <h2>机器身份</h2>
        <button onClick={() => setShowNew(true)}>＋ 创建机器身份</button>
      </div>
      <p className="muted">为 CI/CD、AI Agent 颁发 client_id + client_secret，作用域显式限定到 项目 + 环境 + 读写，禁止通配。</p>
      {error && <div className="error">{error}</div>}
      {once && (
        <div className="card once-secret">
          <h3>「{once.name}」已创建 — client_secret 仅此一次展示，请立即保存</h3>
          <label>Client ID<input readOnly value={once.client_id} onFocus={(e) => e.target.select()} /></label>
          <label>Client Secret<input readOnly value={once.client_secret} onFocus={(e) => e.target.select()} /></label>
          <p className="muted">换 token：<code>POST /api/v1/identities/token</code>，TTL {once.token_ttl}s</p>
          <button className="ghost" onClick={() => setOnce(null)}>我已保存，关闭</button>
        </div>
      )}
      <table className="table">
        <thead><tr><th>名称</th><th>Client ID</th><th>Scope（项目 / 环境 / 权限）</th><th>状态</th><th>TTL</th><th /></tr></thead>
        <tbody>
          {identities.map((i) => (
            <tr key={i.id}>
              <td className="mono">{i.name}</td>
              <td className="mono muted">{i.client_id}</td>
              <td>{i.scopes.map((s, idx) => (
                <div key={idx}><span className="tag">{s.project_slug}</span><span className="tag">{s.env}</span><span className="tag">{s.permission}</span></div>
              ))}</td>
              <td><span className={`tag ${i.status === 'active' ? 'ok' : 'bad'}`}>{i.status}</span></td>
              <td className="muted">{i.token_ttl}s</td>
              <td>{i.status === 'active' && <button className="ghost" onClick={() => revoke(i.id, i.name)}>吊销</button>}</td>
            </tr>
          ))}
          {identities.length === 0 && <tr><td colSpan={6} className="muted center">还没有机器身份</td></tr>}
        </tbody>
      </table>
      {showNew && project && (
        <IdentityCreator
          orgSlug={orgSlug}
          project={project}
          envs={envs}
          onClose={() => setShowNew(false)}
          onCreated={(d) => { setShowNew(false); setOnce(d); load(); }}
        />
      )}
    </div>
  );
}

function IdentityCreator({ orgSlug, project, envs, onClose, onCreated }: {
  orgSlug: string; project: Project; envs: Env[];
  onClose: () => void; onCreated: (d: CreatedIdentity) => void;
}) {
  const [name, setName] = useState('');
  const [ttl, setTtl] = useState(900);
  const [scopes, setScopes] = useState<IdentityScope[]>([{ project_id: project.id, project_slug: project.slug, env: 'dev', permission: 'read' }]);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr('');
    try {
      const d = await api.post<CreatedIdentity>(`/api/v1/orgs/${orgSlug}/identities`, {
        name, token_ttl: ttl,
        scopes: scopes.map((s) => ({ project_id: s.project_id, env: s.env, permission: s.permission })),
      });
      onCreated(d);
    } catch (e2) { setErr(e2 instanceof ApiError ? e2.message : String(e2)); }
    finally { setBusy(false); }
  }

  return (
    <div className="drawer" onClick={onClose}>
      <form className="drawer-inner" onClick={(e) => e.stopPropagation()} onSubmit={save}>
        <h3>创建机器身份</h3>
        <label>名称<input value={name} onChange={(e) => setName(e.target.value)} required placeholder="ci-runner" /></label>
        <label>Token TTL（秒，≤3600）<input type="number" value={ttl} onChange={(e) => setTtl(Number(e.target.value))} min={60} max={3600} /></label>
        <div className="scope-editor">
          <div className="sidebar-title">Scopes（至少一项，禁止通配）</div>
          {scopes.map((s, idx) => (
            <div key={idx} className="scope-row">
              <span className="tag">{project.slug}</span>
              <select value={s.env} onChange={(e) => setScopes(scopes.map((x, i) => i === idx ? { ...x, env: e.target.value } : x))}>
                {envs.map((en) => <option key={en.id} value={en.slug}>{en.name}</option>)}
              </select>
              <select value={s.permission} onChange={(e) => setScopes(scopes.map((x, i) => i === idx ? { ...x, permission: e.target.value } : x))}>
                <option value="read">read</option>
                <option value="write">write</option>
              </select>
              <button type="button" className="ghost" disabled={scopes.length === 1} onClick={() => setScopes(scopes.filter((_, i) => i !== idx))}>✕</button>
            </div>
          ))}
          <button type="button" className="ghost" onClick={() => setScopes([...scopes, { project_id: project.id, project_slug: project.slug, env: envs[0]?.slug || 'dev', permission: 'read' }])}>＋ 添加 scope</button>
        </div>
        {err && <div className="error">{err}</div>}
        <div className="drawer-actions">
          <button type="button" className="ghost" onClick={onClose}>取消</button>
          <button type="submit" disabled={busy}>{busy ? '创建中…' : '创建'}</button>
        </div>
      </form>
    </div>
  );
}

function DynamicPanel({ projectId, projectSlug }: { projectId: string; projectSlug: string }) {
  const [engines, setEngines] = useState<DynamicEngine[]>([]);
  const [error, setError] = useState('');
  const [showNew, setShowNew] = useState(false);

  const load = useCallback(() => {
    api.get<{ engines: DynamicEngine[] }>(`/api/v1/projects/${projectId}/dynamic-engines`)
      .then((d) => setEngines(d.engines))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  }, [projectId]);
  useEffect(load, [load]);
  // 每 10s 刷新：seconds_left 倒计时 + worker 回收结果可见
  useEffect(() => {
    const t = setInterval(load, 10_000);
    return () => clearInterval(t);
  }, [load]);

  async function revokeLease(lid: string, username: string) {
    if (!window.confirm(`立即回收 lease ${username}？（数据库账号将被 DROP）`)) return;
    try {
      await api.post(`/api/v1/projects/${projectId}/dynamic-leases/${lid}/revoke`);
      load();
    } catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
  }

  const fmtLeft = (l: DynamicLease) => {
    if (l.status !== 'active') return l.status;
    const s = l.seconds_left ?? 0;
    if (s <= 0) return '回收中…';
    const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60);
    return h > 0 ? `${h}h ${m}m` : m > 0 ? `${m}m ${s % 60}s` : `${s}s`;
  };

  return (
    <div className="panel">
      <div className="panel-head">
        <h2>{projectSlug} / 动态密钥</h2>
        <button className="ghost" onClick={load}>刷新</button>
        <button onClick={() => setShowNew(true)}>＋ 接入数据库引擎</button>
      </div>
      <p className="muted">
        为数据库配置动态引擎后，机器身份可用 <code>POST /projects/{'{pid}'}/dynamic-engines/{'{eid}'}/lease</code> 申请短期账号（自动创建、到期回收）。
        lease 仅机器身份可申请；此处供 owner/admin 管理引擎与手动回收。
      </p>
      {error && <div className="error">{error}</div>}
      {engines.length === 0 && <p className="muted">尚未接入任何数据库引擎。</p>}
      {engines.map((e) => (
        <div key={e.id} className="card">
          <div className="panel-head">
            <h3><span className="tag">{e.type}</span> {e.name} <span className="muted mono">{e.database}</span></h3>
            <span className="muted">TTL {e.default_ttl}s / 上限 {e.max_ttl}s</span>
          </div>
          <table className="table">
            <thead><tr><th>Lease 账号</th><th>状态</th><th>剩余</th><th>到期时间</th><th /></tr></thead>
            <tbody>
              {(e.leases ?? []).map((l) => (
                <tr key={l.id}>
                  <td className="mono">{l.username}</td>
                  <td><span className={`tag ${l.status === 'active' ? 'ok' : ''}`}>{l.status}</span></td>
                  <td className="mono">{fmtLeft(l)}</td>
                  <td className="muted nowrap">{l.expires_at ? new Date(l.expires_at * 1000).toLocaleString() : ''}</td>
                  <td>{l.status === 'active' && <button className="ghost" onClick={() => l.id && revokeLease(l.id, l.username ?? '')}>回收</button>}</td>
                </tr>
              ))}
              {(e.leases ?? []).length === 0 && <tr><td colSpan={5} className="muted center">当前无活跃 lease</td></tr>}
            </tbody>
          </table>
        </div>
      ))}
      {showNew && (
        <DynamicEngineCreator projectId={projectId}
          onClose={() => setShowNew(false)} onCreated={() => { setShowNew(false); load(); }} />
      )}
    </div>
  );
}

function DynamicEngineCreator({ projectId, onClose, onCreated }: {
  projectId: string; onClose: () => void; onCreated: () => void;
}) {
  const [name, setName] = useState('');
  const [conn, setConn] = useState('');
  const [database, setDatabase] = useState('postgres');
  const [defTTL, setDefTTL] = useState(3600);
  const [maxTTL, setMaxTTL] = useState(86400);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr('');
    try {
      await api.post(`/api/v1/projects/${projectId}/dynamic-engines`, {
        name, connection_string: conn, database, default_ttl: defTTL, max_ttl: maxTTL,
      });
      onCreated();
    } catch (e2) { setErr(e2 instanceof ApiError ? e2.message : String(e2)); }
    finally { setBusy(false); }
  }

  return (
    <div className="drawer" onClick={onClose}>
      <form className="drawer-inner" onClick={(e) => e.stopPropagation()} onSubmit={save}>
        <h3>接入数据库引擎（PostgreSQL）</h3>
        <label>名称<input value={name} onChange={(e) => setName(e.target.value)} required placeholder="pg-main" /></label>
        <label>高权限连接串<input className="mono" value={conn} onChange={(e) => setConn(e.target.value)} required
          placeholder="postgres://admin:****@db.internal:5432/postgres" /></label>
        <label>目标库<input value={database} onChange={(e) => setDatabase(e.target.value)} required /></label>
        <label>默认 TTL（秒）<input type="number" value={defTTL} onChange={(e) => setDefTTL(Number(e.target.value))} min={60} /></label>
        <label>最大 TTL（秒）<input type="number" value={maxTTL} onChange={(e) => setMaxTTL(Number(e.target.value))} min={300} /></label>
        <p className="muted">连接串用 Master Key 加密落库，列表不回显。lease 到期后账号自动 DROP。</p>
        {err && <div className="error">{err}</div>}
        <div className="drawer-actions">
          <button type="button" className="ghost" onClick={onClose}>取消</button>
          <button type="submit" disabled={busy}>{busy ? '接入中…' : '接入'}</button>
        </div>
      </form>
    </div>
  );
}

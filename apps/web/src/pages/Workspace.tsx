import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError, hasBrowserSession, getBrowserSessionInfo } from '../api';
import type { User, Org, Project, Folder, SecretMeta, SecretValue, Version, AuditLog, Identity, CreatedIdentity, IdentityScope, DynamicEngine, DynamicLease } from '../api';
import SettingsPanel from './Settings';
import { OrganizationSwitcher } from '../components/Governance';
import IdentityReplacement from '../components/IdentityReplacement';
import useSecretMutation from '../components/useSecretMutation';
import Modal from '../components/Modal';
import ValidatedForm from '../components/ValidatedForm';
import { fmtTime, timeTitle } from '../time';
import VersionTag from '../components/VersionTag';
import { GitHubPanel, githubErrorMessage } from '../components/GitHub';
import ImportEnv from '../components/ImportEnv';
import VersionDiff from '../components/VersionDiff';
import ScanReports from '../components/ScanReports';
import RotationPolicy from '../components/RotationPolicy';

interface Props {
  user: User;
  orgs: Org[];
  onOrgChange: (orgs: Org[]) => void;
  onUser: (user: User) => void;
  onLogout: () => void;
  logoutError?: string;
}

export default function Workspace({ user, orgs, onOrgChange, onUser, onLogout, logoutError }: Props) {
  const org = orgs[0];
  const federated = hasBrowserSession();
  const session = getBrowserSessionInfo();
  const [projects, setProjects] = useState<Project[]>([]);
  const [project, setProject] = useState<Project | null>(null);
  const env = 'dev';
  const [tab, setTab] = useState<'secrets' | 'audit' | 'identities' | 'dynamic' | 'settings' | 'scans'>('secrets');
  const [menu, setMenu] = useState<'project' | 'account' | null>(null);
  const [draft, setDraft] = useState<'project' | 'org' | null>(null);
  const [draftName, setDraftName] = useState('');
  const [draftCode, setDraftCode] = useState('');
  const [switchOrg,setSwitchOrg] = useState(false);
  const [showGitHub, setShowGitHub] = useState(false);
  const [githubNotice, setGithubNotice] = useState('');
  useEffect(() => {
    const q = new URLSearchParams(window.location.search);
    const gh = q.get('github');
    if (!gh) return;
    window.history.replaceState({}, '', '/');
    if (gh === 'linked') setGithubNotice('GitHub 已绑定，之后可使用 GitHub 登录。');
    else if (gh === 'error') setGithubNotice(githubErrorMessage(q.get('reason')));
  }, []);
  const [show2FA, setShow2FA] = useState(false);
 const [showIdentity, setShowIdentity] = useState(false);
  const [error, setError] = useState('');

  const loadProjects = useCallback(async () => {
    if (!org) return;
    const d = await api.get<{ projects: Project[] }>(`/api/v1/orgs/${org.slug}/projects`);
    setProjects(d.projects);
    setProject((p) => d.projects.find((item) => item.id === p?.id) ?? d.projects[0] ?? null);
  }, [org?.slug]);

  useEffect(() => { loadProjects().catch((e) => setError(String(e))); }, [loadProjects]);

  function itemStyle(active: boolean): React.CSSProperties {
    return { display: 'block', width: '100%', textAlign: 'left', fontWeight: 400, background: active ? undefined : 'transparent', color: active ? undefined : 'inherit' };
  }

  function suggestCode(name: string) {
    return name.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '').slice(0, 39);
  }

  function openDraft(kind: 'project' | 'org') {
    setDraft(kind);
    setDraftName('');
    setDraftCode('');
    setMenu(kind === 'project' ? 'project' : 'account');
  }

  async function submitDraft(e: React.FormEvent) {
    e.preventDefault();
    const name = draftName.trim();
    const code = (draftCode.trim() || suggestCode(name)).toLowerCase();
    if (!name || !code) return;
    try {
      if (draft === 'project') {
        await api.post(`/api/v1/orgs/${org.slug}/projects`, { name, code });
        setDraft(null);
        setMenu(null);
        await loadProjects();
      } else {
        const made = await api.post<{ id: string }>('/api/v1/orgs', { name, code });
        const m = await api.get<{ orgs: Org[] }>('/api/v1/me');
        const next = m.orgs ?? [];
        onOrgChange([...next.filter((item) => item.id === made.id), ...next.filter((item) => item.id !== made.id)]);
        setDraft(null);
        setMenu(null);
        setTab('secrets');
      }
    } catch (err) { setError(err instanceof ApiError ? err.message : String(err)); }
  }

  if (!org) return <div className="center-screen">没有组织</div>;

  const personal = org.kind === 'personal';
  const ownerLabel = personal ? '个人' : org.name;
  const personalOrgs = orgs.filter((item) => item.kind === 'personal');
  const sharedOrgs = orgs.filter((item) => item.kind !== 'personal');
  const canAdmin = org.role === 'owner' || org.role === 'admin';
  const canDelegate = org.role === 'owner' || (!federated && org.role === 'admin');
  const canAudit = org.role === 'owner' || org.role === 'admin';

  return (
    <div className="layout">
      <header className="topbar">
        <div className="brand small">
          <span className="brand-mark">禁</span>
          <strong>taboo · 禁制</strong>
        </div>
        <div style={{ position: 'relative' }}>
          <button type="button" className="ghost" aria-expanded={menu === 'project'} onClick={() => { setMenu(menu === 'project' ? null : 'project'); setDraft(null); }}>
            {ownerLabel} / {project?.name ?? '无项目'}
          </button>
          {menu === 'project' && <div className="card" style={{ position: 'absolute', top: 'calc(100% + 6px)', left: 0, zIndex: 4, width: 280, padding: 8 }}>
            <p className="muted">{ownerLabel}的项目</p>
            {projects.map((item) => (
              <button key={item.id} type="button" className="ghost" style={{ display: 'block', width: '100%', textAlign: 'left' }} onClick={() => { setProject(item); setTab('secrets'); setMenu(null); }}>{item.name}</button>
            ))}
            {projects.length === 0 && <p className="muted">还没有项目</p>}
            {draft === 'project' ? <form onSubmit={(e) => void submitDraft(e)} style={{ display: 'flex', flexDirection: 'column', gap: 8, marginTop: 8 }}>
              <input aria-label="项目名称" placeholder="显示名称" value={draftName} onChange={(e) => setDraftName(e.target.value)} required />
              <input aria-label="项目代号" placeholder="项目代号" value={draftCode} onChange={(e) => setDraftCode(e.target.value)} />
              <div className="drawer-actions"><button type="button" className="ghost" onClick={() => setDraft(null)}>取消</button><button type="submit">创建项目</button></div>
            </form> : canAdmin && <button type="button" className="ghost" onClick={() => openDraft('project')}>新建项目</button>}
          </div>}
        </div>
        <div className="spacer" />
        <div style={{ position: 'relative' }}>
          <button type="button" className="ghost" aria-label="账户菜单" aria-expanded={menu === 'account'} onClick={() => { setMenu(menu === 'account' ? null : 'account'); setDraft(null); }}>{(user.name || user.email || '我').slice(0, 1)}</button>
          {menu === 'account' && <div className="card" style={{ position: 'absolute', top: 'calc(100% + 6px)', right: 0, zIndex: 4, width: 280, padding: 8 }}>
            <p style={{ margin: '4px 8px' }}>{user.name || user.email}{user.handle ? ` · ${user.handle}` : ''}</p>
            <p className="muted" style={{ margin: '0 8px 8px' }}>{user.email}</p>
            <p className="muted" style={{ margin: '0 8px' }}>切换到</p>
            {personalOrgs.map((item) => <button key={item.id} type="button" className="ghost" style={{ display: 'block', width: '100%', textAlign: 'left' }} onClick={() => { onOrgChange([item, ...orgs.filter((o) => o.id !== item.id)]); setMenu(null); }}>个人</button>)}
            {sharedOrgs.map((item) => <button key={item.id} type="button" className="ghost" style={{ display: 'block', width: '100%', textAlign: 'left' }} onClick={() => { onOrgChange([item, ...orgs.filter((o) => o.id !== item.id)]); setMenu(null); }}>{item.name} · {item.role}</button>)}
            {federated && <button type="button" className="ghost" onClick={() => { setMenu(null); setSwitchOrg(true); }}>切换组织</button>}
            {!federated && (draft === 'org' ? <form onSubmit={(e) => void submitDraft(e)} style={{ display: 'flex', flexDirection: 'column', gap: 8, marginTop: 8 }}>
              <input aria-label="组织名称" placeholder="显示名称" value={draftName} onChange={(e) => setDraftName(e.target.value)} required />
              <input aria-label="组织 code" placeholder="组织 code" value={draftCode} onChange={(e) => setDraftCode(e.target.value)} />
              <div className="drawer-actions"><button type="button" className="ghost" onClick={() => setDraft(null)}>取消</button><button type="submit">创建组织</button></div>
            </form> : <button type="button" className="ghost" onClick={() => openDraft('org')}>创建组织</button>)}
            {!federated && <button type="button" className="ghost" onClick={() => { setMenu(null); setShow2FA(true); }}>两步验证</button>}
            {!federated && <button type="button" className="ghost" onClick={() => { setMenu(null); setShowGitHub(true); }}>GitHub</button>}
            {!personal && <button type="button" className="ghost" onClick={() => { setMenu(null); setShowIdentity(true); }}>登录身份</button>}
            <button type="button" className="ghost" onClick={onLogout}>退出</button>
          </div>}
        </div>
      </header>
      {user.handle_confirmed === false && <HandleConfirm user={user} orgs={orgs} onUser={onUser} onOrgChange={onOrgChange} />}

      {githubNotice && <p className="session-notice" role="status">{githubNotice} <button className="ghost" onClick={() => setGithubNotice('')}>知道了</button></p>}
      {federated && <p className="session-notice" role="status">外部登录 · 当前组织：{org.name} · {session?.revocation_profile === 'ONLINE_REQUIRED' ? '每次访问检查上游状态，故障时拒绝访问。' : '会话有效期内使用已验证身份，上游停用最迟在会话到期时生效。'}
        {session?.expires_at && <> 会话到期：{new Date(session.expires_at * 1000).toLocaleTimeString()}。</>}
        {session?.rp_logout ? ' 退出时同时前往身份提供方结束会话。' : ' 退出仅结束本系统会话。'}
        {session?.sensitive_operations_allowed === false && <> 当前认证未满足敏感操作要求。<a href={session.reauthentication_url}>重新验证身份</a></>}
      </p>}
      {logoutError && <p className="error" role="alert">{logoutError}</p>}
      <div className="body">
        <aside className="sidebar">
          <div className="sidebar-title" style={{ display: 'block', textTransform: 'none' }}>
            <strong style={{ color: 'var(--fg)' }}>{ownerLabel}</strong>
            <div>{personal ? '你的项目和密钥' : `组织 · ${org.role}`}</div>
          </div>
          <button type="button" className={`nav-item ${tab === 'secrets' ? 'active' : ''}`} style={itemStyle(tab === 'secrets')} onClick={() => setTab('secrets')}>密钥</button>
          {canDelegate && <button type="button" className={`nav-item ${tab === 'identities' ? 'active' : ''}`} style={itemStyle(tab === 'identities')} onClick={() => setTab('identities')}>机器身份</button>}
          {canDelegate && <button type="button" className={`nav-item ${tab === 'dynamic' ? 'active' : ''}`} style={itemStyle(tab === 'dynamic')} onClick={() => setTab('dynamic')}>动态密钥</button>}
          {canAudit && <button type="button" className={`nav-item ${tab === 'audit' ? 'active' : ''}`} style={itemStyle(tab === 'audit')} onClick={() => setTab('audit')}>审计日志</button>}
          <button type="button" className={`nav-item ${tab === 'scans' ? 'active' : ''}`} style={itemStyle(tab === 'scans')} onClick={() => setTab('scans')}>扫描报告</button>
          <button type="button" className={`nav-item ${tab === 'settings' ? 'active' : ''}`} style={itemStyle(tab === 'settings')} onClick={() => setTab('settings')}>{personal ? '个人设置' : '组织设置'}</button>
          {error && <div className="error">{error}</div>}
          <VersionTag className="sidebar-version" />
        </aside>

        <main className="main">
          {tab === 'secrets'
            ? project && <SecretsPanel key={project.id} projectId={project.id} env={env} projectSlug={project.slug} ownerLabel={ownerLabel} canWrite={org.role === 'developer' || canAdmin} />
            : tab === 'identities'
              ? <IdentitiesPanel orgSlug={org.slug} project={project} />
              : tab === 'dynamic'
                ? project && <DynamicPanel key={project.id} projectId={project.id} projectSlug={project.slug} />
                : tab === 'settings'
                  ? <SettingsPanel key={org.id} orgSlug={org.slug} role={org.role} personal={personal} />
                  : tab === 'scans'
                    ? <ScanReports orgSlug={org.slug} />
                    : canAudit ? <AuditPanel orgSlug={org.slug} /> : <p className="muted">没有审计权限</p>}
        </main>
      </div>
      {switchOrg && <OrganizationSwitcher onClose={() => setSwitchOrg(false)} />}
      {showIdentity && <IdentityReplacement key={org.id} orgSlug={org.slug} onClose={() => setShowIdentity(false)} />}
 {show2FA && <TotpPanel onClose={() => setShow2FA(false)} />}
      {showGitHub && <GitHubPanel onClose={() => setShowGitHub(false)} />}
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
    <Modal title="两步验证（TOTP）" onClose={onClose} busy={busy}>
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
          <ValidatedForm onSubmit={verify} className="totp-setup">
            <p>用验证器扫描下方二维码，或手动录入密钥：</p>
            <img className="qr" src={setup.qr_png} alt="TOTP QR Code" />
            <label>密钥（base32）<input readOnly value={setup.secret} onFocus={(e) => e.target.select()} /></label>
            <label>验证器上的 6 位码<input value={code} onChange={(e) => setCode(e.target.value)} required pattern="\d{6}" placeholder="123456" autoFocus /></label>
            <div className="drawer-actions">
              <button type="button" className="ghost" onClick={() => setSetup(null)}>取消</button>
              <button type="submit" disabled={busy}>{busy ? '激活中…' : '验证并开启'}</button>
            </div>
          </ValidatedForm>
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
          <ValidatedForm onSubmit={disable}>
            <p><span className="tag ok">已开启</span> <span className="muted">登录需密码 + 动态码。</span></p>
            <label>输入密码确认关闭<input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required /></label>
            <div className="drawer-actions">
              <button type="button" className="ghost" disabled={busy} onClick={onClose}>关闭</button>
              <button type="submit" disabled={busy}>关闭两步验证</button>
            </div>
          </ValidatedForm>
        )}
      </div>
    </Modal>
  );
}

function SecretsPanel({ projectId, env, projectSlug, ownerLabel, canWrite: legacyCanWrite }: { projectId: string; env: string; projectSlug: string; ownerLabel: string; canWrite: boolean }) {
  const [folders, setFolders] = useState<Folder[]>([]);
  const [secrets, setSecrets] = useState<SecretMeta[]>([]);
  const [cur, setCur] = useState('/'); // 当前文件夹路径（物化路径 /a/b/）
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<string | null>(null);
  const [importing, setImporting] = useState(false); // key 或 '__new__'
  const [detail, setDetail] = useState<SecretMeta | null>(null);
  const [permissions, setPermissions] = useState<{write:boolean;delete:boolean;rollback:boolean;export:boolean} | null>(null);
  const canWrite = permissions?.write ?? (!hasBrowserSession() && legacyCanWrite);


  const loadFolders = useCallback(() => {
    api.get<{ folders: Folder[] }>(`/api/v1/projects/${projectId}/folders?env=${env}`)
      .then((d) => setFolders(d.folders))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  }, [projectId, env]);

  const listGeneration = useRef(0);
  const selectFolder = (path:string) => { listGeneration.current++; setSecrets([]);setEditing(null);setDetail(null);setCur(path); };
  const load = useCallback(() => {
    const started = ++listGeneration.current;
    api.get<{ secrets: SecretMeta[]; permissions?: {write:boolean;delete:boolean;rollback:boolean;export:boolean} }>(`/api/v1/projects/${projectId}/secrets?env=${env}&path=${encodeURIComponent(cur)}`)
      .then((d) => { if(started !== listGeneration.current)return; setError(''); setSecrets(d.secrets); setPermissions(d.permissions ?? null); if(hasBrowserSession() && !d.permissions?.write)setEditing(null); })
      .catch((e) => { if(started !== listGeneration.current)return; setSecrets([]); setPermissions({write:false,delete:false,rollback:false,export:false}); setEditing(null); setDetail(null); setError(e instanceof ApiError ? e.message : String(e)); });
  }, [projectId, env, cur]);

  useEffect(load, [load]);
  useEffect(() => { const refresh = () => {load();}; window.addEventListener('taboo:permissions-refresh',refresh); return () => window.removeEventListener('taboo:permissions-refresh',refresh); },[load]);
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
      selectFolder(parentOf(cur));
      loadFolders();
    } catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
  }

  const crumbs = cur === '/' ? [] : cur.replace(/^\/+|\/+$/g, '').split('/');

  return (
    <div className="panel">
      <div className="panel-head">
        <div><h2>{projectSlug}</h2><p className="muted" style={{ margin: '4px 0 0' }}>归属于{ownerLabel}</p></div>
        {canWrite && <button className="ghost" onClick={createFolder}>＋ 新建文件夹</button>}
        {(permissions?.delete ?? (!hasBrowserSession() && canWrite)) && cur !== '/' && <button className="ghost" onClick={deleteFolder}>删除此空文件夹</button>}
        {canWrite && <button className="ghost" onClick={() => setImporting(true)}>导入 .env</button>}
        {canWrite && <button onClick={() => setEditing('__new__')}>＋ 新建密钥</button>}
      </div>
      <div className="crumbs mono">
        <button className={`crumb ghost ${cur === '/' ? 'active' : ''}`} onClick={() => selectFolder('/')}>根目录</button>
        {crumbs.map((c, i) => {
          const p = '/' + crumbs.slice(0, i + 1).join('/') + '/';
          return <span key={p}><span className="muted"> / </span>
            <button className={`crumb ghost ${cur === p ? 'active' : ''}`} onClick={() => selectFolder(p)}>{c}</button></span>;
        })}
      </div>
      {error && <div className="error">{error}</div>}
      <div className="split">
        <aside className="folder-tree">
          <FolderTree folders={folders} cur={cur} onSelect={selectFolder} />
        </aside>
        <div className="grow" tabIndex={0} role="region" aria-label="密钥列表">
          <table className="table secrets-table">
            <thead>
              <tr><th>Key</th><th>值</th><th>标签</th><th>版本</th><th>更新时间</th><th /></tr>
            </thead>
            <tbody>
              {secrets.map((s) => (
                <SecretRow key={`${s.id}:${s.version}`} s={s} projectId={projectId} env={env} canWrite={canWrite && (s.canWrite !== false)}
                  onChanged={load} onEdit={() => setEditing(s.key)} onDetail={() => setDetail(s)} />
              ))}
              {secrets.length === 0 && <tr><td colSpan={6} className="muted center">此文件夹还没有密钥</td></tr>}
            </tbody>
          </table>
        </div>
      </div>
      {editing && (
        <SecretEditor projectId={projectId} env={env} path={cur} secretKey={editing === '__new__' ? '' : editing} expectedVersion={editing === '__new__' ? 0 : secrets.find(s=>s.key===editing)?.version}
          onClose={() => setEditing(null)} onSaved={() => { setEditing(null); load(); }} />
      )}
      {importing && (
        <ImportEnv projectId={projectId} env={env} path={cur} onClose={() => setImporting(false)} onImported={load} />
      )}
      {detail && (
        <DetailDrawer projectId={projectId} env={env} meta={detail} onChanged={load}
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
      <div className={`tree-item ${cur === f.path ? 'active' : ''}`}>
        {kids.length > 0
          ? <button className="tree-toggle ghost" aria-label={`${open ? '收起' : '展开'} ${f.name}`} aria-expanded={open} onClick={() => setOpen(!open)}>{open ? '▾' : '▸'}</button>
          : <span className="tree-toggle" />}
        <button className="ghost" onClick={() => onSelect(f.path)}>📁 {f.name}</button>
      </div>
      {open && kids.length > 0 && (
        <ul className="tree">
          {kids.map((k) => <TreeNode key={k.id} f={k} byParent={byParent} cur={cur} onSelect={onSelect} />)}
        </ul>
      )}
    </li>
  );
}

function SecretRow({ s, projectId, env, canWrite, onChanged, onEdit, onDetail }: {
  s: SecretMeta; projectId: string; env: string; canWrite: boolean;
  onChanged: () => void; onEdit: () => void; onDetail: () => void;
}) {
  const mutate=useSecretMutation();
  const [value, setValue] = useState<string | null>(null);
  const revealGeneration=useRef(0);
  useEffect(() => { setValue(null); }, [s.canReveal]);
  useEffect(() => { const clear = () => { revealGeneration.current++; setValue(null); }; window.addEventListener('blur', clear); window.addEventListener('taboo:verification-unavailable',clear); return () => {window.removeEventListener('blur', clear);window.removeEventListener('taboo:verification-unavailable',clear);}; }, []);

  const [err, setErr] = useState('');

  async function reveal() {
    if (value !== null) { setValue(null); return; }
    const started=++revealGeneration.current;
    try {
      const d = await api.get<SecretValue>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(s.key)}?env=${env}&path=${encodeURIComponent(s.folder)}`);
      if(started===revealGeneration.current && document.hasFocus())setValue(d.value);
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
  }

  async function resolveRef() {
    const started = ++revealGeneration.current;
    try {
      const d = await api.get<SecretValue>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(s.key)}?env=${env}&path=${encodeURIComponent(s.folder)}&resolve=true`);
      if (started === revealGeneration.current && document.hasFocus()) { setErr(''); setValue(d.value); }
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
  }

  async function copy() {
    const started=revealGeneration.current;
    try {
      const d: string = value !== null ? value : (await api.get<SecretValue>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(s.key)}?env=${env}&path=${encodeURIComponent(s.folder)}`)).value;
      if(started!==revealGeneration.current || !document.hasFocus())return;
      await navigator.clipboard.writeText(d);
      setTimeout(() => navigator.clipboard.writeText('').catch(() => {}), 20_000); // 20s 自动清空剪贴板
    } catch { setErr('复制失败'); }
  }

  async function remove() {
    if (!window.confirm(`删除密钥 ${s.key}？（版本历史一并删除，不可恢复）`)) return;
    try {
      await mutate('del', `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(s.key)}?env=${env}&path=${encodeURIComponent(s.folder)}&expectedVersion=${s.version}`);
      onChanged();
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
  }

  return (
    <tr>
      <td className="mono key"><button className="ghost" onClick={onDetail} title="查看详情/版本">{s.key}</button></td>
      <td className="mono" data-label="值">
        {value === null ? '••••••••' : value}
        {err && <span className="error"> {err}</span>}
      </td>
      <td data-label="标签" className={s.tags.length ? '' : 'empty'}>{s.tags.map((t) => <span key={t} className="tag">{t}</span>)}</td>
      <td data-label="版本">v{s.version}</td>
      <td className="muted nowrap" data-label="更新时间" title={timeTitle(s.updated_at)}>{fmtTime(s.updated_at)}</td>
      <td className="actions">
        {s.canReveal && <button className="ghost" onClick={reveal}>{value === null ? '显示' : '隐藏'}</button>}
        {s.canReveal && value !== null && value.includes('${') && <button className="ghost" onClick={() => void resolveRef()}>解析引用</button>}
        {s.canReveal && <button className="ghost" onClick={copy}>复制</button>}
        {canWrite && <button className="ghost" onClick={onEdit}>编辑</button>}
        {(s.canDelete ?? (!hasBrowserSession() && canWrite)) && <button className="ghost" onClick={remove}>删除</button>}
      </td>
    </tr>
  );
}

function SecretEditor({ projectId, env, path, secretKey, expectedVersion, onClose, onSaved }: {
  projectId: string; env: string; path: string; secretKey: string; expectedVersion?:number; onClose: () => void; onSaved: () => void;
}) {
  const mutate=useSecretMutation();
  const [key, setKey] = useState(secretKey);
  const [value, setValue] = useState('');
  const [comment, setComment] = useState('');
  const [tags, setTags] = useState('');
  const [err, setErr] = useState('');
  const [status, setStatus] = useState('');
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
      const saved = await mutate('post', `/api/v1/projects/${projectId}/secrets?env=${env}&path=${encodeURIComponent(path)}`, {
        key, value, comment, expectedVersion,
        tags: tags.split(',').map((t) => t.trim()).filter(Boolean),
      }) as { approval_id?: string };
      if (saved?.approval_id) {
        setStatus('已提交审批，另一名管理员批准后才会写入。');
        return;
      }
      onSaved();
    } catch (e2) { setErr(e2 instanceof ApiError ? e2.message : String(e2)); }
    finally { setBusy(false); }
  }

  return (
    <Modal title="编辑密钥" onClose={onClose} busy={busy}>
      <ValidatedForm className="drawer-inner" onClick={(e) => e.stopPropagation()} onSubmit={save}>
        <h3>{secretKey ? `更新密钥 — ${secretKey}` : `新建密钥 — ${path}`}</h3>
        <label>Key<input value={key} onChange={(e) => setKey(e.target.value)} required disabled={!!secretKey} /></label>
        <label>值<textarea className="resize-none" value={value} onChange={(e) => setValue(e.target.value)} required rows={4} /></label>
        <label>备注<input value={comment} onChange={(e) => setComment(e.target.value)} /></label>
        <label>标签（逗号分隔）<input value={tags} onChange={(e) => setTags(e.target.value)} placeholder="db, prod" /></label>
        {status && <p role="status">{status}</p>}
        {err && <div className="error">{err}</div>}
        <div className="drawer-actions">
          <button type="button" className="ghost" disabled={busy} onClick={onClose}>取消</button>
          <button type="submit" disabled={busy}>{busy ? '保存中…' : '保存（产生新版本）'}</button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

function DetailDrawer({ projectId, env, meta, onClose, onChanged }: {
  projectId: string; env: string; meta: SecretMeta; onClose: () => void; onChanged: () => void;
}) {
  const secretKey = meta.key;
  const mutate=useSecretMutation();
  const folder = meta.folder;
  const [versions, setVersions] = useState<Version[]>([]);
  const [latest, setLatest] = useState(0);
  const [canRollback, setCanRollback] = useState(false);
  const [err, setErr] = useState('');

  const load = useCallback(() => {
    api.get<{ versions: Version[]; latest: number; canRollback?: boolean }>(
      `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}/versions?env=${env}&path=${encodeURIComponent(folder)}`)
      .then((d) => { setVersions(d.versions); setLatest(d.latest); setCanRollback(d.canRollback ?? (!hasBrowserSession() && meta.canWrite !== false)); })
      .catch((e) => setErr(e instanceof ApiError ? e.message : String(e)));
  }, [projectId, env, secretKey, folder, meta.canWrite]);
  useEffect(load, [load]);

  async function rollback(v: number) {
    if (!window.confirm(`回滚 ${secretKey} 到 v${v}？（将产生新版本）`)) return;
    try {
      await mutate('post', `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}/rollback?env=${env}&path=${encodeURIComponent(folder)}`, { version: v, expectedVersion:latest });
      load(); onChanged();
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
  }

  return (
    <Modal title="版本历史" onClose={onClose}>
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
                <td className="muted" title={timeTitle(v.created_at)}>{fmtTime(v.created_at)}</td>
                <td>{canRollback && v.version !== latest && <button className="ghost" onClick={() => rollback(v.version)}>回滚到此</button>}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {meta.canReveal
          ? <VersionDiff projectId={projectId} env={env} folder={folder} secretKey={secretKey} versions={versions} />
          : <p className="muted">需要 reveal 权限才能对比版本内容。</p>}
        <RotationPolicy projectId={projectId} env={env} folder={folder} secretKey={secretKey} canWrite={meta.canWrite} />
        <div className="drawer-actions"><button className="ghost" onClick={onClose}>关闭</button></div>
      </div>
    </Modal>
  );
}

function AuditPanel({ orgSlug }: { orgSlug: string }) {
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [action, setAction] = useState('');
  const [actor, setActor] = useState('');
  const [resource, setResource] = useState('');
  const [jobs, setJobs] = useState<{ id: string; status: string; format: string }[]>([]);
  const [verify, setVerify] = useState<string>('');

  const qs = () => {
    const p = new URLSearchParams({ limit: '200' });
    if (action) p.set('action', action);
    if (actor) p.set('actor', actor);
    if (resource) p.set('resource', resource);
    return p.toString();
  };

  const auditGeneration = useRef(0);
  const load = useCallback(() => {
    const started=++auditGeneration.current;
    api.get<{ logs: AuditLog[] }>(`/api/v1/orgs/${orgSlug}/audit?${qs()}`)
      .then((d) => {if(started===auditGeneration.current)setLogs(d.logs);})
      .catch(e => setVerify(e instanceof Error ? e.message : String(e)));
    api.get<{ exports: { id: string; status: string; format: string }[] }>(`/api/v1/orgs/${orgSlug}/audit/exports`)
      .then((d) => {if(started===auditGeneration.current)setJobs(d.exports ?? []);})
      .catch(e => setVerify(e instanceof Error ? e.message : String(e)));
  }, [orgSlug, action, actor, resource]);
  useEffect(load, [load]);

  async function download(format: string) {
    try { await api.get(`/api/v1/orgs/${orgSlug}/audit/export?format=${format}&async=1&${qs()}`); load(); }
    catch(e) { setVerify(e instanceof Error ? e.message : String(e)); }
  }

  async function checkChain() {
    try {
      const d = await api.get<{ ok: boolean; checked: number; broken_at?: string }>(`/api/v1/orgs/${orgSlug}/audit/verify`);
      setVerify(d.ok ? `链完整（${d.checked} 条）` : `链断裂于 ${d.broken_at}`);
    } catch (e) { setVerify(e instanceof ApiError ? e.message : String(e)); }
  }

  return (
    <div className="panel">
      <div className="panel-head">
        <h2>审计日志</h2>
        <input placeholder="action" value={action} onChange={(e) => setAction(e.target.value)} />
        <input placeholder="actor" value={actor} onChange={(e) => setActor(e.target.value)} />
        <input placeholder="resource" value={resource} onChange={(e) => setResource(e.target.value)} />
        <button className="ghost" onClick={() => download('csv')}>异步导出 CSV</button>
        <button className="ghost" onClick={() => download('jsonl')}>异步导出 JSONL</button>
        <button className="ghost" onClick={checkChain}>验真</button>
      </div>
      {verify && <p className="muted">{verify}</p>}
      {jobs.length > 0 && (
        <div>导出任务：{jobs.map(j=><p key={j.id}>{j.format} · {j.status} {j.status==='done'&&<button className="ghost" onClick={()=>api.download(`/api/v1/orgs/${orgSlug}/audit/exports/${j.id}`, `audit.${j.format}`).catch(e=>setVerify(String(e)))}>下载</button>} {j.status==='failed'&&<button className="ghost" onClick={()=>api.post(`/api/v1/orgs/${orgSlug}/audit/exports/${j.id}/retry`).then(load).catch(e=>setVerify(String(e)))}>重新生成</button>}</p>)}<button className="ghost" onClick={load}>刷新任务</button></div>
      )}
      <table className="table">
        <thead><tr><th>时间</th><th>操作者</th><th>动作</th><th>资源</th><th>IP</th></tr></thead>
        <tbody>
          {logs.map((l) => (
            <tr key={l.id}>
              <td className="muted nowrap" title={timeTitle(l.created_at)}>{fmtTime(l.created_at)}</td>
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


function IdentitiesPanel({ orgSlug, project }: { orgSlug: string; project: Project | null }) {
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
      <div className="table-scroll">
      <table className="table secrets-table">
        <thead><tr><th>名称</th><th>Client ID</th><th>Scope（项目 / 环境 / 权限）</th><th>IP 白名单</th><th>扫描报告</th><th>状态</th><th>TTL</th><th /></tr></thead>
        <tbody>
          {identities.map((i) => (
            <tr key={i.id}>
              <td className="mono" data-label="名称">{i.name}</td>
              <td className="mono muted" data-label="Client ID">{i.client_id}</td>
              <td>{i.scopes.map((s, idx) => (
                <div key={idx}><span className="tag">{s.project_slug}</span><span className="tag">{s.permission}</span></div>
              ))}</td>
              <td className="mono" data-label="IP 白名单">{i.allowed_cidrs?.length ? i.allowed_cidrs.join(', ') : '不限制'}</td>
              <td data-label="扫描报告">{i.list_scan_reports ? '全部' : '仅自己'}</td>
              <td><span className={`tag ${i.status === 'active' ? 'ok' : 'bad'}`}>{i.status}</span></td>
              <td className="muted">{i.token_ttl}s</td>
              <td className="actions">{i.status === 'active' && <button className="ghost" onClick={() => revoke(i.id, i.name)}>吊销</button>}</td>
            </tr>
          ))}
          {identities.length === 0 && <tr><td colSpan={8} className="muted center">还没有机器身份</td></tr>}
        </tbody>
      </table>
      </div>
      {showNew && project && (
        <IdentityCreator
          orgSlug={orgSlug}
          project={project}
          onClose={() => setShowNew(false)}
          onCreated={(d) => { setShowNew(false); setOnce(d); load(); }}
        />
      )}
    </div>
  );
}

function IdentityCreator({ orgSlug, project, onClose, onCreated }: {
  orgSlug: string; project: Project;
  onClose: () => void; onCreated: (d: CreatedIdentity) => void;
}) {
  const [name, setName] = useState('');
  const [ttl, setTtl] = useState(900);
  const [cidrs, setCidrs] = useState('');
  const [listScans, setListScans] = useState(false);
  const [scopes, setScopes] = useState<IdentityScope[]>([{ project_id: project.id, project_slug: project.slug, env: 'dev', permission: 'read' }]);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr('');
    try {
      const allowed = cidrs.split(/[\s,]+/).map((s) => s.trim()).filter(Boolean);
      const d = await api.post<CreatedIdentity>(`/api/v1/orgs/${orgSlug}/identities`, {
        name, token_ttl: ttl, allowed_cidrs: allowed, list_scan_reports: listScans,
        scopes: scopes.map((s) => ({ project_id: s.project_id, env: s.env, permission: s.permission })),
      });
      onCreated(d);
    } catch (e2) { setErr(e2 instanceof ApiError ? e2.message : String(e2)); }
    finally { setBusy(false); }
  }

  return (
    <Modal title="创建机器身份" onClose={onClose} busy={busy}>
      <ValidatedForm className="drawer-inner" onClick={(e) => e.stopPropagation()} onSubmit={save}>
        <h3>创建机器身份</h3>
        <label>名称<input value={name} onChange={(e) => setName(e.target.value)} required placeholder="ci-runner" /></label>
        <label>Token TTL（秒，≤3600）<input type="number" value={ttl} onChange={(e) => setTtl(Number(e.target.value))} min={60} max={3600} /></label>
        <label>IP 白名单（可选，逗号或换行分隔）<textarea className="resize-none" rows={2} value={cidrs} onChange={(e) => setCidrs(e.target.value)} placeholder="10.0.0.0/8, 192.0.2.10" /></label>
        <label className="check"><input type="checkbox" checked={listScans} onChange={(e) => setListScans(e.target.checked)} /> 可查看组织全部扫描报告（默认只能看自己上传的）</label>
        <div className="scope-editor">
          <div className="sidebar-title">Scopes（至少一项，禁止通配）</div>
          {scopes.map((s, idx) => (
            <div key={idx} className="scope-row">
              <span className="tag">{project.slug}</span>
              <select value={s.permission} onChange={(e) => setScopes(scopes.map((x, i) => i === idx ? { ...x, permission: e.target.value as IdentityScope['permission'] } : x))}>
                <option value="read">read（仅元数据）</option>
                <option value="reveal">reveal（明文）</option>
                <option value="write">write</option>
              </select>
              <button type="button" className="ghost" disabled={scopes.length === 1} onClick={() => setScopes(scopes.filter((_, i) => i !== idx))}>✕</button>
            </div>
          ))}
          <button type="button" className="ghost" onClick={() => setScopes([...scopes, { project_id: project.id, project_slug: project.slug, env: 'dev', permission: 'read' }])}>＋ 添加 scope</button>
        </div>
        {err && <div className="error">{err}</div>}
        <div className="drawer-actions">
          <button type="button" className="ghost" disabled={busy} onClick={onClose}>取消</button>
          <button type="submit" disabled={busy}>{busy ? '创建中…' : '创建'}</button>
        </div>
      </ValidatedForm>
    </Modal>
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
        为 PostgreSQL 或 MySQL 配置动态引擎后，机器身份可用 <code>POST /projects/{'{pid}'}/dynamic-engines/{'{eid}'}/lease</code> 申请短期只读账号（自动创建、到期回收）。
        lease 仅本地机器身份可申请；外部登录由 owner 管理引擎与手动回收。失败的回收任务可在此重新发起。
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
                  <td>{(l.status === 'active' || l.status === 'failed') && <button className="ghost" onClick={() => l.id && revokeLease(l.id, l.username ?? '')}>回收</button>}</td>
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
  const [engineType, setEngineType] = useState<'postgres' | 'mysql'>('postgres');
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
        name, type: engineType, connection_string: conn, database, default_ttl: defTTL, max_ttl: maxTTL,
      });
      onCreated();
    } catch (e2) { setErr(e2 instanceof ApiError ? e2.message : String(e2)); }
    finally { setBusy(false); }
  }

  return (
    <Modal title="接入数据库引擎" onClose={onClose} busy={busy}>
      <ValidatedForm className="drawer-inner" onClick={(e) => e.stopPropagation()} onSubmit={save}>
        <h3>接入数据库引擎</h3>
        <label>类型
          <select aria-label="数据库类型" value={engineType} onChange={(e) => {
            const next = e.target.value as 'postgres' | 'mysql';
            setEngineType(next);
            setDatabase(next === 'mysql' ? 'mysql' : 'postgres');
          }}>
            <option value="postgres">PostgreSQL</option>
            <option value="mysql">MySQL</option>
          </select>
        </label>
        <label>名称<input value={name} onChange={(e) => setName(e.target.value)} required placeholder={engineType === 'mysql' ? 'mysql-main' : 'pg-main'} /></label>
        <label>高权限连接串<input className="mono" value={conn} onChange={(e) => setConn(e.target.value)} required
          placeholder={engineType === 'mysql' ? 'mysql://admin:****@db.internal:3306/app' : 'postgres://admin:****@db.internal:5432/postgres'} /></label>
        <label>目标库<input value={database} onChange={(e) => setDatabase(e.target.value)} required /></label>
        <label>默认 TTL（秒）<input type="number" value={defTTL} onChange={(e) => setDefTTL(Number(e.target.value))} min={60} /></label>
        <label>最大 TTL（秒）<input type="number" value={maxTTL} onChange={(e) => setMaxTTL(Number(e.target.value))} min={300} /></label>
        <p className="muted">连接串用 Master Key 加密落库，列表不回显。lease 到期后账号自动 DROP。</p>
        {err && <div className="error">{err}</div>}
        <div className="drawer-actions">
          <button type="button" className="ghost" disabled={busy} onClick={onClose}>取消</button>
          <button type="submit" disabled={busy}>{busy ? '接入中…' : '接入'}</button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

function HandleConfirm({ user, orgs, onUser, onOrgChange }: { user: User; orgs: Org[]; onUser: (user: User) => void; onOrgChange: (orgs: Org[]) => void }) {
  const [handle, setHandle] = useState(user.handle ?? '');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  async function save(confirm: boolean) {
    setError('');
    setBusy(true);
    try {
      const next = handle.trim().toLowerCase();
      if (next && next !== user.handle) await api.patch('/api/v1/me/handle', { handle: next });
      if (confirm) await api.post('/api/v1/me/handle/confirm');
      const me = await api.get<{ user: User; orgs: Org[] }>('/api/v1/me');
      onUser(me.user);
      onOrgChange(orgs.map((item) => me.orgs.find((org) => org.id === item.id) ?? item));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="session-notice" onSubmit={(e) => { e.preventDefault(); void save(false); }}>
      <label>用户名<input aria-label="用户名" value={handle} autoComplete="off" onChange={(e) => setHandle(e.target.value)} /></label>
      <button type="submit" className="ghost" disabled={busy}>保存</button>
      <button type="button" disabled={busy} onClick={() => void save(true)}>确认用户名</button>
      <span>确认后不能再改，它会成为定位符的第一段。</span>
      {error && <span className="error" role="alert">{error}</span>}
    </form>
  );
}

import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError, hasBrowserSession, getBrowserSessionInfo } from '../api';
import type { User, Org, Project, Folder, SecretMeta, AuditLog, Identity, CreatedIdentity, IdentityScope, DynamicEngine, DynamicLease } from '../api';
import SettingsPanel from './Settings';
import SecretDetail from './SecretDetail';
import { OrganizationSwitcher } from '../components/Governance';
import IdentityReplacement from '../components/IdentityReplacement';
import useSecretMutation from '../components/useSecretMutation';
import Modal from '../components/Modal';
import ValidatedForm from '../components/ValidatedForm';
import { fmtTime, timeTitle } from '../time';
import VersionTag from '../components/VersionTag';
import { GitHubPanel, githubErrorMessage } from '../components/GitHub';
import ImportEnv from '../components/ImportEnv';
import ScanReports from '../components/ScanReports';
import Icon from '../design/Icon';
import type { IconName } from '../design/Icon';
import { Button, Check, EmptyState, Field, IconButton, MetaList, Notice, PageHead, Pill, Section, Select, Skeleton, TableWrap } from '../design/primitives';
import { FolderTree, SecretRow } from '../ui/SecretRows';

interface Props {
  user: User;
  orgs: Org[];
  onOrgChange: (orgs: Org[]) => void;
  onUser: (user: User) => void;
  onLogout: () => void;
  logoutError?: string;
}

type Tab = 'secrets' | 'audit' | 'identities' | 'dynamic' | 'settings' | 'scans';
type Menu = 'project' | 'account' | null;

interface RailItem { tab: Tab; label: string; icon: IconName; current?: boolean }

export default function Workspace({ user, orgs, onOrgChange, onUser, onLogout, logoutError }: Props) {
  const org = orgs[0];
  const federated = hasBrowserSession();
  const session = getBrowserSessionInfo();
  const [projects, setProjects] = useState<Project[]>([]);
  const [project, setProject] = useState<Project | null>(null);
  const [tab, setTab] = useState<Tab>('secrets');
  const [menu, setMenu] = useState<Menu>(null);
  const [draft, setDraft] = useState<'project' | 'org' | null>(null);
  const [draftName, setDraftName] = useState('');
  const [draftCode, setDraftCode] = useState('');
  const [switchOrg, setSwitchOrg] = useState(false);
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

  // 弹出层：点外部 / Escape 收起，不必对准关闭按钮。
  const appbarRef = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!menu) return;
    const down = (e: PointerEvent) => { if (!appbarRef.current?.contains(e.target as Node)) setMenu(null); };
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape') setMenu(null); };
    window.addEventListener('pointerdown', down);
    window.addEventListener('keydown', key);
    return () => { window.removeEventListener('pointerdown', down); window.removeEventListener('keydown', key); };
  }, [menu]);

  const loadProjects = useCallback(async () => {
    if (!org) return;
    const d = await api.get<{ projects: Project[] }>(`/api/v1/orgs/${org.slug}/projects`);
    setProjects(d.projects);
    setProject((p) => d.projects.find((item) => item.id === p?.id) ?? d.projects[0] ?? null);
  }, [org?.slug]);

  useEffect(() => { loadProjects().catch((e) => setError(String(e))); }, [loadProjects]);

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
  // 组织设置可被外部请求指定初始子页（组织设置页签本身仍是默认入口）。
  const [settingsSub, setSettingsSub] = useState<string | null>(null);

  const railGroups: { label: string; items: RailItem[] }[] = [
    { label: '密钥面', items: [{ tab: 'secrets', label: '密钥', icon: 'key' }] },
    {
      label: '治理',
      items: [
        ...(canDelegate ? ([
          { tab: 'identities', label: '机器身份', icon: 'robot' },
          { tab: 'dynamic', label: '动态密钥', icon: 'db' },
        ] as RailItem[]) : []),
      ],
    },
    {
      label: '印记',
      items: [
        ...(canAudit ? ([{ tab: 'audit', label: '审计日志', icon: 'audit' }] as RailItem[]) : []),
        { tab: 'scans', label: '扫描报告', icon: 'scan' },
      ],
    },
    {
      label: '设置',
      items: [{ tab: 'settings', label: personal ? '个人设置' : '组织设置', icon: 'settings' }],
    },
  ];

  const mobileItems: RailItem[] = [
    { tab: 'secrets', label: '密钥', icon: 'key' },
    ...(canDelegate ? ([{ tab: 'identities', label: '机器身份', icon: 'robot' }] as RailItem[]) : []),
    ...(canAudit ? ([{ tab: 'audit', label: '审计日志', icon: 'audit' }] as RailItem[]) : []),
    { tab: 'settings', label: '设置', icon: 'settings' },
  ];
  // 小屏底栏只有四格：落在治理/印记里的其它页时，把「审计」标记为当前位置。
  const navCurrent: Tab = mobileItems.some((i) => i.tab === tab) ? tab : tab === 'dynamic' ? 'identities' : tab === 'scans' ? 'audit' : 'settings';

  const avatarLetter = (user.name || user.email || '我').slice(0, 1);
  const projectPath = project ? `${ownerLabel} / ${project.name}` : ownerLabel;

  return (
    <div className="layout">
      <header className="appbar" ref={appbarRef}>
        <div className="appbar-brand">
          <span className="seal-mark" aria-hidden="true">禁</span>
          <span className="appbar-wordmark"><span className="en">taboo</span><span className="zh">禁制</span></span>
        </div>

        <div className="appbar-ctx">
          <button type="button" className="ctx-btn" aria-expanded={menu === 'project'} aria-haspopup="menu"
            style={{ minWidth: 0, maxWidth: '100%' }}
            onClick={() => { setMenu(menu === 'project' ? null : 'project'); setDraft(null); }}>
            <Icon name="folder-open" size={14} />
            <span className="ctx-name" style={{ minWidth: 0, flex: '1 1 auto' }} title={`${ownerLabel} / ${project?.name ?? '无项目'}`}>
              <span className="ctx-owner" style={{ color: 'var(--faint)' }}>{ownerLabel} / </span>{project?.name ?? '无项目'}
            </span>
            <Icon name="chevron-down" size={12} />
          </button>
          {menu === 'project' && (
            <div className="popover wide">
              <div className="pop-head">项目</div>
              <ul className="pop-list">
                {projects.map((item) => (
                  <li key={item.id}>
                    <button type="button" className={`pop-item ${item.id === project?.id ? 'active' : ''}`}
                      aria-current={item.id === project?.id ? 'true' : undefined}
                      onClick={() => { setProject(item); setTab('secrets'); setMenu(null); }}>
                      <Icon name={item.id === project?.id ? 'folder-open' : 'folder'} size={14} />
                      <span className="pop-item-main">
                        <span className="pop-item-name">{item.name}</span>
                        <span className="pop-item-sub">{item.slug}{item.approval_required ? ' · 变更审批' : ''}</span>
                      </span>
                      {item.id === project?.id && <Icon name="check" size={13} className="check" />}
                    </button>
                  </li>
                ))}
              </ul>
              {projects.length === 0 && <p className="section-hint" style={{ padding: '0 8px 6px' }}>还没有项目</p>}
              <div className="pop-sep" />
              {canAdmin && draft !== 'project' && (
                <div className="pop-foot"><Button variant="quiet" icon="plus" onClick={() => openDraft('project')}>新建项目</Button></div>
              )}
              {draft === 'project' && (
                <form className="pop-form" onSubmit={(e) => void submitDraft(e)}>
                  <div className="pop-head">新建项目</div>
                  <Field label="显示名称"><input aria-label="项目名称" placeholder="显示名称" value={draftName} onChange={(e) => setDraftName(e.target.value)} required autoFocus /></Field>
                  <Field label="项目代号" hint="留空则按名称生成，创建后作为定位符使用。"><input aria-label="项目代号" placeholder="项目代号" value={draftCode} onChange={(e) => setDraftCode(e.target.value)} /></Field>
                  <div className="form-actions">
                    <Button variant="quiet" onClick={() => setDraft(null)}>取消</Button>
                    <Button variant="primary" type="submit">创建项目</Button>
                  </div>
                </form>
              )}
            </div>
          )}
        </div>

        <div className="appbar-spacer" />
        <div className="appbar-actions">
          <span className="status-chip" title={federated ? '外部登录会话' : '本地账号会话'}>
            <span className={`dot ${federated && session?.sensitive_operations_allowed === false ? 'warn' : federated ? '' : 'warn'}`} aria-hidden="true" />
            <span>{federated ? '外部登录' : '本地会话'}</span>
            <span className="label-long">· {org.role}</span>
          </span>
          <span className="appbar-ctx" style={{ paddingLeft: 0, marginLeft: 0, borderLeft: 0, position: 'relative' }}>
            <button type="button" className="avatar-btn" aria-label="账户菜单" aria-expanded={menu === 'account'} aria-haspopup="menu"
              onClick={() => { setMenu(menu === 'account' ? null : 'account'); setDraft(null); }}>{avatarLetter}</button>
            {menu === 'account' && (
              <div className="popover right">
                <div className="pop-head">账户</div>
                <div style={{ padding: '0 8px 8px', display: 'flex', flexDirection: 'column', gap: 2 }}>
                  <span className="rail-owner-name">{user.name || user.email}{user.handle ? ` · ${user.handle}` : ''}</span>
                  <span className="rail-owner-meta">{user.email}</span>
                </div>
                <div className="pop-sep" />
                <Field label="当前组织" htmlFor="appbar-current-org">
                  <Select id="appbar-current-org" value={org.id}
                    onChange={(e) => { const next = orgs.find((o) => o.id === e.target.value); if (next) { onOrgChange([next, ...orgs.filter((o) => o.id !== next.id)]); setMenu(null); } }}>
                    {orgs.map((item) => (
                      <option key={item.id} value={item.id}>{item.kind === 'personal' ? '个人' : item.name}（{item.role}）</option>
                    ))}
                  </Select>
                </Field>
                <div className="pop-head">切换到</div>
                <ul className="pop-list">
                  {personalOrgs.map((item) => (
                    <li key={item.id}>
                      <button type="button" className={`pop-item ${item.id === org.id ? 'active' : ''}`}
                        onClick={() => { onOrgChange([item, ...orgs.filter((o) => o.id !== item.id)]); setMenu(null); }}>
                        <Icon name="users" size={14} /><span className="pop-item-main"><span className="pop-item-name">个人</span></span>
                        {item.id === org.id && <Icon name="check" size={13} className="check" />}
                      </button>
                    </li>
                  ))}
                  {sharedOrgs.map((item) => (
                    <li key={item.id}>
                      <button type="button" className="pop-item" onClick={() => { onOrgChange([item, ...orgs.filter((o) => o.id !== item.id)]); setMenu(null); }}>
                        <Icon name="shield" size={14} />
                        <span className="pop-item-main">
                          <span className="pop-item-name">{item.name}</span>
                          <span className="pop-item-sub">{item.role}</span>
                        </span>
                        {item.id === org.id && <Icon name="check" size={13} className="check" />}
                      </button>
                    </li>
                  ))}
                </ul>
                {federated && (
                  <Button variant="quiet" icon="refresh" onClick={() => { setMenu(null); setSwitchOrg(true); }}>切换组织</Button>
                )}
                {!federated && (draft === 'org' ? (
                  <form className="pop-form" onSubmit={(e) => void submitDraft(e)}>
                    <div className="pop-head">创建组织</div>
                    <Field label="显示名称"><input aria-label="组织名称" placeholder="显示名称" value={draftName} onChange={(e) => setDraftName(e.target.value)} required autoFocus /></Field>
                    <Field label="组织 code" hint="留空则按名称生成。"><input aria-label="组织 code" placeholder="组织 code" value={draftCode} onChange={(e) => setDraftCode(e.target.value)} /></Field>
                    <div className="form-actions">
                      <Button variant="quiet" onClick={() => setDraft(null)}>取消</Button>
                      <Button variant="primary" type="submit">创建组织</Button>
                    </div>
                  </form>
                ) : (
                  <div className="pop-foot"><Button variant="quiet" icon="plus" onClick={() => openDraft('org')}>创建组织</Button></div>
                ))}
                {!federated && (
                  <div className="pop-foot">
                    <Button variant="quiet" icon="lock" onClick={() => { setMenu(null); setShow2FA(true); }}>两步验证</Button>
                    <Button variant="quiet" icon="globe" onClick={() => { setMenu(null); setShowGitHub(true); }}>GitHub</Button>
                  </div>
                )}
                {!personal && (
                  <div className="pop-foot"><Button variant="quiet" icon="link" onClick={() => { setMenu(null); setShowIdentity(true); }}>登录身份</Button></div>
                )}
                <div className="pop-sep" />
                <div className="pop-foot"><Button variant="quiet" icon="logout" className="pop-item danger" style={{ border: 0 }} onClick={onLogout}>退出</Button></div>
              </div>
            )}
          </span>
        </div>
      </header>

      {user.handle_confirmed === false && <HandleConfirm user={user} orgs={orgs} onUser={onUser} onOrgChange={onOrgChange} />}

      {githubNotice && (
        <p className="session-notice" role="status">
          {githubNotice} <Button variant="quiet" className="btn-sm" onClick={() => setGithubNotice('')}>知道了</Button>
        </p>
      )}
      {federated && (
        <p className="session-notice" role="status">
          外部登录 · 当前组织：{org.name} · {session?.revocation_profile === 'ONLINE_REQUIRED' ? '每次访问检查上游状态，故障时拒绝访问。' : '会话有效期内使用已验证身份，上游停用最迟在会话到期时生效。'}
          {session?.expires_at && <> 会话到期：{new Date(session.expires_at * 1000).toLocaleTimeString()}。</>}
          {session?.rp_logout ? ' 退出时同时前往身份提供方结束会话。' : ' 退出仅结束本系统会话。'}
          {session?.sensitive_operations_allowed === false && <> 当前认证未满足敏感操作要求。<a href={session.reauthentication_url}>重新验证身份</a></>}
        </p>
      )}
      {logoutError && <p className="error" role="alert" style={{ padding: '8px var(--pad)' }}>{logoutError}</p>}

      <div className="body">
        <aside className="rail" aria-label="主导航">
          <div className="rail-owner">
            <span className="rail-owner-name" title={ownerLabel}>{ownerLabel}</span>
            <span className="rail-owner-meta">
              <Pill tone={personal ? 'muted' : 'accent'}>{personal ? '个人空间' : '组织'}</Pill>
              <span className="mono">{org.role}</span>
            </span>
          </div>

          {railGroups.map((group) => (
            <div key={group.label} className="rail-group">
              <div className="rail-label">{group.label}</div>
              {group.items.map((item) => (
                <button key={item.tab} type="button" className={`rail-link ${tab === item.tab ? 'active' : ''}`}
                  aria-current={tab === item.tab ? 'page' : undefined} onClick={() => setTab(item.tab)}>
                  <Icon name={item.icon} size={15} />
                  <span>{item.label}</span>
                </button>
              ))}
            </div>
          ))}

          {error && <p className="error" role="alert" style={{ padding: '8px 10px 0' }}>{error}</p>}

          <div className="rail-foot">
            {project && (
              <button type="button" className="rail-project" style={{ cursor: 'pointer', textAlign: 'left', font: 'inherit' }}
                onClick={() => { setMenu('project'); }}>
                <span className="rail-project-label">当前项目</span>
                <span className="rail-project-name"><Icon name="folder-open" size={13} />{project.name}</span>
                <span className="rail-project-path">{project.slug}</span>
              </button>
            )}
            <VersionTag className="sidebar-version" />
          </div>
        </aside>

        <main className="main">
          <div className="stage">
            {tab === 'secrets'
              ? project && <SecretsPanel key={project.id} projectId={project.id} projectSlug={project.slug} projectName={project.name} ownerLabel={ownerLabel} canWrite={org.role === 'developer' || canAdmin} />
              : tab === 'identities'
                ? <IdentitiesPanel orgSlug={org.slug} project={project} projects={projects} />
                : tab === 'dynamic'
                  ? project && <DynamicPanel key={project.id} projectId={project.id} projectSlug={project.slug} />
                  : tab === 'settings'
                    ? <SettingsPanel key={org.id} orgSlug={org.slug} role={org.role} personal={personal} initialSub={settingsSub ?? undefined} />
                    : tab === 'scans'
                      ? <ScanReports orgSlug={org.slug} />
                      : canAudit ? <AuditPanel orgSlug={org.slug} orgName={ownerLabel} /> : <EmptyState icon="lock" title="没有审计权限">只有 owner 与 admin 可以查看审计链路。</EmptyState>}
          </div>
        </main>
      </div>

      <nav className="mobile-nav" aria-label="主导航（窄屏）">
        {mobileItems.map((item) => (
          <button key={item.tab} type="button" className={navCurrent === item.tab ? 'active' : ''}
            aria-current={navCurrent === item.tab ? 'page' : undefined} onClick={() => setTab(item.tab)}>
            <Icon name={item.icon} size={18} />
            <span>{item.label}</span>
          </button>
        ))}
      </nav>

      {switchOrg && <OrganizationSwitcher onClose={() => setSwitchOrg(false)} />}
      {showIdentity && <IdentityReplacement key={org.id} orgSlug={org.slug} onClose={() => setShowIdentity(false)} />}
      {show2FA && <TotpPanel onClose={() => setShow2FA(false)} />}
      {showGitHub && <GitHubPanel onClose={() => setShowGitHub(false)} />}
    </div>
  );
}

/* ───────────────────────── 两步验证（TOTP 2FA） ───────────────────────── */

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
      <div className="form-stack">
        {err && <Notice tone="danger">{err}</Notice>}
        {enabled === null && !err && <Skeleton rows={3} />}

        {enabled === false && !setup && !codes && (
          <>
            <p className="muted">开启后，登录除密码外还需验证器动态码（兼容 Google Authenticator / 1Password 等）。</p>
            <div className="form-actions"><Button variant="primary" icon="shield" loading={busy} onClick={() => void startSetup()}>开始设置</Button></div>
          </>
        )}

        {enabled === false && setup && (
          <ValidatedForm onSubmit={verify} className="form-stack">
            <p>用验证器扫描下方二维码，或手动录入密钥：</p>
            <img className="qr" src={setup.qr_png} alt="TOTP QR Code" />
            <Field label="密钥（base32）" htmlFor="totp-secret"><input id="totp-secret" className="mono" readOnly value={setup.secret} onFocus={(e) => e.target.select()} /></Field>
            <Field label="验证器上的 6 位码" htmlFor="totp-code"><input id="totp-code" inputMode="numeric" className="mono" value={code} onChange={(e) => setCode(e.target.value)} required pattern="\d{6}" placeholder="123456" autoFocus autoComplete="one-time-code" /></Field>
            <div className="form-actions">
              <Button variant="quiet" onClick={() => setSetup(null)}>取消</Button>
              <Button variant="primary" type="submit" loading={busy}>验证并开启</Button>
            </div>
          </ValidatedForm>
        )}

        {codes && (
          <div className="once-secret">
            <h3>已开启！恢复码仅此一次展示，请立即离线保存</h3>
            <ol className="recovery-codes mono">{codes.map((c) => <li key={c}>{c}</li>)}</ol>
            <div className="form-actions"><Button variant="stamp" icon="check" onClick={() => setCodes(null)}>我已保存</Button></div>
          </div>
        )}

        {enabled === true && !codes && (
          <ValidatedForm onSubmit={disable} className="form-stack">
            <p><Pill tone="ok" icon="shield">已开启</Pill> <span className="muted">登录需密码 + 动态码。</span></p>
            <Field label="输入密码确认关闭" htmlFor="totp-password"><input id="totp-password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required /></Field>
            <div className="form-actions">
              <Button variant="quiet" disabled={busy} onClick={onClose}>关闭</Button>
              <Button variant="danger" type="submit" loading={busy}>关闭两步验证</Button>
            </div>
          </ValidatedForm>
        )}
      </div>
    </Modal>
  );
}

/* ───────────────────────── 密钥面 ───────────────────────── */

function SecretsPanel({ projectId, projectSlug, projectName, ownerLabel, canWrite: legacyCanWrite }: {
  projectId: string; projectSlug: string; projectName: string; ownerLabel: string; canWrite: boolean;
}) {
  const [folders, setFolders] = useState<Folder[]>([]);
  const [secrets, setSecrets] = useState<SecretMeta[]>([]);
  const [cur, setCur] = useState('/'); // 当前文件夹路径（物化路径 /a/b/）
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<string | null>(null);
  const [importing, setImporting] = useState(false); // key 或 '__new__'
  const [detail, setDetail] = useState<SecretMeta | null>(null);
  const [query, setQuery] = useState('');
  const [permissions, setPermissions] = useState<{ write: boolean; delete: boolean; rollback: boolean; export: boolean } | null>(null);
  const canWrite = permissions?.write ?? (!hasBrowserSession() && legacyCanWrite);

  const loadFolders = useCallback(() => {
    api.get<{ folders: Folder[] }>(`/api/v1/projects/${projectId}/folders`)
      .then((d) => setFolders(d.folders))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  }, [projectId]);

  const listGeneration = useRef(0);
  const selectFolder = (path: string) => { listGeneration.current++; setSecrets([]); setEditing(null); setDetail(null); setCur(path); };
  const load = useCallback(() => {
    const started = ++listGeneration.current;
    api.get<{ secrets: SecretMeta[]; permissions?: { write: boolean; delete: boolean; rollback: boolean; export: boolean } }>(`/api/v1/projects/${projectId}/secrets?path=${encodeURIComponent(cur)}`)
      .then((d) => { if (started !== listGeneration.current) return; setError(''); setSecrets(d.secrets); setPermissions(d.permissions ?? null); if (hasBrowserSession() && !d.permissions?.write) setEditing(null); })
      .catch((e) => { if (started !== listGeneration.current) return; setSecrets([]); setPermissions({ write: false, delete: false, rollback: false, export: false }); setEditing(null); setDetail(null); setError(e instanceof ApiError ? e.message : String(e)); });
  }, [projectId, cur]);

  useEffect(load, [load]);
  useEffect(() => { const refresh = () => { load(); }; window.addEventListener('taboo:permissions-refresh', refresh); return () => window.removeEventListener('taboo:permissions-refresh', refresh); }, [load]);
  useEffect(loadFolders, [loadFolders]);

  async function createFolder() {
    const name = window.prompt(`在当前文件夹 ${cur} 下新建子文件夹名称？`);
    if (!name) return;
    const clean = name.trim().replace(/^\/+|\/+$/g, '');
    if (!clean) return;
    try {
      await api.post(`/api/v1/projects/${projectId}/folders`, { path: cur + clean + '/' });
      loadFolders();
    } catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
  }

  async function deleteFolder() {
    if (cur === '/') return;
    if (!window.confirm(`删除空文件夹 ${cur}？（仅空文件夹可删除）`)) return;
    try {
      await api.del(`/api/v1/projects/${projectId}/folders?path=${encodeURIComponent(cur)}`);
      selectFolder(parentOf(cur));
      loadFolders();
    } catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
  }

  const crumbs = cur === '/' ? [] : cur.replace(/^\/+|\/+$/g, '').split('/');
  const q = query.trim().toLowerCase();
  const shown = q
    ? secrets.filter((s) => s.key.toLowerCase().includes(q) || s.comment.toLowerCase().includes(q) || s.tags.some((t) => t.toLowerCase().includes(q)))
    : secrets;
  const currentFolder = folders.find((f) => f.path === cur);
  const canDeleteFolder = (permissions?.delete ?? (!hasBrowserSession() && canWrite)) && cur !== '/';
  const writableCount = secrets.filter((s) => s.canWrite !== false).length;

  return (
    <div className="panel">
      <PageHead
        title={<h1 style={{ font: 'inherit', letterSpacing: 'inherit' }}>{projectSlug}</h1>}
        meta={<><span>归属于{ownerLabel}</span><span aria-hidden="true">·</span><span className="mono">{projectName}</span></>}
        actions={canWrite ? (
          <button type="button" className="btn btn-primary" aria-label="＋ 新建密钥" onClick={() => setEditing('__new__')}>
            <Icon name="plus" size={13} /><span className="btn-label nowrap">新建密钥</span>
          </button>
        ) : undefined}
      />

      {error && <Notice tone="danger">{error}</Notice>}

      <div className="secret-toolbar">
        <div className="search">
          <Icon name="search" size={14} />
          <input type="search" aria-label="搜索密钥" placeholder="搜索 Key / 备注 / 标签" value={query} onChange={(e) => setQuery(e.target.value)} />
          {q ? <IconButton icon="x" label="清除搜索" className="sm" onClick={() => setQuery('')} /> : null}
        </div>
        <div className="toolbar-actions">
          {canWrite && <button type="button" className="btn btn-quiet" aria-label="＋ 新建文件夹" onClick={() => void createFolder()}><Icon name="folder" /><span className="btn-label nowrap">新建文件夹</span></button>}
          {canDeleteFolder && <Button variant="quiet" icon="trash" onClick={() => void deleteFolder()}>删除此空文件夹</Button>}
          {canWrite && <Button variant="default" icon="upload" onClick={() => setImporting(true)}>导入 .env</Button>}
        </div>
      </div>

      <div className="path-bar">
        <Icon name="folder-open" size={13} />
        <button type="button" className={`crumb ${cur === '/' ? 'active' : ''}`} aria-current={cur === '/' ? 'page' : undefined} onClick={() => selectFolder('/')}>根目录</button>
        {crumbs.map((c, i) => {
          const p = '/' + crumbs.slice(0, i + 1).join('/') + '/';
          return (
            <span key={p} style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
              <Icon name="chevron-right" size={11} />
              <button type="button" className={`crumb ${cur === p ? 'active' : ''}`} aria-current={cur === p ? 'page' : undefined} onClick={() => selectFolder(p)}>{c}</button>
            </span>
          );
        })}
        <span className="spacer" />
        <span className="cell-sub mono">{shown.length}{q ? ` / ${secrets.length}` : ''} 把</span>
      </div>

      <div className="split">
        <aside className="folder-tree" aria-label="文件夹">
          <FolderTree folders={folders} cur={cur} onSelect={selectFolder} />
        </aside>
        <div className="grow">
          {secrets.length === 0 && !q ? (
            <EmptyState icon="key" title="此文件夹还没有密钥" action={canWrite ? (
              <Button variant="default" icon="plus" onClick={() => setEditing('__new__')}>在此文件夹新建密钥</Button>
            ) : undefined}>
              key 归属于一个文件夹；同一 key 在不同目录互不冲突。
            </EmptyState>
          ) : shown.length === 0 ? (
            <EmptyState icon="search" title="没有匹配的密钥" action={<Button variant="quiet" onClick={() => setQuery('')}>清除搜索</Button>}>
              没有 Key、备注或标签包含「{query.trim()}」。
            </EmptyState>
          ) : (
            /* 卡片模式钩子：data-mobile="cards" 的折行样式由 styles.css 提供，
               这里只保证单元格可换行、不假设任何固定列宽。 */
            <div className="table-scroll" role="region" aria-label="密钥列表" tabIndex={0}>
              <table className="secret-table" data-mobile="cards">
                <thead>
                  <tr><th>Key</th><th>值</th><th>版本</th><th>标签</th><th>更新时间</th><th className="col-actions">动作</th></tr>
                </thead>
                <tbody>
                  {shown.map((s) => (
                    <SecretRow key={`${s.id}:${s.version}`} s={s} projectId={projectId} canWrite={canWrite && (s.canWrite !== false)}
                      active={detail?.id === s.id} onChanged={load} onEdit={() => setEditing(s.key)} onDetail={() => setDetail(s)} />
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {q && shown.length > 0 && <p className="section-hint">已在当前文件夹内按 Key / 备注 / 标签过滤，共 {shown.length} 把。</p>}
        </div>
      </div>

      {editing && (
        <SecretEditor projectId={projectId} path={cur}
          secretKey={editing === '__new__' ? '' : editing}
          expectedVersion={editing === '__new__' ? 0 : secrets.find((s) => s.key === editing)?.version}
          onClose={() => setEditing(null)} onSaved={() => { setEditing(null); load(); }} />
      )}
      {importing && (
        <ImportEnv projectId={projectId} path={cur} onClose={() => setImporting(false)} onImported={load} />
      )}
      {detail && (
        <SecretDetail projectId={projectId} projectSlug={projectSlug} meta={detail}
          onChanged={load} onClose={() => setDetail(null)}
          onEdit={() => { setEditing(detail.key); setDetail(null); }} />
      )}
    </div>
  );
}

function parentOf(path: string) {
  const t = path.replace(/^\/+|\/+$/g, '');
  const i = t.lastIndexOf('/');
  return i < 0 ? '/' : '/' + t.slice(0, i) + '/';
}

function SecretEditor({ projectId, path, secretKey, expectedVersion, onClose, onSaved }: {
  projectId: string; path: string; secretKey: string; expectedVersion?: number; onClose: () => void; onSaved: () => void;
}) {
  const mutate = useSecretMutation();
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
      const saved = await mutate('post', `/api/v1/projects/${projectId}/secrets?path=${encodeURIComponent(path)}`, {
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
      <ValidatedForm className="form-stack" onSubmit={save}>
        {/* 可访问名必须恰好是「Key」（测试按 exact 匹配），所以说明用 aria-describedby 而不是写进 label */}
        <Field label="Key" htmlFor="secret-editor-key">
          <input id="secret-editor-key" className="mono" aria-describedby="secret-editor-key-hint"
            value={key} onChange={(e) => setKey(e.target.value)} required disabled={!!secretKey} autoFocus={!secretKey} />
        </Field>
        <span id="secret-editor-key-hint" className="field-hint" style={{ marginTop: -8 }}>
          {secretKey ? `Key 即定位符的一段，创建后不可改名 · 位置 ${path}` : `单段 key，用文件夹分层 · 位置 ${path}`}
        </span>
        <Field label="值" htmlFor="secret-editor-value"><textarea id="secret-editor-value" className="resize-none mono" value={value} onChange={(e) => setValue(e.target.value)} required rows={4} spellCheck={false} autoFocus={!!secretKey} /></Field>
        <Field label="备注" htmlFor="secret-editor-comment"><input id="secret-editor-comment" value={comment} onChange={(e) => setComment(e.target.value)} /></Field>
        <Field label="标签（逗号分隔）" htmlFor="secret-editor-tags"><input id="secret-editor-tags" value={tags} onChange={(e) => setTags(e.target.value)} placeholder="db, prod" /></Field>
        {status && <Notice tone="warn">{status}</Notice>}
        {err && <Notice tone="danger">{err}</Notice>}
        <div className="form-actions">
          <Button variant="quiet" disabled={busy} onClick={onClose}>取消</Button>
          <Button variant="primary" type="submit" icon="seal" loading={busy}>保存（产生新版本）</Button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

/* ───────────────────────── 审计 ───────────────────────── */

function actionGlyph(action: string) {
  const head = action.split('.')[0] ?? '';
  const verb = action.split('.')[1] ?? action;
  return { head, verb };
}

function actionClass(action: string) {
  const v = (action.split('.')[1] ?? '').toLowerCase();
  if (v.includes('delete') || v.includes('revoke') || v.includes('rollback')) return 'act-delete';
  if (v.includes('reveal') || v.includes('export')) return 'act-reveal';
  if (v.includes('create') || v.includes('update') || v.includes('write') || v.includes('import')) return 'act-write';
  return 'act-read';
}

function AuditPanel({ orgSlug, orgName }: { orgSlug: string; orgName: string }) {
  const [logs, setLogs] = useState<AuditLog[] | null>(null);
  const [action, setAction] = useState('');
  const [actor, setActor] = useState('');
  const [resource, setResource] = useState('');
  const [jobs, setJobs] = useState<{ id: string; status: string; format: string }[]>([]);
  const [verify, setVerify] = useState<string>('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  const qs = () => {
    const p = new URLSearchParams({ limit: '200' });
    if (action) p.set('action', action);
    if (actor) p.set('actor', actor);
    if (resource) p.set('resource', resource);
    return p.toString();
  };

  const auditGeneration = useRef(0);
  const load = useCallback(() => {
    setLogs(null);
    const started = ++auditGeneration.current;
    setErr('');
    api.get<{ logs: AuditLog[] }>(`/api/v1/orgs/${orgSlug}/audit?${qs()}`)
      .then((d) => { if (started === auditGeneration.current) setLogs(d.logs); })
      .catch((e) => { if (started === auditGeneration.current) { setLogs([]); setErr(e instanceof Error ? e.message : String(e)); } });
    api.get<{ exports: { id: string; status: string; format: string }[] }>(`/api/v1/orgs/${orgSlug}/audit/exports`)
      .then((d) => { if (started === auditGeneration.current) setJobs(d.exports ?? []); })
      .catch(() => { if (started === auditGeneration.current) setJobs([]); });
  }, [orgSlug, action, actor, resource]);
  useEffect(load, [load]);

  async function download(format: string) {
    setBusy(true);
    try { await api.get(`/api/v1/orgs/${orgSlug}/audit/export?format=${format}&async=1&${qs()}`); load(); }
    catch (e) { setErr(e instanceof Error ? e.message : String(e)); }
    finally { setBusy(false); }
  }

  async function checkChain() {
    setBusy(true);
    try {
      const d = await api.get<{ ok: boolean; checked: number; broken_at?: string }>(`/api/v1/orgs/${orgSlug}/audit/verify`);
      setVerify(d.ok ? `链完整（${d.checked} 条）` : `链断裂于 ${d.broken_at}`);
    } catch (e) { setVerify(''); setErr(e instanceof ApiError ? e.message : String(e)); }
    finally { setBusy(false); }
  }

  // 按本地日期分组：审计是第二条时间线，日期是最自然的结界线。
  const days: { day: string; rows: AuditLog[] }[] = [];
  for (const l of logs ?? []) {
    const day = fmtTime(l.created_at).slice(0, 10) || '未知';
    const last = days[days.length - 1];
    if (last && last.day === day) last.rows.push(l);
    else days.push({ day, rows: [l] });
  }
  const filtering = Boolean(action || actor || resource);

  return (
    <div className="panel">
      <PageHead
        title="审计日志"
        meta={<><span>append-only 哈希链 · {orgName}</span><span aria-hidden="true">·</span><span className="mono">org {orgSlug}</span></>}
        actions={<>
          <Button variant="quiet" icon="refresh" onClick={load} disabled={busy}>刷新</Button>
          <Button variant="default" icon="seal" onClick={() => void checkChain()} loading={busy}>验真</Button>
        </>}
      />

      <div className="audit-layout">
        <div className="panel">
          <div className="audit-filters">
            <input aria-label="筛选 action" placeholder="action" value={action} onChange={(e) => setAction(e.target.value)} />
            <input aria-label="筛选 actor" placeholder="actor" value={actor} onChange={(e) => setActor(e.target.value)} />
            <input aria-label="筛选 resource" placeholder="resource" value={resource} onChange={(e) => setResource(e.target.value)} />
            {filtering && <Button variant="quiet" icon="x" onClick={() => { setAction(''); setActor(''); setResource(''); }}>清除筛选</Button>}
            <span className="spacer" />
            <Button variant="quiet" icon="download" onClick={() => void download('csv')} disabled={busy}>异步导出 CSV</Button>
            <Button variant="quiet" icon="download" onClick={() => void download('jsonl')} disabled={busy}>异步导出 JSONL</Button>
          </div>
          {verify && <Notice tone={verify.startsWith('链完整') ? 'ok' : 'warn'}>{verify}</Notice>}
          {err && <Notice tone="danger">{err}</Notice>}
          {logs === null ? (
            <Skeleton rows={6} />
          ) : days.length === 0 ? (
            <EmptyState icon="audit" title={filtering ? '没有匹配的审计记录' : '审计日志为空'}>
              {filtering ? '当前筛选条件下没有记录。清除筛选可查看全部。' : '写入、显示、导出、回滚都会在这里留下一条不可修改的记录。'}
            </EmptyState>
          ) : (
            <div className="audit-list">
              {days.map((group) => (
                <section key={group.day} aria-label={group.day}>
                  <div className="audit-day">{group.day} · {group.rows.length}</div>
                  {group.rows.map((l, i) => (
                    <div className="audit-row" key={l.id ?? `${group.day}-${i}`}>
                      <span className="t" title={timeTitle(l.created_at)}>{fmtTime(l.created_at).slice(11) || fmtTime(l.created_at)}</span>
                      <span className="who" title={l.actor_name ?? ''}>{l.actor_name || '—'}</span>
                      <span className="act action"><span className={`act-glyph ${actionClass(l.action ?? '')}`} title={l.action ?? ''}>{actionGlyph(l.action ?? '').verb || l.action || '—'}</span></span>
                      <span className="res" title={l.resource ?? ''}>{l.resource || '—'}</span>
                      <span className="ip">{l.ip || '—'}</span>
                    </div>
                  ))}
                </section>
              ))}
            </div>
          )}
        </div>

        <aside className="panel" aria-label="导出任务">
          <Section title="导出任务" hint="异步生成，完成后可下载；失败可重新生成。">
            {jobs.length === 0 ? (
              <p className="section-hint">还没有导出任务。</p>
            ) : (
              <div className="audit-list">
                {jobs.map((j) => (
                  <div key={j.id} className="audit-row" style={{ gridTemplateColumns: 'minmax(0, 1fr) auto', gap: 8, gridTemplateAreas: 'none' }}>
                    <span className="res">{j.format} · {j.status}</span>
                    <span className="btn-row" style={{ justifyContent: 'flex-end' }}>
                      {j.status === 'done' && (
                        <Button variant="quiet" className="btn-sm" icon="download"
                          onClick={() => api.download(`/api/v1/orgs/${orgSlug}/audit/exports/${j.id}`, `audit.${j.format}`).catch((e) => setErr(String(e)))}>下载</Button>
                      )}
                      {j.status === 'failed' && (
                        <Button variant="quiet" className="btn-sm" icon="refresh"
                          onClick={() => api.post(`/api/v1/orgs/${orgSlug}/audit/exports/${j.id}/retry`).then(load).catch((e) => setErr(String(e)))}>重新生成</Button>
                      )}
                    </span>
                  </div>
                ))}
              </div>
            )}
            <div className="btn-row"><Button variant="quiet" className="btn-sm" icon="refresh" onClick={load}>刷新任务</Button></div>
          </Section>
          <Section title="链路" hint="每条记录参与哈希链；验真检查链与 checkpoint 是否连续。">
            <MetaList items={[
              { label: '范围', value: `org ${orgSlug}`, mono: true },
              { label: '本次显示', value: `${logs?.length ?? 0} 条`, mono: true },
              { label: '验真', value: verify || '尚未验真' },
            ]} />
          </Section>
        </aside>
      </div>
    </div>
  );
}

/* ───────────────────────── 机器身份 ───────────────────────── */

function IdentitiesPanel({ orgSlug, project, projects }: { orgSlug: string; project: Project | null; projects: Project[] }) {
  const [identities, setIdentities] = useState<Identity[] | null>(null);
  const [error, setError] = useState('');
  const [showNew, setShowNew] = useState(false);
  const [once, setOnce] = useState<CreatedIdentity | null>(null);

  const load = useCallback(() => {
    api.get<{ identities: Identity[] }>(`/api/v1/orgs/${orgSlug}/identities`)
      .then((d) => { setIdentities(d.identities); setError(''); })
      .catch((e) => { setIdentities([]); setError(e instanceof ApiError ? e.message : String(e)); });
  }, [orgSlug]);
  useEffect(load, [load]);

  async function revoke(id: string, name: string) {
    if (!window.confirm(`吊销机器身份「${name}」？其 token 将立即失效。`)) return;
    try {
      await api.post(`/api/v1/orgs/${orgSlug}/identities/${id}/revoke`);
      load();
    } catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
  }

  const active = identities?.filter((i) => i.status === 'active').length ?? 0;

  return (
    <div className="panel">
      <PageHead
        title="机器身份"
        meta={<>为 CI/CD、AI Agent 颁发 <span className="mono">client_id</span> + <span className="mono">client_secret</span>，作用域显式限定到项目和读写权限，禁止通配。</>}
        actions={<button type="button" className="btn btn-primary" aria-label="＋ 创建机器身份" onClick={() => setShowNew(true)}><Icon name="plus" size={13} /><span className="btn-label nowrap">创建机器身份</span></button>}
      />
      {error && <Notice tone="danger">{error}</Notice>}
      {once && (
        <div className="once-secret">
          <h3>「{once.name}」已创建 — client_secret 仅此一次展示，请立即保存</h3>
          <Field label="Client ID" htmlFor="once-client-id"><input id="once-client-id" className="mono" readOnly value={once.client_id} onFocus={(e) => e.target.select()} /></Field>
          <Field label="Client Secret" htmlFor="once-client-secret"><input id="once-client-secret" className="mono" readOnly value={once.client_secret} onFocus={(e) => e.target.select()} /></Field>
          <p>换 token：<code>POST /api/v1/identities/token</code>，TTL {once.token_ttl}s</p>
          <div className="form-actions"><Button variant="stamp" icon="check" onClick={() => setOnce(null)}>我已保存，关闭</Button></div>
        </div>
      )}
      {identities === null ? (
        <Skeleton rows={4} />
      ) : identities.length === 0 ? (
        <EmptyState icon="robot" title="还没有机器身份" action={project ? <Button variant="default" icon="plus" onClick={() => setShowNew(true)}>创建第一个机器身份</Button> : undefined}>
          机器身份用于流水线与自动化，按 scope 访问指定项目的密钥。
        </EmptyState>
      ) : (
        <TableWrap label="机器身份列表">
          <table className="secret-table">
            <thead>
              <tr><th>名称</th><th>Client ID</th><th>Scope（项目 / 权限）</th><th>IP 白名单</th><th>扫描报告</th><th>状态</th><th>TTL</th><th className="col-actions">动作</th></tr>
            </thead>
            <tbody>
              {identities.map((i) => (
                <tr key={i.id}>
                  <td className="col-key"><span className="mono">{i.name}</span></td>
                  <td><span className="mono cell-sub">{i.client_id}</span></td>
                  <td>
                    <span className="tag-row">
                      {i.scopes.map((s, idx) => (
                        <Pill key={idx} tone={s.permission === 'read' ? 'muted' : 'accent'}>
                          {s.project_slug} · {s.permission}
                        </Pill>
                      ))}
                    </span>
                  </td>
                  <td className="mono cell-sub">{i.allowed_cidrs?.length ? i.allowed_cidrs.join(', ') : '不限制'}</td>
                  <td className="cell-sub">{i.list_scan_reports ? '全部' : '仅自己'}</td>
                  <td><Pill tone={i.status === 'active' ? 'ok' : 'bad'}>{i.status}</Pill></td>
                  <td className="mono">{i.token_ttl}s</td>
                  <td className="col-actions">
                    {i.status === 'active' && (
                      <span className="secret-actions">
                        <Button variant="danger" className="btn-sm" icon="unlock" onClick={() => void revoke(i.id, i.name)}>吊销</Button>
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableWrap>
      )}
      {identities && identities.length > 0 && <p className="section-hint">{active} 个生效中，共 {identities.length} 个。</p>}
      {showNew && project && (
        <IdentityCreator
          orgSlug={orgSlug}
          project={project}
          projects={projects}
          onClose={() => setShowNew(false)}
          onCreated={(d) => { setShowNew(false); setOnce(d); load(); }}
        />
      )}
    </div>
  );
}

function IdentityCreator({ orgSlug, project, projects, onClose, onCreated }: {
  orgSlug: string; project: Project; projects: Project[];
  onClose: () => void; onCreated: (d: CreatedIdentity) => void;
}) {
  const [name, setName] = useState('');
  const [ttl, setTtl] = useState(900);
  const [cidrs, setCidrs] = useState('');
  const [listScans, setListScans] = useState(false);
  const [scopes, setScopes] = useState<IdentityScope[]>([{ project_id: project.id, project_slug: project.slug, permission: 'read' }]);
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
        scopes: scopes.map((s) => ({ project_id: s.project_id, permission: s.permission })),
      });
      onCreated(d);
    } catch (e2) { setErr(e2 instanceof ApiError ? e2.message : String(e2)); }
    finally { setBusy(false); }
  }

  return (
    <Modal title="创建机器身份" onClose={onClose} busy={busy}>
      <ValidatedForm className="form-stack" onSubmit={save}>
        <Field label="名称" htmlFor="identity-name"><input id="identity-name" className="mono" value={name} onChange={(e) => setName(e.target.value)} required placeholder="ci-runner" autoFocus /></Field>
        <Field label="Token TTL（秒，≤3600）" htmlFor="identity-ttl">
          <input id="identity-ttl" type="number" value={ttl} onChange={(e) => setTtl(Number(e.target.value))} min={60} max={3600} />
        </Field>
        <Field label="IP 白名单（可选，逗号或换行分隔）" htmlFor="identity-cidrs">
          <textarea id="identity-cidrs" className="resize-none mono" rows={2} value={cidrs} onChange={(e) => setCidrs(e.target.value)} placeholder="10.0.0.0/8, 192.0.2.10" />
        </Field>
        <p className="section-hint" style={{ marginTop: -6 }}>Token TTL 是每次换取 JWT 的有效期；IP 白名单留空表示不限制来源。</p>
        <Check label="可查看组织全部扫描报告（默认只能看自己上传的）" checked={listScans} onChange={(e) => setListScans(e.target.checked)} />
        <div className="scope-editor">
          <div className="form-stack" style={{ gap: 8 }}>
            <span className="field-label">Scopes（至少一项，禁止通配）</span>
            {scopes.map((s, idx) => (
              <div key={idx} className="scope-row">
                <select aria-label={`scope ${idx + 1} 项目`} value={s.project_id ?? project.id}
                  onChange={(e) => {
                    const target = projects.find((p) => p.id === e.target.value);
                    setScopes(scopes.map((x, i) => i === idx ? { ...x, project_id: e.target.value, project_slug: target?.slug ?? x.project_slug } : x));
                  }}>
                  {projects.map((p) => <option key={p.id} value={p.id}>{p.slug}</option>)}
                </select>
                <select aria-label={`scope ${idx + 1} 权限`} value={s.permission}
                  onChange={(e) => setScopes(scopes.map((x, i) => i === idx ? { ...x, permission: e.target.value as IdentityScope['permission'] } : x))}>
                  <option value="read">read（仅元数据）</option>
                  <option value="reveal">reveal（明文）</option>
                  <option value="write">write</option>
                </select>
                <IconButton icon="x" label={`移除 scope ${idx + 1}`} disabled={scopes.length === 1}
                  onClick={() => setScopes(scopes.filter((_, i) => i !== idx))} />
              </div>
            ))}
            <div><Button variant="quiet" className="btn-sm" icon="plus" aria-label="＋ 添加 scope"
              onClick={() => setScopes([...scopes, { project_id: project.id, project_slug: project.slug, permission: 'read' }])}>添加 scope</Button></div>
          </div>
        </div>
        {err && <Notice tone="danger">{err}</Notice>}
        <div className="form-actions">
          <Button variant="quiet" disabled={busy} onClick={onClose}>取消</Button>
          <Button variant="primary" type="submit" icon="seal" loading={busy}>创建</Button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

/* ───────────────────────── 动态密钥 ───────────────────────── */

function fmtLeft(l: DynamicLease, tick = 0) {
  if (l.status !== 'active') return l.status;
  // 两次轮询之间按秒递减，剩余时间不会停在旧值上。
  const s = Math.max(0, (l.seconds_left ?? 0) - tick * 1000);
  if (s <= 0) return '回收中…';
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60);
  return h > 0 ? `${h}h ${m}m` : m > 0 ? `${m}m ${s % 60}s` : `${s}s`;
}

/** 剩余时间的密度条：等宽符号承载状态，进度条填充 = 已经过掉的比例。 */
function leaseBar(l: DynamicLease, tickMs = 0) {
  if (l.status !== 'active') return '────────';
  const start = Date.parse(String(l.created_at ?? '').replace(' ', 'T') + (String(l.created_at ?? '').includes('Z') ? '' : 'Z'));
  const started = Number.isNaN(start) ? Date.now() - tickMs : start;
  const total = Math.max(1, ((l.expires_at ?? 0) * 1000) - started);
  const left = Math.max(0, Math.min(total, (l.seconds_left ?? 0) * 1000 - tickMs));
  const filled = Math.round((left / total) * 8);
  return '█'.repeat(Math.max(0, Math.min(8, filled))) + '░'.repeat(Math.max(0, 8 - Math.min(8, filled)));
}

function DynamicPanel({ projectId, projectSlug }: { projectId: string; projectSlug: string }) {
  const [engines, setEngines] = useState<DynamicEngine[] | null>(null);
  const [error, setError] = useState('');
  const [showNew, setShowNew] = useState(false);
  const [tick, setTick] = useState(0); // 每秒自增，驱动剩余时间的走动

  const load = useCallback(() => {
    api.get<{ engines: DynamicEngine[] }>(`/api/v1/projects/${projectId}/dynamic-engines`)
      .then((d) => { setEngines(d.engines); setError(''); })
      .catch((e) => { setEngines([]); setError(e instanceof ApiError ? e.message : String(e)); });
  }, [projectId]);
  useEffect(load, [load]);
  // 每 10s 刷新：seconds_left 倒计时 + worker 回收结果可见
  useEffect(() => {
    const t = setInterval(load, 10_000);
    return () => clearInterval(t);
  }, [load]);
  // 两次刷新之间让「剩余」逐秒走动，而不是跳变。
  useEffect(() => {
    const t = setInterval(() => setTick((v) => v + 1), 1000);
    return () => clearInterval(t);
  }, []);

  async function revokeLease(lid: string, username: string) {
    if (!window.confirm(`立即回收 lease ${username}？（数据库账号将被 DROP）`)) return;
    try {
      await api.post(`/api/v1/projects/${projectId}/dynamic-leases/${lid}/revoke`);
      load();
    } catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
  }

  const leaseCount = (engines ?? []).reduce((n, e) => n + (e.leases?.filter((l) => l.status === 'active').length ?? 0), 0);

  return (
    <div className="panel">
      <PageHead
        title={<h1 style={{ font: 'inherit', letterSpacing: 'inherit' }}>{projectSlug} <span className="faint" style={{ fontWeight: 400 }}>/ 动态密钥</span></h1>}
        meta={<>为 PostgreSQL 或 MySQL 配置动态引擎后，机器身份可用 <code>POST /projects/{'{pid}'}/dynamic-engines/{'{eid}'}/lease</code> 申请短期只读账号（自动创建、到期回收）。 lease 仅本地机器身份可申请；外部登录由 owner 管理引擎与手动回收。失败的回收任务可在此重新发起。</>}
        actions={<>
          <Button variant="quiet" icon="refresh" onClick={load}>刷新</Button>
          <button type="button" className="btn btn-primary" aria-label="＋ 接入数据库引擎" onClick={() => setShowNew(true)}><Icon name="plus" size={13} /><span className="btn-label nowrap">接入数据库引擎</span></button>
        </>}
      />
      {error && <Notice tone="danger">{error}</Notice>}
      {engines === null ? (
        <Skeleton rows={4} />
      ) : engines.length === 0 ? (
        <EmptyState icon="db" title="尚未接入任何数据库引擎" action={<Button variant="default" icon="plus" onClick={() => setShowNew(true)}>接入第一个数据库引擎</Button>}>
          接入后，机器身份按需申请短期数据库账号，到期由系统自动回收。
        </EmptyState>
      ) : (
        <>
          <div className="page-meta">{engines.length} 个引擎 · {leaseCount} 个活跃 lease</div>
          {engines.map((e) => (
            <Section key={e.id}
              title={`${e.name ?? ''} / ${e.type ?? ''}`}
              hint={<>目标库 <span className="mono">{e.database}</span> · TTL {e.default_ttl}s / 上限 {e.max_ttl}s</>}>
              {(e.leases ?? []).length === 0 ? (
                <p className="section-hint">当前无活跃 lease。</p>
              ) : (
                <TableWrap label={`${e.name} 的 lease`}>
                  <table className="secret-table">
                    <thead><tr><th>Lease 账号</th><th>状态</th><th>剩余</th><th>到期时间</th><th className="col-actions">动作</th></tr></thead>
                    <tbody>
                      {(e.leases ?? []).map((l) => (
                        <tr key={l.id}>
                          <td className="col-key"><span className="mono">{l.username}</span></td>
                          <td><Pill tone={l.status === 'active' ? 'ok' : l.status === 'failed' ? 'bad' : 'muted'}>{l.status}</Pill></td>
                          <td><span className="mono cipher-cell"><span className="cipher" aria-hidden="true" title="剩余时间">{leaseBar(l, tick * 1000)}</span><span>{fmtLeft(l, tick)}</span></span></td>
                          <td className="cell-sub nowrap">{l.expires_at ? new Date(l.expires_at * 1000).toLocaleString() : ''}</td>
                          <td className="col-actions">
                            {(l.status === 'active' || l.status === 'failed') && (
                              <span className="secret-actions">
                                <Button variant="danger" className="btn-sm" icon="unlock" onClick={() => l.id && void revokeLease(l.id, l.username ?? '')}>回收</Button>
                              </span>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </TableWrap>
              )}
            </Section>
          ))}
        </>
      )}
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
      <ValidatedForm className="form-stack" onSubmit={save}>
        <Field label="类型" htmlFor="engine-type">
          <select id="engine-type" value={engineType} onChange={(e) => {
            const next = e.target.value as 'postgres' | 'mysql';
            setEngineType(next);
            setDatabase(next === 'mysql' ? 'mysql' : 'postgres');
          }}>
            <option value="postgres">PostgreSQL</option>
            <option value="mysql">MySQL</option>
          </select>
        </Field>
        <Field label="名称" htmlFor="engine-name"><input id="engine-name" className="mono" value={name} onChange={(e) => setName(e.target.value)} required placeholder={engineType === 'mysql' ? 'mysql-main' : 'pg-main'} /></Field>
        <Field label="高权限连接串" htmlFor="engine-conn">
          <input id="engine-conn" className="mono" value={conn} onChange={(e) => setConn(e.target.value)} required
            placeholder={engineType === 'mysql' ? 'mysql://admin:****@db.internal:3306/app' : 'postgres://admin:****@db.internal:5432/postgres'} spellCheck={false} />
        </Field>
        <p className="section-hint" style={{ marginTop: -6 }}>连接串用 Master Key 加密落库，列表不回显。</p>
        <Field label="目标库" htmlFor="engine-database"><input id="engine-database" className="mono" value={database} onChange={(e) => setDatabase(e.target.value)} required /></Field>
        <div className="form-row">
          <Field label="默认 TTL（秒）" htmlFor="engine-ttl"><input id="engine-ttl" type="number" value={defTTL} onChange={(e) => setDefTTL(Number(e.target.value))} min={60} /></Field>
          <Field label="最大 TTL（秒）" htmlFor="engine-max-ttl"><input id="engine-max-ttl" type="number" value={maxTTL} onChange={(e) => setMaxTTL(Number(e.target.value))} min={300} /></Field>
        </div>
        <p className="section-hint">lease 到期后账号自动 DROP。</p>
        {err && <Notice tone="danger">{err}</Notice>}
        <div className="form-actions">
          <Button variant="quiet" disabled={busy} onClick={onClose}>取消</Button>
          <Button variant="primary" type="submit" icon="seal" loading={busy}>接入</Button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

/* ───────────────────────── 用户名确认 ───────────────────────── */

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
      <div className="inline" style={{ gap: 10 }}>
        <span className="field-label" style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <Icon name="seal" size={13} />确认用户名
        </span>
        <label className="inline" style={{ gap: 6 }}>
          <span className="faint" style={{ fontSize: 'var(--fs-11)' }}>用户名</span>
          <input aria-label="用户名" className="mono" value={handle} autoComplete="off" onChange={(e) => setHandle(e.target.value)} />
        </label>
        <Button type="submit" variant="quiet" className="btn-sm" disabled={busy}>保存</Button>
        <Button type="button" variant="stamp" className="btn-sm" icon="seal" disabled={busy} onClick={() => void save(true)}>确认用户名</Button>
        <span className="faint" style={{ fontSize: 'var(--fs-11)' }}>确认后不能再改，它会成为定位符的第一段。</span>
        {error && <span className="error" role="alert">{error}</span>}
      </div>
    </form>
  );
}

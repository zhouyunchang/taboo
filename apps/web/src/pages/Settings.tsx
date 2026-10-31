import { useCallback, useEffect, useState } from 'react';
import { api, ApiError, hasBrowserSession } from '../api';
import { GrantEditor, ConnectionSecurity, OrganizationSecurity, WorkloadGovernance } from '../components/Governance';
import Modal from '../components/Modal';
import ValidatedForm from '../components/ValidatedForm';
import type { SyncTarget, SyncRun, Webhook, OIDCProvider } from '../api';
import { fmtTime, timeTitle } from '../time';

// 组织设置（M5 #11/#12/#13）：Secret Sync / Webhooks / OIDC SSO
export default function SettingsPanel({ orgSlug, role }: { orgSlug: string; role: string }) {
  const [sub, setSub] = useState<'sync' | 'webhooks' | 'sso' | 'members' | 'security' | 'workloads'>('sync');
  const canManage = role === 'owner' || (!hasBrowserSession() && role === 'admin');
  const canMembers = role === 'owner';

  return (
    <div className="panel">
      <div className="panel-head">
        <h2>组织设置</h2>
        <div className="spacer" />
        <button className={`tab ${sub === 'sync' ? 'active' : ''}`} onClick={() => setSub('sync')}>Secret Sync</button>
        <button className={`tab ${sub === 'webhooks' ? 'active' : ''}`} onClick={() => setSub('webhooks')}>Webhooks</button>
        <button className={`tab ${sub === 'sso' ? 'active' : ''}`} onClick={() => setSub('sso')}>用户源 / SSO</button>
        {canMembers && <><button className={`tab ${sub === 'security' ? 'active' : ''}`} onClick={() => setSub('security')}>登录策略</button><button className={`tab ${sub === 'workloads' ? 'active' : ''}`} onClick={() => setSub('workloads')}>OAuth 工作负载</button></>}
        {canMembers && <button className={`tab ${sub === 'members' ? 'active' : ''}`} onClick={() => setSub('members')}>成员与角色</button>}
      </div>
      {!canManage && <p className="muted">当前角色为 {role}，外部登录由 owner 管理身份连接、授权和秘密同步。</p>}
      {sub === 'sync' && <SyncTab orgSlug={orgSlug} canManage={canManage} />}
      {sub === 'webhooks' && <WebhooksTab orgSlug={orgSlug} canManage={canManage} />}
      {sub === 'sso' && <SsoTab orgSlug={orgSlug} canManage={canManage} />}
      {sub === 'security' && canMembers && <OrganizationSecurity orgSlug={orgSlug} />}
      {sub === 'workloads' && canMembers && <WorkloadGovernance orgSlug={orgSlug} />}
      {sub === 'members' && canMembers && <MembersTab orgSlug={orgSlug} />}
    </div>
  );
}

// ---------- Secret Sync（#11） ----------

const PLATFORM_HINTS: Record<string, string> = {
  github: 'config: token, owner, repo, environment（可空，留空为 repo 级）',
  vercel: 'config: token, project_id, team_id（可空）, targets（逗号分隔，默认 production）',
  cloudflare: 'config: token, account_id, script_name',
};

function SyncTab({ orgSlug, canManage }: { orgSlug: string; canManage: boolean }) {
  const [targets, setTargets] = useState<SyncTarget[]>([]);
  const [runs, setRuns] = useState<SyncRun[]>([]);
  const [showNew, setShowNew] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    api.get<{ targets: SyncTarget[] }>(`/api/v1/orgs/${orgSlug}/sync/targets`)
      .then((d) => setTargets(d.targets))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
    api.get<{ runs: SyncRun[] }>(`/api/v1/orgs/${orgSlug}/sync/runs?limit=50`)
      .then((d) => setRuns(d.runs))
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
  }, [orgSlug]);
  useEffect(() => { load(); }, [load]);

  async function remove(id: string) {
    if (!window.confirm('删除该同步目标？')) return;
    try { await api.del(`/api/v1/orgs/${orgSlug}/sync/targets/${id}`); load(); }
    catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
  }

  async function retry(id: string) {
    try {
      const d = await api.post<{ enqueued: number }>(`/api/v1/orgs/${orgSlug}/sync/targets/${id}/retry`);
      setError(`已入队 ${d.enqueued} 个密钥，稍后刷新查看同步记录`);
      setTimeout(load, 3000);
    } catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
  }

  return (
    <>
      <div className="panel-head">
        <h3>同步目标</h3>
        <div className="spacer" />
        {canManage && <button onClick={() => setShowNew(true)}>＋ 添加目标</button>}
      </div>
      <p className="muted">密钥创建/更新/回滚后自动单向推送到目标平台；失败自动退避重试。同步记录只存内容指纹，不记明文。</p>
      {error && <div className="error">{error}</div>}
      <table className="table">
        <thead><tr><th>名称</th><th>平台</th><th>范围</th><th>状态</th><th /></tr></thead>
        <tbody>
          {targets.map((t) => (
            <tr key={t.id}>
              <td className="mono">{t.name}</td>
              <td><span className="tag">{t.platform}</span></td>
              <td className="muted">{t.project_id ? `项目 ${t.project_id.slice(0, 8)}…` : '全部项目'} / {t.env_slug === '*' ? '全部环境' : t.env_slug}</td>
              <td><span className={`tag ${t.enabled ? 'ok' : ''}`}>{t.enabled ? '启用' : '停用'}</span></td>
              <td className="actions">
                {canManage && <button className="ghost" onClick={() => t.id && retry(t.id)}>全量重推</button>}
                {canManage && <button className="ghost" onClick={() => t.id && remove(t.id)}>删除</button>}
              </td>
            </tr>
          ))}
          {targets.length === 0 && <tr><td colSpan={5} className="muted center">还没有同步目标</td></tr>}
        </tbody>
      </table>

      <h3>同步记录</h3>
      <table className="table">
        <thead><tr><th>时间</th><th>Key</th><th>触发</th><th>状态</th><th>尝试</th><th>错误</th></tr></thead>
        <tbody>
          {runs.map((r) => (
            <tr key={r.id}>
              <td className="muted nowrap" title={timeTitle(r.updated_at)}>{fmtTime(r.updated_at)}</td>
              <td className="mono">{r.secret_key}</td>
              <td className="muted">{r.trigger_type}</td>
              <td><span className={`tag ${r.status === 'success' ? 'ok' : r.status === 'dead' ? 'bad' : ''}`}>{r.status}</span></td>
              <td className="muted">{r.attempts}</td>
              <td className="muted">{r.error}</td>
            </tr>
          ))}
          {runs.length === 0 && <tr><td colSpan={6} className="muted center">暂无同步记录</td></tr>}
        </tbody>
      </table>
      {showNew && <SyncTargetCreator orgSlug={orgSlug} onClose={() => setShowNew(false)} onCreated={() => { setShowNew(false); load(); }} />}
    </>
  );
}

function SyncTargetCreator({ orgSlug, onClose, onCreated }: { orgSlug: string; onClose: () => void; onCreated: () => void }) {
  const [name, setName] = useState('');
  const [platform, setPlatform] = useState('github');
  const [envSlug, setEnvSlug] = useState('*');
  const [configText, setConfigText] = useState('');
  const [ping, setPing] = useState(true);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true); setErr('');
    const config: Record<string, string> = {};
    for (const line of configText.split('\n')) {
      const i = line.indexOf('=');
      if (i > 0) config[line.slice(0, i).trim()] = line.slice(i + 1).trim();
    }
    try {
      await api.post(`/api/v1/orgs/${orgSlug}/sync/targets${ping ? '?ping=1' : ''}`, {
        name, platform, env_slug: envSlug, config,
      });
      onCreated();
    } catch (e2) { setErr(e2 instanceof ApiError ? e2.message : String(e2)); }
    finally { setBusy(false); }
  }

  return (
    <Modal title="添加同步目标" onClose={onClose} busy={busy}>
      <ValidatedForm className="drawer-inner" onClick={(e) => e.stopPropagation()} onSubmit={save}>
        <h3>添加同步目标</h3>
        <label>名称<input value={name} onChange={(e) => setName(e.target.value)} required placeholder="prod-github" /></label>
        <label>平台
          <select value={platform} onChange={(e) => setPlatform(e.target.value)}>
            <option value="github">GitHub Actions Secrets</option>
            <option value="vercel">Vercel</option>
            <option value="cloudflare">Cloudflare Workers</option>
          </select>
        </label>
        <label>环境（'*' 为全部）<input value={envSlug} onChange={(e) => setEnvSlug(e.target.value)} /></label>
        <label>配置（每行 key=value；平台令牌经 Master Key 加密落库）
          <textarea className="mono resize-none" rows={5} value={configText} onChange={(e) => setConfigText(e.target.value)} required
            placeholder={PLATFORM_HINTS[platform]} />
        </label>
        <label className="row"><input type="checkbox" checked={ping} onChange={(e) => setPing(e.target.checked)} /> 创建前测试平台连通性</label>
        {err && <div className="error">{err}</div>}
        <div className="drawer-actions">
          <button type="button" className="ghost" disabled={busy} onClick={onClose}>取消</button>
          <button type="submit" disabled={busy}>{busy ? '创建中…' : '创建'}</button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

// ---------- Webhooks（#12） ----------

function WebhooksTab({ orgSlug, canManage }: { orgSlug: string; canManage: boolean }) {
  const [hooks, setHooks] = useState<Webhook[]>([]);
  const [deliveries, setDeliveries] = useState<Record<string, Array<Record<string, unknown>>>>({});
  const [expanded, setExpanded] = useState('');
  const [showNew, setShowNew] = useState(false);
  const [once, setOnce] = useState<Webhook | null>(null);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    api.get<{ webhooks: Webhook[] }>(`/api/v1/orgs/${orgSlug}/webhooks`)
      .then((d) => setHooks(d.webhooks))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  }, [orgSlug]);
  useEffect(() => { load(); }, [load]);

  async function remove(id: string) {
    if (!window.confirm('删除该订阅？未完成的投递将终止。')) return;
    try { await api.del(`/api/v1/orgs/${orgSlug}/webhooks/${id}`); load(); }
    catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
  }

  async function showDeliveries(id: string) {
    if (expanded === id) { setExpanded(''); return; }
    setExpanded(id);
    try {
      const d = await api.get<{ deliveries: Array<Record<string, unknown>> }>(`/api/v1/orgs/${orgSlug}/webhooks/${id}/deliveries`);
      setDeliveries((m) => ({ ...m, [id]: d.deliveries }));
    } catch { /* 忽略 */ }
  }

  return (
    <>
      <div className="panel-head">
        <h3>Webhook 订阅</h3>
        <div className="spacer" />
        {canManage && <button onClick={() => setShowNew(true)}>＋ 添加订阅</button>}
      </div>
      <p className="muted">密钥创建/更新/回滚/删除时向订阅 URL 广播事件（HMAC-SHA256 签名，负载不含明文）。
        接收方用签名密钥对 <code>&lt;t&gt;.&lt;body&gt;</code> 验签，并校验时间戳窗口防重放。</p>
      {error && <div className="error">{error}</div>}
      {once && (
        <div className="card once-secret">
          <h3>订阅已创建 — 签名密钥仅此一次展示，请立即保存</h3>
          <label>签名密钥<input readOnly value={once.secret} onFocus={(e) => e.target.select()} /></label>
          <button className="ghost" onClick={() => setOnce(null)}>我已保存</button>
        </div>
      )}
      {hooks.map((h) => (
        <div key={h.id} className="card">
          <div className="panel-head">
            <h3 className="mono">{h.url}</h3>
            <span className="muted">{h.events.length === 0 ? '全部事件' : h.events.join(', ')}</span>
            <div className="spacer" />
            <button className="ghost" onClick={() => h.id && showDeliveries(h.id)}>{expanded === h.id ? '收起日志' : '投递日志'}</button>
            {canManage && <button className="ghost" onClick={() => h.id && remove(h.id)}>删除</button>}
          </div>
          {expanded === h.id && (
            <table className="table">
              <thead><tr><th>时间</th><th>事件</th><th>状态</th><th>HTTP</th><th>尝试</th><th>错误</th></tr></thead>
              <tbody>
                {(deliveries[h.id ?? ''] ?? []).map((d) => (
                  <tr key={String(d.id)}>
                    <td className="muted nowrap" title={timeTitle(d.delivered_at ?? d.created_at)}>{fmtTime(d.delivered_at ?? d.created_at)}</td>
                    <td><span className="tag">{String(d.event)}</span></td>
                    <td><span className={`tag ${d.status === 'success' ? 'ok' : d.status === 'dead' ? 'bad' : ''}`}>{String(d.status)}</span></td>
                    <td className="muted">{String(d.http_code ?? '')}</td>
                    <td className="muted">{String(d.attempts)}</td>
                    <td className="muted">{String(d.error ?? '')}</td>
                  </tr>
                ))}
                {(deliveries[h.id ?? ''] ?? []).length === 0 && <tr><td colSpan={6} className="muted center">暂无投递记录</td></tr>}
              </tbody>
            </table>
          )}
        </div>
      ))}
      {hooks.length === 0 && <p className="muted">还没有订阅。</p>}
      {showNew && <WebhookCreator orgSlug={orgSlug} onClose={() => setShowNew(false)}
        onCreated={(w) => { setShowNew(false); setOnce(w); load(); }} />}
    </>
  );
}

function WebhookCreator({ orgSlug, onClose, onCreated }: { orgSlug: string; onClose: () => void; onCreated: (w: Webhook) => void }) {
  const [url, setUrl] = useState('');
  const [secret, setSecret] = useState('');
  const [events, setEvents] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true); setErr('');
    try {
      const d = await api.post<Webhook>(`/api/v1/orgs/${orgSlug}/webhooks`, {
        url,
        secret: secret || undefined,
        events: events.split(',').map((x) => x.trim()).filter(Boolean),
      });
      onCreated(d);
    } catch (e2) { setErr(e2 instanceof ApiError ? e2.message : String(e2)); }
    finally { setBusy(false); }
  }

  return (
    <Modal title="添加 Webhook" onClose={onClose} busy={busy}>
      <ValidatedForm className="drawer-inner" onClick={(e) => e.stopPropagation()} onSubmit={save}>
        <h3>添加 Webhook 订阅</h3>
        <label>接收 URL<input value={url} onChange={(e) => setUrl(e.target.value)} required placeholder="https://hooks.example.com/taboo" /></label>
        <label>签名密钥（留空自动生成）<input value={secret} onChange={(e) => setSecret(e.target.value)} /></label>
        <label>事件过滤（逗号分隔；留空 = 全部）<input value={events} placeholder="secret.created, secret.deleted" /></label>
        {err && <div className="error">{err}</div>}
        <div className="drawer-actions">
          <button type="button" className="ghost" disabled={busy} onClick={onClose}>取消</button>
          <button type="submit" disabled={busy}>{busy ? '创建中…' : '创建'}</button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

// ---------- OIDC SSO（#13） ----------

function SsoTab({ orgSlug, canManage }: { orgSlug: string; canManage: boolean }) {
  const [providers, setProviders] = useState<OIDCProvider[]>([]);
  const [showNew, setShowNew] = useState(false);
  const [error, setError] = useState('');
  const [security, setSecurity] = useState<OIDCProvider | null>(null);
  const [removing, setRemoving] = useState<OIDCProvider | null>(null);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);

  const load = useCallback(() => {
    setLoading(true); setError('');
    return api.get<{ providers: OIDCProvider[] }>(`/api/v1/orgs/${orgSlug}/oidc`)
      .then((d) => setProviders(d.providers))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)))
      .finally(() => setLoading(false));
  }, [orgSlug]);
  useEffect(() => { load(); }, [load]);

  async function remove() {
    if (!removing?.id || busy) return;
    setBusy(true); setError('');
    try { await api.del(`/api/v1/orgs/${orgSlug}/oidc/${removing.id}`); setRemoving(null); await load(); }
    catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
    finally { setBusy(false); }
  }

  async function toggle(p: OIDCProvider, field: 'public_login' | 'sync_role', value: boolean) {
    if (!p.id) return;
    try {
      await api.patch(`/api/v1/orgs/${orgSlug}/oidc/${p.id}`, { [field]: value });
      load();
    } catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
  }

  return (
    <>
      <div className="panel-head">
        <h3>身份连接（OpenID Connect）</h3>
        <div className="spacer" />
        {canManage && <button onClick={() => setShowNew(true)}>＋ 添加身份连接</button>}
      </div>
      <p className="muted">
        连接标准身份提供方；组和角色字段均为可选映射，只匹配明确登记的完整值。
        外部身份不会按邮箱合并本地账号；本地管理员明确设置的角色不被登录同步覆盖。
      </p>
      {error && <div className="error" role="alert">{error} <button className="ghost" onClick={load}>重试</button></div>}
      <div className="table-scroll" tabIndex={0} aria-label="身份连接列表"><table className="table">
        <thead><tr><th>名称</th><th>Issuer</th><th>默认角色</th><th>登录页</th><th>同步角色</th><th /></tr></thead>
        <tbody>
          {providers.map((p) => (
            <tr key={p.id}>
              <td>{p.name}</td>
              <td className="mono muted">{p.issuer}</td>
              <td><span className="tag">{p.default_role}</span>{Object.entries(p.role_map ?? {}).map(([g, r]) => <span key={g} className="tag">{g}→{r}</span>)}</td>
              <td>{p.public_login ? '是' : '否'}</td>
              <td>{p.sync_role ? '是' : '否'}</td>
              <td className="actions">
                {canManage && (
                  <button className="ghost" onClick={() => p.id && toggle(p, 'public_login', !p.public_login)}>
                    {p.public_login ? '从登录页隐藏' : '显示在登录页'}
                  </button>
                )}
                <a className="btn ghost" href={p.login_url ?? '#'}>登录链接</a>
                {canManage && <><button className="ghost" onClick={() => setSecurity(p)}>会话策略</button><button className="ghost" onClick={() => setRemoving(p)}>删除</button></>}
              </td>
            </tr>
          ))}
          {loading && <tr><td colSpan={6} role="status">加载中…</td></tr>}
          {!loading && !error && providers.length === 0 && <tr><td colSpan={6} className="muted center">尚未接入外部用户源</td></tr>}
        </tbody>
      </table></div>
      {security?.id && <ConnectionSecurity orgSlug={orgSlug} id={security.id} onClose={() => setSecurity(null)} />}
      {removing && <Modal title="删除身份连接" onClose={() => setRemoving(null)} busy={busy}>
        <p>删除「{removing.name}」将终止该连接的登录和浏览器会话；组织、成员及秘密仍保留。</p>
        {error && <p className="error" role="alert">{error}</p>}
        <div className="drawer-actions"><button className="ghost" disabled={busy} onClick={() => setRemoving(null)}>取消</button>
          <button disabled={busy} onClick={remove}>{busy ? '删除中…' : '删除连接'}</button></div>
      </Modal>}
      {showNew && <OidcCreator orgSlug={orgSlug} onClose={() => setShowNew(false)} onCreated={() => { setShowNew(false); load(); }} />}
    </>
  );
}

function OidcCreator({ orgSlug, onClose, onCreated }: { orgSlug: string; onClose: () => void; onCreated: () => void }) {
  const [form, setForm] = useState({
    name: 'OpenID Connect',
    issuer: '',
    client_id: '',
    client_secret: '',
    role_claim: 'groups',
    default_role: 'viewer',
    public_login: true,
    autocreate: false,
    sync_role: true,
  });
  const [roleMapText, setRoleMapText] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const set = (k: string) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setForm({ ...form, [k]: e.target.value });

  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true); setErr('');
    const role_map: Record<string, string> = {};
    for (const line of roleMapText.split('\n')) {
      const i = line.indexOf('=');
      if (i > 0) role_map[line.slice(0, i).trim()] = line.slice(i + 1).trim();
    }
    try {
      await api.post(`/api/v1/orgs/${orgSlug}/oidc`, { ...form, role_map });
      onCreated();
    } catch (e2) { setErr(e2 instanceof ApiError ? e2.message : String(e2)); }
    finally { setBusy(false); }
  }

  return (
    <Modal title="添加身份连接" onClose={onClose} busy={busy}>
      <ValidatedForm className="identity-form" onSubmit={save}>
        <p className="muted">
          登记支持授权码、S256、RS256 和 Client Secret Basic 的客户端，并登记下方精确回调地址。
          会话最长 15 分钟，不超过登录凭据有效期；上游停用可能在会话到期后生效。
        </p>
        <label>名称<input value={form.name} onChange={set('name')} required placeholder="公司登录" /></label>
        <label>Issuer URL<input className="mono" value={form.issuer} onChange={set('issuer')} required
          placeholder="https://idp.example.org" /></label>
        <label>Client ID<input value={form.client_id} onChange={set('client_id')} required /></label>
        <label>Client Secret<input type="password" value={form.client_secret} onChange={set('client_secret')} required /></label>
        <label>角色 Claim<input value={form.role_claim} onChange={set('role_claim')} placeholder="可选，例如 groups" /></label>
        <label>默认角色
          <select value={form.default_role} onChange={set('default_role')}>
            {['viewer', 'developer', 'admin'].map((r) => <option key={r} value={r}>{r}</option>)}
          </select>
        </label>
        <label>角色映射（每行 claim值=角色，如 admins=admin）
          <textarea className="mono resize-none" rows={3} value={roleMapText} onChange={(e) => setRoleMapText(e.target.value)} />
        </label>
        <label className="check"><input type="checkbox" checked={form.public_login} onChange={(e) => setForm({ ...form, public_login: e.target.checked })} /> 在登录页显示此连接</label>
        <label className="check"><input type="checkbox" checked={form.autocreate} onChange={(e) => setForm({ ...form, autocreate: e.target.checked })} /> 允许此提供方的任意已认证用户首次登录时加入本组织</label>
        <label className="check"><input type="checkbox" checked={form.sync_role} onChange={(e) => setForm({ ...form, sync_role: e.target.checked })} /> 同步由此连接授予且未经本地修改的角色</label>
        <p className="muted">回调地址：<code>{typeof window !== 'undefined' ? window.location.origin : '{部署域名}'}/api/v1/auth/oidc/{orgSlug}/callback</code></p>
        {err && <div className="error" role="alert">{err}</div>}
        <div className="drawer-actions">
          <button type="button" className="ghost" disabled={busy} onClick={onClose}>取消</button>
          <button type="submit" disabled={busy}>{busy ? '接入中…' : '接入'}</button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

type MemberRow = {
  user_id: string;
  email: string;
  name: string;
  role: string;
  restricted: boolean;
  grants: { project_id: string; project_slug: string; project_name: string; role: string }[];
};

function MembersTab({ orgSlug }: { orgSlug: string }) {
  const [actionMember,setActionMember]=useState<MemberRow|null>(null);
  const [members, setMembers] = useState<MemberRow[]>([]);
  const [projects, setProjects] = useState<{ id: string; slug: string; name: string }[]>([]);
  const [error, setError] = useState('');
  const [inviteUrl, setInviteUrl] = useState('');
  const [email, setEmail] = useState('');
  const [role, setRole] = useState('developer');

  const load = useCallback(() => {
    Promise.all([
      api.get<{ members: MemberRow[] }>(`/api/v1/orgs/${orgSlug}/members`),
      api.get<{ projects: { id: string; slug: string; name: string }[] }>(`/api/v1/orgs/${orgSlug}/projects`),
    ]).then(([m, p]) => {
      setMembers(m.members ?? []);
      setProjects(p.projects ?? []);
    }).catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  }, [orgSlug]);
  useEffect(load, [load]);

  async function invite(e: React.FormEvent) {
    e.preventDefault();
    setError('');
    try {
      const d = await api.post<{ url: string }>(`/api/v1/orgs/${orgSlug}/invites`, { email, role });
      setInviteUrl(d.url);
      setEmail('');
      load();
    } catch (err) { setError(err instanceof ApiError ? err.message : String(err)); }
  }

  async function changeRole(userId: string, next: string) {
    try {
      await api.patch(`/api/v1/orgs/${orgSlug}/members/${userId}`, { role: next });
      load();
    } catch (err) { setError(err instanceof ApiError ? err.message : String(err)); }
  }

  async function toggleRestricted(m: MemberRow) {
    try {
      await api.patch(`/api/v1/orgs/${orgSlug}/members/${m.user_id}`, { restricted: !m.restricted });
      load();
    } catch (err) { setError(err instanceof ApiError ? err.message : String(err)); }
  }

  async function setGrant(m: MemberRow, projectId: string, grantRole: string) {
    const rest = (m.grants ?? []).filter((g) => g.project_id !== projectId);
    const grants = grantRole === '' ? rest : [...rest, { project_id: projectId, role: grantRole }];
    try {
      await api.put(`/api/v1/orgs/${orgSlug}/members/${m.user_id}/grants`, { grants });
      load();
    } catch (err) { setError(err instanceof ApiError ? err.message : String(err)); }
  }

  async function remove(m: MemberRow) {
    if (!window.confirm(`移除成员 ${m.email}？`)) return;
    try {
      await api.del(`/api/v1/orgs/${orgSlug}/members/${m.user_id}`);
      load();
    } catch (err) { setError(err instanceof ApiError ? err.message : String(err)); }
  }

  return (
    <div>
      <p className="muted">admin 可管配置；仅 owner 可邀请、改角色、移除成员。restricted 成员只能访问授权矩阵中的项目。</p>
      <ValidatedForm className="inline" onSubmit={invite}>
        <input aria-label="邀请邮箱" type="email" required placeholder="邀请邮箱" value={email} onChange={(e) => setEmail(e.target.value)} />
        <select aria-label="邀请角色" value={role} onChange={(e) => setRole(e.target.value)}>
          {['viewer', 'developer', 'admin', 'owner'].map((r) => <option key={r} value={r}>{r}</option>)}
        </select>
        <button type="submit">生成邀请链接</button>
      </ValidatedForm>
      {inviteUrl && (
        <p className="card once-secret">邀请链接（仅此一次）：<input readOnly value={inviteUrl} onFocus={(e) => e.target.select()} /></p>
      )}
      {error && <div className="error">{error}</div>}
      <table className="table">
        <thead>
          <tr>
            <th>成员</th><th>角色</th><th>仅授权项目</th>
            {projects.map((p) => <th key={p.id}>{p.slug}</th>)}
            <th />
          </tr>
        </thead>
        <tbody>
          {members.map((m) => (
            <tr key={m.user_id}>
              <td>{m.name || m.email}<div className="muted">{m.email}</div></td>
              <td>
                <select aria-label={`${m.name || m.email} 的组织角色`} value={m.role} onChange={(e) => changeRole(m.user_id, e.target.value)}>
                  {['viewer', 'developer', 'admin', 'owner'].map((r) => <option key={r} value={r}>{r}</option>)}
                </select>
              </td>
              <td><input aria-label={`${m.name || m.email} 仅授权项目`} type="checkbox" checked={m.restricted} onChange={() => toggleRestricted(m)} disabled={m.role === 'owner' || m.role === 'admin'} /></td>
              {projects.map((p) => {
                const g = (m.grants ?? []).find((x) => x.project_id === p.id);
                return (
                  <td key={p.id}>
                    <select aria-label={`${m.name || m.email} 在 ${p.name} 的角色`} value={g?.role ?? ''} onChange={(e) => setGrant(m, p.id, e.target.value)} disabled={m.role === 'owner' || m.role === 'admin'}>
                      <option value="">组织角色</option>
                      {['viewer', 'developer', 'admin'].map((r) => <option key={r} value={r}>{r}</option>)}
                    </select>
                  </td>
                );
              })}
              <td><button className="ghost" onClick={() => setActionMember(m)}>操作授权</button><button className="ghost" onClick={() => remove(m)}>移除</button></td>
            </tr>
          ))}
        </tbody>
      </table>
      {actionMember && <GrantEditor orgSlug={orgSlug} path={`/api/v1/orgs/${orgSlug}/members/${actionMember.user_id}/actions`} onClose={() => setActionMember(null)} />}
    </div>
  );
}

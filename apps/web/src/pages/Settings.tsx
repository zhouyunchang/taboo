import { useCallback, useEffect, useState } from 'react';
import { api, ApiError } from '../api';
import type { SyncTarget, SyncRun, Webhook, OIDCProvider } from '../api';

// 组织设置（M5 #11/#12/#13）：Secret Sync / Webhooks / OIDC SSO
export default function SettingsPanel({ orgSlug, role }: { orgSlug: string; role: string }) {
  const [sub, setSub] = useState<'sync' | 'webhooks' | 'sso'>('sync');
  const canManage = role === 'owner';

  return (
    <div className="panel">
      <div className="panel-head">
        <h2>组织设置</h2>
        <div className="spacer" />
        <button className={`tab ${sub === 'sync' ? 'active' : ''}`} onClick={() => setSub('sync')}>Secret Sync</button>
        <button className={`tab ${sub === 'webhooks' ? 'active' : ''}`} onClick={() => setSub('webhooks')}>Webhooks</button>
        <button className={`tab ${sub === 'sso' ? 'active' : ''}`} onClick={() => setSub('sso')}>用户源 / SSO</button>
      </div>
      {!canManage && <p className="muted">当前角色为 {role}，仅 owner 可增删配置；以下列表所有成员可见。</p>}
      {sub === 'sync' && <SyncTab orgSlug={orgSlug} canManage={canManage} />}
      {sub === 'webhooks' && <WebhooksTab orgSlug={orgSlug} canManage={canManage} />}
      {sub === 'sso' && <SsoTab orgSlug={orgSlug} canManage={canManage} />}
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
      .catch(() => {});
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
              <td className="muted nowrap">{r.updated_at}</td>
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
    <div className="drawer" onClick={onClose}>
      <form className="drawer-inner" onClick={(e) => e.stopPropagation()} onSubmit={save}>
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
          <textarea className="mono" rows={5} value={configText} onChange={(e) => setConfigText(e.target.value)} required
            placeholder={PLATFORM_HINTS[platform]} />
        </label>
        <label className="row"><input type="checkbox" checked={ping} onChange={(e) => setPing(e.target.checked)} /> 创建前测试平台连通性</label>
        {err && <div className="error">{err}</div>}
        <div className="drawer-actions">
          <button type="button" className="ghost" onClick={onClose}>取消</button>
          <button type="submit" disabled={busy}>{busy ? '创建中…' : '创建'}</button>
        </div>
      </form>
    </div>
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
                    <td className="muted nowrap">{String(d.delivered_at ?? d.created_at ?? '')}</td>
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
    <div className="drawer" onClick={onClose}>
      <form className="drawer-inner" onClick={(e) => e.stopPropagation()} onSubmit={save}>
        <h3>添加 Webhook 订阅</h3>
        <label>接收 URL<input value={url} onChange={(e) => setUrl(e.target.value)} required placeholder="https://hooks.example.com/taboo" /></label>
        <label>签名密钥（留空自动生成）<input value={secret} onChange={(e) => setSecret(e.target.value)} /></label>
        <label>事件过滤（逗号分隔；留空 = 全部）<input value={events} placeholder="secret.created, secret.deleted" /></label>
        {err && <div className="error">{err}</div>}
        <div className="drawer-actions">
          <button type="button" className="ghost" onClick={onClose}>取消</button>
          <button type="submit" disabled={busy}>{busy ? '创建中…' : '创建'}</button>
        </div>
      </form>
    </div>
  );
}

// ---------- OIDC SSO（#13） ----------

function SsoTab({ orgSlug, canManage }: { orgSlug: string; canManage: boolean }) {
  const [providers, setProviders] = useState<OIDCProvider[]>([]);
  const [showNew, setShowNew] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    api.get<{ providers: OIDCProvider[] }>(`/api/v1/orgs/${orgSlug}/oidc`)
      .then((d) => setProviders(d.providers))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  }, [orgSlug]);
  useEffect(() => { load(); }, [load]);

  async function remove(id: string, name: string) {
    if (!window.confirm(`删除 IdP「${name}」？已入组用户保留成员关系。`)) return;
    try { await api.del(`/api/v1/orgs/${orgSlug}/oidc/${id}`); load(); }
    catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
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
        <h3>外部用户源（OIDC Realm）</h3>
        <div className="spacer" />
        {canManage && <button onClick={() => setShowNew(true)}>＋ 接入 Keycloak / OIDC</button>}
      </div>
      <p className="muted">
        对标 Proxmox Realm：把 Keycloak（或 Authentik、Google）当作用户目录。勾选「登录页显示」后，无需输入组织 slug 即可一键跳转。
        组 claim 默认 <code>groups</code>（也可用 <code>realm_access.roles</code>）；每次登录可同步角色。新用户不会被映射成 owner。
      </p>
      {error && <div className="error">{error}</div>}
      <table className="table">
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
                {canManage && <button className="ghost" onClick={() => p.id && remove(p.id, p.name ?? p.id)}>删除</button>}
              </td>
            </tr>
          ))}
          {providers.length === 0 && <tr><td colSpan={6} className="muted center">尚未接入外部用户源</td></tr>}
        </tbody>
      </table>
      {showNew && <OidcCreator orgSlug={orgSlug} onClose={() => setShowNew(false)} onCreated={() => { setShowNew(false); load(); }} />}
    </>
  );
}

function OidcCreator({ orgSlug, onClose, onCreated }: { orgSlug: string; onClose: () => void; onCreated: () => void }) {
  const [form, setForm] = useState({
    name: 'Keycloak',
    issuer: '',
    client_id: '',
    client_secret: '',
    role_claim: 'groups',
    username_claim: 'email',
    default_role: 'viewer',
    public_login: true,
    autocreate: true,
    sync_role: true,
  });
  const [roleMapText, setRoleMapText] = useState('admins=admin\ndevelopers=developer');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const set = (k: string) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setForm({ ...form, [k]: e.target.value });

  async function save(e: React.FormEvent) {
    e.preventDefault();
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
    <div className="drawer" onClick={onClose}>
      <form className="drawer-inner" onClick={(e) => e.stopPropagation()} onSubmit={save}>
        <h3>接入 Keycloak / OIDC</h3>
        <p className="muted">
          Keycloak：创建 Confidential Client，打开 Standard flow，Valid redirect URIs 填下方回调地址。
          在 Client scopes 给 access token / ID token 加上 Group Membership（claim <code>groups</code>）或 realm roles。
        </p>
        <label>名称<input value={form.name} onChange={set('name')} required placeholder="Keycloak" /></label>
        <label>Issuer URL<input className="mono" value={form.issuer} onChange={set('issuer')} required
          placeholder="https://keycloak.example.com/realms/company" /></label>
        <label>Client ID<input value={form.client_id} onChange={set('client_id')} required /></label>
        <label>Client Secret<input type="password" value={form.client_secret} onChange={set('client_secret')} required /></label>
        <label>角色 Claim<input value={form.role_claim} onChange={set('role_claim')} placeholder="groups 或 realm_access.roles" /></label>
        <label>用户名 Claim<input value={form.username_claim} onChange={set('username_claim')} /></label>
        <label>默认角色
          <select value={form.default_role} onChange={set('default_role')}>
            {['viewer', 'developer', 'admin'].map((r) => <option key={r} value={r}>{r}</option>)}
          </select>
        </label>
        <label>角色映射（每行 claim值=角色，如 admins=admin）
          <textarea className="mono" rows={3} value={roleMapText} onChange={(e) => setRoleMapText(e.target.value)} />
        </label>
        <label className="check"><input type="checkbox" checked={form.public_login} onChange={(e) => setForm({ ...form, public_login: e.target.checked })} /> 显示在登录页（实例级 Realm）</label>
        <label className="check"><input type="checkbox" checked={form.autocreate} onChange={(e) => setForm({ ...form, autocreate: e.target.checked })} /> 首次登录自动创建用户并入组</label>
        <label className="check"><input type="checkbox" checked={form.sync_role} onChange={(e) => setForm({ ...form, sync_role: e.target.checked })} /> 每次登录按组同步角色</label>
        <p className="muted">回调地址：<code>{typeof window !== 'undefined' ? window.location.origin : '{部署域名}'}/api/v1/auth/oidc/{orgSlug}/callback</code></p>
        {err && <div className="error">{err}</div>}
        <div className="drawer-actions">
          <button type="button" className="ghost" onClick={onClose}>取消</button>
          <button type="submit" disabled={busy}>{busy ? '接入中…' : '接入'}</button>
        </div>
      </form>
    </div>
  );
}

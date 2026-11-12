import { useCallback, useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { api, ApiError, hasBrowserSession } from '../api';
import { GrantEditor, ConnectionSecurity, OrganizationSecurity, WorkloadGovernance } from '../components/Governance';
import Phase6 from '../components/Phase6';
import Modal from '../components/Modal';
import ValidatedForm from '../components/ValidatedForm';
import type { SyncTarget, SyncRun, Webhook, OIDCProvider } from '../api';
import { fmtTime, timeTitle } from '../time';
import Icon from '../design/Icon';
import type { IconName } from '../design/Icon';
import { Button, Pill, Field, Check, PageHead, Section, EmptyState, Notice, Skeleton, TableWrap } from '../design/primitives';

// 组织设置（M5 #11/#12/#13）：稳定子导航 + 每个子节一屏头、分节、表格与空态。
// 所有 API 调用、权限门与无障碍名称保持不变（Playwright 依赖这些名称）。

type Sub = 'members' | 'sso' | 'security' | 'workloads' | 'approvals' | 'sync' | 'webhooks';

const NAV: Array<{ id: Sub; label: string; icon: IconName; owner: boolean; group: string }> = [
  { id: 'members', label: '成员与角色', icon: 'users', owner: true, group: '身份' },
  { id: 'sso', label: '用户源 / SSO', icon: 'shield', owner: false, group: '身份' },
  { id: 'security', label: '登录策略', icon: 'lock', owner: true, group: '身份' },
  { id: 'workloads', label: 'OAuth 工作负载', icon: 'robot', owner: true, group: '治理' },
  { id: 'approvals', label: '审批与访问', icon: 'audit', owner: false, group: '治理' },
  { id: 'sync', label: 'Secret Sync', icon: 'db', owner: false, group: '出口' },
  { id: 'webhooks', label: 'Webhooks', icon: 'bell', owner: false, group: '出口' },
];

export default function SettingsPanel({ orgSlug, role, personal = false, initialSub }: { orgSlug: string; role: string; personal?: boolean; initialSub?: string }) {
  const [sub, setSub] = useState<Sub>((initialSub as Sub) ?? 'members');
  const canManage = role === 'owner' || (!hasBrowserSession() && role === 'admin');
  const canMembers = role === 'owner' && !personal;
  const canWorkloads = role === 'owner';

  const sections = NAV.filter((n) => {
    if (personal && n.id !== 'sync' && n.id !== 'approvals') return false; // 与旧门一致：个人设置只有同步与审批
    if (n.id === 'sso' && personal) return false;
    if (n.owner && !canMembers) return false;
    return true;
  });
  const active = sections.find((n) => n.id === sub) ?? sections[0];
  const activeSub = active?.id ?? 'sync';
  const current = sections.find((n) => n.id === activeSub) ?? sections[0];

  function select(id: Sub) {
    setSub(id);
    if (typeof document !== 'undefined') {
      document.getElementById('settings-panel')?.scrollIntoView({ block: 'start' });
    }
  }

  return (
    <div className="settings">
      <PageHead
        title={personal ? '个人设置' : '组织设置'}
        meta={[
          <Pill key="role" tone={role === 'owner' ? 'accent' : 'muted'} icon="seal">{personal ? '个人身份' : `角色 ${role}`}</Pill>,
          <span key="slug">组织 <span className="mono">{orgSlug}</span></span>,
        ]}
      />

      {!canManage && <Notice tone="muted">当前角色为 {role}，外部登录由 owner 管理身份连接、授权和秘密同步。</Notice>}

      <div className="settings-body">
        <nav className="settings-nav" aria-label="设置分区">
          {['身份', '治理', '出口'].map((group) => {
            const items = sections.filter((n) => n.group === group);
            if (!items.length) return null;
            return (
              <div key={group} className="rail-group">
                <div className="rail-label">{group}</div>
                {items.map((n) => (
                  <button key={n.id} type="button" className={`rail-link ${n.id === activeSub ? 'active' : ''}`}
                    aria-current={n.id === activeSub ? 'page' : undefined} onClick={() => select(n.id)}>
                    <Icon name={n.icon} />
                    <span>{n.label}</span>
                  </button>
                ))}
              </div>
            );
          })}
          <p className="settings-note">变更立即生效并记入审计日志；删除类操作会再次确认。</p>
        </nav>

        <section id="settings-panel" className="settings-panel panel" aria-label={current?.label}>
          <PageHead title={current?.label} meta={PAGE_META[activeSub]} />

          {activeSub === 'sync' && <SyncTab orgSlug={orgSlug} canManage={canManage} />}
          {activeSub === 'webhooks' && <WebhooksTab orgSlug={orgSlug} canManage={canManage} />}
          {activeSub === 'sso' && !personal && <SsoTab orgSlug={orgSlug} canManage={canManage} />}
          {activeSub === 'approvals' && <Phase6 orgSlug={orgSlug} canApprove={role === 'owner' || role === 'admin'} showScim={!personal} />}
          {activeSub === 'security' && canMembers && <OrganizationSecurity orgSlug={orgSlug} />}
          {activeSub === 'workloads' && canWorkloads && <WorkloadGovernance orgSlug={orgSlug} />}
          {activeSub === 'members' && canMembers && <MembersTab orgSlug={orgSlug} />}
        </section>
      </div>
    </div>
  );
}

const PAGE_META: Record<Sub, string> = {
  members: '谁属于这个组织，能看哪些项目',
  sso: '外部身份提供方与默认角色',
  security: '登录方式与本地凭据边界',
  workloads: 'OAuth 资源信任，以及机器工作负载的绑定与授权',
  approvals: '变更审批、限时访问与 SCIM 令牌',
  sync: '把密钥单向推送到外部平台',
  webhooks: '把变更事件广播给订阅方',
};

// ---------- 通用小件 ----------

/** 字段：标签 + 控件 + 说明。名称只用短标签，说明单独成行，保证原生校验提示读起来是「请检查『Issuer URL』」。 */
function LabeledField({ label, hint, children, className = '' }: {
  label: string; hint?: ReactNode; children: ReactNode; className?: string;
}) {
  return (
    <label className={`field ${className}`}>
      <span className="field-label">{label}</span>
      {children}
      {hint && <span className="field-hint">{hint}</span>}
    </label>
  );
}

function CallbackUrl({ orgSlug }: { orgSlug: string }) {
  const path = `/api/v1/auth/oidc/${orgSlug}/callback`;
  const origin = typeof window !== 'undefined' ? window.location.origin : '{部署域名}';
  return <code>{`${origin}${path}`}</code>;
}

function StatusPill({ status }: { status: string }) {
  const tone = status === 'success' ? 'ok' : status === 'dead' ? 'bad' : status === 'failed' ? 'warn' : 'muted';
  return <Pill tone={tone}>{status}</Pill>;
}

function PlatformPill({ platform }: { platform: string }) {
  return <span className="tag">{platform}</span>;
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
  const [notice, setNotice] = useState('');
  const [loading, setLoading] = useState(true);

  const load = useCallback(() => {
    api.get<{ targets: SyncTarget[] }>(`/api/v1/orgs/${orgSlug}/sync/targets`)
      .then((d) => setTargets(d.targets))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)))
      .finally(() => setLoading(false));
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
      setNotice(`已入队 ${d.enqueued} 个密钥，稍后刷新查看同步记录`);
      setTimeout(load, 3000);
    } catch (e) { setError(e instanceof ApiError ? e.message : String(e)); }
  }

  return (
    <>
      {error && <Notice tone="danger">{error}</Notice>}
      {notice && <Notice tone="muted">{notice}</Notice>}

      <Section
        title="同步目标"
        hint="密钥创建/更新/回滚后自动单向推送到目标平台；失败自动退避重试。同步记录只存内容指纹，不记明文。"
        actions={canManage && <Button variant="primary" icon="plus" aria-label="＋ 添加目标" onClick={() => setShowNew(true)}>添加目标</Button>}
      >
        {!loading && !targets.length && (
          <EmptyState icon="db" title="还没有同步目标"
            action={canManage ? <Button variant="default" icon="plus" onClick={() => setShowNew(true)}>添加第一个同步目标</Button> : undefined}>
            接入一个平台后，这个组织里的密钥会在写入或回滚时自动推送到该平台，失败会自动退避重试。
          </EmptyState>
        )}
        {!!targets.length && <TableWrap label="同步目标">
          <table className="table compact">
            <thead><tr><th>名称</th><th>平台</th><th>范围</th><th>状态</th><th><span className="sr-only">操作</span></th></tr></thead>
            <tbody>
              {targets.map((t) => (
                <tr key={t.id}>
                  <td className="mono">{t.name}</td>
                  <td><PlatformPill platform={t.platform ?? ''} /></td>
                  <td className="muted">{t.project_id ? `项目 ${t.project_id.slice(0, 8)}…` : '全部项目'}</td>
                  <td><Pill tone={t.enabled ? 'ok' : 'muted'}>{t.enabled ? '启用' : '停用'}</Pill></td>
                  <td className="actions">
                    <span className="row-actions">
                      {canManage && <Button variant="quiet" className="btn-sm" onClick={() => t.id && retry(t.id)}>全量重推</Button>}
                      {canManage && <Button variant="danger" className="btn-sm" onClick={() => t.id && remove(t.id)}>删除</Button>}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableWrap>}
        {loading && <Skeleton rows={2} />}
      </Section>

      <Section title="同步记录" hint="最近 50 条推送：状态 success 为已送达，failed 等待重试，dead 已停止。">
        <TableWrap label="同步记录">
          <table className="table compact">
            <thead><tr><th>时间</th><th>Key</th><th>触发</th><th>状态</th><th>尝试</th><th>错误</th></tr></thead>
            <tbody>
              {runs.map((r) => (
                <tr key={r.id}>
                  <td className="muted nowrap" title={timeTitle(r.updated_at)}>{fmtTime(r.updated_at)}</td>
                  <td className="mono">{r.secret_key}</td>
                  <td className="muted">{r.trigger_type}</td>
                  <td><StatusPill status={r.status ?? ''} /></td>
                  <td className="muted">{r.attempts}</td>
                  <td className="muted cell-sub">{r.error}</td>
                </tr>
              ))}
              {!runs.length && <tr><td colSpan={6} className="muted center">暂无同步记录</td></tr>}
            </tbody>
          </table>
        </TableWrap>
      </Section>

      {showNew && <SyncTargetCreator orgSlug={orgSlug} onClose={() => setShowNew(false)} onCreated={() => { setShowNew(false); load(); }} />}
    </>
  );
}

function SyncTargetCreator({ orgSlug, onClose, onCreated }: { orgSlug: string; onClose: () => void; onCreated: () => void }) {
  const [name, setName] = useState('');
  const [platform, setPlatform] = useState('github');
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
        name, platform, config,
      });
      onCreated();
    } catch (e2) { setErr(e2 instanceof ApiError ? e2.message : String(e2)); }
    finally { setBusy(false); }
  }

  return (
    <Modal title="添加同步目标" onClose={onClose} busy={busy}>
      <ValidatedForm className="form-stack" onClick={(e) => e.stopPropagation()} onSubmit={save}>
        <LabeledField label="名称" hint="在同步记录与列表里标识这个目标。">
          <input value={name} onChange={(e) => setName(e.target.value)} required placeholder="prod-github" />
        </LabeledField>
        <LabeledField label="平台" hint="不同平台的配置键不同，详见下方配置说明。">
          <select value={platform} onChange={(e) => setPlatform(e.target.value)}>
            <option value="github">GitHub Actions Secrets</option>
            <option value="vercel">Vercel</option>
            <option value="cloudflare">Cloudflare Workers</option>
          </select>
        </LabeledField>
        <LabeledField label="配置" hint={<>每行 key=value。平台令牌经 Master Key 加密落库。<br />{PLATFORM_HINTS[platform]}</>}>
          <textarea className="mono resize-none" rows={5} value={configText} onChange={(e) => setConfigText(e.target.value)} required
            placeholder={PLATFORM_HINTS[platform]} />
        </LabeledField>
        <Check label="创建前测试平台连通性" hint="测试失败不会落库，避免写入一个永远推不出去的目标。"
          checked={ping} onChange={(e) => setPing(e.target.checked)} />
        {err && <Notice tone="danger">{err}</Notice>}
        <div className="form-actions">
          <Button variant="quiet" disabled={busy} onClick={onClose}>取消</Button>
          <Button type="submit" variant="primary" loading={busy}>{busy ? '创建中…' : '创建'}</Button>
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
  const [loading, setLoading] = useState(true);

  const load = useCallback(() => {
    api.get<{ webhooks: Webhook[] }>(`/api/v1/orgs/${orgSlug}/webhooks`)
      .then((d) => setHooks(d.webhooks))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)))
      .finally(() => setLoading(false));
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
      <Section
        title="Webhook 订阅"
        hint={<>密钥创建/更新/回滚/删除时向订阅 URL 广播事件（HMAC-SHA256 签名，负载不含明文）。
          接收方用签名密钥对 <code>{'<t>.<body>'}</code> 验签，并校验时间戳窗口防重放。</>}
        actions={canManage && <Button variant="primary" icon="plus" aria-label="＋ 添加订阅" onClick={() => setShowNew(true)}>添加订阅</Button>}
      >
        {error && <Notice tone="danger">{error}</Notice>}
        {once && (
          <div className="card once-secret">
            <h3>订阅已创建 — 签名密钥仅此一次展示，请立即保存</h3>
            <p>离开本页后无法再次查看；丢失只能重新创建订阅。</p>
            <Field label="签名密钥">
              <input className="mono" readOnly value={once.secret} onFocus={(e) => e.target.select()} />
            </Field>
            <div className="form-actions">
              <Button variant="quiet" onClick={() => setOnce(null)}>我已保存</Button>
            </div>
          </div>
        )}

        {loading && !hooks.length && <Skeleton rows={3} />}
        {!loading && !hooks.length && (
          <EmptyState icon="bell" title="还没有订阅"
            action={canManage ? <Button variant="default" icon="plus" onClick={() => setShowNew(true)}>添加第一个订阅</Button> : undefined}>
            订阅一个接收 URL，密钥每次变更都会带签名推送到那里；投递失败按指数退避重试，你可以在投递日志里逐条追查。
          </EmptyState>
        )}

        {hooks.map((h) => (
          <div key={h.id} className="hook">
            <div className="hook-head">
              <div className="hook-title">
                <span className="mono hook-url">{h.url}</span>
                <span className="muted cell-sub">{h.events.length === 0 ? '全部事件' : h.events.join(', ')}</span>
              </div>
              <div className="section-actions">
                {!h.active && <Pill tone="muted">已停用</Pill>}
                <Button variant="quiet" className="btn-sm" icon={expanded === h.id ? 'chevron-down' : 'chevron-right'}
                  onClick={() => h.id && showDeliveries(h.id)}>{expanded === h.id ? '收起日志' : '投递日志'}</Button>
                {canManage && <Button variant="danger" className="btn-sm" onClick={() => h.id && remove(h.id)}>删除</Button>}
              </div>
            </div>
            {expanded === h.id && (
              <TableWrap label="投递日志">
                <table className="table compact">
                  <thead><tr><th>时间</th><th>事件</th><th>状态</th><th>HTTP</th><th>尝试</th><th>错误</th></tr></thead>
                  <tbody>
                    {(deliveries[h.id ?? ''] ?? []).map((d) => (
                      <tr key={String(d.id)}>
                        <td className="muted nowrap" title={timeTitle(d.delivered_at ?? d.created_at)}>{fmtTime(d.delivered_at ?? d.created_at)}</td>
                        <td><span className="tag">{String(d.event)}</span></td>
                        <td><StatusPill status={String(d.status)} /></td>
                        <td className="muted mono">{String(d.http_code ?? '')}</td>
                        <td className="muted">{String(d.attempts)}</td>
                        <td className="muted cell-sub">{String(d.error ?? '')}</td>
                      </tr>
                    ))}
                    {(deliveries[h.id ?? ''] ?? []).length === 0 && <tr><td colSpan={6} className="muted center">暂无投递记录</td></tr>}
                  </tbody>
                </table>
              </TableWrap>
            )}
          </div>
        ))}
      </Section>
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
      <ValidatedForm className="form-stack" onClick={(e) => e.stopPropagation()} onSubmit={save}>
        <LabeledField label="接收 URL" hint="必须能被本服务直接访问，返回 2xx 才算投递成功。">
          <input className="mono" value={url} onChange={(e) => setUrl(e.target.value)} required placeholder="https://hooks.example.com/taboo" />
        </LabeledField>
        <LabeledField label="签名密钥" hint="留空则服务端自动生成一次，并仅在创建后展示一次。">
          <input value={secret} onChange={(e) => setSecret(e.target.value)} />
        </LabeledField>
        <LabeledField label="事件过滤" hint="逗号分隔；留空 = 全部事件。">
          <input className="mono" value={events} onChange={(e) => setEvents(e.target.value)} placeholder="secret.created, secret.rotation_due" />
        </LabeledField>
        {err && <Notice tone="danger">{err}</Notice>}
        <div className="form-actions">
          <Button variant="quiet" disabled={busy} onClick={onClose}>取消</Button>
          <Button type="submit" variant="primary" loading={busy}>{busy ? '创建中…' : '创建'}</Button>
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
      {error && (
        <p className="notice notice-danger" role="alert">
          {error}{' '}
          <Button variant="quiet" className="btn-sm" onClick={load}>重试</Button>
        </p>
      )}

      <Section
        title="已接入的身份提供方"
        hint="连接标准身份提供方；组和角色字段均为可选映射，只匹配明确登记的完整值。外部身份不会按邮箱合并本地账号；本地管理员明确设置的角色不被登录同步覆盖。"
        actions={canManage && <Button variant="primary" icon="plus" aria-label="＋ 添加身份连接" onClick={() => setShowNew(true)}>添加身份连接</Button>}
      >
        {!loading && !error && providers.length === 0 && (
          <EmptyState icon="shield" title="尚未接入外部用户源"
            action={canManage ? <Button variant="default" icon="plus" onClick={() => setShowNew(true)}>接入第一个身份提供方</Button> : undefined}>
            接入一个 OpenID Connect 提供方后，成员可以用既有身份登录，无需另建本地密码；默认只在登录页显示，自动加入需要显式开启。
          </EmptyState>
        )}
        {!loading && !!providers.length && <TableWrap label="身份连接列表">
          <table className="table compact">
            <thead><tr><th>名称</th><th>Issuer</th><th>默认角色</th><th>登录页</th><th>同步角色</th><th><span className="sr-only">操作</span></th></tr></thead>
            <tbody>
              {providers.map((p) => (
                <tr key={p.id}>
                  <td className="col-who">
                    <div className="settings-member">
                      <span className="avatar chip-initial" aria-hidden="true">{initial(p.name)}</span>
                      <div className="settings-member-main">
                        <span>{p.name}</span>
                        <span className="cell-sub mono">{p.client_id}</span>
                      </div>
                    </div>
                  </td>
                  <td className="mono muted"><span className="trunc">{p.issuer}</span></td>
                  <td>
                    <Pill>{p.default_role}</Pill>
                    {Object.entries(p.role_map ?? {}).map(([g, r]) => (
                      <span key={g} className="tag">{g}→{r}</span>
                    ))}
                  </td>
                  <td>{p.public_login ? <Pill tone="ok">是</Pill> : <Pill tone="muted">否</Pill>}</td>
                  <td>{p.sync_role ? <Pill tone="ok">是</Pill> : <Pill tone="muted">否</Pill>}</td>
                  <td className="actions">
                    <span className="row-actions">
                      {canManage && (
                        <Button variant="quiet" className="btn-sm" aria-pressed={!!p.public_login}
                          onClick={() => p.id && toggle(p, 'public_login', !p.public_login)}>
                          {p.public_login ? '从登录页隐藏' : '显示在登录页'}
                        </Button>
                      )}
                      <a className="btn btn-quiet btn-sm" href={p.login_url ?? '#'}>登录链接</a>
                      {canManage && <>
                        <Button variant="quiet" className="btn-sm" onClick={() => setSecurity(p)}>会话策略</Button>
                        <span className="danger-sep" aria-hidden="true" />
                        <Button variant="danger" className="btn-sm" onClick={() => setRemoving(p)}>删除</Button>
                      </>}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableWrap>}
        {loading && <Skeleton rows={3} />}
      </Section>

      {security?.id && <ConnectionSecurity orgSlug={orgSlug} id={security.id} onClose={() => setSecurity(null)} />}
      {removing && <Modal title="删除身份连接" onClose={() => setRemoving(null)} busy={busy}>
        <p className="notice notice-danger">删除「{removing.name}」将终止该连接的登录和浏览器会话；组织、成员及秘密仍保留。</p>
        {error && <p className="error" role="alert">{error}</p>}
        <div className="form-actions">
          <Button variant="quiet" disabled={busy} onClick={() => setRemoving(null)}>取消</Button>
          <Button variant="danger" disabled={busy} onClick={remove}>{busy ? '删除中…' : '删除连接'}</Button>
        </div>
      </Modal>}
      {showNew && <OidcCreator orgSlug={orgSlug} onClose={() => setShowNew(false)} onCreated={() => { setShowNew(false); load(); }} />}
    </>
  );
}

function initial(value?: string) {
  return (value ?? '?').trim().slice(0, 1).toUpperCase() || '?';
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
      <ValidatedForm className="identity-form" onClick={(e) => e.stopPropagation()} onSubmit={save}>
        <p className="section-hint">
          登记支持授权码、S256、RS256 和 Client Secret Basic 的客户端，并登记下方精确回调地址。
          会话最长 15 分钟，不超过登录凭据有效期；上游停用可能在会话到期后生效。
        </p>

        <fieldset>
          <legend>连接</legend>
          <div className="form-stack">
            <LabeledField label="名称" hint="显示在登录页与身份连接列表。">
              <input value={form.name} onChange={set('name')} required placeholder="公司登录" />
            </LabeledField>
            <LabeledField label="Issuer URL" hint="提供方发现文档的根地址，例如 https://idp.example.org/realms/main。">
              <input className="mono" value={form.issuer} onChange={set('issuer')} required placeholder="https://idp.example.org" />
            </LabeledField>
            <LabeledField label="Client ID" hint="在提供方登记的客户端标识。">
              <input className="mono" value={form.client_id} onChange={set('client_id')} required />
            </LabeledField>
            <LabeledField label="Client Secret" hint="经 Master Key 加密落库，不会回显。">
              <input type="password" value={form.client_secret} onChange={set('client_secret')} required />
            </LabeledField>
          </div>
        </fieldset>

        <fieldset>
          <legend>角色与准入</legend>
          <div className="form-stack">
            <LabeledField label="角色 Claim" hint="可选，例如 groups；支持点分路径。">
              <input value={form.role_claim} onChange={set('role_claim')} placeholder="可选，例如 groups" />
            </LabeledField>
            <LabeledField label="默认角色" hint="映射不命中时使用的组织角色。">
              <select value={form.default_role} onChange={set('default_role')}>
                {['viewer', 'developer', 'admin'].map((r) => <option key={r} value={r}>{r}</option>)}
              </select>
            </LabeledField>
            <LabeledField label="角色映射" hint={<>每行 <code>claim值=角色</code>，如 admins=admin。只匹配 claim 的完整值，不做路径末段推断。</>}>
              <textarea className="mono resize-none" rows={3} value={roleMapText} onChange={(e) => setRoleMapText(e.target.value)} />
            </LabeledField>
            <Check label="在登录页显示此连接" checked={form.public_login}
              onChange={(e) => setForm({ ...form, public_login: e.target.checked })} />
            <Check label="允许此提供方的任意已认证用户首次登录时加入本组织" hint="默认关闭：开启后该提供方任意已认证用户都能加入，不再逐个邀请。"
              checked={form.autocreate} onChange={(e) => setForm({ ...form, autocreate: e.target.checked })} />
            <Check label="同步由此连接授予且未经本地修改的角色" hint="本地管理员显式设置的角色不会被登录同步覆盖。"
              checked={form.sync_role} onChange={(e) => setForm({ ...form, sync_role: e.target.checked })} />
          </div>
        </fieldset>

        <p className="notice">
          <Icon name="link" />
          <span>回调地址（登记到提供方）：<CallbackUrl orgSlug={orgSlug} /></span>
        </p>

        {err && <p className="error" role="alert">{err}</p>}
        <div className="form-actions">
          <Button variant="quiet" disabled={busy} onClick={onClose}>取消</Button>
          <Button type="submit" variant="primary" loading={busy}>{busy ? '接入中…' : '接入'}</Button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

// ---------- 成员与角色 ----------

type MemberRow = {
  user_id: string;
  email: string;
  name: string;
  role: string;
  restricted: boolean;
  grants: { project_id: string; project_slug: string; project_name: string; role: string }[];
};

function MembersTab({ orgSlug }: { orgSlug: string }) {
  const [actionMember, setActionMember] = useState<MemberRow | null>(null);
  const [members, setMembers] = useState<MemberRow[]>([]);
  const [projects, setProjects] = useState<{ id: string; slug: string; name: string }[]>([]);
  const [error, setError] = useState('');
  const [inviteUrl, setInviteUrl] = useState('');
  const [email, setEmail] = useState('');
  const [role, setRole] = useState('developer');
  const [loading, setLoading] = useState(true);

  const load = useCallback(() => {
    Promise.all([
      api.get<{ members: MemberRow[] }>(`/api/v1/orgs/${orgSlug}/members`),
      api.get<{ projects: { id: string; slug: string; name: string }[] }>(`/api/v1/orgs/${orgSlug}/projects`),
    ]).then(([m, p]) => {
      setMembers(m.members ?? []);
      setProjects(p.projects ?? []);
    }).catch((e) => setError(e instanceof ApiError ? e.message : String(e)))
      .finally(() => setLoading(false));
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
    <>
      <Section
        title="邀请成员"
        hint="admin 可管配置；仅 owner 可邀请、改角色、移除成员。restricted 成员只能访问授权矩阵中的项目。"
      >
        {error && <Notice tone="danger">{error}</Notice>}
        <ValidatedForm className="invite-row" onSubmit={invite}>
          <LabeledField label="邀请邮箱" className="invite-email">
            <input type="email" required placeholder="邮箱地址" value={email}
              onChange={(e) => setEmail(e.target.value)} />
          </LabeledField>
          <LabeledField label="初始角色" className="invite-role">
            <select value={role} onChange={(e) => setRole(e.target.value)}>
              {['viewer', 'developer', 'admin', 'owner'].map((r) => <option key={r} value={r}>{r}</option>)}
            </select>
          </LabeledField>
          <Button type="submit">生成邀请链接</Button>
        </ValidatedForm>
        {inviteUrl && (
          <div className="once-secret">
            <h3>邀请链接（仅此一次）</h3>
            <p>复制后通过可信渠道发给对方；离开本页后需要重新生成。</p>
            <input className="mono" readOnly aria-label="邀请链接" value={inviteUrl} onFocus={(e) => e.target.select()} />
          </div>
        )}
      </Section>

      <Section
        title="成员与角色"
        hint="组织角色决定能不能管理配置；「仅授权项目」开启后，成员只能访问授权矩阵里显式授权的项目。"
      >
        {!loading && !!members.length && <TableWrap label="成员与角色">
          <table className="table compact">
            <thead>
              <tr>
                <th>成员</th><th>角色</th><th>仅授权项目</th>
                {projects.map((p) => <th key={p.id} className="mono">{p.slug}</th>)}
                <th><span className="sr-only">操作</span></th>
              </tr>
            </thead>
            <tbody>
              {members.map((m) => (
                <tr key={m.user_id}>
                  <td className="col-who">
                    <div className="settings-member">
                      <span className="avatar chip-initial" aria-hidden="true">{initial(m.name || m.email)}</span>
                      <div className="settings-member-main">
                        <span>{m.name || m.email}</span>
                        <span className="cell-sub">{m.email}</span>
                      </div>
                    </div>
                  </td>
                  <td>
                    <select aria-label={`${m.name || m.email} 的组织角色`} value={m.role} onChange={(e) => changeRole(m.user_id, e.target.value)}>
                      {['viewer', 'developer', 'admin', 'owner'].map((r) => <option key={r} value={r}>{r}</option>)}
                    </select>
                  </td>
                  <td>
                    <input aria-label={`${m.name || m.email} 仅授权项目`} type="checkbox" checked={m.restricted}
                      onChange={() => toggleRestricted(m)} disabled={m.role === 'owner' || m.role === 'admin'} />
                  </td>
                  {projects.map((p) => {
                    const g = (m.grants ?? []).find((x) => x.project_id === p.id);
                    return (
                      <td key={p.id}>
                        <select aria-label={`${m.name || m.email} 在 ${p.name} 的角色`} value={g?.role ?? ''}
                          onChange={(e) => setGrant(m, p.id, e.target.value)} disabled={m.role === 'owner' || m.role === 'admin'}>
                          <option value="">组织角色</option>
                          {['viewer', 'developer', 'admin'].map((r) => <option key={r} value={r}>{r}</option>)}
                        </select>
                      </td>
                    );
                  })}
                  <td className="actions">
                    <span className="row-actions">
                      <Button variant="quiet" className="btn-sm" icon="shield" onClick={() => setActionMember(m)}>操作授权</Button>
                      <span className="danger-sep" aria-hidden="true" />
                      <Button variant="danger" className="btn-sm" onClick={() => remove(m)}>移除</Button>
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableWrap>}
        {loading && <Skeleton rows={3} />}
        {!loading && !members.length && (
          <EmptyState icon="users" title="还没有其他成员">
            用上方的邀请表单生成一条一次性链接；对方打开后即加入这个组织，并按你选的初始角色获得权限。
          </EmptyState>
        )}
      </Section>

      {actionMember && <GrantEditor orgSlug={orgSlug} path={`/api/v1/orgs/${orgSlug}/members/${actionMember.user_id}/actions`} onClose={() => setActionMember(null)} />}
    </>
  );
}

import { useCallback, useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { api, hasBrowserSession } from '../api';
import Modal from './Modal';
import ValidatedForm from './ValidatedForm';
import Icon from '../design/Icon';
import type { IconName } from '../design/Icon';
import { Button, Check, EmptyState, Field, Notice, Pill, Section, Select, Skeleton, TableWrap } from '../design/primitives';

// 治理面：明确授权、登录会话策略、组织登录策略、OAuth 资源信任与工作负载、组织切换。
// 所有 API 调用、权限门与无障碍名称保持不变（Playwright 依赖这些名称）。

export type Grant = {
  project_id: string;
  action: string;
  required_scope?: string;
};

// 顺序即界面顺序：读 → 写 → 明文/出口 → 不可逆。
const actions: Record<string, string> = {
  read: '查看目录',
  write: '写入',
  reveal: '查看明文',
  export: '导出',
  rollback: '回滚',
  delete: '删除',
};
const actionsEnglish: Record<string, string> = {
  read: 'read',
  write: 'write',
  reveal: 'reveal',
  export: 'export',
  rollback: 'rollback',
  delete: 'delete',
};
const actionOrder = ['read', 'write', 'reveal', 'export', 'rollback', 'delete'];

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** 一次性说明行：图标 + 文案，放在表单顶部时比一整段 <p> 更短。 */
function HintLine({ icon, children }: { icon: IconName; children: ReactNode }) {
  return (
    <p className="notice">
      <Icon name={icon} />
      <span>{children}</span>
    </p>
  );
}

/** 破坏性动作要与普通动作隔开：右侧留一条 1px 结界线，再放朱砂按钮。 */
function DangerSeparator() {
  return <span className="danger-sep" aria-hidden="true" />;
}

/** 两段式确认：点一次进入 armed，再点一次才真的执行；armed 状态 6 秒后自动收回。 */
function useArmed() {
  const [armed, setArmed] = useState('');
  useEffect(() => {
    if (!armed) return;
    const timer = window.setTimeout(() => setArmed(''), 6000);
    return () => window.clearTimeout(timer);
  }, [armed]);
  return { armed, setArmed } as const;
}

function ArmedButtons({ armed, busy, armedLabel, label, title, onArm, onConfirm, onCancel }: {
  armed: boolean; busy: boolean; armedLabel: string; label: string; title?: string;
  onArm: () => void; onConfirm: () => void; onCancel: () => void;
}) {
  if (!armed) {
    return <Button variant="danger" className="btn-sm" disabled={busy} title={title} onClick={onArm}>{label}</Button>;
  }
  return (
    <span className="row-actions">
      <Button variant="danger" className="btn-sm" icon="alert" disabled={busy} onClick={onConfirm}>{armedLabel}</Button>
      <Button variant="quiet" className="btn-sm" disabled={busy} onClick={onCancel}>取消</Button>
    </span>
  );
}

/** tabular-nums + 自动换行的数字单元格。 */
function Num({ value }: { value: number }) {
  return <span className="mono">{value}</span>;
}

// ---------------------------------------------------------------------------
// 授权编辑器：项目选择 + 六种动作 + 授权表
// ---------------------------------------------------------------------------

export function GrantEditor({
  orgSlug,
  path,
  machine = false,
  initialGrants,
  onClose,
}: {
  orgSlug: string;
  path: string;
  machine?: boolean;
  initialGrants?: Grant[];
  onClose: () => void;
}) {
  const [grants, setGrants] = useState<Grant[]>([]);
  const [choices, setChoices] = useState<
    { id: string; name: string; project: string; projectName: string }[]
  >([]);
  const [selected, setSelected] = useState('');
  const [loaded, setLoaded] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    let live = true;
    (async () => {
      const [g, p] = await Promise.all([
        initialGrants
          ? Promise.resolve({ grants: initialGrants })
          : api.get<{ grants: Grant[] }>(path),
        api.get<{ projects: { id: string; name: string }[] }>(`/api/v1/orgs/${orgSlug}/projects`),
      ]);
      const projects = p.projects ?? [];
      const next = projects.map((item) => ({
        id: item.id,
        name: item.name,
        project: item.id,
        projectName: item.name,
      }));
      if (live) {
        const withGrants = next.filter((c) => (g.grants ?? []).some((x) => x.project_id === c.project));
        setLoaded(true);
        setGrants(g.grants ?? []);
        setChoices(next);
        // 默认停在第一个已有授权的项目，让操作员先看见现状。
        setSelected((withGrants[0] ?? next[0])?.id ?? '');
      }
    })()
      .catch((e) => {
        if (live) setError(message(e));
      })
      .finally(() => {
        if (live) setLoading(false);
      });
    return () => {
      live = false;
    };
  }, [path, orgSlug, initialGrants]);

  const choice = choices.find((c) => c.id === selected);
  const nameOf = (projectID: string) => choices.find((c) => c.project === projectID)?.name ?? projectID;
  const current = choice ? grants.filter((g) => g.project_id === choice.project) : [];

  function toggle(action: string, on: boolean) {
    if (!choice) return;
    if (!on) {
      setGrants((all) => all.filter((g) => !(g.project_id === choice.project && g.action === action)));
      return;
    }
    setGrants((all) => [
      ...all,
      {
        project_id: choice.project,
        action,
        ...(machine ? { required_scope: `secrets:${action}` } : {}),
      },
    ]);
  }

  function scopeOf(action: string) {
    return grants.find((g) => g.project_id === choice?.project && g.action === action);
  }

  function removeGrant(g: Grant) {
    setGrants((all) => all.filter((x) => x !== g));
  }

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await api.put(path, { grants });
      onClose();
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  const byProject = choices
    .map((c) => ({ choice: c, rows: grants.filter((g) => g.project_id === c.project) }))
    .filter((entry) => entry.rows.length > 0);

  return (
    <Modal title="明确授权的操作" onClose={onClose} busy={busy}>
      <ValidatedForm className="identity-form" onSubmit={save}>
        <HintLine icon="shield">
          角色决定项目可见性；以下操作逐项授权。写入不包含查看明文、导出或删除。保存会替换该成员或工作负载的全部操作授权。
        </HintLine>

        {loading ? (
          <Skeleton rows={5} />
        ) : (
          <>
            <div className="form-row">
              <Field label="项目 / 环境" htmlFor="grant-project" hint="切换项目只改变下方勾选的编辑对象，不会清掉已勾选的项目。">
                <Select id="grant-project" value={selected} onChange={(e) => setSelected(e.target.value)} disabled={!choices.length}>
                  {choices.map((c) => (
                    <option key={c.id} value={c.id}>{c.name}</option>
                  ))}
                </Select>
              </Field>
              <div className="field">
                <span className="field-label">当前项目已授权</span>
                <div className="btn-row">
                  <Pill tone={current.length ? 'accent' : 'muted'} icon={current.length ? 'unlock' : 'lock'}>
                    {current.length} / 6 项
                  </Pill>
                  <span className="field-hint">
                    {current.length === 6 ? '全部动作已开放。' : current.length === 0 ? '这个项目下没有任何动作。' : '未勾选的动作一律拒绝。'}
                  </span>
                </div>
              </div>
            </div>

            {choices.length === 0 ? (
              <EmptyState icon="folder" title="这个组织还没有项目">
                操作授权以「项目 + 动作」为单位；先创建项目，再回到这里逐项开放。
              </EmptyState>
            ) : choice ? (
              <fieldset>
                <legend>允许的动作</legend>
                <div className="form-row">
                  {actionOrder.map((action) => {
                    const checked = current.some((g) => g.action === action);
                    const grant = scopeOf(action);
                    return (
                      <div key={action} className="form-stack grant-action">
                        {/* hint 里不能再出现动作中文名，否则「查看明文」等可访问名会重复匹配 */}
                        <Check
                          label={actions[action]}
                          hint={<span className="mono">{actionsEnglish[action]}</span>}
                          checked={checked}
                          onChange={(e) => toggle(action, e.target.checked)}
                        />
                        {machine && checked && grant && (
                          <label className="field" htmlFor={`grant-scope-${action}`}>
                            <span className="field-hint">令牌必须包含的 scope</span>
                            <input
                              id={`grant-scope-${action}`}
                              className="mono"
                              required
                              value={grant.required_scope ?? ''}
                              onChange={(e) =>
                                setGrants((all) =>
                                  all.map((x) => (x === grant ? { ...x, required_scope: e.target.value } : x)),
                                )
                              }
                            />
                          </label>
                        )}
                      </div>
                    );
                  })}
                </div>
              </fieldset>
            ) : null}

            <Section
              title="授权清单"
              hint={`共 ${grants.length} 项授权。未勾选的操作拒绝访问。`}
            >
              {byProject.length === 0 ? (
                <EmptyState icon="key" title="还没有任何授权">
                  在上方勾选动作后，这里会按项目列出将被保存的授权；保存后立即生效并写入审计。
                </EmptyState>
              ) : (
                <TableWrap label="授权清单">
                  <table className="secret-table">
                    <thead>
                      <tr>
                        <th>项目</th>
                        <th>动作</th>
                        {machine && <th>必需 scope</th>}
                        <th className="actions">操作</th>
                      </tr>
                    </thead>
                    <tbody>
                      {byProject.flatMap(({ choice: c, rows }) =>
                        rows.map((g, index) => (
                          <tr key={`${g.project_id}/${g.action}`}>
                            <td data-label="项目" className="col-key">
                              {index === 0 ? (
                                <>
                                  <div>{c.name}</div>
                                  <div className="mono cell-sub">{c.project}</div>
                                </>
                              ) : (
                                <span className="faint" aria-label={`同 ${c.name}`}>〃</span>
                              )}
                            </td>
                            <td data-label="动作">
                              <Pill tone={g.action === 'delete' ? 'bad' : g.action === 'export' || g.action === 'reveal' ? 'warn' : 'muted'}>
                                {actions[g.action] ?? g.action}
                              </Pill>
                              <span className="mono cell-sub"> {actionsEnglish[g.action] ?? g.action}</span>
                            </td>
                            {machine && <td data-label="必需 scope" className="mono nowrap">{g.required_scope || '—'}</td>}
                            <td data-label="操作" className="col-actions">
                              <span className="secret-actions">
                                <Button variant="quiet" className="btn-sm" disabled={busy} onClick={() => setSelected(c.id)}>
                                  编辑
                                </Button>
                                <DangerSeparator />
                                <Button variant="danger" className="btn-sm" disabled={busy} onClick={() => removeGrant(g)}>
                                  移除
                                </Button>
                              </span>
                            </td>
                          </tr>
                        )),
                      )}
                    </tbody>
                  </table>
                </TableWrap>
              )}
            </Section>
          </>
        )}

        {error && <Notice tone="danger">{error}</Notice>}
        <div className="form-actions">
          <Button variant="quiet" onClick={onClose} disabled={busy}>取消</Button>
          <Button type="submit" variant="primary" icon="seal" loading={busy} disabled={loading || !loaded}>
            {busy ? '保存中…' : '保存授权'}
          </Button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// 登录会话安全策略（外部身份连接）
// ---------------------------------------------------------------------------

type Policy = {
  profile: string;
  session_ttl: number;
  max_auth_age: number;
  required_acr: string[];
  required_amr: string[];
  online_audience: string;
  rp_logout: boolean;
};

const profileCopy: Record<string, { label: string; hint: string }> = {
  BOUNDED: {
    label: '会话到期失效（最长 15 分钟）',
    hint: '把有效窗口压到 15 分钟以内：上游停用最迟在会话到期时生效。',
  },
  ONLINE_REQUIRED: {
    label: '每次请求在线检查，故障时拒绝访问',
    hint: '每个请求都回源内省；提供方不可用时拒绝访问而不是放行。',
  },
};

export function ConnectionSecurity({ orgSlug, id, onClose }: { orgSlug: string; id: string; onClose: () => void }) {
  const path = `/api/v1/orgs/${orgSlug}/oidc/${id}/security`;
  const [policy, setPolicy] = useState<Policy | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let live = true;
    api
      .get<Policy>(path)
      .then((p) => {
        if (live) setPolicy(p);
      })
      .catch((e) => {
        if (live) setError(message(e));
      });
    return () => {
      live = false;
    };
  }, [path]);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await api.put(path, policy);
      onClose();
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title="登录会话安全策略" onClose={onClose} busy={busy}>
      <ValidatedForm className="identity-form" onSubmit={save}>
        <HintLine icon="clock">修改后现有会话需重新登录。提供方必须声明所选的标准能力。</HintLine>

        {!policy ? (
          error ? null : <Skeleton rows={6} />
        ) : (
          <>
            <Field
              label="上游停用检查"
              htmlFor="session-profile"
              hint={profileCopy[policy.profile]?.hint}
            >
              <Select
                id="session-profile"
                value={policy.profile}
                onChange={(e) => setPolicy({ ...policy, profile: e.target.value })}
              >
                <option value="BOUNDED">{profileCopy.BOUNDED.label}</option>
                <option value="ONLINE_REQUIRED">{profileCopy.ONLINE_REQUIRED.label}</option>
              </Select>
            </Field>

            <div className="form-row">
              <Field label="会话有效期（秒）" htmlFor="session-ttl" hint="60–900 秒。到期后浏览器会话直接作废。">
                <input
                  id="session-ttl"
                  type="number"
                  min={60}
                  max={900}
                  required
                  value={policy.session_ttl}
                  onChange={(e) => setPolicy({ ...policy, session_ttl: Number(e.target.value) })}
                />
              </Field>
              <Field
                label="敏感操作认证有效期（秒）"
                htmlFor="session-max-auth-age"
                hint="查看明文、导出、删除这类动作要求最近一次认证不超过这个时长。"
              >
                <input
                  id="session-max-auth-age"
                  type="number"
                  min={60}
                  max={900}
                  required
                  value={policy.max_auth_age}
                  onChange={(e) => setPolicy({ ...policy, max_auth_age: Number(e.target.value) })}
                />
              </Field>
            </div>

            <div className="form-row">
              <Field label="可接受的 ACR（逗号分隔，满足其一）" htmlFor="session-acr" className="mono">
                <input
                  id="session-acr"
                  className="mono"
                  value={(policy.required_acr ?? []).join(',')}
                  onChange={(e) =>
                    setPolicy({ ...policy, required_acr: e.target.value ? e.target.value.split(',') : [] })
                  }
                />
              </Field>
              <Field label="必须满足的 AMR（逗号分隔，全部满足）" htmlFor="session-amr" className="mono">
                <input
                  id="session-amr"
                  className="mono"
                  value={(policy.required_amr ?? []).join(',')}
                  onChange={(e) =>
                    setPolicy({ ...policy, required_amr: e.target.value ? e.target.value.split(',') : [] })
                  }
                />
              </Field>
            </div>

            {policy.profile === 'ONLINE_REQUIRED' && (
              <Field
                label="在线检查的资源 audience"
                htmlFor="session-audience"
                hint="上游内省响应必须包含这个 audience，否则视为无效令牌。"
              >
                <input
                  id="session-audience"
                  className="mono"
                  required
                  value={policy.online_audience}
                  onChange={(e) => setPolicy({ ...policy, online_audience: e.target.value })}
                />
              </Field>
            )}

            <Check
              label="退出时结束提供方会话（RP-Initiated Logout）"
              hint="登录时从提供方发现并显式启用 logout 端点；未启用则退出只结束本系统会话。"
              checked={policy.rp_logout}
              onChange={(e) => setPolicy({ ...policy, rp_logout: e.target.checked })}
            />
          </>
        )}

        {error && <Notice tone="danger">{error}</Notice>}
        <div className="form-actions">
          <Button variant="quiet" disabled={busy} onClick={onClose}>取消</Button>
          <Button type="submit" variant="primary" loading={busy} disabled={!policy}>
            {busy ? '保存中…' : '保存策略'}
          </Button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// 组织登录策略（内联卡片）
// ---------------------------------------------------------------------------

export function OrganizationSecurity({ orgSlug }: { orgSlug: string }) {
  const [policy, setPolicy] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [loading, setLoading] = useState(true);
  const path = `/api/v1/orgs/${orgSlug}/security`;

  useEffect(() => {
    let live = true;
    api
      .get<{ login_policy: string }>(path)
      .then((p) => {
        if (live) setPolicy(p.login_policy);
      })
      .catch((e) => {
        if (live) setError(message(e));
      })
      .finally(() => {
        if (live) setLoading(false);
      });
    return () => {
      live = false;
    };
  }, [path]);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await api.put(path, { login_policy: policy });
      setNotice('登录策略已保存。');
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="card identity-form" onSubmit={save}>
      <div className="section-head">
        <div className="section-head-main">
          <h3>组织登录策略</h3>
          <p className="section-hint">
            仅外部登录要求至少一个 owner 已验证可用的外部身份；本地密码和本地用户令牌将无法访问该组织。
          </p>
        </div>
        <Pill tone={policy === 'EXTERNAL_REQUIRED' ? 'accent' : 'muted'} icon="seal">
          {policy === 'EXTERNAL_REQUIRED' ? '仅外部身份' : policy === 'LOCAL_OR_EXTERNAL' ? '本地或外部' : '读取中'}
        </Pill>
      </div>

      {loading ? (
        <Skeleton rows={2} />
      ) : (
        <Field
          label="允许的登录方式"
          htmlFor="org-login-policy"
          hint={
            policy === 'EXTERNAL_REQUIRED'
              ? '本地密码与本地用户令牌全部被拒绝；只能经由组织的身份连接登录。'
              : '本地账号、本地用户令牌与外部身份都能进入；外部身份必须逐项显式授权。'
          }
        >
          <Select
            id="org-login-policy"
            value={policy}
            onChange={(e) => setPolicy(e.target.value)}
            disabled={!policy}
          >
            <option value="LOCAL_OR_EXTERNAL">本地账号或外部身份</option>
            <option value="EXTERNAL_REQUIRED" disabled={!hasBrowserSession()}>
              仅外部身份（由外部登录的 owner 开启）
            </option>
          </Select>
        </Field>
      )}

      {error && <Notice tone="danger">{error}</Notice>}
      {notice && <Notice tone="ok">{notice}</Notice>}

      <div className="form-actions">
        <Button type="submit" variant="stamp" icon="seal" loading={busy} disabled={!policy}>
          {busy ? '保存中…' : '保存登录策略'}
        </Button>
      </div>
    </form>
  );
}

// ---------------------------------------------------------------------------
// OAuth 资源信任与工作负载
// ---------------------------------------------------------------------------

type Trust = {
  id: string;
  name: string;
  issuer: string;
  audience: string;
  metadata_mode: string;
  token_profile: string;
  client_id: string;
  max_lifetime: number;
  enabled: boolean;
  revocation_profile: string;
};
type Binding = {
  id: string;
  name: string;
  trust_id: string;
  subject: string;
  client_id: string;
  enabled: boolean;
  grants: Grant[];
};

const tokenProfileLabel: Record<string, string> = {
  JWT_RFC9068: 'JWT RFC 9068',
  INTROSPECTION_RFC7662: 'RFC 7662 内省',
};
const revocationLabel: Record<string, string> = {
  BOUNDED: '有界撤销',
  ONLINE_REQUIRED: '在线撤销',
};

export function WorkloadGovernance({ orgSlug }: { orgSlug: string }) {
  const base = `/api/v1/orgs/${orgSlug}`;
  const [trusts, setTrusts] = useState<Trust[]>([]);
  const [bindings, setBindings] = useState<Binding[]>([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const [edit, setEdit] = useState<Trust | 'new' | null>(null);
  const [binding, setBinding] = useState(false);
  const [grant, setGrant] = useState<Binding | null>(null);
  const [confirm, setConfirm] = useState<{ label: string; path: string; method: 'post' | 'del' } | null>(null);
  const [busy, setBusy] = useState(false);
  const [rowBusy, setRowBusy] = useState('');
  const { armed, setArmed } = useArmed();

  useEffect(() => {
    let live = true;
    setLoading(true);
    setError('');
    Promise.all([
      api.get<{ trusts: Trust[] }>(base + '/oauth-trusts'),
      api.get<{ workloads: Binding[] }>(base + '/workloads'),
    ])
      .then(([t, b]) => {
        if (live) {
          setTrusts(t.trusts ?? []);
          setBindings(b.workloads ?? []);
        }
      })
      .catch((e) => {
        if (live) setError(message(e));
      })
      .finally(() => {
        if (live) setLoading(false);
      });
    return () => {
      live = false;
    };
  }, [base, revision]);

  const reload = () => setRevision((v) => v + 1);

  async function revoke() {
    if (!confirm) return;
    setBusy(true);
    setError('');
    try {
      await api[confirm.method](confirm.path);
      setConfirm(null);
      reload();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }

  async function disableTrust(t: Trust) {
    setArmed('');
    setRowBusy(t.id);
    setError('');
    try {
      await api.patch(base + '/oauth-trusts/' + t.id, { enabled: false });
      reload();
    } catch (e) {
      setError(message(e));
    } finally {
      setRowBusy('');
    }
  }

  const trustName = (id: string) => trusts.find((t) => t.id === id)?.name ?? id;
  const enabledTrusts = trusts.filter((t) => t.enabled);

  if (loading) {
    return (
      <div className="form-stack">
        <Skeleton rows={2} />
        <Skeleton rows={4} />
      </div>
    );
  }

  return (
    <>
      <Section
        title="OAuth 资源信任"
        hint="机器使用 OAuth client credentials 获取资源令牌。浏览器身份连接与机器信任分别登记；精确绑定 issuer、subject、client_id，随后逐项授权。"
        actions={
          <>
            <Button variant="quiet" className="btn-sm" icon="refresh" onClick={reload}>刷新</Button>
            <Button variant="primary" icon="plus" onClick={() => setEdit('new')}>添加资源信任</Button>
          </>
        }
      >
        {error && <Notice tone="danger">{error}</Notice>}
        {trusts.length === 0 ? (
          <EmptyState
            icon="globe"
            title="尚未登记资源信任"
            action={<Button icon="plus" onClick={() => setEdit('new')}>登记第一条资源信任</Button>}
          >
            「资源信任」是一条到提供方的信任线：issuer 决定谁来签发，audience 决定令牌给谁用。登记之后才能绑定工作负载。
          </EmptyState>
        ) : (
          <TableWrap label="OAuth 资源信任">
            <table className="secret-table">
              <thead>
                <tr>
                  <th>名称 / Issuer</th>
                  <th>Audience / 格式</th>
                  <th>状态</th>
                  <th>最大寿命</th>
                  <th className="actions">操作</th>
                </tr>
              </thead>
              <tbody>
                {trusts.map((t) => (
                  <tr key={t.id}>
                    <td data-label="名称 / Issuer" className="col-key">
                      <div className="key">{t.name}</div>
                      <div className="mono cell-sub">{t.issuer}</div>
                      <div className="mono cell-sub">{t.id}</div>
                    </td>
                    <td data-label="Audience / 格式">
                      <div className="key mono">{t.audience}</div>
                      <div className="btn-row" style={{ marginTop: 4 }}>
                        <Pill tone="muted">{tokenProfileLabel[t.token_profile] ?? t.token_profile}</Pill>
                        <Pill tone="muted">{t.metadata_mode}</Pill>
                        <Pill tone={t.revocation_profile === 'ONLINE_REQUIRED' ? 'warn' : 'ok'}>
                          {revocationLabel[t.revocation_profile] ?? t.revocation_profile}
                        </Pill>
                      </div>
                    </td>
                    <td data-label="状态">
                      <Pill tone={t.enabled ? 'ok' : 'bad'} icon={t.enabled ? 'unlock' : 'lock'}>
                        {t.enabled ? '启用' : '停用'}
                      </Pill>
                    </td>
                    <td data-label="最大寿命" className="nowrap"><Num value={t.max_lifetime} /><span className="faint"> 秒</span></td>
                    <td data-label="操作" className="col-actions">
                      <span className="secret-actions">
                        <Button variant="quiet" className="btn-sm" onClick={() => setEdit(t)}>编辑信任</Button>
                        {t.enabled && (
                          <>
                            <DangerSeparator />
                            <ArmedButtons
                              armed={armed === `trust:${t.id}`}
                              busy={rowBusy === t.id}
                              label="停用"
                              armedLabel="确认停用"
                              title="停用后该信任下所有工作负载立即拒绝访问"
                              onArm={() => setArmed(`trust:${t.id}`)}
                              onConfirm={() => void disableTrust(t)}
                              onCancel={() => setArmed('')}
                            />
                          </>
                        )}
                        <DangerSeparator />
                        <Button
                          variant="danger"
                          className="btn-sm"
                          icon="trash"
                          onClick={() =>
                            setConfirm({
                              label: `删除信任 ${t.name}`,
                              path: base + '/oauth-trusts/' + t.id,
                              method: 'del',
                            })
                          }
                        >
                          删除
                        </Button>
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableWrap>
        )}
      </Section>

      <Section
        title="工作负载绑定"
        hint="绑定即把某个外部机器的 subject 与 client_id 钉死在这一条信任上；绑定后默认无权限，逐项授权后才可调用。"
        actions={
          <Button
            variant="primary"
            icon="robot"
            disabled={!enabledTrusts.length}
            title={enabledTrusts.length ? undefined : '先添加一条启用的资源信任，才能绑定工作负载'}
            onClick={() => setBinding(true)}
          >
            绑定工作负载
          </Button>
        }
      >
        {bindings.length === 0 ? (
          <EmptyState icon="robot" title="尚未绑定外部工作负载">
            没有绑定的机器身份不能被识别；绑定后还要在「操作授权」里逐项开放动作，否则连目录都读不到。
          </EmptyState>
        ) : (
          <TableWrap label="外部工作负载">
            <table className="secret-table">
              <thead>
                <tr>
                  <th>工作负载</th>
                  <th>外部 subject / client_id</th>
                  <th>授权 / 状态</th>
                  <th className="actions">操作</th>
                </tr>
              </thead>
              <tbody>
                {bindings.map((b) => (
                  <tr key={b.id}>
                    <td data-label="工作负载" className="col-key">
                      <div className="key">{b.name}</div>
                      <div className="cell-sub">{trustName(b.trust_id)}</div>
                    </td>
                    <td data-label="外部 subject / client_id" className="mono">
                      {b.subject}
                      <div className="cell-sub">{b.client_id}</div>
                    </td>
                    <td data-label="授权 / 状态" className="nowrap">
                      <Pill tone={b.enabled ? 'ok' : 'bad'} icon={b.enabled ? 'unlock' : 'lock'}>
                        {b.enabled ? '启用' : '已撤销'}
                      </Pill>
                      <div className="cell-sub mono">{(b.grants ?? []).length} 项授权</div>
                    </td>
                    <td data-label="操作" className="col-actions">
                      <span className="secret-actions">
                        {b.enabled && (
                          <>
                            <Button variant="quiet" className="btn-sm" icon="shield" onClick={() => setGrant(b)}>
                              操作授权
                            </Button>
                            <DangerSeparator />
                            <Button
                              variant="danger"
                              className="btn-sm"
                              onClick={() =>
                                setConfirm({
                                  label: `撤销工作负载 ${b.name}`,
                                  path: base + '/workloads/' + b.id + '/revoke',
                                  method: 'post',
                                })
                              }
                            >
                              撤销
                            </Button>
                          </>
                        )}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableWrap>
        )}
      </Section>

      {edit && (
        <TrustForm
          base={base}
          initial={edit === 'new' ? undefined : edit}
          onClose={() => setEdit(null)}
          onSaved={() => {
            setEdit(null);
            reload();
          }}
        />
      )}
      {binding && (
        <BindingForm
          base={base}
          trusts={enabledTrusts}
          onClose={() => setBinding(false)}
          onSaved={() => {
            setBinding(false);
            reload();
          }}
        />
      )}
      {grant && (
        <WorkloadGrants
          orgSlug={orgSlug}
          binding={grant}
          onClose={() => {
            setGrant(null);
            reload();
          }}
        />
      )}
      {confirm && (
        <Modal title={confirm.label} onClose={() => setConfirm(null)} busy={busy}>
          <HintLine icon="alert">立即拒绝后续访问。撤销绑定不可恢复；需要访问时应重新登记并授权。</HintLine>
          {error && <Notice tone="danger">{error}</Notice>}
          <div className="form-actions">
            <Button variant="quiet" disabled={busy} onClick={() => setConfirm(null)}>取消</Button>
            <Button variant="danger" icon="trash" loading={busy} onClick={revoke}>确认</Button>
          </div>
        </Modal>
      )}
    </>
  );
}

function TrustForm({ base, initial, onClose, onSaved }: {
  base: string;
  initial?: Trust;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [form, setForm] = useState({
    ...initial,
    name: initial?.name ?? '',
    issuer: initial?.issuer ?? '',
    audience: initial?.audience ?? '',
    metadata_mode: initial?.metadata_mode ?? 'oauth',
    token_profile: initial?.token_profile ?? 'JWT_RFC9068',
    client_id: initial?.client_id ?? '',
    client_secret: '',
    max_lifetime: initial?.max_lifetime ?? 900,
    enabled: initial?.enabled ?? true,
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await (initial
        ? api.patch(base + '/oauth-trusts/' + initial.id, form)
        : api.post(base + '/oauth-trusts', form));
      onSaved();
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title={initial ? '编辑资源信任' : '添加资源信任'} onClose={onClose} busy={busy}>
      <ValidatedForm className="identity-form" onSubmit={save}>
        <div className="form-row">
          <Field label="名称" htmlFor="trust-name">
            <input
              id="trust-name"
              required
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
          </Field>
          <Field label="Issuer" htmlFor="trust-issuer" hint={initial ? '已有信任不允许更换 issuer，如需变更请新建一条。' : '接受签发资源令牌的精确 issuer。'}>
            <input
              id="trust-issuer"
              className="mono"
              required
              type="url"
              readOnly={!!initial}
              value={form.issuer}
              onChange={(e) => setForm({ ...form, issuer: e.target.value })}
            />
          </Field>
        </div>

        <div className="form-row">
          <Field label="资源 audience" htmlFor="trust-audience" hint="必须在资源令牌中被验证的 audience。">
            <input
              id="trust-audience"
              className="mono"
              required
              value={form.audience}
              onChange={(e) => setForm({ ...form, audience: e.target.value })}
            />
          </Field>
          <Field label="发现协议" htmlFor="trust-metadata">
            <select
              id="trust-metadata"
              value={form.metadata_mode}
              onChange={(e) => setForm({ ...form, metadata_mode: e.target.value })}
            >
              <option value="oauth">OAuth RFC 8414</option>
              <option value="oidc">OpenID Connect Discovery</option>
            </select>
          </Field>
        </div>

        <Field
          label="令牌验证"
          htmlFor="trust-profile"
          hint={form.token_profile === 'INTROSPECTION_RFC7662'
            ? '每次请求都回源内省，撤销立即生效（在线撤销）。'
            : '本地验签，撤销最迟到令牌寿命结束时生效（有界撤销）。'}
        >
          <select
            id="trust-profile"
            value={form.token_profile}
            onChange={(e) => setForm({ ...form, token_profile: e.target.value })}
          >
            <option value="JWT_RFC9068">JWT RFC 9068（到期前有界撤销）</option>
            <option value="INTROSPECTION_RFC7662">RFC 7662（每次在线检查）</option>
          </select>
        </Field>

        {form.token_profile === 'INTROSPECTION_RFC7662' && (
          <fieldset>
            <legend>内省凭据</legend>
            <div className="form-stack">
              <Field label="资源服务器 client_id" htmlFor="trust-client-id">
                <input
                  id="trust-client-id"
                  className="mono"
                  required
                  value={form.client_id}
                  onChange={(e) => setForm({ ...form, client_id: e.target.value })}
                />
              </Field>
              <Field label="资源服务器 client_secret" htmlFor="trust-client-secret" hint={initial ? '留空保留现有密钥。' : '加密保存，不再显示。'}>
                <input
                  id="trust-client-secret"
                  type="password"
                  autoComplete="new-password"
                  className="mono"
                  required={!initial}
                  value={form.client_secret}
                  onChange={(e) => setForm({ ...form, client_secret: e.target.value })}
                />
              </Field>
            </div>
          </fieldset>
        )}

        <div className="form-row">
          <Field label="令牌最大寿命（秒）" htmlFor="trust-lifetime" hint="60–900 秒。">
            <input
              id="trust-lifetime"
              type="number"
              required
              min={60}
              max={900}
              value={form.max_lifetime}
              onChange={(e) => setForm({ ...form, max_lifetime: Number(e.target.value) })}
            />
          </Field>
          <div className="field">
            <span className="field-label">状态</span>
            <Check
              label="启用"
              hint="新建即为启用；停用后其下工作负载立即拒绝访问。"
              checked={form.enabled}
              onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
            />
          </div>
        </div>

        {error && <Notice tone="danger">{error}</Notice>}
        <div className="form-actions">
          <Button variant="quiet" disabled={busy} onClick={onClose}>取消</Button>
          <Button type="submit" variant="primary" icon="seal" loading={busy}>
            {busy ? '保存中…' : '保存信任'}
          </Button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

function BindingForm({ base, trusts, onClose, onSaved }: {
  base: string;
  trusts: Trust[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [form, setForm] = useState({
    name: '',
    trust_id: trusts[0]?.id ?? '',
    subject: '',
    client_id: '',
    grants: [] as Grant[],
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await api.post(base + '/workloads', form);
      onSaved();
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  const trust = trusts.find((t) => t.id === form.trust_id);

  return (
    <Modal title="绑定外部工作负载" onClose={onClose} busy={busy}>
      <ValidatedForm className="identity-form" onSubmit={save}>
        <HintLine icon="link">绑定后默认无权限。请从令牌发行方登记信息取得精确 subject 和 client_id，再配置操作授权。</HintLine>
        <div className="form-row">
          <Field label="名称" htmlFor="binding-name">
            <input
              id="binding-name"
              required
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
          </Field>
          <Field
            label="资源信任"
            htmlFor="binding-trust"
            hint={trust ? `${trust.issuer}` : '只列出已启用的信任。'}
          >
            <select
              id="binding-trust"
              required
              value={form.trust_id}
              onChange={(e) => setForm({ ...form, trust_id: e.target.value })}
            >
              {trusts.map((t) => (
                <option key={t.id} value={t.id}>{t.name}</option>
              ))}
            </select>
          </Field>
        </div>
        <div className="form-row">
          <Field label="Subject" htmlFor="binding-subject" hint="外部令牌里的精确 sub。">
            <input
              id="binding-subject"
              className="mono"
              required
              value={form.subject}
              onChange={(e) => setForm({ ...form, subject: e.target.value })}
            />
          </Field>
          <Field label="Client ID" htmlFor="binding-client" hint="不假定与 subject 相同。">
            <input
              id="binding-client"
              className="mono"
              required
              value={form.client_id}
              onChange={(e) => setForm({ ...form, client_id: e.target.value })}
            />
          </Field>
        </div>
        {error && <Notice tone="danger">{error}</Notice>}
        <div className="form-actions">
          <Button variant="quiet" onClick={onClose} disabled={busy}>取消</Button>
          <Button type="submit" variant="primary" icon="seal" loading={busy} disabled={!trusts.length}>
            创建绑定
          </Button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

function WorkloadGrants({ orgSlug, binding, onClose }: {
  orgSlug: string;
  binding: Binding;
  onClose: () => void;
}) {
  return (
    <GrantEditor
      orgSlug={orgSlug}
      path={`/api/v1/orgs/${orgSlug}/workloads/${binding.id}/grants`}
      machine
      initialGrants={binding.grants ?? []}
      onClose={onClose}
    />
  );
}

// ---------------------------------------------------------------------------
// 组织切换
// ---------------------------------------------------------------------------

type SwitchRow = {
  id: string;
  name: string;
  connection: string;
  current: boolean;
  login_url: string;
};

export function OrganizationSwitcher({ onClose }: { onClose: () => void }) {
  const [rows, setRows] = useState<SwitchRow[]>([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState('');
  const selectRef = useRef<HTMLSelectElement>(null);

  useEffect(() => {
    let live = true;
    api
      .get<{ organizations: SwitchRow[] }>('/api/v1/auth/organizations')
      .then((d) => {
        if (!live) return;
        const list = d.organizations ?? [];
        setRows(list);
        setSelected((list.find((r) => r.current) ?? list[0])?.login_url ?? '');
      })
      .catch((e) => {
        if (live) setError(message(e));
      })
      .finally(() => {
        if (live) setLoading(false);
      });
    return () => {
      live = false;
    };
  }, []);

  const onlyCurrent = !loading && !error && rows.length > 0 && rows.every((r) => r.current);
  const target = rows.find((r) => r.login_url === selected);
  const canEnter = !!target && !target.current;

  function enter() {
    if (!canEnter || !target) return;
    window.location.href = target.login_url;
  }

  return (
    <Modal title="切换组织" onClose={onClose}>
      <HintLine icon="alert">
        选择已加入的组织后，通过该组织的身份连接重新验证。切换将清空当前页面的秘密和未保存表单。
      </HintLine>

      {loading ? (
        <Skeleton rows={3} />
      ) : (
        <>
          {rows.length > 0 && (
            <Field
              label="当前组织"
              htmlFor="switch-org"
              hint={target
                ? target.current
                  ? '这条就是当前组织；没有需要重新验证的对象。'
                  : `进入后由「${target.connection}」重新验证身份。`
                : undefined}
            >
              <select
                id="switch-org"
                ref={selectRef}
                value={selected}
                onChange={(e) => setSelected(e.target.value)}
              >
                {rows.map((r) => (
                  <option key={r.login_url} value={r.login_url}>
                    {r.name} · {r.connection}
                  </option>
                ))}
              </select>
            </Field>
          )}

          {rows.length > 0 && (
            <TableWrap label="已加入的组织">
              <table className="secret-table">
                <thead>
                  <tr><th>组织</th><th>身份连接</th><th>状态</th></tr>
                </thead>
                <tbody>
                  {rows.map((r) => (
                    <tr key={r.login_url} className={r.current ? 'open' : undefined}>
                      <td data-label="组织" className="col-key">
                        <div className="key">{r.name}</div>
                        <div className="mono cell-sub">{r.id}</div>
                      </td>
                      <td data-label="身份连接">
                        <Pill tone="muted" icon="globe">{r.connection}</Pill>
                      </td>
                      <td data-label="状态">
                        {r.current
                          ? <Pill tone="accent" icon="seal">当前组织</Pill>
                          : <Pill tone="muted">需重新验证</Pill>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </TableWrap>
          )}
        </>
      )}

      {error && <Notice tone="danger">{error}</Notice>}
      {onlyCurrent && (
        <EmptyState icon="globe" title="暂无其他已加入且具有匹配身份连接的组织">
          用同一身份连接登录到另一个组织后，它会出现在这里。
        </EmptyState>
      )}

      <div className="form-actions">
        <Button variant="quiet" onClick={onClose}>关闭</Button>
        {canEnter && (
          <Button variant="primary" icon="arrow-right" onClick={enter}>
            验证并进入
          </Button>
        )}
      </div>
    </Modal>
  );
}

import { useEffect, useState } from "react";
import { api, hasBrowserSession } from "../api";
import Modal from "./Modal";
import ValidatedForm from "./ValidatedForm";

export type Grant = {
  project_id: string;
  environment_id: string;
  action: string;
  required_scope?: string;
};
const actions = {
  read: "查看目录",
  write: "写入",
  reveal: "查看明文",
  export: "导出",
  rollback: "回滚",
  delete: "删除",
};
const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

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
  const [selected, setSelected] = useState("");
  const [loaded, setLoaded] = useState(false);
  const [loading, setLoading] = useState(true),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  useEffect(() => {
    let live = true;
    (async () => {
      const [g, p] = await Promise.all([
        initialGrants
          ? Promise.resolve({ grants: initialGrants })
          : api.get<{ grants: Grant[] }>(path),
        api.get<{ projects: { id: string; name: string }[] }>(
          `/api/v1/orgs/${orgSlug}/projects`,
        ),
      ]);
      const envs = await Promise.all(
        p.projects.map(async (p) =>
          (
            await api.get<{ environments: { id: string; name: string }[] }>(
              `/api/v1/projects/${p.id}/environments`,
            )
          ).environments.map((e) => ({
            ...e,
            project: p.id,
            projectName: p.name,
          })),
        ),
      );
      if (live) {
        setLoaded(true);
        setGrants(g.grants ?? []);
        setChoices(envs.flat());
        setSelected(envs.flat()[0]?.id ?? "");
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
  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.put(path, { grants });
      onClose();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal title="明确授权的操作" onClose={onClose} busy={busy}>
      <ValidatedForm onSubmit={save}>
        <p>
          角色决定项目可见性；以下操作逐项授权。写入不包含查看明文、导出或删除。保存会替换该成员或工作负载的全部操作授权。
        </p>
        {loading ? (
          <p role="status">加载授权…</p>
        ) : (
          <>
            <label>
              项目 / 环境
              <select
                value={selected}
                onChange={(e) => setSelected(e.target.value)}
              >
                {choices.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.projectName} / {c.name}
                  </option>
                ))}
              </select>
            </label>
            {choice &&
              Object.entries(actions).map(([action, label]) => {
                const grant = grants.find(
                  (g) => g.environment_id === selected && g.action === action,
                );
                return (
                  <fieldset key={action}>
                    <label className="row">
                      <input
                        type="checkbox"
                        checked={!!grant}
                        onChange={(e) =>
                          setGrants((g) =>
                            e.target.checked
                              ? [
                                  ...g,
                                  {
                                    project_id: choice.project,
                                    environment_id: selected,
                                    action,
                                    ...(machine
                                      ? { required_scope: `secrets:${action}` }
                                      : {}),
                                  },
                                ]
                              : g.filter((x) => x !== grant),
                          )
                        }
                      />
                      {label}
                    </label>
                    {machine && grant && (
                      <label>
                        令牌必须包含的 scope
                        <input
                          required
                          value={grant.required_scope ?? ""}
                          onChange={(e) =>
                            setGrants((g) =>
                              g.map((x) =>
                                x === grant
                                  ? { ...x, required_scope: e.target.value }
                                  : x,
                              ),
                            )
                          }
                        />
                      </label>
                    )}
                  </fieldset>
                );
              })}
            <p className="muted">
              共 {grants.length} 项授权。未勾选的操作拒绝访问。
            </p>
          </>
        )}
        {error && (
          <p role="alert" className="error">
            {error}
          </p>
        )}
        <div className="drawer-actions">
          <button
            type="button"
            className="ghost"
            onClick={onClose}
            disabled={busy}
          >
            取消
          </button>
          <button disabled={busy || loading || !loaded}>
            {busy ? "保存中…" : "保存授权"}
          </button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

type Policy = {
  profile: string;
  session_ttl: number;
  max_auth_age: number;
  required_acr: string[];
  required_amr: string[];
  online_audience: string;
  rp_logout: boolean;
};
export function ConnectionSecurity({
  orgSlug,
  id,
  onClose,
}: {
  orgSlug: string;
  id: string;
  onClose: () => void;
}) {
  const path = `/api/v1/orgs/${orgSlug}/oidc/${id}/security`;
  const [policy, setPolicy] = useState<Policy | null>(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
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
    setError("");
    try {
      await api.put(path, policy);
      onClose();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal title="登录会话安全策略" onClose={onClose} busy={busy}>
      <ValidatedForm onSubmit={save}>
        <p>修改后现有会话需重新登录。提供方必须声明所选的标准能力。</p>
        {policy && (
          <>
            <label>
              上游停用检查
              <select
                value={policy.profile}
                onChange={(e) =>
                  setPolicy({ ...policy, profile: e.target.value })
                }
              >
                <option value="BOUNDED">会话到期失效（最长 15 分钟）</option>
                <option value="ONLINE_REQUIRED">
                  每次请求在线检查，故障时拒绝访问
                </option>
              </select>
            </label>
            <label>
              会话有效期（秒）
              <input
                type="number"
                min={60}
                max={900}
                required
                value={policy.session_ttl}
                onChange={(e) =>
                  setPolicy({ ...policy, session_ttl: Number(e.target.value) })
                }
              />
            </label>
            <label>
              敏感操作认证有效期（秒）
              <input
                type="number"
                min={60}
                max={900}
                required
                value={policy.max_auth_age}
                onChange={(e) =>
                  setPolicy({ ...policy, max_auth_age: Number(e.target.value) })
                }
              />
            </label>
            <label>
              可接受的 ACR（逗号分隔，满足其一）
              <input
                value={(policy.required_acr ?? []).join(",")}
                onChange={(e) =>
                  setPolicy({
                    ...policy,
                    required_acr: e.target.value
                      ? e.target.value.split(",")
                      : [],
                  })
                }
              />
            </label>
            <label>
              必须满足的 AMR（逗号分隔，全部满足）
              <input
                value={(policy.required_amr ?? []).join(",")}
                onChange={(e) =>
                  setPolicy({
                    ...policy,
                    required_amr: e.target.value
                      ? e.target.value.split(",")
                      : [],
                  })
                }
              />
            </label>
            {policy.profile === "ONLINE_REQUIRED" && (
              <label>
                在线检查的资源 audience
                <input
                  required
                  value={policy.online_audience}
                  onChange={(e) =>
                    setPolicy({ ...policy, online_audience: e.target.value })
                  }
                />
              </label>
            )}
            <label className="row">
              <input
                type="checkbox"
                checked={policy.rp_logout}
                onChange={(e) =>
                  setPolicy({ ...policy, rp_logout: e.target.checked })
                }
              />
              退出时结束提供方会话（RP-Initiated Logout）
            </label>
          </>
        )}
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        <div className="drawer-actions">
          <button
            type="button"
            className="ghost"
            disabled={busy}
            onClick={onClose}
          >
            取消
          </button>
          <button disabled={!policy || busy}>
            {busy ? "保存中…" : "保存策略"}
          </button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

export function OrganizationSecurity({ orgSlug }: { orgSlug: string }) {
  const [policy, setPolicy] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
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
      });
    return () => {
      live = false;
    };
  }, [path]);
  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await api.put(path, { login_policy: policy });
      setNotice("登录策略已保存。");
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <ValidatedForm onSubmit={save} className="card">
      <h3>组织登录策略</h3>
      <p>
        仅外部登录要求至少一个 owner
        已验证可用的外部身份；本地密码和本地用户令牌将无法访问该组织。
      </p>
      <label>
        允许的登录方式
        <select
          value={policy}
          onChange={(e) => setPolicy(e.target.value)}
          disabled={!policy}
        >
          <option value="LOCAL_OR_EXTERNAL">本地账号或外部身份</option>
          <option value="EXTERNAL_REQUIRED" disabled={!hasBrowserSession()}>
            仅外部身份（由外部登录的 owner 开启）
          </option>
        </select>
      </label>
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      {notice && <p role="status">{notice}</p>}
      <button disabled={busy || !policy}>
        {busy ? "保存中…" : "保存登录策略"}
      </button>
    </ValidatedForm>
  );
}

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
export function WorkloadGovernance({ orgSlug }: { orgSlug: string }) {
  const base = `/api/v1/orgs/${orgSlug}`;
  const [trusts, setTrusts] = useState<Trust[]>([]),
    [bindings, setBindings] = useState<Binding[]>([]),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(true),
    [revision, setRevision] = useState(0);
  const [edit, setEdit] = useState<Trust | "new" | null>(null),
    [binding, setBinding] = useState(false),
    [grant, setGrant] = useState<Binding | null>(null),
    [confirm, setConfirm] = useState<{
      label: string;
      path: string;
      method: "post" | "del";
    } | null>(null),
    [busy, setBusy] = useState(false);
  useEffect(() => {
    let live = true;
    setLoading(true);
    setError("");
    Promise.all([
      api.get<{ trusts: Trust[] }>(base + "/oauth-trusts"),
      api.get<{ workloads: Binding[] }>(base + "/workloads"),
    ])
      .then(([t, b]) => {
        if (live) {
          setTrusts(t.trusts);
          setBindings(b.workloads);
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
    setError("");
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
  return (
    <section>
      <div className="panel-head">
        <h3>OAuth 资源信任与工作负载</h3>
        <button className="ghost" onClick={reload}>
          刷新
        </button>
        <button onClick={() => setEdit("new")}>添加资源信任</button>
        <button
          disabled={!trusts.some((t) => t.enabled)}
          onClick={() => setBinding(true)}
        >
          绑定工作负载
        </button>
      </div>
      <p>
        机器使用 OAuth client credentials
        获取资源令牌。浏览器身份连接与机器信任分别登记；精确绑定
        issuer、subject、client_id，随后逐项授权。
      </p>
      {loading && <p role="status">加载中…</p>}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <div className="table-scroll" tabIndex={0} aria-label="OAuth 资源信任">
        <table className="table">
          <thead>
            <tr>
              <th>名称 / Issuer</th>
              <th>Audience / 格式</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {trusts.map((t) => (
              <tr key={t.id}>
                <td>
                  {t.name}
                  <div className="mono">{t.issuer}</div>
                  <small>{t.id}</small>
                </td>
                <td>
                  {t.audience}
                  <div>
                    {t.token_profile} · {t.revocation_profile}
                  </div>
                </td>
                <td>{t.enabled ? "启用" : "停用"}</td>
                <td>
                  <button className="ghost" onClick={() => setEdit(t)}>
                    编辑信任
                  </button>
                  {t.enabled && (
                    <button
                      className="ghost"
                      disabled={busy}
                      onClick={async () => {
                        setBusy(true);
                        try {
                          await api.patch(base + "/oauth-trusts/" + t.id, {
                            enabled: false,
                          });
                          reload();
                        } catch (e) {
                          setError(message(e));
                        } finally {
                          setBusy(false);
                        }
                      }}
                    >
                      停用
                    </button>
                  )}
                  <button
                    className="ghost"
                    onClick={() =>
                      setConfirm({
                        label: `删除信任 ${t.name}`,
                        path: base + "/oauth-trusts/" + t.id,
                        method: "del",
                      })
                    }
                  >
                    删除
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {!loading && !error && !trusts.length && <p>尚未登记资源信任。</p>}
      <div className="table-scroll" tabIndex={0} aria-label="外部工作负载">
        <table className="table">
          <thead>
            <tr>
              <th>工作负载</th>
              <th>外部 subject / client_id</th>
              <th>授权 / 状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {bindings.map((b) => (
              <tr key={b.id}>
                <td>
                  {b.name}
                  <div>{trusts.find((t) => t.id === b.trust_id)?.name}</div>
                </td>
                <td className="mono">
                  {b.subject}
                  <div>{b.client_id}</div>
                </td>
                <td>
                  {(b.grants ?? []).length} 项 · {b.enabled ? "启用" : "已撤销"}
                </td>
                <td>
                  {b.enabled && (
                    <>
                      <button className="ghost" onClick={() => setGrant(b)}>
                        操作授权
                      </button>
                      <button
                        className="ghost"
                        onClick={() =>
                          setConfirm({
                            label: `撤销工作负载 ${b.name}`,
                            path: base + "/workloads/" + b.id + "/revoke",
                            method: "post",
                          })
                        }
                      >
                        撤销
                      </button>
                    </>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {!loading && !error && !bindings.length && <p>尚未绑定外部工作负载。</p>}
      {edit && (
        <TrustForm
          base={base}
          initial={edit === "new" ? undefined : edit}
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
          trusts={trusts.filter((t) => t.enabled)}
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
        <Modal
          title={confirm.label}
          onClose={() => setConfirm(null)}
          busy={busy}
        >
          <p>
            立即拒绝后续访问。撤销绑定不可恢复；需要访问时应重新登记并授权。
          </p>
          {error && (
            <p className="error" role="alert">
              {error}
            </p>
          )}
          <div className="drawer-actions">
            <button
              className="ghost"
              disabled={busy}
              onClick={() => setConfirm(null)}
            >
              取消
            </button>
            <button disabled={busy} onClick={revoke}>
              确认
            </button>
          </div>
        </Modal>
      )}
    </section>
  );
}

function TrustForm({
  base,
  initial,
  onClose,
  onSaved,
}: {
  base: string;
  initial?: Trust;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [form, setForm] = useState({
    ...initial,
    name: initial?.name ?? "",
    issuer: initial?.issuer ?? "",
    audience: initial?.audience ?? "",
    metadata_mode: initial?.metadata_mode ?? "oauth",
    token_profile: initial?.token_profile ?? "JWT_RFC9068",
    client_id: initial?.client_id ?? "",
    client_secret: "",
    max_lifetime: initial?.max_lifetime ?? 900,
    enabled: initial?.enabled ?? true,
  });
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await (initial
        ? api.patch(base + "/oauth-trusts/" + initial.id, form)
        : api.post(base + "/oauth-trusts", form));
      onSaved();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title={initial ? "编辑资源信任" : "添加资源信任"}
      onClose={onClose}
      busy={busy}
    >
      <ValidatedForm onSubmit={save}>
        <label>
          名称
          <input
            required
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
        </label>
        <label>
          Issuer
          <input
            required
            type="url"
            readOnly={!!initial}
            value={form.issuer}
            onChange={(e) => setForm({ ...form, issuer: e.target.value })}
          />
        </label>
        <label>
          资源 audience
          <input
            required
            value={form.audience}
            onChange={(e) => setForm({ ...form, audience: e.target.value })}
          />
        </label>
        <label>
          发现协议
          <select
            value={form.metadata_mode}
            onChange={(e) =>
              setForm({ ...form, metadata_mode: e.target.value })
            }
          >
            <option value="oauth">OAuth RFC 8414</option>
            <option value="oidc">OpenID Connect Discovery</option>
          </select>
        </label>
        <label>
          令牌验证
          <select
            value={form.token_profile}
            onChange={(e) =>
              setForm({ ...form, token_profile: e.target.value })
            }
          >
            <option value="JWT_RFC9068">JWT RFC 9068（到期前有界撤销）</option>
            <option value="INTROSPECTION_RFC7662">
              RFC 7662（每次在线检查）
            </option>
          </select>
        </label>
        {form.token_profile === "INTROSPECTION_RFC7662" && (
          <>
            <label>
              资源服务器 client_id
              <input
                required
                value={form.client_id}
                onChange={(e) =>
                  setForm({ ...form, client_id: e.target.value })
                }
              />
            </label>
            <label>
              资源服务器 client_secret{initial ? "（留空保留）" : ""}
              <input
                type="password"
                autoComplete="new-password"
                required={!initial}
                value={form.client_secret}
                onChange={(e) =>
                  setForm({ ...form, client_secret: e.target.value })
                }
              />
            </label>
          </>
        )}
        <label>
          令牌最大寿命（秒）
          <input
            required
            type="number"
            min={60}
            max={900}
            value={form.max_lifetime}
            onChange={(e) =>
              setForm({ ...form, max_lifetime: Number(e.target.value) })
            }
          />
        </label>
        <label className="row">
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
          />
          启用
        </label>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        <div className="drawer-actions">
          <button
            className="ghost"
            type="button"
            disabled={busy}
            onClick={onClose}
          >
            取消
          </button>
          <button disabled={busy}>{busy ? "保存中…" : "保存信任"}</button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}
function BindingForm({
  base,
  trusts,
  onClose,
  onSaved,
}: {
  base: string;
  trusts: Trust[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [form, setForm] = useState({
    name: "",
    trust_id: trusts[0]?.id ?? "",
    subject: "",
    client_id: "",
    grants: [],
  });
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.post(base + "/workloads", form);
      onSaved();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal title="绑定外部工作负载" onClose={onClose} busy={busy}>
      <ValidatedForm onSubmit={save}>
        <p>
          绑定后默认无权限。请从令牌发行方登记信息取得精确 subject 和
          client_id，再配置操作授权。
        </p>
        <label>
          名称
          <input
            required
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
        </label>
        <label>
          资源信任
          <select
            required
            value={form.trust_id}
            onChange={(e) => setForm({ ...form, trust_id: e.target.value })}
          >
            {trusts.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          Subject
          <input
            required
            value={form.subject}
            onChange={(e) => setForm({ ...form, subject: e.target.value })}
          />
        </label>
        <label>
          Client ID
          <input
            required
            value={form.client_id}
            onChange={(e) => setForm({ ...form, client_id: e.target.value })}
          />
        </label>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        <div className="drawer-actions">
          <button
            type="button"
            className="ghost"
            onClick={onClose}
            disabled={busy}
          >
            取消
          </button>
          <button disabled={busy}>创建绑定</button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}
function WorkloadGrants({
  orgSlug,
  binding,
  onClose,
}: {
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

export function OrganizationSwitcher({ onClose }: { onClose: () => void }) {
  const [rows, setRows] = useState<
      {
        id: string;
        name: string;
        connection: string;
        current: boolean;
        login_url: string;
      }[]
    >([]),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(true);
  useEffect(() => {
    let live = true;
    api
      .get<{ organizations: typeof rows }>("/api/v1/auth/organizations")
      .then((d) => {
        if (live) setRows(d.organizations);
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
  return (
    <Modal title="切换组织" onClose={onClose}>
      <p>
        选择已加入的组织后，通过该组织的身份连接重新验证。切换将清空当前页面的秘密和未保存表单。
      </p>
      {loading && <p role="status">加载组织…</p>}
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      <ul>
        {rows.map((r) => (
          <li key={r.login_url}>
            {r.name} · {r.connection}{" "}
            {r.current ? "（当前组织）" : <a href={r.login_url}>验证并进入</a>}
          </li>
        ))}
      </ul>
      {!loading && !error && rows.every((r) => r.current) && (
        <p>暂无其他已加入且具有匹配身份连接的组织。</p>
      )}
      <button className="ghost" onClick={onClose}>
        关闭
      </button>
    </Modal>
  );
}

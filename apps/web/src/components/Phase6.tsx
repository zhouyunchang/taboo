import { useCallback, useEffect, useState } from 'react';
import { api, ApiError } from '../api';
import type { Project } from '../api';
import { Button, EmptyState, Field, Notice, Pill, Section, Select, TableWrap } from '../design/primitives';
import { fmtTime, timeTitle } from '../time';
import Modal from './Modal';

type Approval = { id: string; key: string; action: string; status: string; requester_id: string; reason: string; expires_at: number };
type Access = { id: string; project_id: string; action: string; status: string; requester_id: string; reason: string; ttl_seconds: number; expires_at: number };
type Token = { id: string; name: string; revoked: boolean; expires_at: number };

// 状态一律走 Pill：审批/申请/令牌的状态只在真实状态上着色。
function statusTone(status: string): 'ok' | 'bad' | 'warn' | 'muted' | 'accent' {
  if (status === 'pending') return 'warn';
  if (status === 'approved') return 'ok';
  if (status === 'rejected') return 'bad';
  return 'muted';
}

export default function Phase6({ orgSlug, canApprove, showScim = true }: { orgSlug: string; canApprove: boolean; showScim?: boolean }) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [approvals, setApprovals] = useState<Approval[]>([]);
  const [requests, setRequests] = useState<Access[]>([]);
  const [tokens, setTokens] = useState<Token[]>([]);
  const [fresh, setFresh] = useState('');
  const [error, setError] = useState('');
  const [projectID, setProjectID] = useState('');
  const [action, setAction] = useState('reveal');
  const [ttl, setTtl] = useState(30);
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [armed, setArmed] = useState('');

  useEffect(() => {
    if (!armed) return;
    const timer = window.setTimeout(() => setArmed(''), 5000);
    return () => window.clearTimeout(timer);
  }, [armed]);

  const load = useCallback(() => {
    api.get<{ projects: Project[] }>(`/api/v1/orgs/${orgSlug}/projects`).then((d) => {
      setProjects(d.projects);
      setProjectID((cur) => cur || d.projects[0]?.id || '');
    }).catch((e) => setError(e instanceof ApiError ? e.message : '无法加载项目'));
    api.get<{ approvals: Approval[] }>(`/api/v1/orgs/${orgSlug}/approvals`).then((d) => setApprovals(d.approvals || [])).catch((e) => setError(e instanceof ApiError ? e.message : '无法加载审批'));
    api.get<{ requests: Access[] }>(`/api/v1/orgs/${orgSlug}/access-requests`).then((d) => setRequests(d.requests || [])).catch((e) => setError(e instanceof ApiError ? e.message : '无法加载访问申请'));
    if (canApprove && showScim) api.get<{ tokens: Token[] }>(`/api/v1/orgs/${orgSlug}/scim/tokens`).then((d) => setTokens(d.tokens || [])).catch((e) => setError(e instanceof ApiError ? e.message : '无法加载 SCIM'));
  }, [orgSlug, canApprove, showScim]);
  useEffect(() => { load(); }, [load]);

  async function toggle(project: string, on: boolean) {
    setError('');
    try {
      await api.patch(`/api/v1/projects/${project}`, { approval_required: on });
      load();
    } catch (e) { setError(e instanceof ApiError ? e.message : '没有更新'); }
  }
  async function act(kind: 'approvals' | 'access-requests', id: string, verb: string) {
    setError('');
    try {
      await api.post(`/api/v1/orgs/${orgSlug}/${kind}/${id}/${verb}`, {});
      load();
    } catch (e) { setError(e instanceof ApiError ? e.message : '没有完成'); }
  }
  async function requestAccess(e: React.FormEvent) {
    e.preventDefault();
    setError('');
    try {
      await api.post(`/api/v1/orgs/${orgSlug}/access-requests`, { project_id: projectID, action, ttl_seconds: ttl * 60, reason });
      setReason('');
      load();
    } catch (err) { setError(err instanceof ApiError ? err.message : '没有提交'); }
  }
  async function mintToken(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget;
    const name = new FormData(form).get('name');
    setError('');
    try {
      const created = await api.post<{ token: string }>(`/api/v1/orgs/${orgSlug}/scim/tokens`, { name, ttl_seconds: 90 * 24 * 3600 });
      setFresh(created.token);
      form.reset();
      load();
    } catch (err) { setError(err instanceof ApiError ? err.message : '没有创建'); }
  }
  async function revokeToken(id: string) {
    setArmed('');
    setBusy(true);
    setError('');
    try {
      await api.del(`/api/v1/orgs/${orgSlug}/scim/tokens/${id}`);
      load();
    } catch (err) { setError(err instanceof ApiError ? err.message : '没有吊销'); }
    finally { setBusy(false); }
  }
  const pending = approvals.filter((a) => a.status === 'pending').length;

  return (
    <div className="phase6 form-stack">
      <div className="section-head">
        <div className="section-head-main">
          <h3>审批与访问</h3>
          <p className="section-hint">
            开启审批的项目里，写入、删除和回滚要另一名管理员批准。限时权限到期后立刻失效。{showScim ? 'SCIM 令牌只显示一次。' : ''}
          </p>
        </div>
        {pending > 0 && <Pill tone="warn" icon="clock"><span className="mono">{pending}</span> 项待批</Pill>}
      </div>
      {error && <Notice tone="danger">{error}</Notice>}

      {canApprove && (
        <Section title="变更审批开关" hint="开启后该项目的写入、删除和回滚不再立即生效，需另一名管理员批准。">
          {projects.length === 0 ? (
            <EmptyState icon="shield" title="没有可配置审批的项目">先创建项目，再决定哪些项目的变更需要第二个人批准。</EmptyState>
          ) : (
            <TableWrap label="项目变更审批">
              <table className="table compact">
                <thead><tr><th>项目</th><th>变更审批</th></tr></thead>
                <tbody>
                  {projects.map((project) => (
                    <tr key={project.id}>
                      <td>
                        <div>{project.name}</div>
                        <div className="mono cell-sub">{project.id}</div>
                      </td>
                      <td>
                        <Button
                          variant={project.approval_required ? 'default' : 'quiet'}
                          className="btn-sm"
                          icon={project.approval_required ? 'lock' : 'unlock'}
                          title={project.approval_required ? '点击关闭变更审批' : '点击开启变更审批'}
                          onClick={() => void toggle(project.id, !project.approval_required)}
                        >
                          {project.approval_required ? '已开启' : '已关闭'}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </TableWrap>
          )}
        </Section>
      )}

      <Section title="待处理的密钥变更" hint="批准即执行该变更（申请人不能批自己的）；拒绝后申请人需要重新提交。">
        {approvals.length === 0 ? (
          <EmptyState icon="audit" title="没有待处理的密钥变更">需要审批的项目里，写入、删除和回滚会出现在这里。</EmptyState>
        ) : (
          <TableWrap label="待处理的密钥变更">
            <table className="table compact">
              <thead><tr><th>密钥</th><th>动作</th><th>状态</th><th>到期</th><th>操作</th></tr></thead>
              <tbody>
                {approvals.map((item) => (
                  <tr key={item.id}>
                    <td>
                      <div className="mono">{item.key}</div>
                      {item.reason && <div className="cell-sub">{item.reason}</div>}
                    </td>
                    <td><span className="act-glyph">{item.action}</span></td>
                    <td><Pill tone={statusTone(item.status)}>{item.status}</Pill></td>
                    <td className="mono cell-sub nowrap" title={timeTitle(item.expires_at)}>{item.expires_at ? fmtTime(item.expires_at) : '—'}</td>
                    <td className="actions">
                      {canApprove && item.status === 'pending' && (
                        <div className="row-actions">
                          <Button className="btn-sm" icon="check" onClick={() => void act('approvals', item.id, 'approve')}>批准</Button>
                          <Button variant="danger" className="btn-sm" icon="x" onClick={() => void act('approvals', item.id, 'reject')}>拒绝</Button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableWrap>
        )}
      </Section>

      <Section title="限时访问" hint="申请一段有明确到期时间的权限；到期后立即失效，提前收回同样立即生效。">
        <form onSubmit={requestAccess} className="identity-form">
          <div className="form-row">
            <Field label="项目" htmlFor="access-project">
              <Select id="access-project" value={projectID} onChange={(e) => setProjectID(e.target.value)}>
                {projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}
              </Select>
            </Field>
            <Field label="动作" htmlFor="access-action">
              <Select id="access-action" value={action} onChange={(e) => setAction(e.target.value)}>
                {['reveal', 'export', 'write', 'delete', 'rollback'].map((item) => <option key={item}>{item}</option>)}
              </Select>
            </Field>
            <Field label="分钟" htmlFor="access-ttl" hint="1–240 分钟。">
              <input id="access-ttl" type="number" min={1} max={240} value={ttl} onChange={(e) => setTtl(Number(e.target.value))} />
            </Field>
            <Field label="原因" htmlFor="access-reason" hint="写清用途，审批人据此判断。">
              <input id="access-reason" value={reason} onChange={(e) => setReason(e.target.value)} maxLength={200} />
            </Field>
          </div>
          <div className="form-actions">
            <Button type="submit" variant="solid" icon="arrow-right">申请</Button>
          </div>
        </form>
        {requests.length === 0 ? (
          <EmptyState icon="key" title="还没有限时访问申请">提交后会出现在这里，批准后按申请分钟数到期。</EmptyState>
        ) : (
          <TableWrap label="限时访问申请">
            <table className="table compact">
              <thead><tr><th>动作</th><th>状态</th><th>到期</th><th>操作</th></tr></thead>
              <tbody>
                {requests.map((item) => (
                  <tr key={item.id}>
                    <td><span className="act-glyph">{item.action}</span></td>
                    <td><Pill tone={statusTone(item.status)}>{item.status}</Pill></td>
                    <td className="mono cell-sub nowrap" title={timeTitle(item.expires_at)}>{item.expires_at ? fmtTime(item.expires_at) : '—'}</td>
                    <td className="actions">
                      {canApprove && item.status === 'pending' && (
                        <Button className="btn-sm" icon="check" onClick={() => void act('access-requests', item.id, 'approve')}>批准</Button>
                      )}
                      {canApprove && item.status === 'approved' && (
                        armed === item.id ? (
                          <div className="row-actions">
                            <Button variant="danger" className="btn-sm" icon="lock" disabled={busy} onClick={() => { setArmed(''); void act('access-requests', item.id, 'revoke'); }}>确认收回</Button>
                            <Button variant="quiet" className="btn-sm" disabled={busy} onClick={() => setArmed('')}>取消</Button>
                          </div>
                        ) : (
                          <Button variant="danger" className="btn-sm" disabled={busy} onClick={() => setArmed(item.id)}>收回</Button>
                        )
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableWrap>
        )}
      </Section>

      {canApprove && showScim && (
        <Section title="SCIM 令牌" hint="端点 /api/v1/scim/v2。令牌属于这个组织，不能按邮箱并入已有账号，也不能授予 owner。">
          <form onSubmit={mintToken} className="inline">
            <Field label="名称" htmlFor="scim-name">
              <input id="scim-name" name="name" required maxLength={80} />
            </Field>
            <Button type="submit" icon="plus">创建令牌</Button>
          </form>
          {tokens.length === 0 ? (
            <EmptyState icon="terminal" title="还没有 SCIM 令牌">创建后只显示一次，请立即写入身份提供方的配置。</EmptyState>
          ) : (
            <TableWrap label="SCIM 令牌">
              <table className="table compact">
                <thead><tr><th>名称</th><th>状态</th><th>到期</th><th>操作</th></tr></thead>
                <tbody>
                  {tokens.map((token) => (
                    <tr key={token.id}>
                      <td><div>{token.name}</div><div className="mono cell-sub">{token.id}</div></td>
                      <td><Pill tone={token.revoked ? 'bad' : 'ok'}>{token.revoked ? '已吊销' : '有效'}</Pill></td>
                      <td className="mono cell-sub nowrap" title={timeTitle(token.expires_at)}>{token.expires_at ? fmtTime(token.expires_at) : '—'}</td>
                      <td className="actions">
                        {!token.revoked && (
                          armed === token.id ? (
                            <div className="row-actions">
                              <Button variant="danger" className="btn-sm" icon="lock" disabled={busy} onClick={() => void revokeToken(token.id)}>确认吊销</Button>
                              <Button variant="quiet" className="btn-sm" disabled={busy} onClick={() => setArmed('')}>取消</Button>
                            </div>
                          ) : (
                            <Button variant="danger" className="btn-sm" disabled={busy} onClick={() => setArmed(token.id)}>吊销</Button>
                          )
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </TableWrap>
          )}
        </Section>
      )}

      {fresh && <Modal title="SCIM 令牌" onClose={() => setFresh('')}>
        <div className="once-secret">
          <h3>这是唯一一次显示。离开后无法再查看。</h3>
          <p className="mono">{fresh}</p>
        </div>
        <div className="form-actions">
          <Button variant="stamp" icon="check" onClick={() => setFresh('')}>我已保存</Button>
        </div>
      </Modal>}
    </div>
  );
}

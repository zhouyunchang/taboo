import { useCallback, useEffect, useState } from 'react';
import { api, ApiError } from '../api';
import type { Project } from '../api';
import Modal from './Modal';

type Approval = { id: string; key: string; action: string; status: string; requester_id: string; reason: string; expires_at: number };
type Access = { id: string; project_id: string; action: string; status: string; requester_id: string; reason: string; ttl_seconds: number; expires_at: number };
type Token = { id: string; name: string; revoked: boolean; expires_at: number };

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

  return (
    <div className="phase6">
      <h3>审批与访问</h3>
      <p className="muted">开启审批的项目里，写入、删除和回滚要另一名管理员批准。限时权限到期后立刻失效。{showScim ? 'SCIM 令牌只显示一次。' : ''}</p>
      {error && <p role="alert">{error}</p>}
      {canApprove && <div className="table-scroll">
        <table>
          <thead><tr><th>项目</th><th>变更审批</th></tr></thead>
          <tbody>
            {projects.map((project) => (
              <tr key={project.id}>
                <td>{project.name}</td>
                <td><button type="button" onClick={() => void toggle(project.id, !project.approval_required)}>{project.approval_required ? '已开启' : '已关闭'}</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>}
      <h3>待处理的密钥变更</h3>
      <div className="table-scroll">
        <table>
          <thead><tr><th>密钥</th><th>动作</th><th>状态</th><th></th></tr></thead>
          <tbody>
            {approvals.length === 0 && <tr><td colSpan={4}>没有审批</td></tr>}
            {approvals.map((item) => (
              <tr key={item.id}>
                <td>{item.key}</td>
                <td>{item.action}</td>
                <td>{item.status}</td>
                <td>{canApprove && item.status === 'pending' && <>
                  <button type="button" onClick={() => void act('approvals', item.id, 'approve')}>批准</button>
                  <button type="button" className="ghost" onClick={() => void act('approvals', item.id, 'reject')}>拒绝</button>
                </>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <h3>限时访问</h3>
      <form onSubmit={requestAccess} className="stack">
        <label>项目
          <select value={projectID} onChange={(e) => setProjectID(e.target.value)}>{projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select>
        </label>
        <label>动作
          <select value={action} onChange={(e) => setAction(e.target.value)}>
            {['reveal', 'export', 'write', 'delete', 'rollback'].map((item) => <option key={item}>{item}</option>)}
          </select>
        </label>
        <label>分钟 <input type="number" min={1} max={240} value={ttl} onChange={(e) => setTtl(Number(e.target.value))} /></label>
        <label>原因 <input value={reason} onChange={(e) => setReason(e.target.value)} maxLength={200} /></label>
        <button type="submit">申请</button>
      </form>
      <div className="table-scroll">
        <table>
          <thead><tr><th>动作</th><th>状态</th><th></th></tr></thead>
          <tbody>
            {requests.map((item) => (
              <tr key={item.id}>
                <td>{item.action}</td>
                <td>{item.status}</td>
                <td>{canApprove && item.status === 'pending' && <button type="button" onClick={() => void act('access-requests', item.id, 'approve')}>批准</button>}
                  {canApprove && item.status === 'approved' && <button type="button" className="ghost" onClick={() => void act('access-requests', item.id, 'revoke')}>收回</button>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {canApprove && showScim && <>
        <h3>SCIM</h3>
        <p className="muted">端点 /api/v1/scim/v2。令牌属于这个组织，不能按邮箱并入已有账号，也不能授予 owner。</p>
        <form onSubmit={mintToken}>
          <label>名称 <input name="name" required maxLength={80} /></label>
          <button type="submit">创建令牌</button>
        </form>
        <ul>{tokens.map((token) => <li key={token.id}>{token.name} {token.revoked ? '已吊销' : <button type="button" className="ghost" onClick={() => void api.del(`/api/v1/orgs/${orgSlug}/scim/tokens/${token.id}`).then(load)}>吊销</button>}</li>)}</ul>
      </>}
      {fresh && <Modal title="SCIM 令牌" onClose={() => setFresh('')}>
        <p>这是唯一一次显示。离开后无法再查看。</p>
        <p className="mono" style={{ overflowWrap: 'anywhere' }}>{fresh}</p>
      </Modal>}
    </div>
  );
}

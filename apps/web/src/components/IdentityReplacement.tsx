import { useEffect, useState } from 'react';
import { api, hasBrowserSession } from '../api';
import Modal from './Modal';
import ValidatedForm from './ValidatedForm';
import Icon from '../design/Icon';

type Connection = { id: string; name: string; issuer: string; current: boolean };
export default function IdentityReplacement({ orgSlug, onClose }: { orgSlug: string; onClose: () => void }) {
  const [connections, setConnections] = useState<Connection[]>([]);
  const [selected, setSelected] = useState('');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  const external = hasBrowserSession();
  useEffect(() => {
    let active = true;
    setLoading(true); setError('');
    api.get<{ connections: Connection[] }>(`/api/v1/auth/oidc/${encodeURIComponent(orgSlug)}/connections`)
      .then((d) => { if (active) setConnections(d.connections); })
      .catch((e) => { if (active) setError(e instanceof Error ? e.message : String(e)); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [orgSlug, attempt]);
  async function replace(e: React.FormEvent) {
    e.preventDefault(); if (busy || !confirmed) return;
    setBusy(true); setError('');
    try {
      const d = await api.post<{ authorization_url: string }>(`/api/v1/auth/oidc/${encodeURIComponent(orgSlug)}/replace?pid=${encodeURIComponent(selected)}`, external ? {} : { password, code });
      setPassword(''); setCode('');
      window.location.assign(d.authorization_url);
    } catch (err) { setError(err instanceof Error ? err.message : String(err)); setPassword(''); setCode(''); setBusy(false); }
  }
  const targets = connections.filter((c) => !c.current);
  return <Modal title="更换登录身份" onClose={onClose} busy={busy}>
    <ValidatedForm className="form-stack identity-form" onSubmit={replace}>
      <div className="panel">
        <dl className="meta-list">
          <div className="meta-row">
            <dt>旧身份</dt>
            <dd><Icon name="seal" size={12} /> 当前登录（将被解除）</dd>
          </div>
          <div className="meta-row">
            <dt>新身份</dt>
            <dd className={selected ? 'mono' : undefined}>
              <Icon name="link" size={12} />{' '}
              {selected ? (targets.find((c) => c.id === selected)?.name ?? '已选择') : '尚未选择'}
            </dd>
          </div>
        </dl>
      </div>
      <p className="muted">验证新身份后，将保留你的成员关系和业务数据，解除旧身份关联并退出所有旧会话。新身份不能已关联其他账号。</p>
      {!external && <p className="notice notice-warn"><Icon name="alert" size={13} />本地密码登录将停止；外部身份的秘密读写权限需要组织 owner 明确授予，不会从旧角色自动复制。</p>}
      {loading && <p role="status" className="muted">正在加载本组织的身份连接…</p>}
      {error && <p className="error" role="alert">{error} <button type="button" className="ghost" onClick={() => setAttempt((v) => v + 1)}>重新加载连接</button></p>}
      {!loading && targets.length === 0 && <p className="muted">本组织还没有可更换的身份连接，请联系组织 owner。</p>}
      <label className="field">新的身份连接<select value={selected} onChange={(e) => setSelected(e.target.value)} required disabled={loading || busy}>
        <option value="">请选择</option>{targets.map((c) => <option key={c.id} value={c.id}>{c.name} · {c.issuer}</option>)}
      </select></label>
      {!external && <><label className="field">当前密码<input type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} /></label>
        <label className="field">两步验证码（已启用时必填）<input className="mono" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" value={code} onChange={(e) => setCode(e.target.value)} /></label></>}
      <label className="check"><input type="checkbox" checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} required />我确认解除旧身份关联，并验证新身份</label>
      <div className="form-actions"><button type="button" className="ghost" onClick={onClose} disabled={busy}>取消</button><button type="submit" disabled={busy || loading || !selected || !confirmed}>{busy ? '正在前往验证…' : '验证新身份并更换'}</button></div>
    </ValidatedForm>
  </Modal>;
}

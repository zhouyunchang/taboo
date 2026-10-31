import { useEffect, useState } from 'react';
import { api, ApiError } from '../api';
import Modal from './Modal';

// GitHub 回调 /?github=error&reason=X 的中文说明（reason 见 openapi githubCallback）。
const reasons: Record<string, string> = {
  state_mismatch: '登录请求校验失败（state 不匹配），请重新发起。',
  flow_expired: 'GitHub 登录已超时或在其他标签页重新发起，请重试。',
  missing_code: 'GitHub 未返回授权码，请重试。',
  authorization_denied: '已在 GitHub 取消授权。',
  token_exchange_failed: '与 GitHub 交换授权码失败，请重试。',
  github_api_failed: '暂时无法访问 GitHub，请稍后重试。',
  no_verified_primary_email: '该 GitHub 账号没有已验证的主邮箱，请先在 GitHub 验证邮箱。',
  not_allowed: '该 GitHub 账号不在允许登录的用户 / 组织 / 团队范围内，请联系管理员。',
  not_linked: '该 GitHub 账号尚未绑定 Taboo 账号：请先用密码登录，在「GitHub」中绑定；或联系管理员开启自动创建。',
  email_in_use: '该 GitHub 主邮箱已被其他 Taboo 账号使用：请用该账号登录后在「GitHub」中绑定。',
  account_disabled: '对应的 Taboo 账号已被停用。',
  already_linked: '当前账号已绑定外部身份。',
  already_linked_elsewhere: '该 GitHub 账号已绑定到其他 Taboo 账号。',
  external_identity_exists: '当前账号已绑定其他外部身份，请先解绑。',
  internal: '服务器内部错误，请稍后重试。',
};

export function githubErrorMessage(reason: string | null): string {
  return reasons[reason ?? ''] ?? `GitHub 登录失败（${reason || 'unknown'}）。`;
}

// GitHub 绑定管理（本地账号会话）。
export function GitHubPanel({ onClose }: { onClose: () => void }) {
  const [st, setSt] = useState<{ enabled: boolean; linked: boolean; github_id?: string; has_password: boolean } | null>(null);
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');
  const [busy, setBusy] = useState(false);
  const load = () => api.get<NonNullable<typeof st>>('/api/v1/auth/github/link').then(setSt)
    .catch((e) => setErr(e instanceof ApiError ? e.message : String(e)));
  useEffect(() => { load(); }, []);

  async function link() {
    setErr(''); setBusy(true);
    try {
      const d = await api.post<{ authorization_url: string }>('/api/v1/auth/github/link');
      window.location.href = d.authorization_url;
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); setBusy(false); }
  }
  async function unlink() {
    if (!window.confirm('解绑 GitHub？此账号所有设备上的登录（refresh token）将被吊销。')) return;
    setErr(''); setBusy(true);
    try {
      await api.del('/api/v1/auth/github/link');
      setMsg('已解绑，其他设备需重新登录。'); await load();
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
    finally { setBusy(false); }
  }

  return (
    <Modal title="GitHub 登录" onClose={onClose} busy={busy}>
      <div className="drawer-inner">
        {err && <div className="error" role="alert">{err}</div>}
        {msg && <p role="status">{msg}</p>}
        {!st && !err && <p className="muted">加载中…</p>}
        {st && !st.enabled && <p className="muted">管理员尚未配置 GitHub 登录。</p>}
        {st && st.enabled && !st.linked && (
          <>
            <p className="muted">绑定后可用 GitHub 账号直接登录此 Taboo 账号（按 GitHub 数字 ID 绑定，改用户名不影响）。</p>
            <button onClick={link} disabled={busy}>{busy ? '跳转中…' : '绑定 GitHub'}</button>
          </>
        )}
        {st && st.linked && (
          <>
            <p><span className="tag ok">已绑定</span> <span className="muted">GitHub ID {st.github_id}</span></p>
            {st.has_password
              ? <button onClick={unlink} disabled={busy}>解绑 GitHub</button>
              : <p className="muted">此账号没有密码，解绑会导致无法登录，因此不可解绑。</p>}
          </>
        )}
        <div className="drawer-actions"><button type="button" className="ghost" disabled={busy} onClick={onClose}>关闭</button></div>
      </div>
    </Modal>
  );
}

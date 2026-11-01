import { useState } from 'react';
import { api, ApiError } from '../api';
import type { Org } from '../api';

// 命令行登录确认。授权码只交给回环地址，不写进控制台状态。
export default function CliConsent({ orgs }: { orgs: Org[] }) {
  const q = new URLSearchParams(location.search);
  const device = location.pathname === '/device' || q.has('user_code');
  const [org, setOrg] = useState(orgs[0]?.slug || '');
  const [code, setCode] = useState(q.get('user_code') || '');
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  async function decide(approve: boolean) {
    setBusy(true);
    setError('');
    try {
      const result = await api.post<{ status: string }>('/api/v1/auth/device/decision', { user_code: code, org_slug: org, approve });
      setMessage(result.status === 'approved' ? '已批准，可以回到终端。' : '已拒绝。');
    } catch (e) {
      setError(e instanceof ApiError ? e.message : '没有完成');
    } finally {
      setBusy(false);
    }
  }

  async function authorize(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      const result = await api.post<{ redirect: string }>('/api/v1/auth/cli/authorize', {
        code_challenge: q.get('code_challenge'),
        redirect_uri: q.get('redirect_uri'),
        org_slug: org,
      });
      const target = new URL(result.redirect);
      if (target.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]', '::1'].includes(target.hostname) || target.pathname !== '/callback') {
        setError('服务器返回的地址不是本机回调。');
        return;
      }
      const state = q.get('state');
      if (state) target.searchParams.set('state', state);
      location.href = target.toString();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '没有完成');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="center-screen">
      <section className="card auth-card" aria-label="命令行登录">
        <h2>命令行登录</h2>
        <p className="muted">{device ? '把终端上的代码填在这里，批准后 CLI 会拿到你的用户会话。' : '批准后浏览器会回到本机的 taboo 命令。'}</p>
        <label>组织
          <select value={org} onChange={(e) => setOrg(e.target.value)}>
            {orgs.map((item) => <option key={item.id} value={item.slug}>{item.name}</option>)}
          </select>
        </label>
        {device ? (
          <>
            <label>代码
              <input value={code} onChange={(e) => setCode(e.target.value)} autoComplete="off" spellCheck={false} />
            </label>
            <div className="row">
              <button type="button" disabled={busy || !code || !org} onClick={() => void decide(true)}>批准</button>
              <button type="button" className="ghost" disabled={busy || !code || !org} onClick={() => void decide(false)}>拒绝</button>
            </div>
          </>
        ) : (
          <form onSubmit={authorize}>
            <button type="submit" disabled={busy || !org || !q.get('code_challenge') || !q.get('redirect_uri')}>批准并返回终端</button>
          </form>
        )}
        {message && <p role="status">{message}</p>}
        {error && <p role="alert">{error}</p>}
        <button type="button" className="ghost" onClick={() => { location.href = '/'; }}>回到控制台</button>
      </section>
    </div>
  );
}

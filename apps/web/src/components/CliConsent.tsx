import { useState } from 'react';
import { api, ApiError } from '../api';
import type { Org } from '../api';
import Icon from '../design/Icon';
import ThemeSwitch from '../design/ThemeSwitch';
import { Field, Select } from '../design/primitives';
import VersionTag from './VersionTag';

// 命令行登录确认。授权码只交给回环地址，不写进控制台状态。
export default function CliConsent({ orgs }: { orgs: Org[] }) {
  const q = new URLSearchParams(location.search);
  const device = location.pathname === '/device';
  const [org, setOrg] = useState(orgs[0]?.slug || '');
  // 代码必须手工输入，不从链接预填：带代码的链接可以被别人发来钓鱼。
  const [code, setCode] = useState('');
  const [confirmed, setConfirmed] = useState(false);
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
      if (target.protocol !== 'http:' || !['127.0.0.1', '[::1]', '::1'].includes(target.hostname) || target.pathname !== '/callback') {
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
      <section className="auth-card" aria-label="命令行登录">
        <header className="brand">
          <span className="seal-mark lg" aria-hidden="true">禁</span>
          <div className="brand-text">
            <h2>命令行登录</h2>
            <p className="auth-sub">给终端签发一张会话凭证</p>
          </div>
        </header>

        <p className="auth-sub">{device ? '把终端上的代码填在这里，批准后 CLI 会拿到你的用户会话。' : '批准后浏览器会回到本机的 taboo 命令。'}</p>

        <div className="once-secret">
          <h3><Icon name="terminal" size={14} /> 本次授权</h3>
          <dl className="meta-list">
            <div className="meta-row"><dt>授权方</dt><dd className="mono">taboo-cli</dd></div>
            <div className="meta-row"><dt>空间</dt><dd>
              <Select aria-label="空间" value={org} onChange={(e) => setOrg(e.target.value)}>
                {orgs.map((item) => <option key={item.id} value={item.slug}>{item.kind === 'personal' ? '个人' : item.name}</option>)}
              </Select>
            </dd></div>
            <div className="meta-row"><dt>凭证</dt><dd>{message ? '已裁决' : '待裁决'} · 回传至本机回环地址</dd></div>
          </dl>
          <p className="faint">批准后，终端可以读取该空间内你有权访问的密钥；每次读取记入审计。回传只发往 <span className="mono">127.0.0.1</span> 或 <span className="mono">[::1]</span> 的 <span className="mono">/callback</span>。</p>
        </div>

        {device ? (
          <>
            <p role="note" className="notice notice-warn">
              <Icon name="alert" size={14} />
              <span>只在你自己刚刚在终端运行 <code>taboo login --device</code> 时批准。别人发给你的代码或链接一律拒绝：批准后对方的终端会拿到你在所选组织的权限。</span>
            </p>
            <Field label="代码（照着自己终端上显示的输入）" hint="代码 10 分钟内有效，只能批准一次。">
              <input className="mono" value={code} onChange={(e) => setCode(e.target.value)}
                autoComplete="off" spellCheck={false} placeholder="ABCD-EFGH" />
            </Field>
            <label className="check" style={{ width: '100%' }}>
              <input type="checkbox" aria-label="这个代码来自我自己正在使用的终端"
                checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} />
              <span className="check-text" style={{ flex: 1, minWidth: 0 }}>这个代码来自我自己正在使用的终端</span>
            </label>
            <div className="form-actions split">
              <button type="button" className="btn btn-lg" disabled={busy || !code || !org} onClick={() => void decide(false)}>
                <Icon name="x" size={15} /><span className="btn-label">拒绝</span>
              </button>
              <button type="button" className="btn btn-primary btn-lg" disabled={busy || !code || !org || !confirmed} onClick={() => void decide(true)}>
                <Icon name="check" size={15} /><span className="btn-label">批准</span>
              </button>
            </div>
          </>
        ) : (
          <form onSubmit={authorize} className="form-actions">
            <button type="submit" className="btn btn-primary btn-lg" disabled={busy || !org || !q.get('code_challenge') || !q.get('redirect_uri')}>
              <Icon name="check" size={15} /><span className="btn-label">批准并返回终端</span>
            </button>
          </form>
        )}

        {message && <p role="status" className="notice notice-ok"><Icon name="check" size={13} />{message}</p>}
        {error && <p role="alert" className="error">{error}</p>}

        <footer className="auth-foot">
          <span className="seal-stamp"><Icon name="seal" size={11} />{message ? '已裁决' : '待裁决'}</span>
          <button type="button" className="switch" onClick={() => { location.href = '/'; }}>回到控制台</button>
        </footer>
        <VersionTag />
      </section>
      <ThemeSwitch className="theme-corner" />
    </div>
  );
}

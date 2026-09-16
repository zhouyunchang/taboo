import { useState } from 'react';
import { api, ApiError } from '../api';
import type { User } from '../api';

interface Tokens { access: string; refresh: string }
interface Props { onSuccess: (d: { user: User; tokens: Tokens }) => void }

export default function Login({ onSuccess }: Props) {
  const [mode, setMode] = useState<'login' | 'register'>('login');
  const [email, setEmail] = useState('');
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');
  const [challenge, setChallenge] = useState(''); // 非空 = 进入 2FA 第二步
  const [code, setCode] = useState('');
  const [orgSlug, setOrgSlug] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError('');
    setBusy(true);
    try {
      const path = mode === 'login' ? '/api/v1/auth/login' : '/api/v1/auth/register';
      const d = await api.post<{ user: User; tokens: Tokens; totp_required?: boolean; challenge?: string }>(
        path, mode === 'login' ? { email, password } : { email, password, name });
      if (d.totp_required && d.challenge) {
        setChallenge(d.challenge); // 已开启 2FA：等待二次验证
      } else if (d.tokens) {
        onSuccess(d as { user: User; tokens: Tokens });
      }
    } catch (err) {
      setError(err instanceof ApiError ? `${err.code}: ${err.message}` : String(err));
    } finally {
      setBusy(false);
    }
  }

  async function submit2FA(e: React.FormEvent) {
    e.preventDefault();
    setError('');
    setBusy(true);
    try {
      const d = await api.post<{ user: User; tokens: Tokens }>('/api/v1/auth/totp/login', { challenge, code: code.trim() });
      onSuccess(d);
    } catch (err) {
      setError(err instanceof ApiError ? `${err.code}: ${err.message}` : String(err));
    } finally {
      setBusy(false);
    }
  }

  if (challenge) {
    return (
      <div className="center-screen">
        <form className="card auth-card" onSubmit={submit2FA}>
          <div className="brand">
            <span className="brand-mark">禁</span>
            <div>
              <h1>两步验证</h1>
              <p className="muted">输入验证器上的 6 位动态码，或恢复码</p>
            </div>
          </div>
          <label>
            验证码 / 恢复码
            <input value={code} onChange={(e) => setCode(e.target.value)} required autoFocus
              placeholder="123456 或 abcd-efgh-ijkl" autoComplete="one-time-code" />
          </label>
          {error && <div className="error">{error}</div>}
          <button type="submit" disabled={busy}>{busy ? '验证中…' : '验证并登录'}</button>
          <p className="muted switch" onClick={() => { setChallenge(''); setCode(''); setError(''); }}>
            返回重新登录
          </p>
        </form>
      </div>
    );
  }

  return (
    <div className="center-screen">
      <form className="card auth-card" onSubmit={submit}>
        <div className="brand">
          <span className="brand-mark">禁</span>
          <div>
            <h1>taboo · 禁制</h1>
            <p className="muted">开源自部署密钥管理平台 — MVP</p>
          </div>
        </div>
        <label>
          邮箱
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus />
        </label>
        {mode === 'register' && (
          <label>
            昵称
            <input value={name} onChange={(e) => setName(e.target.value)} />
          </label>
        )}
        <label>
          密码
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
        </label>
        {error && <div className="error">{error}</div>}
        <button type="submit" disabled={busy}>{busy ? '处理中…' : mode === 'login' ? '登录' : '注册'}</button>
        {mode === 'login' && (
          <>
            <hr className="divider" />
            <label>
              组织 Slug（SSO）
              <input value={orgSlug} onChange={(e) => setOrgSlug(e.target.value)} placeholder="my-org" />
            </label>
            <button type="button" className="ghost" disabled={!orgSlug}
              onClick={() => { window.location.href = `/api/v1/auth/oidc/${encodeURIComponent(orgSlug)}/login`; }}>
              使用组织 SSO 登录
            </button>
          </>
        )}
        <p className="muted switch" onClick={() => setMode(mode === 'login' ? 'register' : 'login')}>
          {mode === 'login' ? '没有账号？注册（自动创建个人组织）' : '已有账号？去登录'}
        </p>
      </form>
    </div>
  );
}

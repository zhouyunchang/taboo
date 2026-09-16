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
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError('');
    setBusy(true);
    try {
      const path = mode === 'login' ? '/api/v1/auth/login' : '/api/v1/auth/register';
      const d = await api.post<{ user: User; tokens: Tokens }>(path, mode === 'login' ? { email, password } : { email, password, name });
      onSuccess(d);
    } catch (err) {
      setError(err instanceof ApiError ? `${err.code}: ${err.message}` : String(err));
    } finally {
      setBusy(false);
    }
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
        <p className="muted switch" onClick={() => setMode(mode === 'login' ? 'register' : 'login')}>
          {mode === 'login' ? '没有账号？注册（自动创建个人组织）' : '已有账号？去登录'}
        </p>
      </form>
    </div>
  );
}

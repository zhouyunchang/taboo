import { useEffect, useState } from 'react';
import { api, ApiError } from '../api';
import ValidatedForm from '../components/ValidatedForm';
import type { User } from '../api';
import VersionTag from '../components/VersionTag';
import { githubErrorMessage } from '../components/GitHub';
import Icon from '../design/Icon';
import type { IconName } from '../design/Icon';

interface Tokens { access: string; refresh: string }
interface Props { onSuccess: (d: { user: User; tokens: Tokens }) => void }
interface AuthSource { id: string; name: string; login_url: string }

// 三条承诺即 README 第一段的原话；body 只做一句事实性解释，不扩写产品能力。
const COVENANTS: { icon: IconName; title: string; body: string }[] = [
  { icon: 'lock', title: '密钥永不落盘明文', body: '信封加密：Root Key → 组织 DEK → 值，AES-256-GCM' },
  { icon: 'shield', title: '永不进 Git 仓库', body: '值只在显式显示或导出时出现，浏览器不缓存' },
  { icon: 'audit', title: '全操作可审计', body: 'append-only 审计，谁读了哪个密钥逐条留痕' },
];

/** 门面：左边是那道禁制本身，右边是开门的动作。 */
export default function Login({ onSuccess }: Props) {
  const [mode, setMode] = useState<'login' | 'register'>('login');
  const [email, setEmail] = useState('');
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');
  const [challenge, setChallenge] = useState('');
  const [code, setCode] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [sources, setSources] = useState<AuthSource[]>([]);
  const [registerEnabled, setRegisterEnabled] = useState(true);
  const [passwordEnabled, setPasswordEnabled] = useState(true);
  const [githubEnabled, setGithubEnabled] = useState(false);

  useEffect(() => {
    api.get<{ register_enabled?: boolean; password_enabled?: boolean; github_enabled?: boolean; sources?: AuthSource[] }>('/api/v1/auth/sources')
      .then((d) => {
        setGithubEnabled(d.github_enabled === true);
        setSources(d.sources ?? []);
        setRegisterEnabled(d.register_enabled !== false);
        setPasswordEnabled(d.password_enabled !== false);
      })
      .catch(() => {});
  }, []);

  // GitHub 回调落地：?github=ok 用一次性 ticket cookie 换 token；?github=error 显示原因。
  useEffect(() => {
    const q = new URLSearchParams(window.location.search);
    const gh = q.get('github');
    if (!gh) return;
    window.history.replaceState({}, '', '/');
    if (gh === 'error') { setError(githubErrorMessage(q.get('reason'))); return; }
    if (gh !== 'ok') return;
    setBusy(true);
    api.post<{ user: User; tokens: Tokens; totp_required?: boolean; challenge?: string }>('/api/v1/auth/github/session')
      .then((d) => {
        if (d.totp_required && d.challenge) setChallenge(d.challenge);
        else if (d.tokens) onSuccess(d as { user: User; tokens: Tokens });
      })
      .catch((err) => setError(err instanceof ApiError ? `GitHub 登录失败：${err.message}` : String(err)))
      .finally(() => setBusy(false));
  }, []);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError('');
    setBusy(true);
    try {
      const path = mode === 'login' ? '/api/v1/auth/login' : '/api/v1/auth/register';
      const d = await api.post<{ user: User; tokens: Tokens; totp_required?: boolean; challenge?: string }>(
        path, mode === 'login' ? { email, password } : { email, password, name });
      if (d.totp_required && d.challenge) {
        setChallenge(d.challenge);
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

  const showPassword = passwordEnabled && (mode === 'login' || registerEnabled);
  const showRegisterToggle = registerEnabled && passwordEnabled;
  const hasIdentity = sources.length > 0 || githubEnabled;

  return (
    <div className="auth-shell">
      <section className="auth-lead" aria-label="taboo 禁制">
        <div className="section">
          <div className="auth-brand">
            <span className="seal-mark lg" aria-hidden="true">禁</span>
            <div className="auth-wordmark">
              <span className="zh">禁制</span>
              <span className="en">TABOO · 洞府有禁</span>
            </div>
          </div>

          <p className="auth-caps">密钥管理 · 自部署 · 单二进制</p>
          <p className="auth-principle">
            密钥<strong>永不落盘明文</strong>，<strong>永不进 Git 仓库</strong>，<strong>全操作可审计</strong>。
          </p>
          <ul className="auth-covenants">
            {COVENANTS.map((item) => (
              <li key={item.title}>
                <Icon name={item.icon} size={14} />
                <span><strong>{item.title}</strong><span className="faint"> — {item.body}</span></span>
              </li>
            ))}
          </ul>
        </div>

        <div className="section">
          <div className="auth-brand">
            <span className="seal-stamp"><Icon name="seal" size={11} /> MIT</span>
            <span className="auth-caps">无 open-core 付费墙</span>
          </div>
          <p className="faint">藏有重宝的洞府必有禁制：外人不可见、不可触，擅动者必被察觉。</p>
        </div>
      </section>

      <section className="auth-aside" aria-label="登录">
        <div className="auth-card">
          {challenge ? (
            <>
              <header className="brand">
                <span className="seal-mark" aria-hidden="true">禁</span>
                <div className="brand-text">
                  <h2>两步验证</h2>
                  <p className="auth-sub">输入验证器上的 6 位动态码，或一枚恢复码</p>
                </div>
              </header>
              <ValidatedForm className="form-stack" onSubmit={submit2FA}>
                <label className="field">
                  <span className="field-label">验证码 / 恢复码</span>
                  <input className="mono" value={code} onChange={(e) => setCode(e.target.value)} required autoFocus
                    placeholder="123456 或 abcd-efgh-ijkl" autoComplete="one-time-code" />
                </label>
                {error && <p className="error" role="alert">{error}</p>}
                <button type="submit" className="btn btn-primary btn-lg btn-block" disabled={busy}>
                  {busy ? '验证中…' : '验证并登录'}
                </button>
                <button type="button" className="switch" onClick={() => { setChallenge(''); setCode(''); setError(''); }}>
                  返回重新登录
                </button>
              </ValidatedForm>
              <footer className="auth-foot"><VersionTag /></footer>
            </>
          ) : (
            <>
              <header className="brand">
                <span className="seal-mark" aria-hidden="true">禁</span>
                <div className="brand-text">
                  <h2>taboo · 禁制</h2>
                  <p className="auth-sub">进入即被记录：每一次读取都会留痕。</p>
                </div>
              </header>

              {hasIdentity && mode === 'login' && (
                <div className="sso-block">
                  {githubEnabled && (
                    <button type="button" className="sso-btn" disabled={busy} onClick={() => { window.location.href = '/api/v1/auth/github/login'; }}>
                      <Icon name="globe" size={14} />
                      使用 GitHub 登录
                    </button>
                  )}
                  {sources.map((s) => (
                    <button key={s.id} type="button" className="sso-btn" onClick={() => { window.location.href = s.login_url; }}>
                      <Icon name="link" size={14} />
                      使用 {s.name} 登录
                    </button>
                  ))}
                  {showPassword && <p className="sso-or">或用本地账号</p>}
                </div>
              )}

              {showPassword && (
                <ValidatedForm className="form-stack" onSubmit={submit}>
                  <label className="field">
                    <span className="field-label">邮箱</span>
                    <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus
                      placeholder="you@example.com" autoComplete="email" />
                  </label>
                  {mode === 'register' && (
                    <label className="field">
                      <span className="field-label">昵称</span>
                      <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="nickname" />
                    </label>
                  )}
                  <label className="field">
                    <span className="field-label">密码</span>
                    <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8}
                      autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
                      placeholder={mode === 'register' ? '至少 8 位' : undefined} />
                  </label>
                  {error && <p className="error" role="alert">{error}</p>}
                  <button type="submit" className="btn btn-primary btn-lg btn-block" disabled={busy}>
                    {busy ? (mode === 'login' ? '登录中…' : '注册中…') : mode === 'login' ? '登录' : '注册'}
                  </button>
                </ValidatedForm>
              )}

              {!showPassword && error && <p className="error" role="alert">{error}</p>}
              {!passwordEnabled && sources.length === 0 && !githubEnabled && (
                <p className="notice notice-warn">未配置外部用户源，且已关闭密码登录。请联系管理员配置身份连接。</p>
              )}

              {mode === 'register' && showPassword && (
                <p className="auth-sub">注册即建个人组织，并自动生成默认项目的 dev / staging / prod 三个环境。</p>
              )}

              {showRegisterToggle && (
                <button type="button" className="switch" onClick={() => setMode(mode === 'login' ? 'register' : 'login')}>
                  {mode === 'login' ? '没有账号？注册' : '已有账号？去登录'}
                </button>
              )}

              <footer className="auth-foot">
                <VersionTag />
              </footer>
            </>
          )}
        </div>
      </section>
    </div>
  );
}

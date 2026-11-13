import { useEffect, useState } from 'react';
import { api, ApiError } from '../api';
import ValidatedForm from '../components/ValidatedForm';
import type { User } from '../api';
import VersionTag from '../components/VersionTag';
import { githubErrorMessage } from '../components/GitHub';
import Icon from '../design/Icon';
import ThemeSwitch from '../design/ThemeSwitch';

interface Tokens { access: string; refresh: string }
interface Props { onSuccess: (d: { user: User; tokens: Tokens }) => void }
interface AuthSource { id: string; name: string; login_url: string }

// 三条承诺即 README 第一段的原话；body 只做一句事实性解释，不扩写产品能力。
const COVENANTS: { title: string; body: string }[] = [
  { title: '密钥永不落盘明文', body: '信封加密：Root Key → 组织 DEK → 值，AES-256-GCM' },
  { title: '永不进 Git 仓库', body: '值只在显式显示或导出时出现，浏览器不缓存' },
  { title: '全操作可审计', body: 'append-only 审计，谁读了哪个密钥逐条留痕' },
];

// 竖排的条目序号：古书以「一、二、三」起条，不用现代项目符号。
const NUMERALS = ['一', '二', '三'] as const;

/** 门面：左边是开门的动作，右边是那道禁制本身（仿古籍竖排书页）。 */
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
      <section className="auth-aside" aria-label="登录">
        <div className="auth-card">
          {challenge ? (
            <>
              <header className="brand">
                <span className="seal-mark" aria-hidden="true">禁</span>
                <div className="brand-text">
                  <h2><span className="cjk">两步验证</span></h2>
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
              <footer className="auth-foot"><VersionTag /><ThemeSwitch /></footer>
            </>
          ) : (
            <>
              <header className="brand">
                <span className="seal-mark" aria-hidden="true">禁</span>
                <div className="brand-text">
                  <h2>taboo · <span className="cjk">禁制</span></h2>
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

              {showRegisterToggle && (
                <button type="button" className="switch" onClick={() => setMode(mode === 'login' ? 'register' : 'login')}>
                  {mode === 'login' ? '没有账号？注册' : '已有账号？去登录'}
                </button>
              )}

              <footer className="auth-foot">
                <VersionTag />
                <ThemeSwitch />
              </footer>
            </>
          )}
        </div>
      </section>

      {/* 古籍书页：版框、界行、竖排；从左向右分列，每列自上而下。 */}
      <section className="auth-lead" aria-label="taboo 禁制">
        <div className="auth-book">
          <div className="auth-book-spine">
            <div className="auth-book-spine-top">
              <span className="auth-book-line"><span className="auth-book-title">禁制</span></span>
              <span className="auth-book-line"><span className="auth-book-imprint">TABOO</span></span>
              <span className="auth-book-line"><span className="auth-book-motto">洞府有禁</span></span>
            </div>
            <span className="auth-book-kicker">密钥管理 · 自部署 · 单二进制</span>
          </div>
          <div className="auth-book-body">
            <p className="auth-book-text">
              <span className="auth-book-principle">
                密钥<strong>永不落盘明文</strong>，<strong>永不进 Git 仓库</strong>，<strong>全操作可审计</strong>。
              </span>
              {COVENANTS.map((item, i) => (
                <span className="auth-book-item" key={item.title}>
                  {'　'}<span className="auth-book-num">{NUMERALS[i]} </span><strong>{item.title}</strong>
                  <span className="auth-book-note"> — {item.body}</span>
                </span>
              ))}
              {'　'}<span className="auth-book-prose">藏有重宝的洞府必有禁制：外人不可见、不可触，擅动者必被察觉。</span>
            </p>
            <p className="auth-book-colophon">MIT · 无 open-core 付费墙</p>
          </div>
          <span className="seal-mark lg auth-book-seal" aria-hidden="true">禁</span>
        </div>
      </section>
    </div>
  );
}

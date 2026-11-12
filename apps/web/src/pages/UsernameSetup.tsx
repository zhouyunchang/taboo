import { useState } from 'react';
import type { FormEvent } from 'react';
import { api, ApiError } from '../api';
import type { User, Org } from '../api';
import Icon from '../design/Icon';
import { codeIssue, normalizeCode } from '../pathcode';

// 与 server-go/internal/pathcode 的规则保持一致：定位符第一段是小写字母、
// 数字和中划线，首尾不能是中划线；保留词不能用作第一段。
function handleProblem(handle: string): string {
  switch (codeIssue(handle)) {
    case 'empty': return '用户名不能为空。';
    case 'long': return '用户名最多 39 个字符。';
    case 'reserved': return `「${handle}」是保留字，换一个。`;
    case 'chars': return '只能用小写字母、数字和中划线，首尾不能是中划线。';
    default: return '';
  }
}

function describeError(err: unknown, handle: string): string {
  if (err instanceof ApiError) {
    if (err.code === 'CODE_TAKEN') return `「${handle}」已被占用，换一个再试。`;
    if (err.code === 'INVALID_CODE') return '用户名格式不对：1–39 位，小写字母、数字或中划线。';
    if (err.code === 'HANDLE_LOCKED') return '用户名已经确认，不能再修改。';
    return `${err.code}: ${err.message}`;
  }
  return err instanceof Error ? err.message : String(err);
}

interface Props {
  user: User;
  onDone: (user: User, orgs: Org[]) => void;
  onLogout: () => void;
  logoutError?: string;
}

/** 入府前落款：注册后、进入控制台前，先让用户确认自己的用户名（定位符第一段）。 */
export default function UsernameSetup({ user, onDone, onLogout, logoutError }: Props) {
  const initial = normalizeCode(user.handle ?? '');
  const [value, setValue] = useState(initial);
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const handle = normalizeCode(value);
  const problem = handleProblem(handle);
  const dirty = handle !== initial;
  const shown = handle || 'username';
  const formatError = touched && problem ? problem : '';

  async function submit(event: FormEvent) {
    event.preventDefault();
    setTouched(true);
    setError('');
    if (problem || busy) return;
    setBusy(true);
    try {
      if (dirty) await api.patch('/api/v1/me/handle', { handle });
      await api.post('/api/v1/me/handle/confirm');
      const me = await api.get<{ user: User; orgs: Org[] }>('/api/v1/me');
      onDone(me.user, me.orgs);
    } catch (err) {
      setError(describeError(err, handle));
      setBusy(false);
    }
  }

  return (
    <div className="onboard-shell">
      <main className="onboard-card" aria-labelledby="onboard-title">
        <header className="onboard-head">
          <span className="seal-mark lg" aria-hidden="true">禁</span>
          <div className="onboard-head-text">
            <span className="onboard-brand">taboo · <span className="cjk">禁制</span></span>
            <span className="onboard-kicker">入府 · 落款</span>
          </div>
        </header>

        <div className="onboard-intro">
          <h1 id="onboard-title">定下你的用户名</h1>
          <p className="onboard-lead">
            {initial
              ? '已按你的账号前缀生成了一个用户名，可以直接用，也可以改。'
              : '第一次进来，先给自己取一个名字。'}
            它是个人空间地址的第一段，也会写进每一次读取的审计记录。
          </p>
        </div>

        <form className="onboard-form" onSubmit={submit} noValidate>
          <div className="field">
            <label className="field-label-wrap" htmlFor="onboard-handle">
              <span className="field-label">用户名</span>
            </label>
            <div className={`handle-input ${formatError ? 'is-invalid' : ''}`}>
              <span className="handle-prefix" aria-hidden="true">/</span>
              <input id="onboard-handle" aria-label="用户名" className="mono" value={value}
                autoFocus autoComplete="off" autoCapitalize="off" spellCheck={false} maxLength={39}
                aria-describedby="onboard-handle-hint"
                aria-invalid={formatError ? true : undefined}
                onChange={(e) => setValue(e.target.value)} onBlur={() => setTouched(true)} />
            </div>
            <span className="field-hint" id="onboard-handle-hint">
              1–39 位，小写字母、数字或中划线；首尾不能是中划线。
            </span>
          </div>

          <div className="onboard-preview" aria-live="polite">
            <span className="field-label">个人空间地址</span>
            <code className="onboard-locator">
              <strong>{shown}</strong><span className="onboard-sep">/</span>default<span className="onboard-sep">/</span><span className="faint">目录 / 密钥</span>
            </code>
            <span className="field-hint">定位符按 / 切分：归属 / 项目 / 目录。第一段就是你。</span>
          </div>

          {formatError && <p className="error" role="alert">{formatError}</p>}
          {error && <p className="error" role="alert">{error}</p>}
          {logoutError && <p className="error" role="alert">{logoutError}</p>}

          <div className="onboard-actions">
            <button type="submit" className="btn btn-primary btn-lg btn-block" disabled={busy || !!problem}>
              <Icon name="seal" size={14} />
              <span className="btn-label">{busy ? '落款中…' : '确认用户名，进入控制台'}</span>
            </button>
            <button type="button" className="switch" onClick={onLogout} disabled={busy}>退出登录</button>
          </div>
        </form>
      </main>
    </div>
  );
}

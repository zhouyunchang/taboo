import { useEffect, useState } from 'react';
import { api, getToken, setTokens, initializeBrowserSession, logout, checkSessionContinuity, invalidateRequests, announceSession } from './api';
import type { User, Org } from './api';
import Login from './pages/Login';
import Workspace from './pages/Workspace';
import CliConsent from './components/CliConsent';
import Icon from './design/Icon';
import { Skeleton } from './design/primitives';
import VersionTag from './components/VersionTag';

/** 启动面：先立印，再读库。不是一句「加载中…」，而是同一道门的骨架。 */
function BootSurface({ label = '正在检查登录状态' }: { label?: string }) {
  return (
    <div className="auth-shell">
      <div className="auth-card" role="status" aria-live="polite">
        <div className="auth-brand">
          <span className="seal-mark lg" aria-hidden="true">禁</span>
          <div className="auth-wordmark">
            <span className="zh">禁制</span>
            <span className="en">TABOO</span>
          </div>
        </div>
        <p className="auth-sub">{label}</p>
        <Skeleton rows={3} />
        <p className="faint mono">
          <Icon name="lock" size={13} />
          正在核对会话与组织边界
        </p>
      </div>
    </div>
  );
}

export default function App() {
  const [user, setUser] = useState<User | null>(null);
  const [orgs, setOrgs] = useState<Org[]>([]);
  const [selectedOrg, setSelectedOrg] = useState('');
  const [changed, setChanged] = useState(false);
  const [loading, setLoading] = useState(true);

  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (new URLSearchParams(location.search).has('signed_in')) { announceSession(); history.replaceState({}, '', '/'); }
    const changed = () => { setChanged(true); setUser(null); setOrgs([]); };
    window.addEventListener('taboo:session-changed', changed);
    return () => window.removeEventListener('taboo:session-changed', changed);
  }, []);
  useEffect(() => {
    if (!user || changed) return;
    let live=true, running=false;
    const refresh=async()=>{
      if(running || document.visibilityState !== 'visible')return;
      running=true;
      try {
        await checkSessionContinuity();
        const d=await api.get<{user:User;orgs:Org[]}>('/api/v1/me');
        if(live){setUser(d.user);setOrgs(d.orgs); window.dispatchEvent(new Event('taboo:permissions-refresh'));}
      } catch { if(live) window.dispatchEvent(new Event('taboo:verification-unavailable')); }
      finally {running=false;}
    };
    const timer=setInterval(refresh,30000);
    window.addEventListener('focus',refresh);
    document.addEventListener('visibilitychange',refresh);
    return()=>{live=false;clearInterval(timer);window.removeEventListener('focus',refresh);document.removeEventListener('visibilitychange',refresh);};
  },[user?.id,changed]);
  useEffect(() => {
    let active = true;
    const expired = () => { setUser(null); setOrgs([]); };
    window.addEventListener('taboo:logout', expired);
    setLoading(true);
    setError('');
    (async () => {
      const browser = await initializeBrowserSession();
      if (!browser && !getToken()) return;
      const d = await api.get<{ user: User; orgs: Org[] }>('/api/v1/me');
      if (!active) return;
      setUser(d.user);
      setOrgs(d.orgs);
      const invite = window.location.pathname.startsWith('/invite/')
        ? window.location.pathname.slice('/invite/'.length) : '';
      if (invite && !browser) {
        await api.post(`/api/v1/invites/${invite}/accept`);
        const m = await api.get<{ orgs: Org[] }>('/api/v1/me');
        if (active) setOrgs(m.orgs);
        window.history.replaceState({}, '', '/');
      }
    })().catch((e) => { if (active) setError(e instanceof Error ? e.message : '暂时无法检查登录状态。'); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; window.removeEventListener('taboo:logout', expired); };
  }, [attempt]);

  if (changed) return (
    <div className="center-screen">
      <section className="card auth-card" aria-label="登录身份已变化">
        <header className="brand">
          <span className="seal-mark" aria-hidden="true">禁</span>
          <div className="brand-text">
            <h2>登录身份已变化</h2>
            <p className="auth-sub">这道门现在属于另一个身份。</p>
          </div>
        </header>
        <p role="alert">其他页面已登录、切换身份或退出。当前秘密和表单已清空，请重新加载确认当前组织。</p>
        <button type="button" className="btn btn-primary btn-block" onClick={() => location.reload()}>
          <Icon name="refresh" size={14} />
          <span className="btn-label">重新加载</span>
        </button>
      </section>
    </div>
  );

  if (loading) return <BootSurface />;

  if (!user && error) return (
    <div className="center-screen">
      <section className="card auth-card" aria-label="登录状态">
        <header className="brand">
          <span className="seal-mark" aria-hidden="true">禁</span>
          <div className="brand-text">
            <h2>无法确认登录状态</h2>
            <p className="auth-sub">会话检查没有完成，没有读取任何密钥。</p>
          </div>
        </header>
        <p role="alert">{error}</p>
        <button type="button" className="btn btn-primary btn-block" onClick={() => setAttempt((v) => v + 1)}>
          <Icon name="refresh" size={14} />
          <span className="btn-label">重试</span>
        </button>
        <footer className="auth-foot"><VersionTag /></footer>
      </section>
    </div>
  );

  const cliLogin = location.pathname === '/device' || location.pathname === '/cli/login';

  if (!user) {
    return (
      <Login
        onSuccess={(d) => {
          setTokens(d.tokens);
          setUser(d.user);
          const invite = window.location.pathname.startsWith('/invite/')
            ? window.location.pathname.slice('/invite/'.length)
            : '';
          const go = () => api.get<{ user: User; orgs: Org[] }>('/api/v1/me').then((m) => { setUser(m.user); setOrgs(m.orgs); });
          if (invite) {
            api.post(`/api/v1/invites/${invite}/accept`).then(() => { window.history.replaceState({}, '', '/'); go(); }).catch(go);
          } else {
            go();
          }
        }}
      />
    );
  }

  if (cliLogin) return <CliConsent orgs={orgs} />;

  return (
    <Workspace
      key={`${orgs.find(o=>o.id===selectedOrg)?.id || orgs[0]?.id}:${orgs.find(o=>o.id===selectedOrg)?.role || orgs[0]?.role}`}
      user={user}
      onUser={setUser}
      orgs={selectedOrg ? [...orgs.filter(o=>o.id===selectedOrg),...orgs.filter(o=>o.id!==selectedOrg)] : orgs}
      onOrgChange={(next) => { invalidateRequests(); setOrgs(next); setSelectedOrg(next[0]?.id ?? ''); }}
      logoutError={error}
      onLogout={() => {
        setError('');
        logout().then(() => { setUser(null); setOrgs([]); })
          .catch(() => setError('退出未完成，请检查网络后重试。'));
      }}
    />
  );
}

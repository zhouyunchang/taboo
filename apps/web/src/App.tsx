import { useEffect, useState } from 'react';
import { api, getToken, setTokens, initializeBrowserSession, logout, checkSessionContinuity, invalidateRequests, announceSession } from './api';
import type { User, Org } from './api';
import Login from './pages/Login';
import Workspace from './pages/Workspace';
import UsernameSetup from './pages/UsernameSetup';
import Organizations from './pages/Organizations';
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
  const [changed, setChanged] = useState(false);
  // 全屏管理页与工作台都是同一会话下的两个入口；/organizations 可直接刷新进入。
  const [view, setView] = useState<'workspace' | 'organizations'>(() =>
    location.pathname === '/organizations' ? 'organizations' : 'workspace');
  const [loading, setLoading] = useState(true);
  // 登录成功到 /me 返回之间的过渡：避免用不完整的登录响应先画出控制台。
  const [entering, setEntering] = useState(false);

  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (new URLSearchParams(location.search).has('signed_in')) { announceSession(); history.replaceState({}, '', '/'); }
    const changed = () => { setChanged(true); setUser(null); setOrgs([]); };
    const route = () => setView(location.pathname === '/organizations' ? 'organizations' : 'workspace');
    window.addEventListener('taboo:session-changed', changed);
    window.addEventListener('popstate', route);
    return () => { window.removeEventListener('taboo:session-changed', changed); window.removeEventListener('popstate', route); };
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
        if(live){
          setUser(d.user);
          // 刷新 /me 时把当前空间留在第一位：组织页与工作台都以 orgs[0] 为当前上下文。
          setOrgs((prev) => {
            const currentId = prev[0]?.id;
            const current = currentId ? d.orgs.find((item) => item.id === currentId) : undefined;
            return current ? [current, ...d.orgs.filter((item) => item.id !== currentId)] : d.orgs;
          });
          window.dispatchEvent(new Event('taboo:permissions-refresh'));
        }
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

  function goWorkspace() {
    if (location.pathname !== '/') history.pushState({}, '', '/');
    setView('workspace');
  }

  function goOrganizations() {
    if (location.pathname !== '/organizations') history.pushState({}, '', '/organizations');
    setView('organizations');
  }

  /** 从管理页进入某个空间：把它排到第一位，工作台的 key 随之变化。 */
  function enterSpace(orgId: string) {
    const target = orgs.find((item) => item.id === orgId);
    if (!target) return;
    invalidateRequests();
    setOrgs([target, ...orgs.filter((item) => item.id !== orgId)]);
    goWorkspace();
  }

  function signOut() {
    setError('');
    logout().then(() => {
      setUser(null); setOrgs([]);
      if (location.pathname !== '/') history.pushState({}, '', '/');
      setView('workspace');
    }).catch(() => setError('退出未完成，请检查网络后重试。'));
  }

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

  if (entering) return <BootSurface label="正在进入控制台" />;

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
          setEntering(true);
          const invite = window.location.pathname.startsWith('/invite/')
            ? window.location.pathname.slice('/invite/'.length)
            : '';
          const go = () => api.get<{ user: User; orgs: Org[] }>('/api/v1/me')
            .then((m) => { setUser(m.user); setOrgs(m.orgs); })
            .catch((e) => { setError(e instanceof Error ? e.message : '暂时无法进入控制台。'); })
            .finally(() => setEntering(false));
          if (invite) {
            api.post(`/api/v1/invites/${invite}/accept`).then(() => { window.history.replaceState({}, '', '/'); go(); }).catch(go);
          } else {
            go();
          }
        }}
      />
    );
  }

  // 每次进入都按服务端最新状态判断（登录后的 /me、启动时的 /me、30s 刷新）：
  // 未确认用户名就先落款，不因「注册过一次」而放行，也不在首页留提示条。
  if (user.handle_confirmed === false) {
    return (
      <UsernameSetup
        user={user}
        onDone={(nextUser, nextOrgs) => { setUser(nextUser); setOrgs(nextOrgs); }}
        onLogout={signOut}
        logoutError={error}
      />
    );
  }

  if (cliLogin) return <CliConsent orgs={orgs} />;

  if (view === 'organizations') {
    return <Organizations
      user={user}
      orgs={orgs}
      onOrgsChange={setOrgs}
      onEnter={enterSpace}
      onBack={goWorkspace}
    />;
  }

  return (
    <Workspace
      key={`${orgs[0]?.id}:${orgs[0]?.role}`}
      user={user}
      orgs={orgs}
      onManageOrgs={goOrganizations}
      logoutError={error}
      onLogout={signOut}
    />
  );
}

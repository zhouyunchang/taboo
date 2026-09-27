import { useEffect, useState } from 'react';
import { api, getToken, setTokens, initializeBrowserSession, logout } from './api';
import type { User, Org } from './api';
import Login from './pages/Login';
import Workspace from './pages/Workspace';

export default function App() {
  const [user, setUser] = useState<User | null>(null);
  const [orgs, setOrgs] = useState<Org[]>([]);
  const [loading, setLoading] = useState(true);

  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
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

  if (loading) return <div className="center-screen">加载中…</div>;

  if (!user && error) return <div className="center-screen"><section className="card auth-card" aria-label="登录状态">
    <p role="alert">{error}</p><button onClick={() => setAttempt((v) => v + 1)}>重试</button>
  </section></div>;

  if (!user) {
    return (
      <Login
        onSuccess={(d) => {
          setTokens(d.tokens);
          setUser(d.user);
          const invite = window.location.pathname.startsWith('/invite/')
            ? window.location.pathname.slice('/invite/'.length)
            : '';
          const go = () => api.get<{ orgs: Org[] }>('/api/v1/me').then((m) => setOrgs(m.orgs));
          if (invite) {
            api.post(`/api/v1/invites/${invite}/accept`).then(() => { window.history.replaceState({}, '', '/'); go(); }).catch(go);
          } else {
            go();
          }
        }}
      />
    );
  }

  return (
    <Workspace
      user={user}
      orgs={orgs}
      onOrgChange={(o) => setOrgs(o)}
      logoutError={error}
      onLogout={() => {
        setError('');
        logout().then(() => { setUser(null); setOrgs([]); })
          .catch(() => setError('退出未完成，请检查网络后重试。'));
      }}
    />
  );
}

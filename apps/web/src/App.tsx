import { useEffect, useState } from 'react';
import { api, getToken, setTokens, clearTokens } from './api';
import type { User, Org } from './api';
import Login from './pages/Login';
import Workspace from './pages/Workspace';

export default function App() {
  const [user, setUser] = useState<User | null>(null);
  const [orgs, setOrgs] = useState<Org[]>([]);
  const [loading, setLoading] = useState(!!getToken());

  useEffect(() => {
    window.addEventListener('taboo:logout', () => setUser(null));
    const invite = window.location.pathname.startsWith('/invite/')
      ? window.location.pathname.slice('/invite/'.length)
      : '';
    if (getToken()) {
      const afterMe = (d: { user: User; orgs: Org[] }) => {
        setUser(d.user);
        setOrgs(d.orgs);
        if (invite) {
          api.post(`/api/v1/invites/${invite}/accept`)
            .then(() => api.get<{ orgs: Org[] }>('/api/v1/me').then((m) => { setOrgs(m.orgs); window.history.replaceState({}, '', '/'); }))
            .catch(() => {});
        }
      };
      api.get<{ user: User; orgs: Org[] }>('/api/v1/me')
        .then(afterMe)
        .catch(() => clearTokens())
        .finally(() => setLoading(false));
    }
  }, []);

  if (loading) return <div className="center-screen">加载中…</div>;

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
      onLogout={() => {
        const idp = localStorage.getItem('taboo.idp_logout');
        clearTokens();
        localStorage.removeItem('taboo.idp_logout');
        setUser(null);
        if (idp) window.location.href = idp;
      }}
    />
  );
}

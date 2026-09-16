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
    if (getToken()) {
      api.get<{ user: User; orgs: Org[] }>('/api/v1/me')
        .then((d) => { setUser(d.user); setOrgs(d.orgs); })
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
          api.get<{ orgs: Org[] }>('/api/v1/me').then((m) => setOrgs(m.orgs));
        }}
      />
    );
  }

  return (
    <Workspace
      user={user}
      orgs={orgs}
      onOrgChange={(o) => setOrgs(o)}
      onLogout={() => { clearTokens(); setUser(null); }}
    />
  );
}

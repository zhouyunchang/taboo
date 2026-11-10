import { useEffect, useState } from 'react';
import { api, ApiError } from '../api';
import { fmtTime, timeTitle } from '../time';

type Policy = {
  interval_seconds: number;
  cron: string;
  enabled: boolean;
  next_run_at: number;
  last_run_at: number;
};

export default function RotationPolicy({ projectId, folder, secretKey, canWrite }: {
  projectId: string; folder: string; secretKey: string; canWrite: boolean;
}) {
  const endpoint = `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}/rotation?path=${encodeURIComponent(folder)}`;
  const [policy, setPolicy] = useState<Policy | null>(null);
  const [mode, setMode] = useState<'interval' | 'cron'>('interval');
  const [hours, setHours] = useState(24);
  const [cron, setCron] = useState('0 3 * * *');
  const [enabled, setEnabled] = useState(true);
  const [err, setErr] = useState('');
  const [status, setStatus] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api.get<Policy>(endpoint)
      .then((item) => {
        setPolicy(item);
        setEnabled(item.enabled);
        if (item.cron) { setMode('cron'); setCron(item.cron); }
        else { setMode('interval'); setHours(Math.max(1, Math.round(item.interval_seconds / 3600))); }
      })
      .catch((e) => { if (!(e instanceof ApiError) || e.status !== 404) setErr(e instanceof ApiError ? e.message : String(e)); });
  }, [endpoint]);

  async function save() {
    setBusy(true);
    setErr('');
    setStatus('');
    try {
      const body = mode === 'cron'
        ? { cron, interval_seconds: 0, enabled }
        : { interval_seconds: Math.max(1, hours) * 3600, cron: '', enabled };
      setPolicy(await api.put<Policy>(endpoint, body));
      setStatus('已保存。到期后通知外部轮换器，本系统不生成新明文。');
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!window.confirm(`停止轮换 ${secretKey}？`)) return;
    setBusy(true);
    setErr('');
    try {
      await api.del(endpoint);
      setPolicy(null);
      setStatus('已停止轮换。');
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="rotation-policy">
      <h4>轮换策略</h4>
      <p className="muted">按间隔或 cron 发出 <code>secret.rotation_due</code>。外部轮换器收到 webhook 后回写新版本。</p>
      {policy && <p className="muted" role="status" title={timeTitle(policy.next_run_at)}>下次 {fmtTime(policy.next_run_at)}{policy.last_run_at ? ` · 上次 ${fmtTime(policy.last_run_at)}` : ''}</p>}
      {canWrite && (
        <>
          <label>方式
            <select aria-label="轮换方式" value={mode} onChange={(e) => setMode(e.target.value as 'interval' | 'cron')}>
              <option value="interval">固定间隔</option>
              <option value="cron">cron（UTC）</option>
            </select>
          </label>
          {mode === 'interval'
            ? <label>间隔（小时，至少 1）<input type="number" min={1} value={hours} onChange={(e) => setHours(Number(e.target.value))} /></label>
            : <label>cron<input className="mono" value={cron} onChange={(e) => setCron(e.target.value)} placeholder="0 3 * * *" /></label>}
          <label className="check"><input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} /> 启用</label>
          <div className="drawer-actions">
            <button type="button" disabled={busy} onClick={() => void save()}>{busy ? '保存中…' : '保存轮换'}</button>
            {policy && <button type="button" className="ghost" disabled={busy} onClick={() => void remove()}>停止</button>}
          </div>
        </>
      )}
      {status && <p role="status">{status}</p>}
      {err && <div className="error" role="alert">{err}</div>}
    </section>
  );
}

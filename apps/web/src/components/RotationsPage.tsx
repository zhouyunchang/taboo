import { useCallback, useEffect, useMemo, useState } from 'react';
import { api, ApiError } from '../api';
import { fmtTime, timeTitle } from '../time';
import { Button, EmptyState, Notice, PageHead, Pill, Section, Skeleton, TableWrap } from '../design/primitives';
import Icon from '../design/Icon';

type Policy = {
  id: string; key: string; folder: string; status: string; mode: string;
  interval_seconds: number; cron: string; owner?: { id: string; name: string };
  next_run_at: number; fixed_reason?: string;
};
type Health = {
  online: boolean; status: string; last_seen_at: number; last_sweep_at: number;
  active_policies: number; open_runs: number; overdue_runs: number; failed_runs: number;
};

const statusLabel: Record<string, string> = {
  draft: '草稿', active: '运行中', paused: '已暂停', fixed: '固定',
};
const statusTone = (s: string): 'ok' | 'warn' | 'bad' | 'muted' =>
  s === 'active' ? 'ok' : s === 'fixed' ? 'muted' : s === 'draft' ? 'muted' : 'warn';
const modeLabel: Record<string, string> = { generate: '生成', manual: '人工', webhook: '外部' };

export default function RotationsPage({ projectId, projectSlug, orgSlug, onOpen }: {
  projectId: string; projectSlug: string; orgSlug: string; onOpen: (key: string, folder: string) => void;
}) {
  const [policies, setPolicies] = useState<Policy[] | null>(null);
  const [counts, setCounts] = useState<Record<string, number>>({});
  const [unset, setUnset] = useState(0);
  const [health, setHealth] = useState<Health | null>(null);
  const [err, setErr] = useState('');
  const [q, setQ] = useState('');
  const [filter, setFilter] = useState('all');

  const load = useCallback(async () => {
    setErr('');
    try {
      const d = await api.get<{ policies: Policy[]; counts: Record<string, number>; unset: number }>(`/api/v1/projects/${projectId}/rotations`);
      setPolicies(d.policies ?? []);
      setCounts(d.counts ?? {});
      setUnset(d.unset ?? 0);
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); setPolicies([]); }
    try { setHealth(await api.get<Health>(`/api/v1/orgs/${orgSlug}/rotation/health`)); }
    catch { setHealth(null); }
  }, [projectId, orgSlug]);
  useEffect(() => { void load(); }, [load]);

  const shown = useMemo(() => {
    const list = policies ?? [];
    const needle = q.trim().toLowerCase();
    return list.filter((p) => {
      if (filter !== 'all' && p.status !== filter) return false;
      return !needle || p.key.toLowerCase().includes(needle) || p.folder.toLowerCase().includes(needle);
    }).sort((a, b) => {
      const rank = (s: string) => s === 'fixed' ? 2 : s === 'draft' ? 1 : 0;
      return rank(a.status) - rank(b.status) || (a.next_run_at || Number.MAX_SAFE_INTEGER) - (b.next_run_at || Number.MAX_SAFE_INTEGER) || a.key.localeCompare(b.key);
    });
  }, [policies, q, filter]);

  const total = policies?.length ?? 0;
  return (
    <div className="panel">
      <PageHead title="轮换" meta={<><span>{projectSlug}</span><span aria-hidden="true">·</span><span>{total} 条策略 · 另有 {unset} 条未设置</span></>} />
      {err && <Notice tone="danger">{err}</Notice>}
      {health ? (
        <div className={`rotation-health ${health.status === 'online' ? '' : health.status === 'slow' ? 'warn' : 'bad'}`} role="status">
          <span className={`dot ${health.online ? 'ok' : 'bad'}`} aria-hidden="true" />
          <span>
            {health.status === 'online' ? `调度器运行中 · 最近心跳 ${fmtTime(health.last_seen_at)}`
              : health.status === 'slow' ? '调度器心跳变慢，轮换可能延迟'
                : '调度器离线：到期轮换不会触发；已到期轮次仍可手动完成。'}
          </span>
        </div>
      ) : <div className="rotation-health warn"><span>暂时无法读取调度器状态。</span></div>}

      <div className="rotation-stats" aria-label="轮换统计">
        <div><span className="k">运行中</span><span className="v mono">{counts.active ?? 0}</span></div>
        <div><span className="k">草稿</span><span className="v mono">{counts.draft ?? 0}</span></div>
        <div><span className="k">已暂停</span><span className="v mono">{counts.paused ?? 0}</span></div>
        <div><span className="k">固定</span><span className="v mono">{counts.fixed ?? 0}</span></div>
        <div><span className="k">待处理</span><span className="v mono">{health?.open_runs ?? 0}</span></div>
        <div><span className="k">已逾期</span><span className="v mono">{health?.overdue_runs ?? 0}</span></div>
      </div>

      <div className="rotation-toolbar">
        <label className="field inline">状态
          <select value={filter} onChange={(e) => setFilter(e.target.value)} aria-label="筛选轮换状态">
            <option value="all">全部</option>
            <option value="active">运行中</option>
            <option value="draft">草稿</option>
            <option value="paused">已暂停</option>
            <option value="fixed">固定</option>
          </select>
        </label>
        <div className="search">
          <Icon name="search" size={14} />
          <input type="search" aria-label="搜索轮换密钥" placeholder="搜索 Key / 目录" value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
      </div>

      <Section title="策略" hint="固定和未设置都不会有到期时间；运行中的策略按计划产生轮次。">
        {policies === null ? <Skeleton rows={4} /> : shown.length === 0 ? (
          <EmptyState icon="clock" title={total ? '没有匹配的策略' : '还没有轮换策略'}>
            {total ? '换个状态或搜索词。' : '轮换是 opt-in：为密钥设置周期和负责人后，taboo 才会在到期时提醒；不想轮换的密钥可以明确标记为固定。'}
          </EmptyState>
        ) : (
          <TableWrap label="轮换策略">
            <table className="table compact" data-mobile="cards">
              <thead><tr><th>Key</th><th>状态</th><th>方式</th><th>周期</th><th>下次</th><th>负责人</th><th className="actions"><span className="sr-only">操作</span></th></tr></thead>
              <tbody>
                {shown.map((p) => (
                  <tr key={p.id} onClick={() => onOpen(p.key, p.folder)} className="row-click">
                    <td data-label="Key"><div className="mono">{p.key}</div><div className="cell-sub mono">{p.folder}</div></td>
                    <td data-label="状态"><Pill tone={statusTone(p.status)}>{statusLabel[p.status] ?? p.status}</Pill></td>
                    <td data-label="方式" className="muted">{modeLabel[p.mode] || '—'}</td>
                    <td data-label="周期" className="muted">{p.cron ? `cron ${p.cron}` : p.interval_seconds ? `每 ${Math.round(p.interval_seconds / 86400)} 天` : '—'}</td>
                    <td data-label="下次" className="mono nowrap">{p.next_run_at ? fmtTime(p.next_run_at) : '—'}</td>
                    <td data-label="负责人" className="muted">{p.owner?.name || '—'}</td>
                    <td className="actions"><Button variant="quiet" className="btn-sm" onClick={(e) => { e.stopPropagation(); onOpen(p.key, p.folder); }}>查看</Button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableWrap>
        )}
      </Section>
    </div>
  );
}

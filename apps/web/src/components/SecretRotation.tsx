// 轮换章：策略摘要、轮次、策略表单。只负责轮换；密钥值和版本在记录页其他章。
// API 与既有 RotationPanel 完全一致，避免出现第二套轮换语义。
import { useCallback, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { api, ApiError } from '../api';
import { fmtTime, timeTitle } from '../time';
import Icon from '../design/Icon';
import Modal from './Modal';
import { Button, Check, Field, Notice, Pill, Skeleton } from '../design/primitives';

type Generation = { profile: string; length: number; prefix?: string; min_entropy_bits?: number };
type Policy = {
  id: string; key: string; folder: string; status: 'draft' | 'active' | 'paused' | 'fixed';
  mode: 'generate' | 'manual' | 'webhook'; interval_seconds: number; cron: string;
  owner?: { id: string; name: string }; grace_seconds: number; next_run_at: number;
  last_run_at: number; last_run_id?: string; fixed_reason?: string; fixed_by_name?: string;
  fixed_at?: string; generation?: Generation; auto_generate?: boolean;
  open_run?: { id: string; status: string; due_at: number };
};
type Run = {
  id: string; status: string; trigger: string; due_at: number; grace_until: number;
  old_version: number; new_version: number | null; owner_name?: string; attempts: number;
  last_error?: string; created_at: string; finished_at?: string;
};
export type RotationSummary = {
  status: Policy['status'];
  nextAt: number | null;
  cron: string;
  intervalSeconds: number;
  ownerName?: string;
  openRunStatus?: string;
};

const OPEN_RUN = new Set(['awaiting_generation', 'awaiting_distribution', 'awaiting_manual', 'awaiting_external', 'overdue']);
const modeLabel: Record<string, string> = { generate: '生成并写入', manual: '人工轮换', webhook: '外部 Webhook' };
const statusLabel: Record<string, string> = {
  draft: '草稿', active: '运行中', paused: '已暂停', fixed: '固定',
  due: '待处理', awaiting_generation: '待生成', awaiting_distribution: '待分发',
  awaiting_manual: '待人工', awaiting_external: '等待回写', overdue: '已逾期',
  succeeded: '已完成', failed: '失败', skipped: '已跳过', canceled: '已取消',
};
const statusTone = (s: string): 'ok' | 'warn' | 'bad' | 'muted' | 'accent' =>
  s === 'active' || s === 'succeeded' ? 'ok'
    : s === 'failed' || s === 'overdue' ? 'bad'
      : s === 'awaiting_generation' || s === 'awaiting_distribution' || s === 'awaiting_manual' || s === 'awaiting_external' ? 'warn'
        : 'muted';

function periodLabel(p: Policy) {
  if (p.cron) return `cron ${p.cron}（UTC）`;
  const seconds = p.interval_seconds || 0;
  if (!seconds) return '不适用';
  if (seconds >= 86400 && seconds % 86400 === 0) return `每 ${seconds / 86400} 天`;
  return `每 ${Math.max(1, Math.round(seconds / 3600))} 小时`;
}

function summaryOf(p: Policy | null): RotationSummary | null {
  if (!p) return null;
  return {
    status: p.status,
    nextAt: p.next_run_at || null,
    cron: p.cron || '',
    intervalSeconds: p.interval_seconds || 0,
    ownerName: p.owner?.name,
    openRunStatus: p.open_run?.status,
  };
}

export default function SecretRotation({ projectId, folder, secretKey, canWrite, canReveal, onSummary, onActivity, onSecretChanged }: {
  projectId: string; folder: string; secretKey: string; canWrite: boolean; canReveal: boolean;
  onSummary: (summary: RotationSummary | null | undefined) => void;
  onActivity: () => void;
  onSecretChanged: () => void;
}) {
  const endpoint = `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}/rotation?path=${encodeURIComponent(folder)}`;
  const [policy, setPolicy] = useState<Policy | null>(null);
  const [loading, setLoading] = useState(true);
  const [runs, setRuns] = useState<Run[]>([]);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(false);
  const [openRuns, setOpenRuns] = useState<Set<string>>(new Set());
  const [generated, setGenerated] = useState('');
  const [generatedCopied, setGeneratedCopied] = useState(false);
  const [fixOpen, setFixOpen] = useState(false);
  const [fixReason, setFixReason] = useState('');
  const [fixError, setFixError] = useState('');
  const [deleteOpen, setDeleteOpen] = useState(false);

  // 表单状态
  const [mode, setMode] = useState<Policy['mode']>('webhook');
  const [kind, setKind] = useState<'interval' | 'cron'>('interval');
  const [hours, setHours] = useState(720);
  const [cron, setCron] = useState('0 3 * * *');
  const [graceDays, setGraceDays] = useState(7);
  const [profile, setProfile] = useState('alnum32');
  const [genLength, setGenLength] = useState(32);
  const [prefix, setPrefix] = useState('');
  const [autoGenerate, setAutoGenerate] = useState(false);
  const [formError, setFormError] = useState('');

  const hydrate = useCallback((item: Policy | null) => {
    // 固定状态会被服务端清掉 mode/interval；「改为轮换」要用可编辑的默认值重新开始，
    // 不能把空串塞进 select（浏览器会显示成第一个选项，看上去像已经选了 generate）。
    const mode = item?.mode === 'manual' || item?.mode === 'webhook' || item?.mode === 'generate' ? item.mode : 'webhook';
    setMode(mode);
    setKind(item?.cron ? 'cron' : 'interval');
    setHours(item?.interval_seconds ? Math.max(1, Math.round(item.interval_seconds / 3600)) : 720);
    setCron(item?.cron || '0 3 * * *');
    setGraceDays(Math.max(0, Math.round((item?.grace_seconds || 604800) / 86400)));
    setProfile(item?.generation?.profile || 'alnum32');
    setGenLength(item?.generation?.length || 32);
    setPrefix(item?.generation?.prefix || '');
    setAutoGenerate(Boolean(item?.auto_generate));
  }, []);

  const load = useCallback(async () => {
    setError('');
    try {
      const item = await api.get<Policy>(endpoint);
      setPolicy(item);
      hydrate(item);
      onSummary(summaryOf(item));
      const data = await api.get<{ runs: Run[] }>(`${endpoint.replace('/rotation?', '/rotation/runs?')}&limit=10`);
      setRuns(data.runs ?? []);
      setOpenRuns((prev) => new Set([...prev].filter((id) => (data.runs ?? []).some((run) => run.id === id))));
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) {
        setPolicy(null);
        setRuns([]);
        hydrate(null);
        onSummary(null);
      } else {
        setError(e instanceof ApiError ? e.message : String(e));
      }
    } finally {
      setLoading(false);
    }
  }, [endpoint, hydrate, onSummary]);

  useEffect(() => { void load(); }, [load]);

  // 一次性新值只活在这一屏；失焦或 30 秒后收回。
  useEffect(() => {
    if (!generated) return;
    const clear = () => { setGenerated(''); setGeneratedCopied(false); };
    const timer = setTimeout(clear, 30_000);
    window.addEventListener('blur', clear);
    window.addEventListener('taboo:verification-unavailable', clear);
    return () => {
      clearTimeout(timer);
      window.removeEventListener('blur', clear);
      window.removeEventListener('taboo:verification-unavailable', clear);
    };
  }, [generated]);

  async function reloadAndAnnounce(message: string) {
    setNotice(message);
    await load();
    onActivity();
  }

  async function save(status: 'draft' | 'active') {
    setFormError('');
    if (kind === 'interval' && (!Number.isFinite(hours) || hours < 1)) {
      setFormError('间隔至少 1 小时。');
      return;
    }
    if (kind === 'cron' && cron.trim().split(/\s+/).filter(Boolean).length !== 5) {
      setFormError('cron 必须恰好五段，例如 0 3 * * *。');
      return;
    }
    setBusy(true);
    setError('');
    try {
      const body: Record<string, unknown> = {
        status,
        mode,
        owner_id: policy?.owner?.id ?? '',
        grace_seconds: Math.max(0, graceDays) * 86400,
        interval_seconds: kind === 'interval' ? Math.max(1, hours) * 3600 : 0,
        cron: kind === 'cron' ? cron.trim() : '',
      };
      if (mode === 'generate') {
        body.generation = { profile, length: genLength, prefix };
        body.auto_generate = autoGenerate;
      }
      const item = await api.put<Policy>(endpoint, body);
      setPolicy(item);
      hydrate(item);
      onSummary(summaryOf(item));
      setEditing(false);
      setNotice(status === 'active' ? '已保存并启用。下次时间由服务端计算。' : '已保存草稿，不会提醒或生成。');
      await load();
      onActivity();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function setStatus(status: 'active' | 'paused') {
    if (!policy) return;
    setBusy(true);
    setError('');
    try {
      const body: Record<string, unknown> = {
        status,
        mode: policy.mode,
        owner_id: policy.owner?.id ?? '',
        grace_seconds: policy.grace_seconds,
        interval_seconds: policy.interval_seconds,
        cron: policy.cron,
      };
      if (policy.mode === 'generate') {
        body.generation = policy.generation;
        body.auto_generate = Boolean(policy.auto_generate);
      }
      const item = await api.put<Policy>(endpoint, body);
      setPolicy(item);
      onSummary(summaryOf(item));
      await reloadAndAnnounce(status === 'active' ? '已恢复轮换。' : '已暂停：不会提醒或生成。');
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function markFixed() {
    if (!policy) return;
    if (!fixReason.trim()) { setFixError('请填写固定原因。'); return; }
    setBusy(true);
    try {
      const body: Record<string, unknown> = {
        status: 'fixed', fixed_reason: fixReason.trim(), owner_id: policy.owner?.id ?? '',
        mode: policy.mode, grace_seconds: policy.grace_seconds,
        interval_seconds: policy.interval_seconds, cron: policy.cron,
      };
      if (policy.mode === 'generate') {
        body.generation = policy.generation;
        body.auto_generate = Boolean(policy.auto_generate);
      }
      const item = await api.put<Policy>(endpoint, body);
      setPolicy(item);
      onSummary(summaryOf(item));
      setFixOpen(false);
      setFixReason('');
      setFixError('');
      await reloadAndAnnounce('已标记固定：没有下次轮换，也不会提醒。');
    } catch (e) {
      setFixError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function removePolicy() {
    setBusy(true);
    try {
      await api.del(endpoint);
      setPolicy(null);
      setRuns([]);
      setEditing(false);
      setDeleteOpen(false);
      onSummary(null);
      await reloadAndAnnounce('轮换策略已删除；版本不受影响。');
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function runNow() {
    if (!policy) return;
    setBusy(true);
    setError('');
    try {
      await api.post(endpoint.replace('/rotation?', '/rotation/run?'), {});
      await reloadAndAnnounce('已创建轮次；不会自动替换上游凭据。');
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function runAction(run: Run, action: 'generate' | 'distribute' | 'complete' | 'skip' | 'notify') {
    setBusy(true);
    setError('');
    setGenerated('');
    setGeneratedCopied(false);
    try {
      const q = endpoint.indexOf('?');
      const base = `${q >= 0 ? endpoint.slice(0, q) : endpoint}/runs/${run.id}`;
      const qs = q >= 0 ? endpoint.slice(q) : '';
      if (action === 'generate') {
        const data = await api.post<{ value: string }>(`${base}/generate${qs}`, {});
        setGenerated(data.value);
        onSecretChanged();
        setNotice('已生成新版本。确认消费方已切换后再完成分发。');
      } else if (action === 'distribute') {
        await api.post(`${base}/distribute${qs}`, { note: '', verified: true });
        setNotice('已确认分发。');
      } else if (action === 'complete') {
        const versions = await api.get<{ latest: number }>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}/versions?path=${encodeURIComponent(folder)}`);
        await api.post(`${base}/complete${qs}`, { new_version: versions.latest, note: '', verified: true });
        onSecretChanged();
        setNotice('本轮已完成。');
      } else if (action === 'skip') {
        const reason = window.prompt('跳过本轮的原因');
        if (!reason?.trim()) return;
        await api.post(`${base}/skip${qs}`, { reason: reason.trim() });
        setNotice('已跳过本轮。');
      } else {
        await api.post(`${base}/notify${qs}`, {});
        setNotice('已重新发送通知。');
      }
      await load();
      onActivity();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function copyGenerated() {
    if (!generated) return;
    try {
      await navigator.clipboard.writeText(generated);
      setGeneratedCopied(true);
      setTimeout(() => setGeneratedCopied(false), 1600);
    } catch {
      setNotice('复制失败：浏览器不允许写入剪贴板。');
    }
  }

  const openRun = policy?.open_run;
  const isFixed = policy?.status === 'fixed';
  const nextText = !policy ? '—'
    : isFixed ? '没有下次'
      : policy.status === 'paused' ? '已暂停'
        : policy.status === 'draft' ? '未启用'
          : policy.cron ? '按 cron'
            : policy.next_run_at ? fmtTime(policy.next_run_at) : '—';

  const metaBits = useMemo(() => {
    if (!policy) return [] as Array<{ key: string; node: ReactNode }>;
    if (isFixed) {
      return [
        policy.fixed_reason ? { key: 'reason', node: <>原因 {policy.fixed_reason}</> } : null,
        policy.fixed_by_name ? { key: 'by', node: <>由 {policy.fixed_by_name} 标记</> } : null,
      ].filter(Boolean) as Array<{ key: string; node: ReactNode }>;
    }
    return [
      policy.owner?.name ? { key: 'owner', node: <>负责人 <span className="mono">{policy.owner.name}</span></> } : null,
      { key: 'grace', node: <>宽限 {Math.round(policy.grace_seconds / 86400)} 天</> },
      policy.last_run_at ? { key: 'last', node: <>上次 <span className="mono">{fmtTime(policy.last_run_at).slice(0, 10)}</span></> } : null,
      openRun ? { key: 'open', node: <>本轮 {statusPill(openRun.status)}</> } : null,
    ].filter(Boolean) as Array<{ key: string; node: ReactNode }>;
  }, [policy, isFixed, openRun]);

  if (loading) return <Skeleton rows={5} />;

  return (
    <>
      <div className="chapter-notice" role="status" aria-live="polite">
        {error && <Notice tone="danger">{error}</Notice>}
        {notice && <Notice tone="ok">{notice}</Notice>}
      </div>

      <header className="chapter-head">
        <div className="chapter-head-main">
          <h2>轮换策略</h2>
          <p className="chapter-hint">到期只创建待办与通知；生成新值不等于分发完成，外部真源仍要在上游更新。</p>
        </div>
        <div className="chapter-actions" id="rotation-actions">
          {policy && <Pill tone={statusTone(policy.status)}>{statusLabel[policy.status]}</Pill>}
          {openRun && <Pill tone={statusTone(openRun.status)}>{statusLabel[openRun.status] ?? openRun.status}</Pill>}
          {policy && canWrite && !editing && (
            <>
              {!isFixed && policy.status !== 'draft' && <Button variant="default" className="btn-sm" icon="play" disabled={busy} onClick={() => void runNow()}>立即运行</Button>}
              <Button variant="default" className="btn-sm" icon="pencil" onClick={() => { setEditing(true); hydrate(policy); }}>{isFixed ? '改为轮换' : '编辑策略'}</Button>
              {policy.status === 'active' && <Button variant="quiet" className="btn-sm" icon="clock" disabled={busy} onClick={() => void setStatus('paused')}>暂停</Button>}
              {policy.status === 'paused' && <Button variant="quiet" className="btn-sm" icon="play" disabled={busy} onClick={() => void setStatus('active')}>恢复</Button>}
              {!isFixed && <Button variant="quiet" className="btn-sm" icon="lock" onClick={() => { setFixOpen(true); setFixReason(policy.fixed_reason || ''); }}>标记固定</Button>}
            </>
          )}
        </div>
      </header>

      {!policy && (
        <div className="chapter-empty">
          <span className="chapter-empty-mark"><Icon name="clock" size={20} /></span>
          <div className="chapter-empty-body">
            <p className="chapter-empty-title">这把密钥还没有轮换计划</p>
            <p className="chapter-empty-text">到期时 taboo 只创建待办与通知，不会擅自替换外部真源。设置周期和回写方式后，轮次会记录在这里。</p>
            <div className="lifecycle" aria-label="轮换生命周期">
              <span className="step"><b>到期</b> 创建轮次</span><span className="arrow">→</span>
              <span className="step"><b>生成 / 回写</b> 产生新版本</span><span className="arrow">→</span>
              <span className="step"><b>确认分发</b> 本轮完成</span>
            </div>
            {canWrite
              ? <div className="btn-row"><Button variant="default" icon="clock" onClick={() => { setEditing(true); hydrate(null); }}>设置轮换策略</Button></div>
              : <p className="section-hint">当前角色只能查看，不能设置轮换。</p>}
          </div>
        </div>
      )}

      {policy && !editing && (
        <>
          <div className="policy-summary">
            <div className="policy-cell"><span className="k">方式</span><span className="v">{modeLabel[policy.mode] ?? '不适用'}</span></div>
            <div className="policy-cell"><span className="k">周期</span><span className="v">{periodLabel(policy)}</span></div>
            <div className="policy-cell"><span className="k">下次</span><span className="v mono">{nextText}</span></div>
          </div>
          {metaBits.length > 0 && <div className="policy-meta">{metaBits.map((bit, index) => <span key={bit.key}>{index > 0 && <span aria-hidden="true"> · </span>}{bit.node}</span>)}</div>}

          <div className="policy-section">
            <div className="policy-section-head"><h3>最近轮次</h3><p>到期、生成、分发、完成都留痕；旧版本不自动删除。</p></div>
            {runs.length === 0 ? <p className="section-hint">还没有轮次；到期后会在这里出现。</p> : (
              <div className="run-list">
                <div className="run-head"><span>到期</span><span>状态</span><span>版本</span><span>触发</span><span style={{ textAlign: 'right' }}>操作</span></div>
                {runs.map((run) => {
                  const open = openRuns.has(run.id);
                  return (
                    <div key={run.id}>
                      <div className="run-row">
                        <span className="run-due" title={timeTitle(run.due_at)}>{fmtTime(run.due_at)}</span>
                        <span className="run-status"><Pill tone={statusTone(run.status)}>{statusLabel[run.status] ?? run.status}</Pill></span>
                        <span className="run-ver">v{run.old_version} → {run.new_version ? `v${run.new_version}` : '—'}</span>
                        <span className="run-trigger">{run.trigger === 'schedule' ? '定时' : run.trigger === 'retry' ? '重试' : '手动'}</span>
                        <span className="run-act">
                          <button type="button" className="btn btn-quiet btn-sm" aria-expanded={open} aria-controls={`run-detail-${run.id}`}
                            onClick={() => setOpenRuns((prev) => { const next = new Set(prev); if (next.has(run.id)) next.delete(run.id); else next.add(run.id); return next; })}>
                            {open ? '收起' : '详情'}
                          </button>
                        </span>
                      </div>
                      {open && (
                        <div className="run-expand" id={`run-detail-${run.id}`} role="region" aria-label={`轮次 ${run.id} 详情`}>
                          <div><span className="k">到期</span><span className="v mono">{fmtTime(run.due_at)}</span></div>
                          <div><span className="k">负责人</span><span className="v">{run.owner_name || '未指派'}</span></div>
                          <div><span className="k">尝试</span><span className="v mono">{run.attempts}</span></div>
                          <div><span className="k">轮次</span><span className="v mono">{run.id}</span></div>
                          <div><span className="k">版本</span><span className="v mono">v{run.old_version}{run.new_version ? ` → v${run.new_version}` : ''}</span></div>
                          <div><span className="k">备注</span><span className={`v ${run.status === 'failed' || run.status === 'overdue' ? 'run-error' : ''}`}>{run.last_error || '—'}</span></div>
                          {canWrite && run.status !== 'succeeded' && run.status !== 'skipped' && run.status !== 'canceled' && (
                            <div className="run-expand-actions">
                              {(run.status === 'awaiting_generation' || (run.status === 'overdue' && !run.new_version)) && policy.mode === 'generate' && (
                                <Button variant="default" className="btn-sm" icon="refresh" disabled={busy || !canReveal} onClick={() => void runAction(run, 'generate')}>生成新值</Button>
                              )}
                              {(run.status === 'awaiting_distribution' || (run.status === 'overdue' && run.new_version)) && policy.mode === 'generate' && (
                                <Button variant="default" className="btn-sm" icon="check" disabled={busy} onClick={() => void runAction(run, 'distribute')}>完成分发</Button>
                              )}
                              {(run.status === 'awaiting_manual' || run.status === 'awaiting_external' || (run.status === 'overdue' && policy.mode !== 'generate')) && (
                                <Button variant="default" className="btn-sm" icon="check" disabled={busy} onClick={() => void runAction(run, 'complete')}>完成本轮</Button>
                              )}
                              <Button variant="quiet" className="btn-sm" disabled={busy} onClick={() => void runAction(run, 'skip')}>跳过</Button>
                              <Button variant="quiet" className="btn-sm" disabled={busy} onClick={() => void runAction(run, 'notify')}>重新通知</Button>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
          <p className="policy-note">{isFixed ? '固定后不会创建新轮次，也不会提醒；历史轮次保留。改为轮换后会重新计算下次时间。' : '轮换只负责创建待办、生成或提醒；外部真源仍要在上游更新。确认分发后本轮才算完成。'}</p>
        </>
      )}

      {editing && canWrite && (
        <form className="form-stack policy-form" onSubmit={(event) => event.preventDefault()}>
          <div className="form-row">
            <Field label="方式" htmlFor="record-rot-mode">
              <select id="record-rot-mode" value={mode} onChange={(e) => setMode(e.target.value as Policy['mode'])}>
                <option value="generate">生成并写入（taboo 作为真源）</option>
                <option value="manual">人工轮换</option>
                <option value="webhook">外部 Webhook</option>
              </select>
            </Field>
            <Field label="周期" htmlFor="record-rot-kind">
              <select id="record-rot-kind" value={kind} onChange={(e) => setKind(e.target.value as 'interval' | 'cron')}>
                <option value="interval">固定间隔（按计划节拍）</option>
                <option value="cron">cron（UTC）</option>
              </select>
            </Field>
          </div>
          <div className="form-row">
            {kind === 'interval'
              ? <Field label="间隔（小时，至少 1）" htmlFor="record-rot-hours"><input id="record-rot-hours" type="number" min={1} max={8760} value={hours} onChange={(e) => setHours(Number(e.target.value))} /></Field>
              : <Field label="cron（五段 UTC）" htmlFor="record-rot-cron"><input id="record-rot-cron" className="mono" value={cron} onChange={(e) => setCron(e.target.value)} placeholder="0 3 * * *" /></Field>}
            <Field label="宽限期（天）" htmlFor="record-rot-grace" hint="超过后轮次显示逾期，不会让密钥失效。"><input id="record-rot-grace" type="number" min={0} max={30} value={graceDays} onChange={(e) => setGraceDays(Number(e.target.value))} /></Field>
          </div>
          <div className="form-row">
            <Field label="负责人" htmlFor="record-rot-owner" hint="沿用当前策略的负责人；成员管理在组织设置。">
              <input id="record-rot-owner" readOnly value={policy?.owner?.name || '未指派'} />
            </Field>
            <div className="policy-preview">
              <span className="k">保存并启用后</span>
              <p className="v">由服务端计算下次时间</p>
              <p className="hint">到期只创建轮次与通知，不会自动替换上游。</p>
            </div>
          </div>
          {mode === 'generate' && (
            <div className="form-row">
              <Field label="生成档位" htmlFor="record-rot-profile">
                <select id="record-rot-profile" value={profile} onChange={(e) => {
                  const value = e.target.value;
                  setProfile(value);
                  setGenLength(value === 'alnum64' ? 64 : value === 'base64url32' ? 43 : value === 'hex32' ? 64 : value === 'password24' ? 24 : 32);
                }}>
                  <option value="alnum32">字母数字 32 位（默认）</option>
                  <option value="alnum64">字母数字 64 位</option>
                  <option value="base64url32">base64url 32 字节</option>
                  <option value="hex32">hex 32 字节</option>
                  <option value="password24">密码 24–32 位</option>
                  <option value="custom">自定义字母数字长度</option>
                </select>
              </Field>
              <Field label="随机长度" htmlFor="record-rot-length" hint="服务端确保至少 128 bit 熵。">
                <input id="record-rot-length" type="number" min={16} max={128} value={genLength} disabled={!['custom', 'alnum32', 'alnum64', 'password24'].includes(profile)} onChange={(e) => setGenLength(Number(e.target.value))} />
              </Field>
            </div>
          )}
          {mode === 'generate' && (
            <div className="form-row">
              <Field label="前缀（可选）" htmlFor="record-rot-prefix"><input id="record-rot-prefix" className="mono" value={prefix} onChange={(e) => setPrefix(e.target.value)} placeholder="taboo_" /></Field>
              <div className="field">
                <span className="field-label">自动生成</span>
                <Check aria-label="到期自动生成" checked={autoGenerate} onChange={(e) => setAutoGenerate(e.target.checked)} label="到期自动生成" hint="默认关闭；消费方必须支持热重载。" />
              </div>
            </div>
          )}
          {mode === 'generate' && !canReveal && <Notice tone="warn">你没有 reveal 权限，不能生成或查看新值；不能启用生成模式。</Notice>}
          <p className="field-hint">保存草稿不会启用；保存并启用才计算下次时间。到期只创建轮次和通知。</p>
          {formError && <Notice tone="danger">{formError}</Notice>}
          <div className="form-actions split">
            <div className="btn-row">
              {policy && <Button variant="quiet" icon="trash" onClick={() => setDeleteOpen(true)}>删除策略</Button>}
            </div>
            <div className="btn-row">
              <Button variant="quiet" onClick={() => { setEditing(false); setFormError(''); }} disabled={busy}>取消</Button>
              <Button variant="default" onClick={() => void save('draft')} disabled={busy}>保存草稿</Button>
              <Button variant="primary" icon="seal" onClick={() => void save('active')} disabled={busy || (mode === 'generate' && !canReveal)} loading={busy}>保存并启用</Button>
            </div>
          </div>
        </form>
      )}

      {generated && (
        <div className="once-secret">
          <h3>新值（仅此屏显示一次）</h3>
          <p className="mono">{generated}</p>
          <div className="form-actions">
            <Button variant="quiet" onClick={() => { setGenerated(''); setGeneratedCopied(false); }}>隐藏</Button>
            <Button variant="default" onClick={() => void copyGenerated()}>{generatedCopied ? '已复制' : '复制'}</Button>
          </div>
        </div>
      )}

      {fixOpen && (
        <Modal title="标记为固定？" onClose={() => { if (!busy) setFixOpen(false); }} busy={busy}>
          <div className="form-stack">
            <p>标记后没有下次轮换，也不会提醒。密钥本身、版本历史和已有轮次都不变；之后可以「改为轮换」。</p>
            <Field label="固定原因（必填）" htmlFor="record-rot-fixed-reason">
              <input id="record-rot-fixed-reason" value={fixReason} maxLength={120} onChange={(e) => setFixReason(e.target.value)} placeholder="例如：厂商托管，无法轮换" autoFocus />
            </Field>
            {fixError && <Notice tone="danger">{fixError}</Notice>}
            <div className="form-actions">
              <Button variant="quiet" onClick={() => setFixOpen(false)} disabled={busy}>取消</Button>
              <Button variant="stamp" icon="seal" onClick={() => void markFixed()} disabled={busy || !fixReason.trim()}>标记固定</Button>
            </div>
          </div>
        </Modal>
      )}

      {deleteOpen && (
        <Modal title="删除轮换策略？" onClose={() => { if (!busy) setDeleteOpen(false); }} busy={busy}>
          <div className="form-stack">
            <p>删除后不会再计算下次时间，也不会提醒。历史轮次保留；密钥本身和版本不受影响。</p>
            <div className="form-actions">
              <Button variant="quiet" onClick={() => setDeleteOpen(false)} disabled={busy}>取消</Button>
              <Button variant="danger" onClick={() => void removePolicy()} disabled={busy}>删除策略</Button>
            </div>
          </div>
        </Modal>
      )}
    </>
  );
}

function statusPill(status: string) {
  return <Pill tone={statusTone(status)}>{statusLabel[status] ?? status}</Pill>;
}

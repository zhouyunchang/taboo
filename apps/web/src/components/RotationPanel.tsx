import { useCallback, useEffect, useMemo, useState } from 'react';
import { api, ApiError } from '../api';
import { fmtTime, timeTitle } from '../time';
import { Button, Check, EmptyState, Field, Notice, Pill, Section, Skeleton } from '../design/primitives';

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

const statusLabel: Record<string, string> = {
  draft: '草稿', active: '运行中', paused: '已暂停', fixed: '固定',
  due: '待处理', awaiting_generation: '待生成', awaiting_distribution: '待分发',
  awaiting_manual: '待人工', awaiting_external: '等待回写', overdue: '已逾期',
  succeeded: '已完成', failed: '失败', skipped: '已跳过', canceled: '已取消',
};
const statusTone = (s: string): 'ok' | 'warn' | 'bad' | 'muted' | 'accent' =>
  s === 'active' || s === 'succeeded' ? 'ok'
    : s === 'failed' ? 'bad'
      : s === 'awaiting_generation' || s === 'awaiting_distribution' || s === 'awaiting_manual' || s === 'awaiting_external' || s === 'overdue' ? 'warn'
        : 'muted';

const profiles = [
  { value: 'alnum32', label: '字母数字 32 位（默认）' },
  { value: 'alnum64', label: '字母数字 64 位' },
  { value: 'base64url32', label: 'base64url 32 字节' },
  { value: 'hex32', label: 'hex 32 字节' },
  { value: 'password24', label: '密码 24–32 位' },
  { value: 'custom', label: '自定义字母数字长度' },
];

export default function RotationPanel({ projectId, folder, secretKey, canWrite, canReveal, onChanged }: {
  projectId: string; folder: string; secretKey: string; canWrite: boolean; canReveal: boolean; onChanged?: () => void;
}) {
  const endpoint = `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}/rotation?path=${encodeURIComponent(folder)}`;
  const [policy, setPolicy] = useState<Policy | null>(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(false);
  const [runs, setRuns] = useState<Run[]>([]);
  const [selected, setSelected] = useState<Run | null>(null);
  const [generated, setGenerated] = useState('');
  const [mode, setMode] = useState<'generate' | 'manual' | 'webhook'>('manual');
  const [kind, setKind] = useState<'interval' | 'cron'>('interval');
  const [hours, setHours] = useState(720);
  const [cron, setCron] = useState('0 3 * * *');
  const [graceDays, setGraceDays] = useState(7);
  const [profile, setProfile] = useState('alnum32');
  const [genLength, setGenLength] = useState(32);
  const [prefix, setPrefix] = useState('');
  const [autoGenerate, setAutoGenerate] = useState(false);
  const [fixedReason, setFixedReason] = useState('');
  const [ownerId, setOwnerId] = useState('');

  const load = useCallback(async () => {
    setErr('');
    try {
      const item = await api.get<Policy>(endpoint);
      setPolicy(item);
      hydrate(item);
      const data = await api.get<{ runs: Run[] }>(`${endpoint.replace('/rotation?', '/rotation/runs?')}&limit=10`);
      setRuns(data.runs ?? []);
      setSelected((prev) => prev ? (data.runs ?? []).find((r) => r.id === prev.id) ?? prev : null);
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) {
        setPolicy(null);
        setRuns([]);
      } else setErr(e instanceof ApiError ? e.message : String(e));
    } finally { setLoading(false); }
  }, [endpoint]);
  useEffect(() => { void load(); }, [load]);

  function hydrate(item: Policy) {
    setMode(item.mode);
    setKind(item.cron ? 'cron' : 'interval');
    setHours(Math.max(1, Math.round((item.interval_seconds || 86400) / 3600)));
    setCron(item.cron || '0 3 * * *');
    setGraceDays(Math.max(0, Math.round((item.grace_seconds || 604800) / 86400)));
    setProfile(item.generation?.profile || 'alnum32');
    setGenLength(item.generation?.length || 32);
    setPrefix(item.generation?.prefix || '');
    setAutoGenerate(!!item.auto_generate);
    setOwnerId(item.owner?.id || '');
    setFixedReason(item.fixed_reason || '');
  }

  const open = policy?.open_run;
  const canEdit = canWrite && !!policy;
  const showForm = canWrite && (editing || !policy);

  async function save(status: 'draft' | 'active' | 'paused') {
    setBusy(true); setErr(''); setNotice('');
    try {
      const body: Record<string, unknown> = {
        status, mode, owner_id: ownerId, grace_seconds: Math.max(0, graceDays) * 86400,
        interval_seconds: kind === 'interval' ? Math.max(1, hours) * 3600 : 0,
        cron: kind === 'cron' ? cron : '',
      };
      if (mode === 'generate') {
        body.generation = { profile, length: genLength, prefix };
        body.auto_generate = autoGenerate;
      }
      const item = await api.put<Policy>(endpoint, body);
      setPolicy(item); hydrate(item); setEditing(false);
      setNotice(status === 'active' ? '已启用轮换。' : status === 'paused' ? '已暂停轮换。' : '已保存草稿。');
      await load();
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
    finally { setBusy(false); }
  }

  async function fix() {
    if (!fixedReason.trim()) { setErr('请填写固定原因。'); return; }
    setBusy(true); setErr(''); setNotice('');
    try {
      const item = await api.put<Policy>(endpoint, { status: 'fixed', fixed_reason: fixedReason.trim(), owner_id: ownerId });
      setPolicy(item); hydrate(item); setEditing(false);
      setNotice('已标记为固定：没有下次轮换，也不会提醒。');
      setSelected(null);
      await load();
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
    finally { setBusy(false); }
  }

  async function remove() {
    if (!window.confirm(`删除 ${secretKey} 的轮换策略？历史轮次会保留。`)) return;
    setBusy(true); setErr('');
    try {
      await api.del(endpoint);
      setPolicy(null); setRuns([]); setEditing(false); setNotice('已删除轮换策略。');
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
    finally { setBusy(false); }
  }

  async function runNow() {
    if (!window.confirm('立即创建一轮轮换任务？不会自动更换密钥。')) return;
    setBusy(true); setErr('');
    try {
      await api.post(`${endpoint.replace('/rotation?', '/rotation/run?')}`, {});
      setNotice('已创建轮换轮次。');
      await load();
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
    finally { setBusy(false); }
  }

  async function runAction(run: Run, action: 'generate' | 'distribute' | 'complete' | 'skip' | 'notify') {
    setBusy(true); setErr(''); setNotice(''); setGenerated('');
    try {
      const q = endpoint.indexOf('?');
      const base = `${q >= 0 ? endpoint.slice(0, q) : endpoint}/runs/${run.id}`;
      const qs = q >= 0 ? endpoint.slice(q) : '';
      if (action === 'generate') {
        const d = await api.post<{ value: string }>(`${base}/generate${qs}`, {});
        setGenerated(d.value);
        setNotice('已生成新版本。确认消费方已切换后再完成分发。');
      } else if (action === 'distribute') {
        await api.post(`${base}/distribute${qs}`, { note: '', verified: true });
        setNotice('已确认分发。');
      } else if (action === 'complete') {
        const versions = await api.get<{ latest: number }>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}/versions?path=${encodeURIComponent(folder)}`);
        await api.post(`${base}/complete${qs}`, { new_version: versions.latest, note: '', verified: true });
        setNotice('本轮已完成。');
      } else if (action === 'skip') {
        const reason = window.prompt('跳过本轮的原因');
        if (!reason?.trim()) return;
        await api.post(`${base}/skip${qs}`, { reason });
        setNotice('已跳过本轮。');
      } else {
        await api.post(`${base}/notify${qs}`, {});
        setNotice('已重新发送通知。');
      }
      await load();
      onChanged?.();
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
    finally { setBusy(false); }
  }

  const summary = useMemo(() => {
    if (!policy) return '';
    if (policy.status === 'fixed') return policy.fixed_reason ? `固定原因：${policy.fixed_reason}` : '固定不轮换';
    if (policy.status === 'draft') return '草稿：尚未启用，不会提醒或生成。';
    if (policy.status === 'paused') return '已暂停：不会提醒或生成。';
    if (!policy.next_run_at) return '没有下次轮换。';
    return `下次 ${fmtTime(policy.next_run_at)}`;
  }, [policy]);

  if (loading) return <Skeleton rows={4} />;

  return (
    <div className="rotation-policy">
      <div className="section-head">
        <div className="section-head-main">
          <h4>轮换策略</h4>
          <p className="section-hint">到期只创建待办/通知；生成新值不等于分发完成。外部真源仍需在上游更新。</p>
        </div>
        <div className="section-actions">
          {policy && <Pill tone={statusTone(policy.status)}>{statusLabel[policy.status]}</Pill>}
          {policy?.open_run && <Pill tone={statusTone(policy.open_run.status)}>{statusLabel[policy.open_run.status]}</Pill>}
        </div>
      </div>
      {err && <Notice tone="danger">{err}</Notice>}
      {notice && <Notice tone="ok">{notice}</Notice>}
      {!policy && !canWrite && <EmptyState icon="clock" title="未设置轮换">没有策略，也没有到期时间。</EmptyState>}

      {policy && (
        <>
          <p className="muted" role="status">
            {summary}
            {policy.owner?.name && policy.status !== 'fixed' ? ` · 负责人 ${policy.owner.name}` : ''}
            {policy.status === 'fixed' && policy.fixed_by_name ? ` · 由 ${policy.fixed_by_name} 标记` : ''}
          </p>
          {policy.status === 'fixed' ? (
            <div className="btn-row">
              {canWrite && <Button variant="default" icon="refresh" onClick={() => { setEditing(true); setFixedReason(policy.fixed_reason || ''); }}>改为轮换</Button>}
              {canWrite && <Button variant="quiet" onClick={() => void remove()}>删除</Button>}
            </div>
          ) : (
            <div className="btn-row">
              {canWrite && <Button variant="primary" icon="play" onClick={() => void runNow()} disabled={busy}>立即运行</Button>}
              {canWrite && <Button variant="default" icon="pencil" onClick={() => setEditing(true)}>编辑</Button>}
              {canWrite && policy.status === 'active' && <Button variant="quiet" onClick={() => void save('paused')} disabled={busy}>暂停</Button>}
              {canWrite && policy.status === 'paused' && <Button variant="quiet" onClick={() => void save('active')} disabled={busy}>恢复</Button>}
              {canWrite && <Button variant="quiet" onClick={() => setEditing(true)}>标记固定</Button>}
              {canWrite && <Button variant="quiet" onClick={() => void remove()}>删除</Button>}
            </div>
          )}
        </>
      )}

      {showForm && (
        <Section title={policy ? '编辑策略' : '设置策略'} hint="保存草稿不会启用；保存并启用才计算下次时间。">
          <div className="form-stack">
            <div className="form-row">
              <Field label="方式" htmlFor="rot-mode">
                <select id="rot-mode" value={mode} onChange={(e) => setMode(e.target.value as typeof mode)}>
                  <option value="generate">生成并写入（taboo 作为真源）</option>
                  <option value="manual">人工轮换</option>
                  <option value="webhook">外部 Webhook</option>
                </select>
              </Field>
              <Field label="宽限期（天）" htmlFor="rot-grace" hint="超过后轮次显示逾期，不会让密钥失效。">
                <input id="rot-grace" type="number" min={0} max={30} value={graceDays} onChange={(e) => setGraceDays(Number(e.target.value))} />
              </Field>
            </div>
            <div className="form-row">
              <Field label="周期" htmlFor="rot-kind">
                <select id="rot-kind" value={kind} onChange={(e) => setKind(e.target.value as typeof kind)}>
                  <option value="interval">固定间隔（按计划节拍）</option>
                  <option value="cron">cron（UTC）</option>
                </select>
              </Field>
              {kind === 'interval'
                ? <Field label="间隔（小时，至少 1）" htmlFor="rot-hours"><input id="rot-hours" type="number" min={1} value={hours} onChange={(e) => setHours(Number(e.target.value))} /></Field>
                : <Field label="cron（五段 UTC）" htmlFor="rot-cron"><input id="rot-cron" className="mono" value={cron} onChange={(e) => setCron(e.target.value)} placeholder="0 3 * * *" /></Field>}
            </div>
            {mode === 'generate' && (
              <div className="form-row">
                <Field label="生成档位" htmlFor="rot-profile">
                  <select id="rot-profile" value={profile} onChange={(e) => { setProfile(e.target.value); const p = e.target.value; setGenLength(p === 'alnum64' ? 64 : p === 'base64url32' ? 43 : p === 'hex32' ? 64 : p === 'password24' ? 24 : 32); }}>
                    {profiles.map((p) => <option key={p.value} value={p.value}>{p.label}</option>)}
                  </select>
                </Field>
                <Field label="随机长度" htmlFor="rot-length" hint="服务端确保至少 128 bit 熵。">
                  <input id="rot-length" type="number" min={16} max={128} value={genLength} onChange={(e) => setGenLength(Number(e.target.value))} disabled={profile !== 'custom' && profile !== 'alnum32' && profile !== 'alnum64' && profile !== 'password24'} />
                </Field>
              </div>
            )}
            {mode === 'generate' && (
              <>
                <Field label="前缀（可选）" htmlFor="rot-prefix"><input id="rot-prefix" className="mono" value={prefix} onChange={(e) => setPrefix(e.target.value)} placeholder="taboo_" /></Field>
                <Check label="到期自动生成" hint="默认关闭；消费方必须支持热重载。生成后仍需确认分发。" checked={autoGenerate} onChange={(e) => setAutoGenerate(e.target.checked)} />
                {!canReveal && <Notice tone="warn">你没有 reveal 权限，不能生成或查看新值；不能启用生成模式。</Notice>}
              </>
            )}
            <Field label="固定原因（仅“标记固定”时需要）" htmlFor="rot-fixed">
              <input id="rot-fixed" value={fixedReason} onChange={(e) => setFixedReason(e.target.value)} placeholder="例如：厂商托管，无法轮换" />
            </Field>
            <div className="form-actions">
              <Button variant="quiet" onClick={() => { setEditing(false); if (!policy) setFixedReason(''); }} disabled={busy}>取消</Button>
              <Button variant="quiet" onClick={() => void fix()} disabled={busy || !fixedReason.trim()}>标记固定</Button>
              <Button variant="default" onClick={() => void save('draft')} disabled={busy}>保存草稿</Button>
              <Button variant="primary" icon="seal" onClick={() => void save('active')} disabled={busy || (mode === 'generate' && !canReveal)} loading={busy}>保存并启用</Button>
            </div>
          </div>
        </Section>
      )}

      {generated && (
        <div className="once-secret">
          <h3>新值（仅此屏显示一次）</h3>
          <p className="mono">{generated}</p>
          <div className="form-actions">
            <Button variant="quiet" onClick={() => setGenerated('')}>隐藏</Button>
            <Button variant="default" onClick={() => void navigator.clipboard?.writeText(generated)}>复制</Button>
          </div>
        </div>
      )}

      {policy && (
        <Section title="轮次" hint="到期、生成、分发、完成都留痕；旧版本不自动删除。">
          {runs.length === 0 ? <p className="muted">还没有轮次。</p> : (
            <div className="table-scroll" role="region" aria-label="轮换轮次" tabIndex={0}>
              <table className="table compact">
                <thead><tr><th>到期</th><th>状态</th><th>版本</th><th>触发</th><th>操作</th></tr></thead>
                <tbody>
                  {runs.map((run) => (
                    <tr key={run.id}>
                      <td className="mono nowrap" title={timeTitle(run.due_at)}>{fmtTime(run.due_at)}</td>
                      <td><Pill tone={statusTone(run.status)}>{statusLabel[run.status] ?? run.status}</Pill></td>
                      <td className="mono">v{run.old_version} → {run.new_version ? `v${run.new_version}` : '—'}</td>
                      <td className="muted">{run.trigger === 'schedule' ? '定时' : run.trigger === 'retry' ? '重试' : '手动'}</td>
                      <td><Button variant="quiet" className="btn-sm" onClick={() => setSelected(selected?.id === run.id ? null : run)}>查看</Button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {selected && (
            <div className="rotation-run-detail">
              <div className="section-head">
                <div className="section-head-main"><h4>轮次详情</h4><p className="section-hint mono">{selected.id}</p></div>
                <Pill tone={statusTone(selected.status)}>{statusLabel[selected.status] ?? selected.status}</Pill>
              </div>
              <p className="muted">到期 {fmtTime(selected.due_at)} · 旧版本 v{selected.old_version} · {selected.owner_name ? `负责人 ${selected.owner_name}` : '未指派'}{selected.last_error ? ` · ${selected.last_error}` : ''}</p>
              <div className="btn-row">
                {canWrite && (selected.status === 'awaiting_generation' || (selected.status === 'overdue' && !selected.new_version && policy?.mode === 'generate')) && <Button variant="primary" icon="refresh" onClick={() => void runAction(selected, 'generate')} disabled={busy || !canReveal}>生成新值</Button>}
                {canWrite && (selected.status === 'awaiting_distribution' || (selected.status === 'overdue' && selected.new_version && policy?.mode === 'generate')) && <Button variant="primary" icon="check" onClick={() => void runAction(selected, 'distribute')} disabled={busy}>完成分发</Button>}
                {canWrite && (selected.status === 'awaiting_manual' || selected.status === 'awaiting_external' || (selected.status === 'overdue' && policy?.mode !== 'generate')) && <Button variant="default" icon="check" onClick={() => void runAction(selected, 'complete')} disabled={busy}>完成本轮</Button>}
                {canWrite && selected.status !== 'succeeded' && selected.status !== 'skipped' && selected.status !== 'canceled' && <Button variant="quiet" onClick={() => void runAction(selected, 'skip')} disabled={busy}>跳过</Button>}
                {canWrite && selected.status !== 'succeeded' && selected.status !== 'skipped' && selected.status !== 'canceled' && <Button variant="quiet" onClick={() => void runAction(selected, 'notify')} disabled={busy}>重新通知</Button>}
              </div>
            </div>
          )}
        </Section>
      )}
    </div>
  );
}

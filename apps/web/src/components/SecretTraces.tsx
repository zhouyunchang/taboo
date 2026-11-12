// 一把密钥的痕迹：只读、按天分组、可筛选、可加载更早。
// 组织全量审计仍在 AuditPanel；这里只消费同一份 append-only 记录。
import type { AuditLog } from '../api';
import { fmtTime, timeTitle } from '../time';
import Icon from '../design/Icon';
import { Button, Notice, Skeleton } from '../design/primitives';
import { useMemo } from 'react';

export type TraceFilter = 'all' | 'reveal' | 'write' | 'rotation';

const FILTERS: Array<{ key: TraceFilter; label: string }> = [
  { key: 'all', label: '全部' },
  { key: 'reveal', label: '显示' },
  { key: 'write', label: '写入' },
  { key: 'rotation', label: '轮换' },
];

export function traceMeta(log: AuditLog): Record<string, unknown> {
  // Go 端把 metadata 作为 json.RawMessage 返回：既可能是 JSON 字符串，也可能是对象。
  // 生成类型为了保守写成 string，这里两种都接受，避免把版本号读丢。
  const raw = log.metadata as unknown;
  if (!raw) return {};
  if (typeof raw === 'object') return raw as Record<string, unknown>;
  if (typeof raw === 'string') {
    try {
      const parsed = JSON.parse(raw);
      return parsed && typeof parsed === 'object' ? parsed as Record<string, unknown> : {};
    } catch {
      return {};
    }
  }
  return {};
}

export function traceCategory(action = ''): TraceFilter | 'other' {
  if (action === 'secrets.reveal') return 'reveal';
  if (action === 'secrets.create' || action === 'secrets.update' || action === 'secrets.import' || action === 'secrets.rollback' || action === 'secrets.delete') return 'write';
  if (action.startsWith('rotation.') || action.startsWith('secrets.rotation.')) return 'rotation';
  return 'other';
}

export function traceKind(action = ''): { className: string; label: string } {
  if (action === 'secrets.reveal') return { className: 'reveal', label: '显示' };
  if (action === 'secrets.rollback') return { className: 'rollback', label: '回滚' };
  if (action === 'secrets.delete') return { className: 'rollback', label: '删除' };
  if (action === 'secrets.create' || action === 'secrets.update' || action === 'secrets.import') return { className: 'write', label: '写入' };
  if (action.startsWith('rotation.') || action.startsWith('secrets.rotation.')) return { className: 'rotation', label: '轮换' };
  if (action === 'authz.denied') return { className: 'deny', label: '拒绝' };
  if (action.startsWith('approval.')) return { className: 'rotation', label: '审批' };
  return { className: 'read', label: action || '记录' };
}

function v(meta: Record<string, unknown>, key: string): string {
  const value = meta[key];
  return value === undefined || value === null || value === '' ? '' : String(value);
}

/** 把服务端 action/metadata 翻译成操作者能读懂的一句话。 */
export function describeTrace(log: AuditLog): string {
  const meta = traceMeta(log);
  const action = log.action ?? '';
  const version = v(meta, 'version');
  switch (action) {
    case 'secrets.create': return `创建 v${version || '1'}`;
    case 'secrets.update': return `写入 v${version || '—'}${v(meta, 'reason') ? ` · ${v(meta, 'reason')}` : ''}`;
    case 'secrets.reveal': return `读取 v${version || '—'} 明文`;
    case 'secrets.rollback': return `回滚 v${v(meta, 'from') || '—'} → v${v(meta, 'to') || '—'}`;
    case 'secrets.delete': return `删除密钥（v${version || '—'}）`;
    case 'secrets.rotation.update': {
      const status = meta.status === 'active' ? '启用' : meta.status === 'paused' ? '暂停' : meta.status === 'fixed' ? '固定' : meta.status === 'draft' ? '草稿' : '';
      return `更新轮换策略${status ? ` · ${status}` : ''}`;
    }
    case 'secrets.rotation.delete': return '删除轮换策略';
    case 'rotation.run.created': return `创建轮次${v(meta, 'trigger') ? ` · ${v(meta, 'trigger') === 'manual' ? '手动' : '定时'}` : ''}`;
    case 'rotation.run.generated': return `生成 v${v(meta, 'new_version') || '—'}`;
    case 'rotation.run.distributed': return '确认分发';
    case 'rotation.run.completed': return v(meta, 'new_version') ? `完成轮次 · v${v(meta, 'new_version')}` : '完成轮次';
    case 'rotation.run.failed': return `轮次失败${v(meta, 'reason') ? ` · ${v(meta, 'reason')}` : ''}`;
    case 'rotation.run.skipped': return `跳过轮次${v(meta, 'reason') ? ` · ${v(meta, 'reason')}` : ''}`;
    case 'rotation.run.notified': return '重新发送轮换通知';
    case 'rotation.run.retried': return '重试轮换轮次';
    case 'rotation.run.overdue': return '轮次逾期';
    case 'approval.create': return '提交审批';
    case 'approval.approve': return '审批通过';
    case 'authz.denied': return `拒绝操作${v(meta, 'action') ? ` · ${v(meta, 'action')}` : ''}`;
    default: return action || '记录';
  }
}

export default function SecretTraces({ logs, loading, error, canAudit, projectSlug, secretKey, filter, limit, onFilter, onMore }: {
  logs: AuditLog[] | null; loading: boolean; error: string; canAudit: boolean;
  projectSlug: string; secretKey: string;
  filter: TraceFilter; limit: number; onFilter: (f: TraceFilter) => void; onMore: () => void;
}) {
  const counts = useMemo(() => {
    const list = logs ?? [];
    return {
      all: list.length,
      reveal: list.filter((log) => traceCategory(log.action) === 'reveal').length,
      write: list.filter((log) => traceCategory(log.action) === 'write').length,
      rotation: list.filter((log) => traceCategory(log.action) === 'rotation').length,
    } satisfies Record<TraceFilter, number>;
  }, [logs]);

  if (!canAudit) {
    return (
      <div className="chapter-empty">
        <span className="chapter-empty-mark"><Icon name="lock" size={20} /></span>
        <div className="chapter-empty-body">
          <p className="chapter-empty-title">需要审计权限</p>
          <p className="chapter-empty-text">只有 owner 与 admin 可以查看这条密钥的痕迹。当前角色不能读取审计链，也不影响密钥本身的显示与使用。</p>
        </div>
      </div>
    );
  }

  if (loading && logs === null) return <Skeleton rows={6} />;
  if (error) return <Notice tone={error.includes('权限') ? 'warn' : 'danger'}>{error}</Notice>;

  const list = logs ?? [];
  const filtered = filter === 'all' ? list : list.filter((log) => traceCategory(log.action) === filter);
  const visible = filtered.slice(0, limit);
  const groups: Array<{ day: string; rows: AuditLog[] }> = [];
  for (const log of visible) {
    const day = fmtTime(log.created_at).slice(0, 10) || '未知';
    const last = groups[groups.length - 1];
    if (last && last.day === day) last.rows.push(log);
    else groups.push({ day, rows: [log] });
  }
  const remaining = filtered.length - visible.length;

  return (
    <div>
      <header className="chapter-head">
        <div className="chapter-head-main">
          <h2>此密钥的痕迹</h2>
          <p className="chapter-hint">显示、写入、回滚与轮换都会留下记录；审计链 append-only，不可修改。</p>
        </div>
        <div className="trace-filters" role="group" aria-label="筛选痕迹">
          {FILTERS.map((item) => (
            <button key={item.key} type="button" className="chip" aria-pressed={filter === item.key} onClick={() => onFilter(item.key)}>
              {item.label} <span className="n">{counts[item.key]}</span>
            </button>
          ))}
        </div>
      </header>

      {list.length === 0 ? (
        <div className="chapter-empty">
          <span className="chapter-empty-mark"><Icon name="audit" size={20} /></span>
          <div className="chapter-empty-body">
            <p className="chapter-empty-title">还没有记录</p>
            <p className="chapter-empty-text">显示明文、写入或轮换一次后，这里会出现第一条不可修改的痕迹。</p>
          </div>
        </div>
      ) : filtered.length === 0 ? (
        <p className="trace-empty">这个筛选下还没有记录。</p>
      ) : (
        <div>
          {groups.map((group) => (
            <section className="trace-day" key={group.day} aria-label={group.day}>
              <div className="trace-day-head"><span>{group.day}</span><span>{group.rows.length} 条</span></div>
              {group.rows.map((log, index) => {
                const kind = traceKind(log.action);
                return (
                  <div className="trace-row" key={log.id ?? `${group.day}:${index}`} title={log.action ?? ''}>
                    <span className="trace-time" title={timeTitle(log.created_at)}>{fmtTime(log.created_at).slice(11)}</span>
                    <span className={`trace-act ${kind.className}`}>{kind.label}</span>
                    <span className="trace-actor" title={log.actor_name ?? ''}>{log.actor_name || '—'}</span>
                    <span className="trace-desc">{describeTrace(log)}</span>
                    <span className="trace-ip">{log.ip || '—'}</span>
                  </div>
                );
              })}
            </section>
          ))}
          {remaining > 0 && (
            <div className="trace-more">
              <Button variant="quiet" className="btn-sm" onClick={onMore}>显示更早的 {remaining} 条</Button>
            </div>
          )}
        </div>
      )}

      <div className="chapter-foot">
        <span>只显示 <span className="mono" style={{ color: 'var(--porcelain-dim)' }}>project/{projectSlug}/secret/{secretKey}</span> 的 {list.length} 条记录；组织全量审计在「审计日志」。</span>
        <span className="mono">append-only</span>
      </div>
    </div>
  );
}

// 密钥记录页：一页卷宗。
// 左叶是封印与档案；右叶是版本 / 轮换 / 痕迹三章，一次只读一章。
// 页面不是抽屉：服务器仍是唯一鉴权事实源；明文不落盘、不预填、不常驻。
import { useCallback, useEffect, useRef, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent } from 'react';
import { api, ApiError, hasBrowserSession, type AuditLog, type SecretMeta, type SecretValue, type Version } from '../api';
import { fmtTime, timeTitle } from '../time';
import Icon from '../design/Icon';
import { Button, Notice, Pill, Skeleton } from '../design/primitives';
import Modal from '../components/Modal';
import useSecretMutation from '../components/useSecretMutation';
import { mask, useFlash } from '../ui/SecretRows';
import { FlashDialog } from '../design/feedback';
import SecretCompare from '../components/SecretCompare';
import SecretRotation, { type RotationSummary } from '../components/SecretRotation';
import SecretTraces, { type TraceFilter, describeTrace } from '../components/SecretTraces';

type Chapter = 'versions' | 'rotation' | 'traces';

function copyText(text: string) {
  if (navigator.clipboard?.writeText) return navigator.clipboard.writeText(text);
  return Promise.reject(new Error('clipboard unavailable'));
}

const statusLabel: Record<string, string> = {
  active: '运行中', paused: '已暂停', draft: '草稿', fixed: '固定',
};
const runStatusLabel: Record<string, string> = {
  due: '待处理', awaiting_generation: '待生成', awaiting_distribution: '待分发',
  awaiting_manual: '待人工', awaiting_external: '等待回写', overdue: '已逾期',
  succeeded: '已完成', failed: '失败', skipped: '已跳过', canceled: '已取消',
};

export default function SecretRecord({ meta, orgSlug, projectId, projectSlug, canAudit, onClose, onEdit, onChanged, onDeleted }: {
  meta: SecretMeta; orgSlug: string; projectId: string; projectSlug: string; canAudit: boolean;
  onClose: () => void; onEdit: () => void; onChanged: () => void; onDeleted: () => void;
}) {
  const key = meta.key;
  const folder = meta.folder;
  const canReveal = meta.canReveal;
  const canWrite = meta.canWrite;
  const canDelete = meta.canDelete ?? (!hasBrowserSession() && canWrite);
  const mutate = useSecretMutation();

  const [versions, setVersions] = useState<Version[] | null>(null);
  const [latest, setLatest] = useState(meta.version);
  const [canRollback, setCanRollback] = useState(Boolean(meta.canRollback));
  const [versionsError, setVersionsError] = useState('');
  const [chapter, setChapter] = useState<Chapter>('versions');
  const [compare, setCompare] = useState<number | null>(null);

  const [value, setValue] = useState<string | null>(null);
  const [shownLength, setShownLength] = useState<number | null>(null);
  const [resolved, setResolved] = useState(false);
  const [valueError, setValueError] = useState('');
  const [valueBusy, setValueBusy] = useState(false);
  const [copyBusy, setCopyBusy] = useState(false);
  const revealGeneration = useRef(0);
  const copied = useFlash(1600);

  const [audit, setAudit] = useState<AuditLog[] | null>(null);
  const [auditError, setAuditError] = useState('');
  const [auditLoading, setAuditLoading] = useState(false);
  const auditGeneration = useRef(0);
  const [traceFilter, setTraceFilter] = useState<TraceFilter>('all');
  const [traceLimit, setTraceLimit] = useState(8);

  const [policySummary, setPolicySummary] = useState<RotationSummary | null | undefined>(undefined);
  const [notice, setNotice] = useState('');
  const [rollbackTarget, setRollbackTarget] = useState<number | null>(null);
  const [rollbackBusy, setRollbackBusy] = useState(false);
  const [rollbackError, setRollbackError] = useState('');
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState('');
  const [deleteError, setDeleteError] = useState('');

  const titleRef = useRef<HTMLHeadingElement>(null);

  const loadVersions = useCallback(async () => {
    setVersionsError('');
    try {
      const data = await api.get<{ versions: Version[]; latest: number; canRollback?: boolean }>(
        `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(key)}/versions?path=${encodeURIComponent(folder)}`);
      const ordered = [...data.versions].sort((a, b) => b.version - a.version);
      setVersions(ordered);
      setLatest(data.latest);
      setCanRollback(data.canRollback ?? false);
    } catch (e) {
      setVersionsError(e instanceof ApiError ? e.message : String(e));
    }
  }, [projectId, key, folder]);

  const loadAudit = useCallback(async () => {
    if (!canAudit) {
      setAudit([]);
      setAuditError('');
      return;
    }
    const started = ++auditGeneration.current;
    setAuditLoading(true);
    try {
      const data = await api.get<{ logs: AuditLog[] }>(
        `/api/v1/orgs/${orgSlug}/audit?limit=200&resource=${encodeURIComponent(`project/${projectSlug}/secret/${key}`)}`);
      if (started !== auditGeneration.current) return;
      setAudit(data.logs ?? []);
      setAuditError('');
    } catch (e) {
      if (started !== auditGeneration.current) return;
      setAudit([]);
      setAuditError(e instanceof ApiError && e.status === 403 ? '需要审计权限才能查看这条密钥的痕迹。' : e instanceof ApiError ? e.message : String(e));
    } finally {
      if (started === auditGeneration.current) setAuditLoading(false);
    }
  }, [canAudit, orgSlug, projectSlug, key]);

  useEffect(() => { void loadVersions(); }, [loadVersions, meta.version]);
  useEffect(() => { void loadAudit(); }, [loadAudit, meta.version]);

  // 每次服务端写入后重新取版本；明文与对比的易失状态同时收回。
  useEffect(() => {
    revealGeneration.current += 1;
    setValue(null);
    setShownLength(null);
    setResolved(false);
    setCompare(null);
  }, [meta.version]);

  useEffect(() => {
    titleRef.current?.focus({ preventScroll: true });
  }, []);

  // 权限被刷新收回时，已挂载的明文与对比值也要立刻消失，不等失焦。
  useEffect(() => {
    if (canReveal) return;
    revealGeneration.current += 1;
    setValue(null);
    setResolved(false);
    setCompare(null);
  }, [canReveal]);

  useEffect(() => {
    const clear = () => {
      revealGeneration.current += 1;
      setValue(null);
      setResolved(false);
    };
    window.addEventListener('blur', clear);
    window.addEventListener('taboo:verification-unavailable', clear);
    return () => {
      window.removeEventListener('blur', clear);
      window.removeEventListener('taboo:verification-unavailable', clear);
    };
  }, []);

  useEffect(() => {
    if (!notice) return;
    const timer = setTimeout(() => setNotice(''), 7000);
    return () => clearTimeout(timer);
  }, [notice]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      if (document.querySelector('dialog[open]')) return;
      if (compare !== null) { event.preventDefault(); setCompare(null); return; }
      const target = event.target as HTMLElement | null;
      if (target?.closest('input, textarea, select, [contenteditable="true"]')) return;
      event.preventDefault();
      onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [compare, onClose]);

  async function reveal() {
    if (value !== null) {
      setValue(null);
      setResolved(false);
      return;
    }
    const started = ++revealGeneration.current;
    setValueBusy(true);
    setValueError('');
    try {
      const data = await api.get<SecretValue>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(key)}?path=${encodeURIComponent(folder)}`);
      if (started !== revealGeneration.current || !document.hasFocus()) return;
      setValue(String(data.value ?? ''));
      setShownLength(String(data.value ?? '').length);
      setResolved(Boolean(data.resolved));
      void loadAudit();
    } catch (e) {
      setValueError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setValueBusy(false);
    }
  }

  async function resolveReference() {
    const started = ++revealGeneration.current;
    setValueBusy(true);
    setValueError('');
    try {
      const data = await api.get<SecretValue>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(key)}?path=${encodeURIComponent(folder)}&resolve=true`);
      if (started !== revealGeneration.current || !document.hasFocus()) return;
      setValue(String(data.value ?? ''));
      setShownLength(String(data.value ?? '').length);
      setResolved(true);
      void loadAudit();
    } catch (e) {
      setValueError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setValueBusy(false);
    }
  }

  async function copy() {
    const started = revealGeneration.current;
    setCopyBusy(true);
    setValueError('');
    try {
      let text = value;
      let fetched = false;
      if (text === null) {
        const data = await api.get<SecretValue>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(key)}?path=${encodeURIComponent(folder)}`);
        text = String(data.value ?? '');
        fetched = true;
      }
      if (started !== revealGeneration.current || !document.hasFocus()) return;
      await copyText(text);
      copied.flash();
      setTimeout(() => { copyText('').catch(() => {}); }, 20_000);
      if (fetched) void loadAudit();
    } catch (e) {
      setValueError(e instanceof ApiError ? e.message : '复制失败');
    } finally {
      setCopyBusy(false);
    }
  }

  function switchChapter(next: Chapter, focus = false) {
    setChapter(next);
    if (next !== 'versions') setCompare(null);
    if (focus) requestAnimationFrame(() => document.querySelector<HTMLButtonElement>(`.chapter-tab[data-chapter="${next}"]`)?.focus());
  }

  function onTabsKey(event: ReactKeyboardEvent<HTMLDivElement>) {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    const tabs = Array.from(document.querySelectorAll<HTMLButtonElement>('.record .chapter-tab'));
    const index = tabs.indexOf(document.activeElement as HTMLButtonElement);
    let next = index;
    if (event.key === 'ArrowLeft') next = (index - 1 + tabs.length) % tabs.length;
    if (event.key === 'ArrowRight') next = (index + 1) % tabs.length;
    if (event.key === 'Home') next = 0;
    if (event.key === 'End') next = tabs.length - 1;
    switchChapter(tabs[next].dataset.chapter as Chapter, true);
  }

  async function rollback() {
    if (rollbackTarget === null) return;
    setRollbackBusy(true);
    setRollbackError('');
    try {
      const result = await mutate('post',
        `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(key)}/rollback?path=${encodeURIComponent(folder)}`,
        { version: rollbackTarget, expectedVersion: latest }) as { approval_id?: string } | undefined;
      if (result?.approval_id) {
        setRollbackTarget(null);
        setNotice('已提交审批，另一名管理员批准后才会回滚。');
        onChanged();
        return;
      }
      setRollbackTarget(null);
      setNotice(`已回滚到 v${rollbackTarget}，产生新版本。`);
      await loadVersions();
      void loadAudit();
      onChanged();
    } catch (e) {
      setRollbackError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setRollbackBusy(false);
    }
  }

  async function removeSecret() {
    if (deleteConfirm !== key) return;
    setDeleteBusy(true);
    setDeleteError('');
    try {
      const result = await mutate('del',
        `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(key)}?path=${encodeURIComponent(folder)}&expectedVersion=${latest}`) as { approval_id?: string } | undefined;
      if (result?.approval_id) {
        setDeleteOpen(false);
        setNotice('已提交删除审批；批准后密钥才会被删除。');
        return;
      }
      setDeleteOpen(false);
      onDeleted();
    } catch (e) {
      setDeleteError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setDeleteBusy(false);
    }
  }

  const latestVersion = latest;
  const current = versions ? versions.find((v) => v.version === latestVersion) : undefined;
  const lastUpdated = current?.created_at ?? meta.updated_at;
  const versionCount = versions?.length;

  const rotationState = (() => {
    if (policySummary === undefined) return { tone: 'none' as const, label: '读取中…', dot: 'none', dotText: '读取中', open: false };
    if (policySummary === null) return { tone: 'none' as const, label: '未设置', dot: 'none', dotText: '未设置', open: false };
    const open = Boolean(policySummary.openRunStatus);
    const tone = open ? 'warn' as const
      : policySummary.status === 'active' ? 'ok' as const
        : policySummary.status === 'draft' ? 'warn' as const : 'muted' as const;
    return {
      tone,
      label: statusLabel[policySummary.status] ?? policySummary.status,
      dot: tone === 'ok' ? 'ok' : tone === 'warn' ? 'warn' : 'muted',
      dotText: open ? (runStatusLabel[policySummary.openRunStatus!] ?? policySummary.openRunStatus!) : statusLabel[policySummary.status],
      open,
    };
  })();

  const nextRotationText = (() => {
    if (!policySummary) return '';
    if (policySummary.status === 'fixed') return '没有下次';
    if (policySummary.status !== 'active') return '';
    if (policySummary.cron) return '按 cron';
    if (!policySummary.nextAt) return '';
    return `下次 ${fmtTime(policySummary.nextAt).slice(5, 16)}`;
  })();

  const reveals = (audit ?? []).filter((log) => log.action === 'secrets.reveal').slice(0, 2);

  return (
    <div className="record">
      <header className="record-head">
        <div className="record-head-main">
          <button type="button" className="record-back" onClick={onClose}>
            <Icon name="arrow-left" size={13} />
            返回密钥
          </button>
          <div className="record-title-row">
            <h1 className="record-title" ref={titleRef} tabIndex={-1}>{key}</h1>
            <span className="pill pill-accent">v{latestVersion} 当前</span>
          </div>
          <p className="record-meta">
            <span className="mono">{projectSlug}{folder}</span>
            <span className="dot-sep" aria-hidden="true">·</span>
            <span>最后写入 {fmtTime(lastUpdated)}</span>
            <span className="dot-sep" aria-hidden="true">·</span>
            <span>{versionCount ?? '—'} 个版本</span>
          </p>
        </div>
        {canWrite && (
          <div className="record-head-actions">
            <Button variant="default" icon="pencil" aria-label="编辑" onClick={onEdit}>编辑</Button>
          </div>
        )}
      </header>

      <div className="record-sheet">
        <div className="record-left">
          <section className="seal-chamber" data-revealed={value !== null} aria-labelledby="record-seal-label">
            <div className="seal-chamber-head">
              <span className="chamber-label" id="record-seal-label">封印</span>
              <span className="chamber-state">{value !== null ? '明文中' : '已封印'}</span>
            </div>
            <div className="seal-value">
              {value !== null
                ? <code className="seal-plain">{value}</code>
                : shownLength !== null
                  ? <span className={`${mask(shownLength).cls} seal-cipher`} aria-label={`已显示过的值 · ${shownLength} 字符`}>{mask(shownLength).dots}</span>
                  : <span className="cipher cipher-mid seal-cipher" aria-label="密文，尚未显示">••••••••••••</span>}
            </div>
            <div className="seal-wm" aria-hidden="true">禁</div>
            <div className="seal-sub">
              <span>{value !== null ? `${value.length} 字符` : shownLength !== null ? `${shownLength} 字符` : '密文'}</span>
              <span className="sep" aria-hidden="true">·</span>
              <span>AES-256-GCM</span>
            </div>
            {canReveal ? (
              <div className="seal-actions">
                <Button variant="default" icon={value !== null ? 'eye-off' : 'eye'} aria-pressed={value !== null}
                  disabled={valueBusy} onClick={() => void reveal()}>
                  {valueBusy ? '读取中…' : value !== null ? '隐藏' : '显示明文'}
                </Button>
                <Button variant="primary" icon={copied.on ? 'check' : 'copy'} disabled={copyBusy} onClick={() => void copy()}>
                  {copied.on ? '已复制' : '复制'}
                </Button>
              </div>
            ) : (
              <p className="seal-hint">当前角色没有 reveal 权限，不能在这里显示或复制明文。</p>
            )}
            {value !== null && value.includes('${') && !resolved && canReveal && (
              <div className="seal-actions">
                <Button variant="quiet" icon="link" disabled={valueBusy} onClick={() => void resolveReference()}>解析引用</Button>
              </div>
            )}
            <p className="seal-hint">
              {value !== null ? '失焦即收回；明文不会写入剪贴板，除非你主动复制。' : '明文只在此处停留，失焦即收回。复制后 20 秒清空剪贴板。'}
            </p>
            {valueError && <div style={{ marginTop: 8 }}><Notice tone="danger">{valueError}</Notice></div>}
          </section>

          <section className="register" aria-labelledby="record-register-label">
            <div className="block-head">
              <h2 id="record-register-label">档案</h2>
              <span className="block-hint">随版本写入</span>
            </div>
            <dl className="ledger">
              <div className="ledger-row"><dt>位置</dt><dd className="mono">{projectSlug}{folder}</dd></div>
              <div className="ledger-row">
                <dt>标签</dt>
                <dd>{meta.tags.length > 0 ? <span className="tag-row">{meta.tags.map((tag) => <Pill key={tag} tone="muted">{tag}</Pill>)}</span> : <span className="faint">—</span>}</dd>
              </div>
              <div className="ledger-row"><dt>备注</dt><dd>{meta.comment || '—'}</dd></div>
              <div className="ledger-row">
                <dt>轮换</dt>
                <dd id="record-rotation">
                  {policySummary === undefined ? <span className="faint">读取中…</span> : policySummary === null ? (
                    <>
                      <Pill tone="muted">未设置</Pill>
                      <button type="button" className="link-inline" onClick={() => switchChapter('rotation')}>设置</button>
                    </>
                  ) : (
                    <>
                      {policySummary.openRunStatus
                        ? <Pill tone="warn">{runStatusLabel[policySummary.openRunStatus] ?? policySummary.openRunStatus}</Pill>
                        : <Pill tone={rotationState.tone === 'ok' ? 'ok' : rotationState.tone === 'warn' ? 'warn' : 'muted'}>{rotationState.label}</Pill>}
                      {nextRotationText && <span className="mono">{nextRotationText}</span>}
                      <button type="button" className="link-inline" onClick={() => switchChapter('rotation')}>管理</button>
                    </>
                  )}
                </dd>
              </div>
            </dl>
          </section>

          {canDelete && (
            <div className="danger-zone">
              <button type="button" className="danger-btn" onClick={() => { setDeleteConfirm(''); setDeleteError(''); setDeleteOpen(true); }}>
                <Icon name="trash" size={13} />
                删除密钥
              </button>
              <p className="danger-note">{policySummary ? '版本历史与轮换记录会一并删除，不可恢复。' : '版本历史会一并删除，不可恢复。'}</p>
            </div>
          )}
        </div>

        <div className="record-main">
          <div className="chapters" role="tablist" aria-label="密钥卷宗章节" onKeyDown={onTabsKey}>
            <button type="button" className="chapter-tab" id="tab-versions" role="tab" aria-selected={chapter === 'versions'}
              aria-controls="chapter-versions" tabIndex={chapter === 'versions' ? 0 : -1} data-chapter="versions" onClick={() => switchChapter('versions')}>
              版本 <span className="chapter-count">{versionCount ?? '—'}</span>
            </button>
            <button type="button" className="chapter-tab" id="tab-rotation" role="tab" aria-selected={chapter === 'rotation'}
              aria-controls="chapter-rotation" tabIndex={chapter === 'rotation' ? 0 : -1} data-chapter="rotation" onClick={() => switchChapter('rotation')}>
              轮换 <span className={`chapter-dot ${rotationState.dot}`} aria-hidden="true" />
              <span className="sr-only">{rotationState.dotText}</span>
            </button>
            <button type="button" className="chapter-tab" id="tab-traces" role="tab" aria-selected={chapter === 'traces'}
              aria-controls="chapter-traces" tabIndex={chapter === 'traces' ? 0 : -1} data-chapter="traces" onClick={() => switchChapter('traces')}>
              痕迹 {canAudit && <span className="chapter-count">{audit?.length ?? '—'}</span>}
            </button>
          </div>

          <div className="chapter" id="chapter-versions" role="tabpanel" aria-labelledby="tab-versions" tabIndex={0} hidden={chapter !== 'versions'}>
            <div className="chapter-notice" role="status" aria-live="polite">{notice && <Notice tone="ok">{notice}</Notice>}</div>
            {versionsError && <Notice tone="danger">{versionsError}</Notice>}
            {compare !== null ? (
              <SecretCompare projectId={projectId} folder={folder} secretKey={key}
                versions={versions ?? []} initialLeft={compare} canReveal={canReveal}
                onBack={() => setCompare(null)} onAudit={() => void loadAudit()} />
            ) : (
              <>
                <header className="chapter-head">
                  <div className="chapter-head-main">
                    <h2>版本历史</h2>
                    <p className="chapter-hint">每次写入产生新版本；回滚同样产生新版本，旧版本不删除。{!canReveal ? ' 需要 reveal 权限才能对比版本内容。' : ''}</p>
                  </div>
                  <div className="chapter-actions">
                    <Button variant="default" icon="arrow-right" disabled={!versions || versions.length < 2 || !canReveal}
                      onClick={() => setCompare(versions && versions.length > 1 ? versions[1].version : latest)}>
                      对比版本
                    </Button>
                  </div>
                </header>

                {versions === null && !versionsError ? <Skeleton rows={4} /> : (
                  <>
                    <ol className="version-list">
                      {(versions ?? []).map((version) => {
                        const isCurrent = version.version === latest;
                        return (
                          <li key={version.version} className={`version-row ${isCurrent ? 'current' : ''}`}>
                            <div>
                              <div className="version-top">
                                <span className="version-num">v{version.version}</span>
                                {isCurrent && <Pill tone="accent">当前</Pill>}
                                <span className="version-time" title={timeTitle(version.created_at)}>{fmtTime(version.created_at)}</span>
                              </div>
                              <div className="version-sub">写入者 <span className="mono">{version.created_by.slice(0, 8)}</span></div>
                            </div>
                            <div className="version-actions">
                              {!isCurrent && canReveal && (
                                <Button variant="quiet" onClick={() => setCompare(version.version)}>对比</Button>
                              )}
                              {!isCurrent && canRollback && (
                                <Button variant="quiet" disabled={rollbackBusy} onClick={() => { setRollbackError(''); setRollbackTarget(version.version); }}>回滚到此</Button>
                              )}
                            </div>
                          </li>
                        );
                      })}
                    </ol>
                    {versions && versions.length < 2 && (
                      <div className="version-teach">
                        <Icon name="history" />
                        <div className="version-teach-body">
                          <p className="version-teach-title">至少需要两个版本才能对比</p>
                          <p className="version-teach-text">下一次写入会产生 v{latest + 1}，这一版会作为 v{latest} 保留。对比时默认掩码，只有主动显示才读明文。</p>
                        </div>
                      </div>
                    )}
                    <div className="chapter-foot">
                      <span>{versions && versions.length < 2 ? '这是第一版；下一次写入后，旧值会保留在这里。' : `共 ${versions?.length ?? 0} 个版本；旧版本不会因为回滚或轮换被删除。`}</span>
                      <span className="mono">{versions && versions.length > 0 ? `v${versions[versions.length - 1].version} — v${versions[0].version}` : ''}</span>
                    </div>
                  </>
                )}

                <section className="recent-reads" aria-labelledby="recent-reads-label">
                  <div className="block-head">
                    <h2 id="recent-reads-label">最近读取</h2>
                    {canAudit && <button type="button" className="link-inline" onClick={() => switchChapter('traces')}>全部痕迹 <Icon name="arrow-right" size={11} /></button>}
                  </div>
                  {!canAudit ? (
                    <p className="recent-empty">需要审计权限才能查看读取记录。</p>
                  ) : auditLoading && audit === null ? (
                    <p className="recent-empty">读取中…</p>
                  ) : auditError ? (
                    <p className="recent-empty">{auditError}</p>
                  ) : reveals.length === 0 ? (
                    <p className="recent-empty">还没有人显示过这把密钥。第一次显示会出现在这里，并进入审计。</p>
                  ) : (
                    <div className="recent-list">
                      {reveals.map((log, index) => (
                        <div className="recent-row" key={log.id ?? index}>
                          <span className="recent-who" title={log.actor_name ?? ''}>{log.actor_name || '—'}</span>
                          <span className="recent-desc">{describeTrace(log)}</span>
                          <span className="recent-when" title={timeTitle(log.created_at)}>{fmtTime(log.created_at).slice(5, 16)}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </section>
              </>
            )}
          </div>

          <div className="chapter" id="chapter-rotation" role="tabpanel" aria-labelledby="tab-rotation" tabIndex={0} hidden={chapter !== 'rotation'}>
            <SecretRotation projectId={projectId} folder={folder} secretKey={key} canWrite={canWrite} canReveal={canReveal}
              onSummary={setPolicySummary}
              onActivity={() => void loadAudit()}
              onSecretChanged={() => { void loadVersions(); onChanged(); }} />
          </div>

          <div className="chapter" id="chapter-traces" role="tabpanel" aria-labelledby="tab-traces" tabIndex={0} hidden={chapter !== 'traces'}>
            <SecretTraces logs={audit} loading={auditLoading} error={auditError} canAudit={canAudit}
              projectSlug={projectSlug} secretKey={key} filter={traceFilter} limit={traceLimit}
              onFilter={(next) => { setTraceFilter(next); setTraceLimit(8); }}
              onMore={() => setTraceLimit((limit) => limit + 20)} />
          </div>
        </div>
      </div>

      {copied.on && <FlashDialog text="已复制 · 20 秒后清空剪贴板" />}

      {rollbackTarget !== null && (
        <Modal title={`回滚到 v${rollbackTarget}？`} onClose={() => { if (!rollbackBusy) setRollbackTarget(null); }} busy={rollbackBusy}>
          <div className="form-stack">
            <p>将 v{rollbackTarget} 的值写成 v{latest + 1}，旧版本全部保留。</p>
            <p>回滚不是删除，也不会让旧值失效；消费方需要重新读取。这次动作会进入审计链。</p>
            {rollbackError && <Notice tone="danger">{rollbackError}</Notice>}
            <div className="form-actions">
              <Button variant="quiet" disabled={rollbackBusy} onClick={() => setRollbackTarget(null)}>取消</Button>
              <Button variant="stamp" icon="seal" loading={rollbackBusy} onClick={() => void rollback()}>回滚到此</Button>
            </div>
          </div>
        </Modal>
      )}

      {deleteOpen && (
        <Modal title={`删除 ${key}？`} onClose={() => { if (!deleteBusy) setDeleteOpen(false); }} busy={deleteBusy}>
          <div className="form-stack">
            <p>这会移除 {versions ? `${versions.length} 个版本` : '全部版本'}{policySummary ? '与轮换记录' : ''}。审计痕迹保留，但密钥本身不可恢复。</p>
            <label className="field" htmlFor="record-delete-confirm">
              <span className="field-label">输入 <span className="mono">{key}</span> 以确认</span>
              <input id="record-delete-confirm" className="mono" autoComplete="off" spellCheck={false} value={deleteConfirm} onChange={(e) => setDeleteConfirm(e.target.value)} />
            </label>
            {deleteError && <Notice tone="danger">{deleteError}</Notice>}
            <div className="form-actions">
              <Button variant="quiet" disabled={deleteBusy} onClick={() => setDeleteOpen(false)}>取消</Button>
              <Button variant="danger" disabled={deleteBusy || deleteConfirm !== key} loading={deleteBusy} onClick={() => void removeSecret()}>删除密钥</Button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}

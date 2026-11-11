// 密钥详情抽屉：原生 <dialog> 右侧滑入，焦点捕获 / Escape / 焦点归还由浏览器负责。
// 版本历史是这条密钥留下的「留痕」，所以用时间线而不是表格卡片。
import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError, hasBrowserSession } from '../api';
import type { SecretMeta, Version } from '../api';
import useSecretMutation from '../components/useSecretMutation';
import VersionDiff from '../components/VersionDiff';
import RotationPolicy from '../components/RotationPolicy';
import { fmtTime, timeTitle } from '../time';
import Icon from '../design/Icon';
import { Button, IconButton, MetaList, Notice, Pill, Skeleton } from '../design/primitives';
import { useFlash } from '../ui/SecretRows';
import { FlashDialog } from '../design/feedback';

function copyText(text: string) {
  return navigator.clipboard?.writeText(text) ?? Promise.reject(new Error('clipboard unavailable'));
}

export default function SecretDetail({ projectId, projectSlug, meta, onClose, onChanged, onEdit }: {
  projectId: string; projectSlug: string; meta: SecretMeta;
  onClose: () => void; onChanged: () => void; onEdit: () => void;
}) {
  const secretKey = meta.key;
  const folder = meta.folder;
  const mutate = useSecretMutation();
  const dialogRef = useRef<HTMLDialogElement>(null);
  const titleRef = useRef<HTMLHeadingElement>(null);
  const [versions, setVersions] = useState<Version[] | null>(null);
  const [latest, setLatest] = useState(0);
  const [canRollback, setCanRollback] = useState(false);
  const [err, setErr] = useState('');
  const [busyVersion, setBusyVersion] = useState<number | null>(null);
  const [value, setValue] = useState<string | null>(null);
  const [valueErr, setValueErr] = useState('');
  const [valueBusy, setValueBusy] = useState(false);
  const copied = useFlash(1600);

  const load = useCallback(() => {
    api.get<{ versions: Version[]; latest: number; canRollback?: boolean }>(
      `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}/versions?path=${encodeURIComponent(folder)}`)
      .then((d) => { setVersions(d.versions); setLatest(d.latest); setCanRollback(d.canRollback ?? (!hasBrowserSession() && meta.canWrite !== false)); setErr(''); })
      .catch((e) => setErr(e instanceof ApiError ? e.message : String(e)));
  }, [projectId, secretKey, folder, meta.canWrite]);
  useEffect(load, [load]);

  // showModal：背景 inert、焦点入内。关闭时把焦点显式还给打开它的那个元素
  // （组件卸载时 showModal 的 dialog 已离开文档，浏览器无法自动归还）。
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog || dialog.open) return;
    const trigger = document.activeElement as HTMLElement | null;
    dialog.showModal();
    titleRef.current?.focus();
    const restore = () => {
      const target = trigger?.isConnected ? trigger : null;
      // 同一帧内同步归还，避免键盘用户焦点落到 body。
      target?.focus({ preventScroll: false });
      if (!target) return;
      requestAnimationFrame(() => { if (document.activeElement === document.body) target.focus(); });
    };
    dialog.addEventListener('close', restore, { once: true });
    return () => {
      dialog.removeEventListener('close', restore);
      restore();
    };
  }, []);

  // 抽屉里的明文同样是易失状态：失焦或权限刷新即收起。
  useEffect(() => {
    const clear = () => setValue(null);
    window.addEventListener('blur', clear);
    window.addEventListener('taboo:verification-unavailable', clear);
    return () => { window.removeEventListener('blur', clear); window.removeEventListener('taboo:verification-unavailable', clear); };
  }, [meta.canReveal]);

  async function reveal() {
    if (value !== null) { setValue(null); return; }
    setValueBusy(true); setValueErr('');
    try {
      const d = await api.get<{ value: string }>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}?path=${encodeURIComponent(folder)}`);
      if (document.hasFocus()) setValue(d.value);
    } catch (e) { setValueErr(e instanceof ApiError ? e.message : String(e)); }
    finally { setValueBusy(false); }
  }

  async function copy() {
    setValueErr('');
    try {
      const text = value ?? (await api.get<{ value: string }>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}?path=${encodeURIComponent(folder)}`)).value;
      await copyText(text);
      copied.flash();
      setTimeout(() => { copyText('').catch(() => {}); }, 20_000);
    } catch { setValueErr('复制失败'); }
  }

  async function rollback(v: number) {
    if (!window.confirm(`回滚 ${secretKey} 到 v${v}？（将产生新版本）`)) return;
    setBusyVersion(v); setErr('');
    try {
      await mutate('post', `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}/rollback?path=${encodeURIComponent(folder)}`, { version: v, expectedVersion: latest });
      load(); onChanged();
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
    finally { setBusyVersion(null); }
  }

  return (
    <dialog ref={dialogRef} className="detail-dialog" aria-label="版本历史"
      onCancel={(e) => { e.preventDefault(); onClose(); }}>
      <header className="detail-head">
        <div className="detail-title">
          <h2 className="k" ref={titleRef} tabIndex={-1}>{secretKey}</h2>
          <span className="path">{projectSlug}{folder}</span>
        </div>
        <IconButton icon="x" label="关闭" onClick={onClose} />
      </header>

      <div className="detail-body">
        <div className="detail-value">
          <div className="detail-value-row">
            <span className="detail-value-text">
              {value !== null
                ? value
                : <span className="cipher">••••••••••••</span>}
            </span>
            {meta.canReveal && (
              <>
                <button type="button" className={`row-act ${value !== null ? 'on' : ''}`} aria-label={value !== null ? '隐藏' : '显示'}
                  aria-pressed={value !== null} title={value !== null ? '隐藏' : '显示'} disabled={valueBusy} onClick={() => void reveal()}>
                  <Icon name={value !== null ? 'eye-off' : 'eye'} />
                </button>
                <button type="button" className={`row-act ${copied.on ? 'done' : ''}`} aria-label="复制" title={copied.on ? '已复制' : '复制'} onClick={() => void copy()}>
                  <Icon name={copied.on ? 'check' : 'copy'} />
                </button>
              </>
            )}
          </div>
          <span className="cell-sub">明文只在此处停留，失焦即收回</span>
          {meta.canWrite && (
            <div className="btn-row">
              <Button icon="pencil" variant="default" className="btn-sm" onClick={onEdit}>编辑</Button>
            </div>
          )}
          {valueErr && <Notice tone="danger">{valueErr}</Notice>}
        </div>

        <MetaList items={[
          { label: '位置', value: `${projectSlug}${folder}`, mono: true },
          { label: 'Key', value: secretKey, mono: true },
          { label: '当前版本', value: `v${meta.version}` },
          { label: '更新时间', value: fmtTime(meta.updated_at) },
          { label: '标签', value: meta.tags.length ? meta.tags.join(' · ') : '—' },
          { label: '备注', value: meta.comment || '—' },
        ]} />

        <section className="section">
          <div className="section-head">
            <div className="section-head-main">
              <h3>版本历史</h3>
              <p className="section-hint">每次写入产生新版本；回滚同样产生新版本，旧版本不删除。</p>
            </div>
            {canRollback && <Pill tone="muted" icon="history">{versions?.length ?? 0} 版</Pill>}
          </div>
          {err && <Notice tone="danger">{err}</Notice>}
          {versions === null && !err && <Skeleton rows={4} />}
          {versions !== null && (
            <ol className="timeline">
              {versions.map((v) => (
                <li key={v.version} className={v.version === latest ? 'current' : undefined}>
                  <span className="v">v{v.version}</span>
                  <span className="who">{v.created_by.slice(0, 8)}{v.version === latest ? '（当前）' : ''}</span>
                  <span className="when" title={timeTitle(v.created_at)}>{fmtTime(v.created_at)}</span>
                  {canRollback && v.version !== latest && (
                    <Button variant="quiet" className="btn-sm" disabled={busyVersion !== null} onClick={() => void rollback(v.version)}>
                      {busyVersion === v.version ? '回滚中…' : '回滚到此'}
                    </Button>
                  )}
                </li>
              ))}
            </ol>
          )}
        </section>

        {meta.canReveal
          ? <VersionDiff projectId={projectId} folder={folder} secretKey={secretKey} versions={versions ?? []} />
          : <p className="muted">需要 reveal 权限才能对比版本内容。</p>}

        <RotationPolicy projectId={projectId} folder={folder} secretKey={secretKey} canWrite={meta.canWrite} />

        {copied.on && <FlashDialog text="已复制 · 20 秒后清空剪贴板" />}
        <div className="form-actions">
          <Button variant="quiet" onClick={onClose}>关闭</Button>
        </div>
      </div>
    </dialog>
  );
}

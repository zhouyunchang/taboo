// 版本对比：默认掩码，主动显示明文时才读取两个历史版本；失焦即收回。
// 这里不预先知道值的长度——掩码用固定的 12 枚封条，不对长度做任何声称。
import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError, type SecretVersionValue, type Version } from '../api';
import { fmtTime } from '../time';
import Icon from '../design/Icon';
import { Button, Notice } from '../design/primitives';

type Line = { kind: 'same' | 'add' | 'del'; text: string; no: number };

export function commonPrefix(a: string, b: string): number {
  const max = Math.min(a.length, b.length);
  let i = 0;
  while (i < max && a[i] === b[i]) i += 1;
  return i;
}

export function commonSuffix(a: string, b: string, prefix: number): number {
  const max = Math.min(a.length, b.length) - prefix;
  let i = 0;
  while (i < max && a[a.length - 1 - i] === b[b.length - 1 - i]) i += 1;
  return i;
}

export function charDelta(a: string, b: string) {
  const prefix = commonPrefix(a, b);
  const suffix = commonSuffix(a, b, prefix);
  return {
    prefix: a.slice(0, prefix),
    suffix: suffix ? a.slice(a.length - suffix) : '',
    oldMid: a.slice(prefix, a.length - suffix),
    newMid: b.slice(prefix, b.length - suffix),
  };
}

/** 带行号的逐行对照；维持既有 line-diff 的 LCS 行为。 */
export function lineDiffRows(a: string, b: string): Line[] {
  const A = a.split('\n');
  const B = b.split('\n');
  if (A.length * B.length > 20000) {
    const out: Line[] = [];
    const n = Math.max(A.length, B.length);
    for (let i = 0; i < n; i += 1) {
      if (A[i] === B[i]) out.push({ kind: 'same', text: A[i] ?? '', no: i + 1 });
      else {
        if (A[i] !== undefined) out.push({ kind: 'del', text: A[i], no: i + 1 });
        if (B[i] !== undefined) out.push({ kind: 'add', text: B[i], no: i + 1 });
      }
    }
    return out;
  }
  const dp = Array.from({ length: A.length + 1 }, () => new Array<number>(B.length + 1).fill(0));
  for (let i = A.length - 1; i >= 0; i -= 1) {
    for (let j = B.length - 1; j >= 0; j -= 1) {
      dp[i][j] = A[i] === B[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }
  const out: Line[] = [];
  let i = 0;
  let j = 0;
  let oldNo = 1;
  let newNo = 1;
  while (i < A.length && j < B.length) {
    if (A[i] === B[j]) {
      out.push({ kind: 'same', text: A[i], no: oldNo });
      i += 1; j += 1; oldNo += 1; newNo += 1;
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      out.push({ kind: 'del', text: A[i], no: oldNo });
      i += 1; oldNo += 1;
    } else {
      out.push({ kind: 'add', text: B[j], no: newNo });
      j += 1; newNo += 1;
    }
  }
  while (i < A.length) { out.push({ kind: 'del', text: A[i], no: oldNo }); i += 1; oldNo += 1; }
  while (j < B.length) { out.push({ kind: 'add', text: B[j], no: newNo }); j += 1; newNo += 1; }
  return out;
}

function maskedPlate(meta: Version | undefined, side: 'old' | 'new') {
  return <section className={`value-side ${side === 'new' ? 'new' : ''}`} aria-label={`${side === 'old' ? '基准' : '对比'}版本 v${meta?.version ?? ''}`}>
    <div className="value-side-head"><span className="mono">v{meta?.version ?? ''}</span><span className="mono">{meta?.created_by?.slice(0, 8) || '—'}</span></div>
    <div className="value-side-body"><span className="cipher cipher-mid">••••••••••••</span></div>
    <div className="value-side-foot">掩码 · {meta ? fmtTime(meta.created_at).slice(11) : '—'}</div>
  </section>;
}

export default function SecretCompare({ projectId, folder, secretKey, versions, initialLeft, canReveal, onBack, onAudit }: {
  projectId: string; folder: string; secretKey: string; versions: Version[];
  initialLeft?: number; canReveal: boolean; onBack: () => void; onAudit?: () => void;
}) {
  const ordered = [...versions].sort((a, b) => a.version - b.version);
  const latest = ordered[ordered.length - 1];
  const fallbackLeft = ordered.length > 1 ? ordered[ordered.length - 2].version : latest?.version ?? 1;
  const [left, setLeft] = useState(initialLeft && initialLeft !== latest?.version ? initialLeft : fallbackLeft);
  const [right, setRight] = useState(latest?.version ?? 1);
  const [revealed, setRevealed] = useState(false);
  const [leftValue, setLeftValue] = useState('');
  const [rightValue, setRightValue] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const generation = useRef(0);
  const backRef = useRef<HTMLButtonElement>(null);

  useEffect(() => { backRef.current?.focus({ preventScroll: true }); }, []);

  const clearSensitive = useCallback(() => {
    generation.current += 1;
    setRevealed(false);
    setLeftValue('');
    setRightValue('');
  }, []);

  useEffect(() => {
    const clear = () => clearSensitive();
    window.addEventListener('blur', clear);
    window.addEventListener('taboo:verification-unavailable', clear);
    return () => {
      window.removeEventListener('blur', clear);
      window.removeEventListener('taboo:verification-unavailable', clear);
    };
  }, [clearSensitive]);

  const leftMeta = ordered.find((v) => v.version === left);
  const rightMeta = ordered.find((v) => v.version === right);

  async function togglePlain() {
    if (!canReveal) return;
    if (revealed) { clearSensitive(); return; }
    const started = ++generation.current;
    setBusy(true);
    setErr('');
    try {
      const base = `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}/versions`;
      const query = `?path=${encodeURIComponent(folder)}`;
      const [a, b] = await Promise.all([
        api.get<SecretVersionValue>(`${base}/${left}${query}`),
        api.get<SecretVersionValue>(`${base}/${right}${query}`),
      ]);
      if (started !== generation.current || !document.hasFocus()) return;
      setLeftValue(a.value);
      setRightValue(b.value);
      setRevealed(true);
      onAudit?.();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : String(e));
    } finally {
      if (started === generation.current) setBusy(false);
    }
  }

  function choose(side: 'left' | 'right', value: number) {
    clearSensitive();
    setErr('');
    if (side === 'left') setLeft(value); else setRight(value);
  }

  const same = leftValue === rightValue;
  const multiline = leftValue.includes('\n') || rightValue.includes('\n');
  const delta = charDelta(leftValue, rightValue);
  const rows = multiline ? lineDiffRows(leftValue, rightValue) : [];
  const adds = multiline ? rows.filter((r) => r.kind === 'add').length : delta.newMid.length;
  const dels = multiline ? rows.filter((r) => r.kind === 'del').length : delta.oldMid.length;

  return (
    <div id="compare-view">
      <div className="compare-head">
        <button type="button" className="back-btn" ref={backRef} onClick={onBack}>
          <Icon name="arrow-left" size={13} />版本历史
        </button>
        <div className="compare-title"><h2>对比版本</h2><span className="mono">v{left} ⇄ v{right}</span></div>
      </div>

      <div className="compare-bar">
        <label className="field">
          <span className="field-label">基准</span>
          <select aria-label="基准版本" value={left} onChange={(e) => choose('left', Number(e.target.value))} disabled={busy || revealed}>
            {ordered.map((v) => <option key={v.version} value={v.version} disabled={v.version === right}>v{v.version} · {fmtTime(v.created_at).slice(0, 10)}</option>)}
          </select>
        </label>
        <span className="compare-arrow" aria-hidden="true"><Icon name="arrow-right" size={14} /></span>
        <label className="field">
          <span className="field-label">对比</span>
          <select aria-label="对比版本" value={right} onChange={(e) => choose('right', Number(e.target.value))} disabled={busy || revealed}>
            {ordered.map((v) => <option key={v.version} value={v.version} disabled={v.version === left}>v{v.version} · {fmtTime(v.created_at).slice(0, 10)}</option>)}
          </select>
        </label>
        <button type="button" className="btn btn-quiet btn-sm" id="compare-swap" aria-label="交换基准与对比版本" title="交换"
          disabled={busy || revealed} onClick={() => { const l = left; choose('left', right); setRight(l); }}>
          <Icon name="swap" />
        </button>
        <span className="spacer" />
        <Button variant="default" id="compare-reveal" onClick={() => void togglePlain()} disabled={busy || !canReveal}
          icon={revealed ? 'eye-off' : 'eye'}>
          {busy ? '读取中…' : revealed ? '隐藏明文' : '显示明文'}
        </Button>
      </div>
      <p className="compare-note" role="status">
        {!canReveal
          ? '需要 reveal 权限才能读取版本内容。'
          : revealed
            ? '正在显示明文；失焦即收回。对比不会写入新版本。'
            : '值默认掩码。显示明文才会读取两个版本并进入审计；失焦即收回。'}
      </p>

      {err && <div style={{ marginTop: 12 }}><Notice tone="danger">{err}</Notice></div>}

      {revealed && (
        <div className="compare-summary" role="status">
          <span>{same ? '两个版本的值相同' : multiline ? '逐行对照' : '整值对照'}</span>
          {!same && adds > 0 && <span className="diff-add-count">+{adds} {multiline ? '行' : '字符'}</span>}
          {!same && dels > 0 && <span className="diff-del-count">−{dels} {multiline ? '行' : '字符'}</span>}
          <span className="mono">{fmtTime(leftMeta?.created_at)} → {fmtTime(rightMeta?.created_at)}</span>
        </div>
      )}

      {!revealed ? (
        <>
          <div className="compare-summary"><span>值已掩码</span><span className="mono">{fmtTime(leftMeta?.created_at)} → {fmtTime(rightMeta?.created_at)}</span></div>
          <div className="value-compare">
            {maskedPlate(leftMeta, 'old')}
            <div className="value-arrow" aria-hidden="true"><Icon name="arrow-right" /></div>
            {maskedPlate(rightMeta, 'new')}
          </div>
        </>
      ) : multiline ? (
        <div className="diff-box">
          <div className="diff-box-head"><span>行级对照</span><span>明文</span></div>
          <div className="diff-lines">
            {rows.map((row, index) => (
              <div className={`diff-line ${row.kind}`} key={`${row.kind}:${index}`}>
                <span className="sign">{row.kind === 'add' ? '+' : row.kind === 'del' ? '−' : ''}</span>
                <span className="ln">{row.no}</span>
                <span className="text">{row.text || '（空行）'}</span>
              </div>
            ))}
          </div>
        </div>
      ) : (
        <div className="value-compare">
          <section className="value-side" aria-label={`基准版本 v${left}`}>
            <div className="value-side-head"><span className="mono">v{left}</span><span className="mono">{leftMeta?.created_by?.slice(0, 8) || '—'}</span></div>
            <div className="value-side-body">
              <span className="common">{delta.prefix}</span>
              {delta.oldMid && <span className="changed-del">{delta.oldMid}</span>}
              <span className="common">{delta.suffix}</span>
            </div>
            <div className="value-side-foot">{leftValue.length} 字符 · {fmtTime(leftMeta?.created_at).slice(11)}</div>
          </section>
          <div className="value-arrow" aria-hidden="true"><Icon name="arrow-right" /></div>
          <section className="value-side new" aria-label={`对比版本 v${right}`}>
            <div className="value-side-head"><span className="mono">v{right}</span><span className="mono">{rightMeta?.created_by?.slice(0, 8) || '—'}</span></div>
            <div className="value-side-body">
              <span className="common">{delta.prefix}</span>
              {delta.newMid && <span className="changed-add">{delta.newMid}</span>}
              <span className="common">{delta.suffix}</span>
            </div>
            <div className="value-side-foot">{rightValue.length} 字符 · {fmtTime(rightMeta?.created_at).slice(11)}</div>
          </section>
        </div>
      )}

      <div className="compare-actions">
        <button type="button" className="btn btn-quiet" onClick={onBack}><Icon name="arrow-left" /><span className="btn-label">返回版本历史</span></button>
        <span className="cell-sub">对比读的是历史值；不会写入新版本。</span>
      </div>
    </div>
  );
}

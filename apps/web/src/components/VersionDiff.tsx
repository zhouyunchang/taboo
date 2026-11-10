import { useState } from 'react';
import { api, ApiError, type SecretVersionValue, type Version } from '../api';

export function lineDiff(left: string, right: string): { kind: 'same' | 'add' | 'del'; text: string }[] {
  const a = left.split('\n');
  const b = right.split('\n');
  if (a.length * b.length > 20000) {
    const out: { kind: 'same' | 'add' | 'del'; text: string }[] = [];
    const n = Math.max(a.length, b.length);
    for (let i = 0; i < n; i++) {
      if (a[i] === b[i]) out.push({ kind: 'same', text: a[i] ?? '' });
      else {
        if (a[i] !== undefined) out.push({ kind: 'del', text: a[i] });
        if (b[i] !== undefined) out.push({ kind: 'add', text: b[i] });
      }
    }
    return out;
  }
  const dp = Array.from({ length: a.length + 1 }, () => new Array<number>(b.length + 1).fill(0));
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
  }
  const out: { kind: 'same' | 'add' | 'del'; text: string }[] = [];
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) { out.push({ kind: 'same', text: a[i] }); i++; j++; }
    else if (dp[i + 1][j] >= dp[i][j + 1]) out.push({ kind: 'del', text: a[i++] });
    else out.push({ kind: 'add', text: b[j++] });
  }
  while (i < a.length) out.push({ kind: 'del', text: a[i++] });
  while (j < b.length) out.push({ kind: 'add', text: b[j++] });
  return out;
}

export default function VersionDiff({ projectId, folder, secretKey, versions }: {
  projectId: string; folder: string; secretKey: string; versions: Version[];
}) {
  const ordered = [...versions].sort((a, b) => a.version - b.version);
  const signature = ordered.map((v) => v.version).join(',');
  const [left, setLeft] = useState('');
  const [right, setRight] = useState('');
  const [rows, setRows] = useState<{ kind: 'same' | 'add' | 'del'; text: string }[] | null>(null);
  const [shown, setShown] = useState(false);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [seen, setSeen] = useState('');
  if (seen !== signature) {
    setSeen(signature);
    setLeft(ordered[0] ? String(ordered[0].version) : '');
    setRight(ordered.length ? String(ordered[ordered.length - 1].version) : '');
    setRows(null);
  }

  async function compare() {
    if (!left || !right || left === right) {
      setErr('请选择两个不同的版本。');
      return;
    }
    setBusy(true);
    setErr('');
    setShown(false);
    try {
      const base = `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(secretKey)}/versions`;
      const query = `?path=${encodeURIComponent(folder)}`;
      const [a, b] = await Promise.all([
        api.get<SecretVersionValue>(`${base}/${left}${query}`),
        api.get<SecretVersionValue>(`${base}/${right}${query}`),
      ]);
      setRows(lineDiff(a.value, b.value));
    } catch (e) {
      setRows(null);
      setErr(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  if (ordered.length < 2) return <p className="muted">至少需要两个版本才能对比。</p>;

  return (
    <div className="version-diff-block">
      <div className="version-diff">
        <label>基准版本
          <select aria-label="基准版本" value={left} onChange={(e) => { setLeft(e.target.value); setRows(null); }}>
            {ordered.map((v) => <option key={v.version} value={v.version}>v{v.version}</option>)}
          </select>
        </label>
        <label>对比版本
          <select aria-label="对比版本" value={right} onChange={(e) => { setRight(e.target.value); setRows(null); }}>
            {ordered.map((v) => <option key={v.version} value={v.version}>v{v.version}</option>)}
          </select>
        </label>
        <button type="button" disabled={busy} onClick={() => void compare()}>{busy ? '对比中…' : '对比'}</button>
      </div>
      {err && <div className="error" role="alert">{err}</div>}
      {rows && (
        <div>
          <p role="status">{shown ? '正在显示明文，关闭抽屉后不会保留。' : '值已掩码。需要查看差异内容时再显示明文。'}</p>
          <button type="button" className="ghost" onClick={() => setShown((v) => !v)}>{shown ? '隐藏明文' : '显示明文'}</button>
          <ul className="diff-list">
            {rows.map((row, index) => (
              <li key={index} className={row.kind === 'add' ? 'diff-add' : row.kind === 'del' ? 'diff-del' : ''}>
                {row.kind === 'same' ? '相同' : row.kind === 'add' ? '新增' : '删除'}
                {shown ? ` ${row.text || '（空行）'}` : ` · ${row.text.length} 个字符`}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

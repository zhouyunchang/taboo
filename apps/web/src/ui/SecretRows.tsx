// 密钥面：结界表格的一行、文件夹树的节点、一次性动作状态。
// 视觉定义全部在 src/styles.css（secret-table / secret-actions / row-act / tree-*）；
// 这里只负责语义与组合，不新增全局类名。
import { useEffect, useRef, useState } from 'react';
import { api, ApiError, hasBrowserSession } from '../api';
import type { Folder, SecretMeta, SecretValue } from '../api';
import useSecretMutation from '../components/useSecretMutation';
import { fmtTime, timeTitle } from '../time';
import Icon from '../design/Icon';
import { Pill } from '../design/primitives';
import { FlashDialog } from '../design/feedback';
import type { IconName } from '../design/Icon';

/** 封印的长度就是值的长短：已显示过的值按字符数决定封条宽度（4 段），未显示过时用中档。
    长度是密文自带的形状信息——私钥不该长得像密码。 */
export function mask(valueLen: number | null) {
  const len = valueLen;
  if (len === null) return { dots: '••••••••', cls: 'cipher cipher-mid' };
  if (len < 8) return { dots: '•••', cls: 'cipher cipher-xs' };
  if (len < 24) return { dots: '••••••••', cls: 'cipher cipher-sm' };
  if (len < 96) return { dots: '••••••••••••', cls: 'cipher cipher-md' };
  if (len < 512) return { dots: '••••••••••••••••••', cls: 'cipher cipher-lg' };
  return { dots: '••••••••••••••••••••••••', cls: 'cipher cipher-xl' };
}

/** 由 Key 的形状决定行首符号：URL/连接串、凭据、私钥各用一种，便于扫读。 */
function keyGlyph(key: string): IconName {
  if (/KEY|SECRET|TOKEN|PASSWORD|PASS$|_PWD$/i.test(key)) return 'lock';
  if (/URL|DSN|URI|HOST|ENDPOINT/i.test(key)) return 'link';
  if (/CERT|KEY_PEM|PRIVATE/i.test(key)) return 'file';
  return 'key';
}

/** 一次性动作回执（复制成功），~1.6s 后自愈。 */
export function useFlash(ms = 1600) {
  const [on, setOn] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);
  return {
    on,
    flash() {
      setOn(true);
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => setOn(false), ms);
    },
  };
}

function copyText(text: string) {
  if (navigator.clipboard?.writeText) return navigator.clipboard.writeText(text);
  return Promise.reject(new Error('clipboard unavailable'));
}

export function SecretRow({ s, projectId, canWrite, onChanged, onEdit, onDetail, active }: {
  s: SecretMeta; projectId: string; canWrite: boolean;
  onChanged: () => void; onEdit: () => void; onDetail: () => void; active: boolean;
}) {
  const mutate = useSecretMutation();
  const [value, setValue] = useState<string | null>(null);
  // 本次会话里已经显示过的值长度；未显示过时用 8 枚占位（不臆造长度）。
  const shownLen = useRef<number | null>(null);
  const revealGeneration = useRef(0);
  // 明文只活在内存里：失焦、验证不可用、权限变化都立即收回。
  useEffect(() => { setValue(null); }, [s.canReveal]);
  useEffect(() => {
    const clear = () => { revealGeneration.current++; setValue(null); };
    window.addEventListener('blur', clear);
    window.addEventListener('taboo:verification-unavailable', clear);
    return () => { window.removeEventListener('blur', clear); window.removeEventListener('taboo:verification-unavailable', clear); };
  }, []);

  const [err, setErr] = useState('');
  const [busy, setBusy] = useState<'reveal' | 'copy' | 'ref' | 'del' | null>(null);
  const copied = useFlash();

  async function reveal() {
    if (value !== null) { setValue(null); return; }
    const started = ++revealGeneration.current;
    setBusy('reveal'); setErr('');
    try {
      const d = await api.get<SecretValue>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(s.key)}?path=${encodeURIComponent(s.folder)}`);
      if (started === revealGeneration.current && document.hasFocus()) {
        shownLen.current = String(d.value ?? '').length;
        setValue(d.value);
      }
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
    finally { setBusy(null); }
  }

  async function resolveRef() {
    const started = ++revealGeneration.current;
    setBusy('ref'); setErr('');
    try {
      const d = await api.get<SecretValue>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(s.key)}?path=${encodeURIComponent(s.folder)}&resolve=true`);
      if (started === revealGeneration.current && document.hasFocus()) {
        shownLen.current = String(d.value ?? '').length;
        setErr('');
        setValue(d.value);
      }
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
    finally { setBusy(null); }
  }

  async function copy() {
    const started = revealGeneration.current;
    setBusy('copy'); setErr('');
    try {
      const d: string = value !== null ? value : (await api.get<SecretValue>(`/api/v1/projects/${projectId}/secrets/${encodeURIComponent(s.key)}?path=${encodeURIComponent(s.folder)}`)).value;
      if (started !== revealGeneration.current || !document.hasFocus()) return;
      await copyText(d);
      copied.flash();
      setTimeout(() => { copyText('').catch(() => {}); }, 20_000); // 20s 自动清空剪贴板
    } catch { setErr('复制失败'); }
    finally { setBusy(null); }
  }

  async function remove() {
    if (!window.confirm(`删除密钥 ${s.key}？（版本历史一并删除，不可恢复）`)) return;
    setBusy('del'); setErr('');
    try {
      await mutate('del', `/api/v1/projects/${projectId}/secrets/${encodeURIComponent(s.key)}?path=${encodeURIComponent(s.folder)}&expectedVersion=${s.version}`);
      onChanged();
    } catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
    finally { setBusy(null); }
  }

  const revealed = value !== null;
  const canDelete = s.canDelete ?? (!hasBrowserSession() && canWrite);
  // 密文列与动作列共用同一组事实，窄屏卡片里不再重复「值」标签。
  const valueCell = (
    <span className="cipher-cell">
      {revealed
        ? <span className="cipher-value">{value}{value.includes('${') && <span className="ref-chip" title="含引用字面量 ${env:KEY}">ref</span>}</span>
        : (
          <span className={mask(shownLen.current).cls}
            title={shownLen.current !== null ? `已显示过的值 · ${shownLen.current} 字符` : '密文 · 点击显示'}>
            {mask(shownLen.current).dots}
          </span>
        )}
      {err && <span className="error">{err}</span>}
    </span>
  );

  const actions = (
    <span className="secret-actions">
      {copied.on && <FlashDialog text="已复制 · 20 秒后清空剪贴板" />}
      {s.canReveal && (
        <button type="button" className={`row-act action-fade ${revealed ? 'on' : ''}`}
          aria-label={revealed ? '隐藏' : '显示'} aria-pressed={revealed}
          title={revealed ? '隐藏' : '显示'} disabled={busy === 'reveal'} onClick={() => void reveal()}>
          <Icon name={revealed ? 'eye-off' : 'eye'} />
        </button>
      )}
      {s.canReveal && revealed && value.includes('${') && (
        <button type="button" className="row-act action-fade" aria-label="解析引用" title="解析引用"
          disabled={busy === 'ref'} onClick={() => void resolveRef()}>
          <Icon name="link" />
        </button>
      )}
      {s.canReveal && (
        <button type="button" className={`row-act action-fade ${copied.on ? 'done' : ''}`}
          aria-label="复制" title={copied.on ? '已复制' : '复制'} disabled={busy === 'copy'} onClick={() => void copy()}>
          <Icon name={copied.on ? 'check' : 'copy'} />
        </button>
      )}
      {canWrite && (
        <button type="button" className="row-act action-fade" aria-label="编辑" title="编辑" onClick={onEdit}>
          <Icon name="pencil" />
        </button>
      )}
      {canDelete && (
        <>
          <span className="actions-divider" aria-hidden="true" />
          <button type="button" className="row-act row-act-danger action-fade" aria-label="删除" title="删除"
            disabled={busy === 'del'} onClick={() => void remove()}>
            <Icon name="trash" />
          </button>
        </>
      )}
    </span>
  );

  return (
    <tr className={active ? 'open' : ''} onDoubleClick={(e) => { if (!(e.target as HTMLElement).closest('button')) onDetail(); }}>
        <td className="col-key">
          <button type="button" className="secret-key-btn" onClick={onDetail} title="查看密钥记录">
            <Icon name={keyGlyph(s.key)} />
            <span>{s.key}</span>
          </button>
        </td>
        <td className="col-value">{valueCell}</td>
        <td className="col-version"><span className="mono">v{s.version}</span></td>
        <td className="col-tags">
          {s.tags.length > 0 && (
            <span className="tag-row">{s.tags.map((t) => <Pill key={t} tone="muted">{t}</Pill>)}</span>
          )}
        </td>
        <td className="col-updated" title={timeTitle(s.updated_at)}><span className="nowrap">{fmtTime(s.updated_at)}</span></td>
        <td className="col-actions">{actions}</td>
    </tr>
  );
}

// 平铺 folders → 按 parent_id 嵌套的树（后端已保证根 '/' 存在）
export function FolderTree({ folders, cur, onSelect }: { folders: Folder[]; cur: string; onSelect: (p: string) => void }) {
  const byParent = new Map<string, Folder[]>();
  for (const f of folders) {
    const list = byParent.get(f.parent_id) ?? [];
    list.push(f);
    byParent.set(f.parent_id, list);
  }
  const root = folders.find((f) => f.path === '/');
  if (!root) return <p className="section-hint">还没有文件夹。</p>;
  return (
    <ul className="tree">
      <TreeNode f={root} byParent={byParent} cur={cur} onSelect={onSelect} depth={0} />
    </ul>
  );
}

function TreeNode({ f, byParent, cur, onSelect, depth }: {
  f: Folder; byParent: Map<string, Folder[]>; cur: string; onSelect: (p: string) => void; depth: number;
}) {
  const [open, setOpen] = useState(true);
  const kids = byParent.get(f.id) ?? [];
  const active = cur === f.path;
  return (
    <li>
      <div className={`tree-item ${active ? 'active' : ''}`}>
        {kids.length > 0 ? (
          <button type="button" className="tree-toggle" aria-expanded={open} aria-label={`${open ? '收起' : '展开'} ${f.name || '根目录'}`}
            onClick={() => setOpen(!open)}>
            <Icon name={open ? 'chevron-down' : 'chevron-right'} size={12} />
          </button>
        ) : <span className="tree-toggle" aria-hidden="true" />}
        <button type="button" className="tree-label" aria-current={active ? 'page' : undefined} onClick={() => onSelect(f.path)}>
          <Icon name={active || (open && kids.length > 0) ? 'folder-open' : 'folder'} size={14} />
          <span className="tree-name">{depth === 0 ? (f.name || '/') : f.name}</span>
        </button>
      </div>
      {open && kids.length > 0 && (
        <ul className="tree">
          {kids.map((k) => <TreeNode key={k.id} f={k} byParent={byParent} cur={cur} onSelect={onSelect} depth={depth + 1} />)}
        </ul>
      )}
    </li>
  );
}

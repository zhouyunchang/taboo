import { useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { api, ApiError, type ImportResult } from '../api';
import Modal from './Modal';
import ValidatedForm from './ValidatedForm';
import useSecretMutation from './useSecretMutation';
import Icon from '../design/Icon';
import type { IconName } from '../design/Icon';

export default function ImportEnv({ projectId, path, onClose, onImported }: {
  projectId: string; path: string; onClose: () => void; onImported: () => void;
}) {
  const mutate = useSecretMutation();
  const fileRef = useRef<HTMLInputElement>(null);
  const [content, setContent] = useState('');
  const [format, setFormat] = useState<'dotenv' | 'doppler' | '1password'>('dotenv');
  const [strategy, setStrategy] = useState<'' | 'skip' | 'overwrite'>('');
  const [plan, setPlan] = useState<ImportResult | null>(null);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const endpoint = `/api/v1/projects/${projectId}/import?path=${encodeURIComponent(path)}`;

  async function readFile(file: File) {
    setErr('');
    setPlan(null);
    setContent(await file.text());
  }

  async function preview() {
    setBusy(true);
    setErr('');
    try {
      const result = await api.post<ImportResult>(endpoint, { content, format, preview: true, ...(strategy ? { strategy } : {}) });
      setPlan(result);
    } catch (e) {
      setPlan(null);
      setErr(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function commit(event: FormEvent) {
    event.preventDefault();
    if (plan && (plan.conflicts?.length ?? 0) > 0 && !strategy) {
      setErr('有冲突的 key，请选择跳过已有或覆盖已有。');
      return;
    }
    setBusy(true);
    setErr('');
    try {
      const result = await mutate('post', endpoint, { content, format, ...(strategy ? { strategy } : {}) }) as ImportResult;
      setPlan(result);
      onImported();
      onClose();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  const conflicts = plan?.conflicts?.length ?? 0;
  const lines = content.split('\n').filter((line) => line.trim() && !line.trim().startsWith('#')).length;

  return (
    <Modal title="导入 .env" onClose={onClose} busy={busy}>
      <ValidatedForm className="form-stack" onSubmit={commit}>
        <div className="panel-head">
          <span className="mono" title={path}><Icon name="folder" size={13} />{path}</span>
          <span className="cell-sub">
            {format === 'dotenv' ? '.env' : format === 'doppler' ? 'Doppler JSON' : '1Password JSON'}
            {lines > 0 && <> · {lines} 行</>}
            {plan && <> · 新建 {plan.created} 覆盖 {plan.updated} 跳过 {plan.skipped}{conflicts > 0 ? ` 冲突 ${conflicts}` : ''}</>}
          </span>
        </div>
        <p className="muted">先预览再写入。支持 .env、Doppler JSON 和 1Password <code>op</code> JSON。已有 key 必须选择跳过或覆盖，整批在一个事务里完成。</p>
        <div className="form-row">
          <label className="field">格式
            <select aria-label="导入格式" value={format} onChange={(e) => { setFormat(e.target.value as 'dotenv' | 'doppler' | '1password'); setPlan(null); }}>
              <option value="dotenv">.env</option>
              <option value="doppler">Doppler JSON</option>
              <option value="1password">1Password JSON</option>
            </select>
          </label>
          <label className="field">冲突策略
            <select aria-label="冲突策略" value={strategy} onChange={(e) => { setStrategy(e.target.value as '' | 'skip' | 'overwrite'); setPlan(null); }}>
              <option value="">未选择</option>
              <option value="skip">跳过已有</option>
              <option value="overwrite">覆盖已有</option>
            </select>
          </label>
        </div>
        <label className="field">文件
          <input ref={fileRef} type="file" accept=".env,text/plain,.txt" onChange={(e) => { const file = e.target.files?.[0]; if (file) void readFile(file); }} />
        </label>
        <label className="field">.env 内容
          <textarea className="mono resize-none" value={content} onChange={(e) => { setContent(e.target.value); setPlan(null); }} required rows={8} spellCheck={false}
            placeholder={'DATABASE_URL=postgres://…\nAPI_TOKEN=…'} />
        </label>
        <div className="form-actions">
          <button type="button" className="ghost" disabled={busy || !content.trim()} onClick={() => void preview()}>
            <Icon name="eye" size={13} />预览
          </button>
        </div>
        {plan && (
          <div className="import-preview" tabIndex={0} role="region" aria-label="导入预览">
            <table className="table secrets-table">
              <thead><tr><th>Key</th><th>动作</th><th>版本</th></tr></thead>
              <tbody>
                {(plan.entries ?? []).map((entry) => (
                  <tr key={entry.key} className={entry.action === 'conflict' ? 'conflict' : undefined}>
                    <td className="mono" data-label="Key">{entry.key}</td>
                    <td data-label="动作" className={actionClass(entry.action)}>
                      <span aria-hidden="true">{actionGlyph(entry.action)} </span>{actionLabel(entry.action)}
                    </td>
                    <td className="mono" data-label="版本">v{entry.version}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {err && <p className="error" role="alert">{err}</p>}
        <div className="form-actions">
          <button type="button" className="ghost" disabled={busy} onClick={onClose}>取消</button>
          <button type="submit" disabled={busy || !plan || (conflicts > 0 && !strategy)}>{busy ? '导入中…' : '确认导入'}</button>
        </div>
      </ValidatedForm>
    </Modal>
  );
}

function actionLabel(action: string) {
  switch (action) {
    case 'create': return '新建';
    case 'overwrite': return '覆盖';
    case 'skip': return '跳过';
    case 'conflict': return '冲突';
    default: return action;
  }
}

// Vault 式变更字形：+ 新建 / ~ 覆盖 / = 跳过 / ! 冲突。文字标签始终并存，不只靠颜色与符号。
function actionGlyph(action: string): string {
  switch (action) {
    case 'create': return '+';
    case 'overwrite': return '~';
    case 'skip': return '=';
    case 'conflict': return '!';
    default: return '·';
  }
}

function actionClass(action: string): string | undefined {
  switch (action) {
    case 'create': return 'import-add';
    case 'overwrite': return 'import-over';
    case 'skip': return 'import-skip';
    case 'conflict': return 'import-bad';
    default: return undefined;
  }
}

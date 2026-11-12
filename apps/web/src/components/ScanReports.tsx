import { useCallback, useEffect, useState } from 'react';
import { api, ApiError, type ScanReport } from '../api';
import { fmtTime, timeTitle } from '../time';
import Modal from './Modal';
import Icon from '../design/Icon';
import { EmptyState, Pill, Section } from '../design/primitives';

export default function ScanReports({ orgSlug }: { orgSlug: string }) {
  const [reports, setReports] = useState<ScanReport[]>([]);
  const [canDelete, setCanDelete] = useState(false);
  const [err, setErr] = useState('');
  const [open, setOpen] = useState<ScanReport | null>(null);

  const load = useCallback(() => {
    api.get<{ reports: ScanReport[]; can_delete?: boolean }>(`/api/v1/orgs/${orgSlug}/scan-reports`)
      .then((d) => { setReports(d.reports ?? []); setCanDelete(d.can_delete === true); setErr(''); })
      .catch((e) => setErr(e instanceof ApiError ? e.message : String(e)));
  }, [orgSlug]);
  useEffect(load, [load]);

  async function remove(id: string) {
    if (!window.confirm('删除这份扫描报告？')) return;
    try {
      await api.del(`/api/v1/orgs/${orgSlug}/scan-reports/${id}`);
      if (open?.id === id) setOpen(null);
      load();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : String(e));
    }
  }

  async function openReport(id: string) {
    try {
      setOpen(await api.get<ScanReport>(`/api/v1/orgs/${orgSlug}/scan-reports/${id}`));
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : String(e));
    }
  }

  return (
    <div className="panel">
      <Section
        title="扫描报告"
        hint={<>报告只包含规则、相对路径、行号和指纹。命中原文只在上传时用于计算组织 HMAC，不会保存，也不会显示在这里。升级前的报告会标成 sha256。命令：<code>taboo scan --upload</code></>}
        actions={<button type="button" className="ghost" onClick={load}><Icon name="refresh" size={13} />刷新</button>}
      >
        {err && <p className="error" role="alert">{err}</p>}
        {reports.length === 0 ? (
          <EmptyState icon="scan" title="还没有扫描报告">
            在仓库里运行 <code>taboo scan --upload</code>，把泄漏扫描结果送到这里。
          </EmptyState>
        ) : (
          <div className="table-scroll" role="region" aria-label="扫描报告列表" tabIndex={0}>
            <table className="table secrets-table">
              <thead><tr><th>时间</th><th>来源</th><th>操作者</th><th>严重</th><th>警告</th><th /></tr></thead>
              <tbody>
                {reports.map((report) => (
                  <tr key={report.id}>
                    <td className="muted nowrap" data-label="时间" title={report.created_at ? timeTitle(report.created_at) : undefined}>{report.created_at ? fmtTime(report.created_at) : ''}</td>
                    <td className="mono" data-label="来源">{report.source}</td>
                    <td data-label="操作者">{report.actor_name || '—'}</td>
                    <td data-label="严重">{report.critical_count > 0 ? <Pill tone="bad" icon="alert">{report.critical_count}</Pill> : <span className="faint mono">0</span>}</td>
                    <td data-label="警告">{report.warning_count > 0 ? <Pill tone="warn" icon="alert">{report.warning_count}</Pill> : <span className="faint mono">0</span>}</td>
                    <td className="actions" data-label="操作">
                      <span className="row-actions">
                        <button type="button" className="ghost btn-sm" onClick={() => void openReport(report.id)}><Icon name="eye" size={12} />查看</button>
                        {canDelete && <button type="button" className="ghost btn-sm" onClick={() => void remove(report.id)}><Icon name="trash" size={12} />删除</button>}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Section>
      {open && (
        <Modal title="扫描结果" onClose={() => setOpen(null)}>
          <div className="form-stack">
            <div className="panel-head">
              <span className="mono"><Icon name="scan" size={13} />{open.source}</span>
              <span className="btn-row">
                <Pill tone={open.critical_count > 0 ? 'bad' : 'muted'} icon="alert">严重 {open.critical_count}</Pill>
                <Pill tone={open.warning_count > 0 ? 'warn' : 'muted'} icon="alert">警告 {open.warning_count}</Pill>
              </span>
            </div>
            <p className="muted">此页没有命中原文。每一行只有规则、文件、行号与指纹。</p>
            {(open.findings ?? []).length === 0 ? (
              <EmptyState icon="shield" title="这次扫描没有命中">仓库里没有触发规则的凭据痕迹。</EmptyState>
            ) : (
              <div className="table-scroll" role="region" aria-label="扫描命中列表" tabIndex={0}>
                <table className="table secrets-table">
                  <thead><tr><th>级别</th><th>规则</th><th>文件</th><th>行</th><th>指纹</th></tr></thead>
                  <tbody>
                    {(open.findings ?? []).map((finding, index) => (
                      <tr key={`${finding.fingerprint}:${index}`}>
                        <td data-label="级别">{finding.level === 'critical' ? <Pill tone="bad">严重</Pill> : <Pill tone="warn">警告</Pill>}</td>
                        <td className="mono" data-label="规则">{finding.rule_id}</td>
                        <td className="mono" data-label="文件">{finding.file}</td>
                        <td className="mono" data-label="行">{finding.line}</td>
                        <td className="mono faint" data-label="指纹" title={finding.fingerprint}>{(finding.fingerprint ?? '').slice(0, 12)}{finding.alg === 'sha256' ? ' · 旧' : ''}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <div className="form-actions"><button type="button" className="ghost" onClick={() => setOpen(null)}>关闭</button></div>
          </div>
        </Modal>
      )}
    </div>
  );
}

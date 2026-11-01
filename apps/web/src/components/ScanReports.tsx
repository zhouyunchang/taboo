import { useCallback, useEffect, useState } from 'react';
import { api, ApiError, type ScanReport } from '../api';
import { fmtTime, timeTitle } from '../time';
import Modal from './Modal';

export default function ScanReports({ orgSlug }: { orgSlug: string }) {
  const [reports, setReports] = useState<ScanReport[]>([]);
  const [err, setErr] = useState('');
  const [open, setOpen] = useState<ScanReport | null>(null);

  const load = useCallback(() => {
    api.get<{ reports: ScanReport[] }>(`/api/v1/orgs/${orgSlug}/scan-reports`)
      .then((d) => { setReports(d.reports ?? []); setErr(''); })
      .catch((e) => setErr(e instanceof ApiError ? e.message : String(e)));
  }, [orgSlug]);
  useEffect(load, [load]);

  async function openReport(id: string) {
    try {
      setOpen(await api.get<ScanReport>(`/api/v1/orgs/${orgSlug}/scan-reports/${id}`));
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : String(e));
    }
  }

  return (
    <div className="panel">
      <div className="panel-head">
        <h2>扫描报告</h2>
        <button className="ghost" onClick={load}>刷新</button>
      </div>
      <p className="muted">报告只包含规则、相对路径、行号和指纹。命中原文不会上传，也不会显示在这里。命令：<code>taboo scan --upload</code></p>
      {err && <div className="error" role="alert">{err}</div>}
      <div className="table-scroll">
        <table className="table secrets-table">
          <thead><tr><th>时间</th><th>来源</th><th>操作者</th><th>严重</th><th>警告</th><th /></tr></thead>
          <tbody>
            {reports.map((report) => (
              <tr key={report.id}>
                <td className="muted nowrap" data-label="时间" title={report.created_at ? timeTitle(report.created_at) : undefined}>{report.created_at ? fmtTime(report.created_at) : ''}</td>
                <td className="mono" data-label="来源">{report.source}</td>
                <td data-label="操作者">{report.actor_name || '—'}</td>
                <td data-label="严重">{report.critical_count}</td>
                <td data-label="警告">{report.warning_count}</td>
                <td className="actions"><button className="ghost" onClick={() => void openReport(report.id)}>查看</button></td>
              </tr>
            ))}
            {reports.length === 0 && <tr><td colSpan={6} className="muted center">还没有扫描报告</td></tr>}
          </tbody>
        </table>
      </div>
      {open && (
        <Modal title="扫描结果" onClose={() => setOpen(null)}>
          <div className="drawer-inner">
            <p className="muted">{open.source} · 严重 {open.critical_count} · 警告 {open.warning_count}。此页没有命中原文。</p>
            <div className="table-scroll">
              <table className="table secrets-table">
                <thead><tr><th>级别</th><th>规则</th><th>文件</th><th>行</th><th>指纹</th></tr></thead>
                <tbody>
                  {(open.findings ?? []).map((finding, index) => (
                    <tr key={`${finding.fingerprint}:${index}`}>
                      <td data-label="级别">{finding.level === 'critical' ? '严重' : '警告'}</td>
                      <td className="mono" data-label="规则">{finding.rule_id}</td>
                      <td className="mono" data-label="文件">{finding.file}</td>
                      <td data-label="行">{finding.line}</td>
                      <td className="mono" data-label="指纹">{(finding.fingerprint ?? '').slice(0, 12)}</td>
                    </tr>
                  ))}
                  {(open.findings ?? []).length === 0 && <tr><td colSpan={5} className="muted center">这次扫描没有命中</td></tr>}
                </tbody>
              </table>
            </div>
            <div className="drawer-actions"><button className="ghost" onClick={() => setOpen(null)}>关闭</button></div>
          </div>
        </Modal>
      )}
    </div>
  );
}

// 服务端时间戳统一为 UTC：'YYYY-MM-DD HH:MM:SS'（无时区后缀）、ISO 字符串或 Unix 秒/毫秒。
// 界面一律按查看者本地时区显示，title 中保留原始 UTC 值便于对照审计/日志。
const pad = (n: number) => String(n).padStart(2, '0');

export function parseServerTime(v: unknown): Date | null {
  if (v === null || v === undefined || v === '') return null;
  if (typeof v === 'number') return new Date(v < 1e12 ? v * 1000 : v);
  const s = String(v).trim();
  if (/^\d+$/.test(s)) return parseServerTime(Number(s));
  // 无时区的 'YYYY-MM-DD HH:MM:SS[.fff]' 视为 UTC
  const m = /^(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?)$/.exec(s);
  const d = new Date(m ? `${m[1]}T${m[2]}Z` : s);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** 本地时区 'YYYY-MM-DD HH:MM:SS'；无法解析时原样返回。 */
export function fmtTime(v: unknown): string {
  const d = parseServerTime(v);
  if (!d) return v === null || v === undefined ? '' : String(v);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

/** title 提示：UTC 原值 + 本地时区名。 */
export function timeTitle(v: unknown): string {
  const d = parseServerTime(v);
  if (!d) return '';
  const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || 'local';
  return `${d.toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' UTC')}（显示为 ${tz}）`;
}

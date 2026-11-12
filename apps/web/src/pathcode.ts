// 定位符第一段（用户名 / 组织 code）的共用规则。服务端 internal/pathcode 是唯一真源，
// 这里只做提交前的提示与预览，不替代服务端的原子占位。
export const CODE_RE = /^[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?$/;

export const RESERVED_CODES = new Set(['api', 'invite', 'login', 'settings', 'new']);

export type CodeIssue = '' | 'empty' | 'long' | 'chars' | 'reserved';

/** 传入 normalized（trim + lowercase）后的 code。 */
export function codeIssue(code: string): CodeIssue {
  if (!code) return 'empty';
  if (code.length > 39) return 'long';
  if (!CODE_RE.test(code)) return 'chars';
  if (RESERVED_CODES.has(code)) return 'reserved';
  return '';
}

export function normalizeCode(value: string): string {
  return value.trim().toLowerCase();
}

/** 按显示名称收出一个候选 code；收不出可用词时返回空串。 */
export function suggestCode(seed: string): string {
  const out = seed.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '').slice(0, 39).replace(/-+$/g, '');
  return codeIssue(out) ? '' : out;
}

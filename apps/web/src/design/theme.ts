// 主题运行时：选择模式存 localStorage，解析值写到 <html data-theme>，模式写到 data-theme-mode。
// 模块顶层不触碰 window，保证被导入时安全；所有浏览器 API 都在函数内访问。
import { useCallback, useEffect, useState } from 'react';

export type ThemeMode = 'system' | 'light' | 'dark';
export type ResolvedTheme = 'light' | 'dark';

/** 与 index.html 的内联 bootstrap 脚本共用同一个键：改这里时同步改那边。 */
const STORAGE_KEY = 'taboo:theme';
const SYSTEM_QUERY = '(prefers-color-scheme: dark)';

// 读不到 --meta-theme-color 时的兜底：与 index.html 的 meta 初值、两套主题的底色一致。
const FALLBACK_META: Record<ResolvedTheme, string> = { dark: '#0a0907', light: '#f7f4ec' };

function isThemeMode(value: unknown): value is ThemeMode {
  return value === 'system' || value === 'light' || value === 'dark';
}

/** 读取用户选择；缺省、非法值、localStorage 不可用（隐私模式）都回落到 system。 */
export function readThemeMode(): ThemeMode {
  if (typeof window === 'undefined') return 'system';
  try {
    const saved = window.localStorage.getItem(STORAGE_KEY);
    return isThemeMode(saved) ? saved : 'system';
  } catch {
    return 'system';
  }
}

/** system 跟随操作系统配色；显式 light/dark 不再看系统。无 window 时按暗色（同 index.html 的无 JS 回退）。 */
export function resolveTheme(mode: ThemeMode): ResolvedTheme {
  if (mode !== 'system') return mode;
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return 'dark';
  return window.matchMedia(SYSTEM_QUERY).matches ? 'dark' : 'light';
}

/** 主题色同步到 <meta name="theme-color">：只改 content 属性；meta 不参与布局，不会触发重排。 */
function syncMetaThemeColor(resolved: ResolvedTheme): void {
  const meta = document.querySelector('meta[name="theme-color"]');
  if (!meta) return;
  const themed = getComputedStyle(document.documentElement).getPropertyValue('--meta-theme-color').trim();
  const color = themed || FALLBACK_META[resolved];
  if (meta.getAttribute('content') !== color) meta.setAttribute('content', color);
}

/** 把解析值与选择模式写到文档上，返回解析值。同一个 mode 重复调用是幂等的。 */
export function applyTheme(mode: ThemeMode): ResolvedTheme {
  const resolved = resolveTheme(mode);
  if (typeof document === 'undefined') return resolved;
  const root = document.documentElement;
  root.setAttribute('data-theme', resolved);
  root.setAttribute('data-theme-mode', mode);
  syncMetaThemeColor(resolved);
  return resolved;
}

/** 持久化用户选择；写失败（禁用存储）时不影响本次切换。 */
function writeThemeMode(mode: ThemeMode): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, mode);
  } catch {
    // 隐私模式 / 存储被禁用：下次启动回落到 system，但界面本次仍然生效。
  }
}

/**
 * 主题状态。初值同步应用，避免首帧与内联脚本的解析结果不一致。
 * 订阅两件事：system 模式下系统配色变化；其他标签页改写 localStorage。
 */
export function useTheme(): { mode: ThemeMode; resolved: ResolvedTheme; setMode: (mode: ThemeMode) => void } {
  const [state, setState] = useState<{ mode: ThemeMode; resolved: ResolvedTheme }>(() => {
    const mode = readThemeMode();
    return { mode, resolved: applyTheme(mode) };
  });

  useEffect(() => {
    // 跨标签页：另一个标签改了选择，当前页立即跟上。
    const onStorage = (e: StorageEvent) => {
      if (e.key !== STORAGE_KEY) return;
      const mode = isThemeMode(e.newValue) ? e.newValue : 'system';
      const resolved = applyTheme(mode);
      setState({ mode, resolved });
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, []);

  useEffect(() => {
    // 只有跟随系统时才需要听系统配色变化；显式 light/dark 不受系统影响。
    if (state.mode !== 'system') return;
    const media = window.matchMedia(SYSTEM_QUERY);
    const onSystemChange = () => {
      const resolved = applyTheme('system');
      setState((prev) => (resolved === prev.resolved ? prev : { mode: prev.mode, resolved }));
    };
    // StrictMode 双挂载：监听要在 cleanup 里摘掉。
    media.addEventListener('change', onSystemChange);
    return () => media.removeEventListener('change', onSystemChange);
  }, [state.mode]);

  const setMode = useCallback((mode: ThemeMode) => {
    writeThemeMode(mode);
    const resolved = applyTheme(mode);
    setState({ mode, resolved });
  }, []);

  return { mode: state.mode, resolved: state.resolved, setMode };
}

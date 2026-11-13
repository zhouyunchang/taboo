// 三态主题开关：跟随系统 → 亮色 → 暗色。顺序固定，图标即标签。
// 布局与外观（.theme-switch / .theme-opt / .theme-corner）全部由 src/styles.css 提供。
import Icon from './Icon';
import { useTheme } from './theme';

/** 主题选择器。className 用于挂载点的变体（如 CliConsent 的固定角落 .theme-corner）。 */
export default function ThemeSwitch({ className }: { className?: string }) {
  const { mode, setMode } = useTheme();
  return (
    <div className={`theme-switch${className ? ` ${className}` : ''}`} role="group" aria-label="界面主题">
      <button type="button" className="theme-opt" aria-pressed={mode === 'system'} aria-label="跟随系统" title="跟随系统" onClick={() => setMode('system')}><Icon name="monitor" size={14} /></button>
      <button type="button" className="theme-opt" aria-pressed={mode === 'light'} aria-label="亮色" title="亮色" onClick={() => setMode('light')}><Icon name="sun" size={14} /></button>
      <button type="button" className="theme-opt" aria-pressed={mode === 'dark'} aria-label="暗色" title="暗色" onClick={() => setMode('dark')}><Icon name="moon" size={14} /></button>
    </div>
  );
}

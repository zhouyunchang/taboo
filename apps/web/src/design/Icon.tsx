// 禁制图标语言：16px 栅格、1.5px 描边、方角圆角 1px。自绘 SVG，不引入图标库。
// 所有图标共享同一 stroke 与端点，保证在同一行里视觉重量一致。
import type { SVGProps } from 'react';

export type IconName =
  | 'key' | 'folder' | 'folder-open' | 'search' | 'plus' | 'chevron-right' | 'chevron-down'
  | 'eye' | 'eye-off' | 'copy' | 'check' | 'pencil' | 'trash' | 'clock' | 'history'
  | 'shield' | 'seal' | 'audit' | 'robot' | 'db' | 'settings' | 'users' | 'logout'
  | 'alert' | 'lock' | 'unlock' | 'arrow-right' | 'arrow-left' | 'x' | 'menu' | 'more'
  | 'upload' | 'download' | 'refresh' | 'scan' | 'link' | 'terminal' | 'globe' | 'bell'
  | 'chevron-left' | 'sliders' | 'file' | 'play';

const paths: Record<IconName, string> = {
  key: 'M6.5 2.5a4 4 0 1 0 2.9 6.7l1.1 1.1v1.9h1.9v1.9h1.9v-1.9l-3.4-3.4A4 4 0 0 0 6.5 2.5Z M6.5 5.2h.01',
  folder: 'M2 4.5h4.2l1.3 1.5H14v6.5a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1v-8Z',
  'folder-open': 'M2 13V4.5h4.2l1.3 1.5H13v1.5 M2 13l2-5.5h11.5L13.4 13H2Z',
  search: 'M7 2.5a4.5 4.5 0 1 0 0 9 4.5 4.5 0 0 0 0-9Z M10.4 10.4 14 14',
  plus: 'M8 3v10 M3 8h10',
  'chevron-right': 'M6 3.5 10.5 8 6 12.5',
  'chevron-down': 'M3.5 6 8 10.5 12.5 6',
  'chevron-left': 'M10 3.5 5.5 8 10 12.5',
  eye: 'M1.5 8S4 3.5 8 3.5 14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8Z M8 6a2 2 0 1 0 0 4 2 2 0 0 0 0-4Z',
  'eye-off': 'M2 2l12 12 M6.2 6.4A2 2 0 0 0 8 10a2 2 0 0 0 1.6-.8 M4.2 4.6C2.7 5.7 1.5 8 1.5 8s2.5 4.5 6.5 4.5c1 0 1.9-.3 2.7-.7 M12.6 10.3c1.2-1 1.9-2.3 1.9-2.3S12 3.5 8 3.5c-.5 0-1 .1-1.4.2',
  copy: 'M5.5 5.5h7v8h-7v-8Z M3.5 10.5h-1v-8h7v1',
  check: 'M3 8.5 6.4 12 13 4',
  pencil: 'M3 13l.6-3.1 7.6-7.6 2.5 2.5-7.6 7.6L3 13Z M9.6 4.4l2.5 2.5',
  trash: 'M2.5 4.5h11 M6 4.5V3h4v1.5 M4 4.5l.7 9h6.6l.7-9 M6.6 7v4 M9.4 7v4',
  clock: 'M8 1.8a6.2 6.2 0 1 0 0 12.4A6.2 6.2 0 0 0 8 1.8Z M8 4.6V8l2.4 1.6',
  history: 'M2.2 8a5.8 5.8 0 1 0 1.8-4.2 M2 2.4v3.2h3.2 M8 5v3.2l2.2 1.4',
  shield: 'M8 1.8 3 3.6v4.2c0 3 2.1 5.3 5 6.4 2.9-1.1 5-3.4 5-6.4V3.6L8 1.8Z M6 7.8l1.6 1.6 2.6-2.8',
  seal: 'M8 1.6 9.6 4l2.8.3.5 2.8L14.4 8l-1.5 2 .1 2.8-2.8.5L8 14.4l-2.2-.9-2.8-.5.1-2.8L1.6 8l1.5-2 .5-2.8L6.4 4 8 1.6Z M6.2 8l1.3 1.4 2.6-2.8',
  audit: 'M3 2.2h7l3 3v8.6H3V2.2Z M10 2.2v3h3 M5.5 8.4h5 M5.5 11h3.2',
  robot: 'M3.2 5.6h9.6v7.2H3.2V5.6Z M8 2.4v3.2 M2 9.2H3.2 M12.8 9.2H14 M6.2 8.8v1.4 M9.8 8.8v1.4',
  db: 'M8 1.8c3 0 5.2.9 5.2 1.9S11 5.6 8 5.6 2.8 4.7 2.8 3.7 5 1.8 8 1.8Z M2.8 3.7v8.6c0 1 2.2 1.9 5.2 1.9s5.2-.9 5.2-1.9V3.7 M2.8 8c0 1 2.2 1.9 5.2 1.9s5.2-.9 5.2-1.9',
  settings: 'M8 6a2 2 0 1 0 0 4 2 2 0 0 0 0-4Z M8 1.6l.7 1.7 1.8-.3 1 1.5 1.7.8-.3 1.8 1 1.5-1 1.5.3 1.8-1.7.8-1 1.5-1.8-.3L8 14.4l1.5-1.6-.3-1.8 1-1.5-1-1.5.3-1.8-1.5-1.6Z',
  users: 'M6 7.4a2.6 2.6 0 1 0 0-5.2 2.6 2.6 0 0 0 0 5.2Z M1.6 14c.4-2.6 2.2-3.9 4.4-3.9S10 11.4 10.4 14 M10.8 2.6a2.4 2.4 0 0 1 0 4.6 M11.6 10.4c1.4.4 2.4 1.4 2.8 3.6',
  logout: 'M6.6 2.2H3v11.6h3.6 M10.4 5.2 13.2 8l-2.8 2.8 M13.2 8H6.4',
  alert: 'M8 2 15 14H1L8 2Z M8 6.4v3.4 M8 11.6h.01',
  lock: 'M3.6 7.2h8.8v6.4H3.6V7.2Z M5.4 6.2V4.8a2.6 2.6 0 0 1 5.2 0v1.4',
  unlock: 'M3.6 7.2h8.8v6.4H3.6V7.2Z M5.4 5V4.8a2.6 2.6 0 0 1 5-.9',
  'arrow-right': 'M2.6 8h10.8 M9.2 3.8 13.4 8l-4.2 4.2',
  'arrow-left': 'M13.4 8H2.6 M6.8 3.8 2.6 8l4.2 4.2',
  x: 'M3.6 3.6l8.8 8.8 M12.4 3.6l-8.8 8.8',
  menu: 'M2 4.5h12 M2 8h12 M2 11.5h12',
  more: 'M3.6 8h.01 M8 8h.01 M12.4 8h.01',
  upload: 'M8 11V2.4 M4.4 6 8 2.4 11.6 6 M2.4 13.6h11.2',
  download: 'M8 2.4V11 M4.4 7.6 8 11.2l3.6-3.6 M2.4 14h11.2',
  refresh: 'M13.2 8a5.2 5.2 0 1 1-1.6-3.8 M13.4 2.2v3.2h-3.2',
  scan: 'M2 6V2.6h3.4 M10.6 2.6H14V6 M14 10v3.4h-3.4 M5.4 13.4H2V10 M4.4 8h7.2',
  link: 'M6.4 9.6a2.8 2.8 0 0 0 4 0l2-2a2.8 2.8 0 0 0-4-4l-.8.8 M9.6 6.4a2.8 2.8 0 0 0-4 0l-2 2a2.8 2.8 0 0 0 4 4l.8-.8',
  terminal: 'M2.2 3h11.6v10H2.2V3Z M4.8 6.6 6.8 8.5l-2 1.9 M8.2 10.6h3',
  globe: 'M8 1.8a6.2 6.2 0 1 0 0 12.4A6.2 6.2 0 0 0 8 1.8Z M1.8 8h12.4 M8 1.8c1.8 1.8 2.6 3.9 2.6 6.2S9.8 12.4 8 14.2C6.2 12.4 5.4 10.3 5.4 8s.8-4.4 2.6-6.2Z',
  bell: 'M4 6.6a4 4 0 0 1 8 0c0 3.4 1.4 4.6 1.4 4.6H2.6S4 10 4 6.6Z M6.6 13.4a1.6 1.6 0 0 0 2.8 0',
  sliders: 'M2 4.6h3 M9 4.6h5 M2 11.4h5 M11 11.4h3 M5 2.6v4 M11 9.4v4',
  file: 'M3.6 2h5.2l3.6 3.6V14H3.6V2Z M8.6 2v3.8h3.8',
  play: 'M4.6 2.6 13 8l-8.4 5.4V2.6Z',
};

export default function Icon({ name, size = 16, className = '', strokeWidth = 1.5 }: {
  name: IconName; size?: number; className?: string; strokeWidth?: number;
}) {
  return (
    <svg viewBox="0 0 16 16" width={size} height={size} className={`icon ${className}`}
      fill="none" stroke="currentColor" strokeWidth={strokeWidth}
      strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
      <path d={paths[name]} />
    </svg>
  );
}

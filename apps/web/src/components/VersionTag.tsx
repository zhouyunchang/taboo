import { useEffect, useState } from 'react';
import type { operations } from '../api-types';
import Icon from '../design/Icon';

type Health = operations['healthz']['responses']['200']['content']['application/json'];

let cached: Promise<Health | null> | null = null;
function loadHealth(): Promise<Health | null> {
  cached ??= fetch('/api/v1/healthz', { cache: 'no-store' })
    .then((r) => (r.ok ? (r.json() as Promise<Health>) : null))
    .catch(() => null);
  return cached;
}

/** 当前服务端构建版本（/api/v1/healthz），用于确认升级/回滚后实际运行的版本。 */
export default function VersionTag({ className = '' }: { className?: string }) {
  const [h, setH] = useState<Health | null>(null);
  useEffect(() => { let live = true; loadHealth().then((v) => { if (live) setH(v); }); return () => { live = false; }; }, []);
  if (!h) return null;
  // 版本号是标识符，用等宽；前面的盾形图标表示这是「服务端自报的构建」，不是客户端常量。
  return (
    <p className={`version-tag ${className}`} data-testid="server-version" title={h.build_time ? `构建于 ${h.build_time}` : undefined}>
      <Icon name="shield" size={12} /> taboo v{h.version} · {h.commit}
    </p>
  );
}

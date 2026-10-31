import { useEffect, useState } from 'react';
import type { operations } from '../api-types';

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
  return (
    <p className={`version-tag muted ${className}`} data-testid="server-version" title={h.build_time ? `构建于 ${h.build_time}` : undefined}>
      taboo v{h.version} · {h.commit}
    </p>
  );
}

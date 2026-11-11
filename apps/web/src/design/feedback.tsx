// 一次性回执（复制成功 / 已保存）：原生 <dialog> 自带 top layer 与 role 语义，
// 不新增页内节点、不改变主壳 DOM 结构；视觉是 1px 结界线的小戳，不是 toast 卡片。
import { useEffect, useRef } from 'react';

export function FlashDialog({ text }: { text: string }) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const dialog = ref.current;
    if (!dialog || dialog.open) return;
    dialog.show();
    return () => { if (dialog.open) dialog.close(); };
  }, []);
  return (
    <dialog ref={ref} className="flash-dialog" aria-live="polite">
      <span role="status">{text}</span>
    </dialog>
  );
}

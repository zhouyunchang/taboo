import { useEffect, useId, useRef } from 'react';
import type { ReactNode } from 'react';
import Icon from '../design/Icon';

// Native dialog owns focus containment, inert background, Escape and restoration.
export default function Modal({ title, children, onClose, busy = false }: {
  title: string; children: ReactNode; onClose: () => void; busy?: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const dialog = ref.current;
    const trigger = document.activeElement as HTMLElement | null;
    dialog?.showModal();
    return () => { dialog?.close(); trigger?.focus(); };
  }, []);
  return <dialog ref={ref} className="modal" aria-labelledby={titleId}
    onCancel={(e) => { e.preventDefault(); if (!busy) onClose(); }}>
    <header className="modal-head">
      <h3 id={titleId}>{title}</h3>
      <button type="button" className="icon-btn" aria-label="关闭" disabled={busy} onClick={onClose}>
        <Icon name="x" />
      </button>
    </header>
    {children}
  </dialog>;
}

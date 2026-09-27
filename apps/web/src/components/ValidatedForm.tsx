import { useId, useState } from 'react';
import type { FormHTMLAttributes } from 'react';

// Keep native input constraints, but own validation feedback and first-error focus.
export default function ValidatedForm({ children, onSubmit, ...props }: Omit<FormHTMLAttributes<HTMLFormElement>, 'noValidate'>) {
  const [error, setError] = useState('');
  const errorId = useId();
  return <form {...props} noValidate onSubmit={(event) => {
    const form = event.currentTarget;
    form.querySelectorAll('[data-form-invalid]').forEach((el) => {
      el.removeAttribute('aria-invalid'); el.removeAttribute('aria-describedby'); el.removeAttribute('data-form-invalid');
    });
    const invalid = form.querySelector<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>('input:invalid, select:invalid, textarea:invalid');
    if (invalid) {
      event.preventDefault();
      const label = invalid.labels?.[0]?.textContent?.trim() || '字段';
      setError(`请检查「${label}」的必填内容和格式。`);
      invalid.setAttribute('aria-invalid', 'true'); invalid.setAttribute('aria-describedby', errorId); invalid.setAttribute('data-form-invalid', 'true');
      invalid.focus(); return;
    }
    setError(''); onSubmit?.(event);
  }}>
    {children}
    {error && <p id={errorId} className="error" role="alert">{error}</p>}
  </form>;
}

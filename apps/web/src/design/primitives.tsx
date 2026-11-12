// 共享 UI 原语：所有页面（含并行施工的治理面板）只使用这里的结构与类名。
// 视觉定义全部在 src/styles.css；这里只负责语义与组合。
import type { ButtonHTMLAttributes, HTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes, TextareaHTMLAttributes } from 'react';
import Icon from './Icon';
import type { IconName } from './Icon';

type Variant = 'primary' | 'solid' | 'default' | 'quiet' | 'danger' | 'stamp';

/** 操作按钮。label 由内容提供；icon 只是视觉前缀。 */
export function Button({ variant = 'default', icon, loading = false, className = '', children, disabled, ...rest }:
  ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; icon?: IconName; loading?: boolean }) {
  return (
    <button type="button" {...rest} disabled={disabled || loading}
      className={`btn btn-${variant} ${className}`} data-loading={loading || undefined}>
      {icon && <Icon name={icon} />}
      <span className="btn-label">{children}</span>
    </button>
  );
}

export function IconButton({ icon, label, className = '', ...rest }:
  ButtonHTMLAttributes<HTMLButtonElement> & { icon: IconName; label: string }) {
  return (
    <button type="button" {...rest} className={`icon-btn ${className}`} aria-label={label} title={label}>
      <Icon name={icon} />
    </button>
  );
}

/** 状态标记：ok/bad/warn/muted/accent，可选图标。 */
export function Pill({ tone = 'muted', icon, children, title }: {
  tone?: 'ok' | 'bad' | 'warn' | 'muted' | 'accent'; icon?: IconName; children: ReactNode; title?: string;
}) {
  return <span className={`pill pill-${tone}`} title={title}>{icon && <Icon name={icon} size={12} />}{children}</span>;
}

export function Field({ label, hint, htmlFor, children, className = '' }: {
  label: string; hint?: ReactNode; htmlFor?: string; children: ReactNode; className?: string;
}) {
  // 提示放在 <label> 之外：否则控件（和 aria-labelledby 的对话框）的可访问名会被提示句吞掉。
  return (
    <div className={`field ${className}`}>
      <label className="field-label-wrap" htmlFor={htmlFor}>
        <span className="field-label">{label}</span>
      </label>
      {children}
      {hint && <span className="field-hint">{hint}</span>}
    </div>
  );
}

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} />;
}

export function Textarea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea {...props} />;
}

export function Select(props: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select {...props} />;
}

/** 复选框 + 说明文本。原生 input 保留键盘与表单语义。 */
export function Check({ label, hint, ...rest }: InputHTMLAttributes<HTMLInputElement> & { label: ReactNode; hint?: ReactNode }) {
  // 原生 checkbox 的可访问名取自 <label> 的全部文本，附注会让「查看明文」变成「查看明文 reveal」。
  // 显式的 aria-label 让名字稳定为动作本身，附注仍作为描述朗读。
  const labelText = typeof label === 'string' ? label : undefined;
  return (
    <label className="check">
      <input type="checkbox" {...rest} {...(labelText && !rest['aria-label'] ? { 'aria-label': labelText } : {})} />
      <span className="check-text">{label}{hint && <span className="field-hint">{hint}</span>}</span>
    </label>
  );
}

/** 屏级标题区：标题、说明、右侧动作。 */
export function PageHead({ title, meta, actions, children }: {
  title: ReactNode; meta?: ReactNode; actions?: ReactNode; children?: ReactNode;
}) {
  return (
    <header className="page-head">
      <div className="page-head-main">
        {/* title 可以是字符串，也可以是页面传入的 h1；字符串时由这里生成唯一的标题元素 */}
        {typeof title === 'string' ? <h2>{title}</h2> : title}
        {meta && <p className="page-meta">{meta}</p>}
      </div>
      {actions && <div className="page-actions">{actions}</div>}
      {children}
    </header>
  );
}

/** 内容分节：标题 + 可选说明 + 动作 + 内容。不要嵌套使用。 */
export function Section({ title, hint, actions, className = '', children, ...rest }: HTMLAttributes<HTMLElement> & {
  title?: ReactNode; hint?: ReactNode; actions?: ReactNode;
}) {
  return (
    <section {...rest} className={`section ${className}`}>
      {(title || actions) && <div className="section-head">
        <div className="section-head-main">{title && <h3>{title}</h3>}{hint && <p className="section-hint">{hint}</p>}</div>
        {actions && <div className="section-actions">{actions}</div>}
      </div>}
      {children}
    </section>
  );
}

export function EmptyState({ icon = 'key', title, children, action }: {
  icon?: IconName; title: string; children?: ReactNode; action?: ReactNode;
}) {
  return (
    <div className="empty">
      <span className="empty-mark"><Icon name={icon} size={22} /></span>
      <p className="empty-title">{title}</p>
      {children && <div className="empty-body">{children}</div>}
      {action && <div className="empty-action">{action}</div>}
    </div>
  );
}

/** 内联提示条。tone=danger 时自动 role=alert，其余 role=status。 */
export function Notice({ tone = 'muted', children }: { tone?: 'muted' | 'ok' | 'warn' | 'danger'; children: ReactNode }) {
  return <p className={`notice notice-${tone}`} role={tone === 'danger' ? 'alert' : 'status'}>{children}</p>;
}

/** 加载骨架：列表/表格在真实内容前使用，不使用居中转圈。 */
export function Skeleton({ rows = 3 }: { rows?: number }) {
  return <div className="skeleton" aria-busy="true" aria-label="正在加载">{Array.from({ length: rows }, (_, i) => <span key={i} />)}</div>;
}

/** 键值元信息行，用于抽屉/详情。 */
export function MetaList({ items }: { items: Array<{ label: string; value: ReactNode; mono?: boolean }> }) {
  return <dl className="meta-list">{items.map((item) => (
    <div key={item.label} className="meta-row">
      <dt>{item.label}</dt>
      <dd className={item.mono ? 'mono' : undefined}>{item.value}</dd>
    </div>
  ))}</dl>;
}

/** 表格滚动容器：窄屏横向滚动，表头可列名。 */
export function TableWrap({ children, label }: { children: ReactNode; label: string }) {
  return <div className="table-scroll" role="region" aria-label={label} tabIndex={0}>{children}</div>;
}

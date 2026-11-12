import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { api, ApiError, hasBrowserSession } from '../api';
import type { User, Org } from '../api';
import Icon from '../design/Icon';
import { Button, EmptyState, Field, MetaList, Notice, Pill, Section, Skeleton } from '../design/primitives';
import { codeIssue, normalizeCode, suggestCode } from '../pathcode';

interface OrgProject { id: string; name: string; slug: string; approval_required?: boolean }
interface OrgMember { user_id: string; email: string; name: string; role: string; restricted?: boolean }

interface Props {
  user: User;
  /** 当前空间排第一；其余按加入顺序。 */
  orgs: Org[];
  onOrgsChange: (orgs: Org[]) => void;
  /** 切到指定空间并进入控制台。 */
  onEnter: (orgId: string) => void;
  onBack: () => void;
}

function errorText(err: unknown): string {
  if (err instanceof ApiError) return `${err.code}: ${err.message}`;
  return err instanceof Error ? err.message : String(err);
}

function displayName(org: Org): string {
  return org.kind === 'personal' ? '个人空间' : org.name;
}

function initialOf(org: Org, user: User): string {
  const source = org.kind === 'personal' ? (user.name || user.email || '我') : org.name || '组';
  return source.trim().slice(0, 1).toUpperCase();
}

/** 组织管理页：左侧空间列表，右侧选中空间的管理面，新建组织是页内一张独立表单。 */
export default function Organizations({ user, orgs, onOrgsChange, onEnter, onBack }: Props) {
  const federated = hasBrowserSession();
  const canCreate = !federated;
  const current = orgs[0];
  const [selectedId, setSelectedId] = useState(current?.id ?? '');
  const [mode, setMode] = useState<'detail' | 'create'>('detail');
  const [mobileView, setMobileView] = useState<'list' | 'detail'>('list');
  const [notice, setNotice] = useState('');

  const selected = orgs.find((item) => item.id === selectedId) ?? current;

  const [projects, setProjects] = useState<OrgProject[] | null>(null);
  const [members, setMembers] = useState<OrgMember[] | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState('');

  useEffect(() => {
    if (mode !== 'detail' || !selected) return;
    let live = true;
    setDetailLoading(true);
    setDetailError('');
    setProjects(null);
    setMembers(null);
    const projectReq = api.get<{ projects: OrgProject[] }>(`/api/v1/orgs/${selected.slug}/projects`);
    const memberReq = selected.role === 'owner'
      ? api.get<{ members: OrgMember[] }>(`/api/v1/orgs/${selected.slug}/members`)
      : Promise.resolve(null);
    Promise.allSettled([projectReq, memberReq]).then(([projectResult, memberResult]) => {
      if (!live) return;
      if (projectResult.status === 'fulfilled') setProjects(projectResult.value.projects ?? []);
      else setDetailError(errorText(projectResult.reason));
      if (memberResult.status === 'fulfilled') setMembers(memberResult.value?.members ?? null);
      setDetailLoading(false);
    });
    return () => { live = false; };
  }, [selected?.id, selected?.slug, selected?.role, mode]);

  function select(org: Org) {
    setSelectedId(org.id);
    setMode('detail');
    setNotice('');
    setMobileView('detail');
  }

  function openCreate() {
    if (!canCreate) return;
    setMode('create');
    setMobileView('detail');
    setCreateError('');
  }

  const [name, setName] = useState('');
  const [codeInput, setCodeInput] = useState('');
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState('');

  const rawCode = normalizeCode(codeInput);
  const suggested = suggestCode(name);
  const previewCode = rawCode || suggested;
  const rawIssue = rawCode ? codeIssue(rawCode) : '';
  const codeProblem = rawIssue === 'reserved'
    ? `「${rawCode}」是保留字，换一个。`
    : rawIssue ? '1–39 位，小写字母、数字或中划线；首尾不能是中划线。' : '';

  async function create(event: FormEvent) {
    event.preventDefault();
    if (!canCreate || creating || !name.trim() || codeProblem) return;
    setCreating(true);
    setCreateError('');
    try {
      const made = await api.post<{ id: string; name: string; slug: string; role: string }>('/api/v1/orgs', {
        name: name.trim(),
        code: rawCode || suggested,
      });
      const me = await api.get<{ orgs: Org[] }>('/api/v1/me');
      const currentId = orgs[0]?.id;
      const next = currentId
        ? [...me.orgs.filter((item) => item.id === currentId), ...me.orgs.filter((item) => item.id !== currentId)]
        : me.orgs;
      onOrgsChange(next);
      setSelectedId(made.id);
      setMode('detail');
      setMobileView('detail');
      setName('');
      setCodeInput('');
      setNotice(`已创建组织「${made.name}」。确认后进入控制台。`);
    } catch (err) {
      if (err instanceof ApiError && err.code === 'CODE_TAKEN') setCreateError(`组织 code「${rawCode || suggested}」已被占用，换一个。`);
      else if (err instanceof ApiError && err.code === 'INVALID_CODE') setCreateError('组织 code 格式不对：1–39 位，小写字母、数字或中划线。');
      else if (err instanceof ApiError && err.code === 'FEDERATED_SESSION') setCreateError('外部登录会话不能创建组织；请改用本地账号登录。');
      else setCreateError(errorText(err));
    } finally {
      setCreating(false);
    }
  }

  function enter(org: Org) {
    if (federated && org.id !== current?.id) return;
    onEnter(org.id);
  }

  const isCurrent = selected?.id === current?.id;
  const enterBlocked = federated && !isCurrent;

  return (
    <div className="spaces-shell" data-mobile-view={mobileView}>
      <header className="spaces-topbar">
        <div className="spaces-brand">
          <span className="seal-mark" aria-hidden="true">禁</span>
          <span className="spaces-wordmark">taboo <span className="cjk">禁制</span></span>
          <span className="spaces-crumb" aria-hidden="true">/</span>
          <span className="spaces-crumb-title">组织</span>
        </div>
        <div className="spaces-topbar-actions">
          <span className="spaces-user">{user.name || user.email}</span>
          <Button variant="quiet" icon="arrow-left" onClick={onBack}>返回控制台</Button>
        </div>
      </header>

      <div className="spaces-body">
        <aside className="spaces-side" aria-label="个人与组织列表">
          <div className="spaces-side-head">
            <span>个人与组织</span>
            <span className="mono">{orgs.length}</span>
          </div>
          <ul className="space-list">
            {orgs.map((item) => (
              <li key={item.id}>
                <button type="button" className={`space-row ${mode === 'detail' && item.id === selected?.id ? 'active' : ''}`}
                  aria-current={mode === 'detail' && item.id === selected?.id ? 'true' : undefined}
                  onClick={() => select(item)}>
                  <span className={`space-mark ${item.kind === 'personal' ? 'personal' : ''}`} aria-hidden="true">{initialOf(item, user)}</span>
                  <span className="space-row-main">
                    <span className="space-row-name">{displayName(item)}</span>
                    <span className="space-row-sub"><span className="mono">{item.slug}</span> · {item.role}</span>
                  </span>
                  {item.id === current?.id && <Pill tone="accent">当前</Pill>}
                </button>
              </li>
            ))}
          </ul>
          <div className="spaces-side-foot">
            <Button variant="default" icon="plus" className="btn-block" onClick={openCreate} disabled={!canCreate}>新建组织</Button>
            {!canCreate && <p className="field-hint">外部登录会话不能创建组织；请用本地账号登录。</p>}
            <p className="field-hint">组织有独立的 DEK、成员与审计；个人空间只有你能进入。</p>
          </div>
        </aside>

        <main className="spaces-main">
          <button type="button" className="space-back" onClick={() => setMobileView('list')}>
            <Icon name="arrow-left" size={14} /> 空间列表
          </button>

          {mode === 'create' ? (
            <article className="space-create">
              <header className="space-create-head">
                <h1>新建组织</h1>
                <p>组织有独立的 DEK、成员与审计边界；创建后你是它的 owner。</p>
              </header>
              <form className="space-create-form" onSubmit={create} noValidate>
                <Field label="显示名称" htmlFor="org-create-name" hint="成员看到的名字，80 字以内。">
                  <input id="org-create-name" value={name} onChange={(event) => setName(event.target.value)}
                    required maxLength={80} autoFocus placeholder="例如：平台工程" />
                </Field>
                <Field label="组织 code" htmlFor="org-create-code" hint="定位符第一段；留空按名称生成，创建后不能修改。">
                  <input id="org-create-code" className="mono" value={codeInput} onChange={(event) => setCodeInput(event.target.value)}
                    maxLength={39} spellCheck={false} autoComplete="off" placeholder={suggested || '自动生成'} />
                </Field>
                <div className="space-code-preview" aria-live="polite">
                  <span className="field-label">定位符前缀</span>
                  <code>{previewCode || '自动生成'}<span className="faint">/default/…</span></code>
                  <span className="field-hint">组织 code 是定位符的第一段：code / 项目 / 目录。</span>
                </div>
                {codeProblem && <p className="error" role="alert">{codeProblem}</p>}
                {createError && <Notice tone="danger">{createError}</Notice>}
                <div className="form-actions">
                  <Button variant="quiet" onClick={() => setMode('detail')} disabled={creating}>取消</Button>
                  <Button type="submit" variant="primary" icon="seal" loading={creating}
                    disabled={!canCreate || !name.trim() || !!codeProblem}>创建组织</Button>
                </div>
              </form>
            </article>
          ) : selected ? (
            <article className="space-detail">
              <header className="space-detail-head">
                <span className={`space-mark xl ${selected.kind === 'personal' ? 'personal' : ''}`} aria-hidden="true">
                  {initialOf(selected, user)}
                </span>
                <div className="space-detail-id">
                  <h1>{displayName(selected)}</h1>
                  <p className="space-detail-meta">
                    <span className="mono">{selected.slug}</span>
                    <span aria-hidden="true">·</span>
                    <span>角色 <span className="mono">{selected.role}</span></span>
                    {isCurrent && <Pill tone="accent" icon="seal">当前空间</Pill>}
                  </p>
                </div>
                <div className="space-detail-actions">
                  <Button variant="primary" icon="arrow-right" onClick={() => enter(selected)} disabled={enterBlocked}>进入控制台</Button>
                </div>
              </header>

              {notice && <Notice tone="ok">{notice}</Notice>}
              {enterBlocked && <Notice tone="warn">外部登录会话只属于当前组织；进入其他组织请从账户菜单的「切换组织」重新验证身份。</Notice>}
              {detailError && <Notice tone="danger">读取空间信息失败：{detailError}</Notice>}

              <MetaList items={[
                { label: '空间 code', value: <span className="mono">{selected.slug}</span>, mono: true },
                { label: '类型', value: selected.kind === 'personal' ? '个人空间' : '组织' },
                { label: '你的角色', value: <span className="mono">{selected.role}</span> },
                { label: '成员', value: selected.role !== 'owner' ? '仅 owner 可见' : members ? `${members.length} 人` : detailLoading ? '读取中…' : '—' },
                { label: '项目', value: projects ? `${projects.length} 个` : detailLoading ? '读取中…' : '—' },
              ]} />

              <Section title="项目" hint="项目是定位符的第二段；密钥在项目内按目录组织。">
                {detailLoading ? <Skeleton rows={3} /> : projects && projects.length > 0 ? (
                  <ul className="space-projects">
                    {projects.map((project) => (
                      <li key={project.id} className="space-project">
                        <Icon name="folder" size={14} />
                        <span className="space-project-name">{project.name}</span>
                        <span className="space-project-code mono">{project.slug}</span>
                        {project.approval_required && <Pill tone="warn">变更审批</Pill>}
                      </li>
                    ))}
                  </ul>
                ) : (
                  <EmptyState icon="folder" title="还没有项目">进入控制台后可以新建项目。</EmptyState>
                )}
              </Section>

              <Section title="成员" hint={selected.kind === 'personal' ? '个人空间只有你能进入。' : '成员角色决定可见项目与可执行的动作。'}>
                {selected.role !== 'owner' ? (
                  <Notice tone="muted">成员与角色仅组织 owner 可管理。</Notice>
                ) : detailLoading ? (
                  <Skeleton rows={3} />
                ) : members && members.length > 0 ? (
                  <ul className="space-members">
                    {members.map((member) => (
                      <li key={member.user_id} className="space-member">
                        <span className="space-member-mark" aria-hidden="true">{(member.name || member.email || '?').trim().slice(0, 1).toUpperCase()}</span>
                        <span className="space-member-main">
                          <span className="space-member-name">{member.name || member.email}</span>
                          <span className="space-member-email mono">{member.email}</span>
                        </span>
                        {member.restricted && <Pill tone="warn">受限</Pill>}
                        <Pill tone="muted">{member.role}</Pill>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <Notice tone="muted">还没有读到成员列表。</Notice>
                )}
              </Section>

              <p className="spaces-footnote">成员角色、身份连接、登录策略与 Secret Sync 在进入控制台后的「组织设置」里修改。</p>
            </article>
          ) : (
            <EmptyState icon="users" title="没有可管理的空间">用本地账号登录后可以创建组织。</EmptyState>
          )}
        </main>
      </div>
    </div>
  );
}

import { useState } from 'react';
import type { Schema } from '../data/client';
import { useUpdateAuthorProfile } from '../data/author-profiles';
import { ErrorNotice } from '../components/Feedback';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { UnsavedGuard } from '../lib/navigation';
import { profileBlockTitles, profileFieldLabels, profilePatch, rebaseProfileDraft } from '../lib/profile-draft';
import type { ProfileValues } from '../lib/profile-draft';
import { ProfileEvidenceEditor } from './ProfileEvidenceEditor';
import { ProfileEvidenceList, ProfileValuesView } from './ProfilePresentation';

type Block = Schema<'AuthorProfileBlock'>;
type Candidate = Schema<'AuthorProfileCandidate'>;
type ProfileRead = () => Promise<{ data?: Schema<'AuthorProfile'>; isSuccess: boolean; error?: unknown }>;

export function ProfileBlockEditor({ block, candidate, reload, close }: {
  block: Block; candidate?: Candidate; reload: ProfileRead; close: () => void;
}) {
  const [base, setBase] = useState(block);
  const [draft, setDraft] = useState<ProfileValues>({ ...block.values, ...candidate?.values });
  const [evidence, setEvidence] = useState(candidate?.evidence ?? block.evidence);
  const [translationReviewed, setTranslationReviewed] = useState(false);
  const [candidateReviewed, setCandidateReviewed] = useState(false);
  const [discard, setDiscard] = useState(false);
  const [comparison, setComparison] = useState<Block>();
  const [readError, setReadError] = useState<unknown>();
  const mutation = useUpdateAuthorProfile(block.authorId, block.block);
  const changes = profilePatch(base.values, draft);
  const evidenceChanged = JSON.stringify(evidence) !== JSON.stringify(base.evidence);
  const dirty = Object.keys(changes).length > 0 || evidenceChanged || !!candidate;
  const originalChanged = changes.biographyOriginal !== undefined || changes.profileUrl !== undefined;
  const translationChanged = changes.biographyZh !== undefined && !!draft.biographyZh;
  const translationNeedsReview = originalChanged && translationChanged && !translationReviewed;
  const validEvidence = evidence.every(item => item.url.trim() && item.fields.length > 0);
  const update = (patch: ProfileValues) => setDraft(current => ({ ...current, ...patch }));

  const save = () => {
    const values = { ...changes };
    if (originalChanged && translationReviewed) values.biographyZh = draft.biographyZh ?? '';
    const body: Schema<'AuthorProfileWrite'> = {
      expectedRevision: base.revision, values,
      ...(evidenceChanged || candidate ? { evidence } : {}),
      ...(candidate ? { candidateId: candidate.id } : {}),
      translationReviewed,
    };
    mutation.mutate(body, { onSuccess: close });
  };
  const loadCurrent = async () => {
    const result = await reload();
    const current = result.data?.blocks.find(item => item.block === block.block);
    if (result.isSuccess && current) { setComparison(current); setReadError(undefined); }
    else setReadError(result.error ?? new Error('未取得当前资料块，草稿保持不变。'));
  };
  const rebase = () => {
    if (!comparison) return;
    setDraft(rebaseProfileDraft(base.values, draft, comparison.values));
    if (!evidenceChanged) setEvidence(comparison.evidence);
    setBase(comparison); setComparison(undefined); setTranslationReviewed(false); setCandidateReviewed(false); mutation.reset();
  };

  return (
    <form className="panel form-stack profile-editor" onSubmit={event => { event.preventDefault(); save(); }}>
      <div className="section-heading"><h3>编辑{profileBlockTitles[block.block]}</h3><span className="muted">修订 {base.revision}</span></div>
      {candidate && <div className="notice"><strong>候选已填入草稿</strong><p>来源候选基于修订 {candidate.baseRevision}。请核对当前资料、证据和下面的草稿，保存后才会生效。</p>{candidate.baseRevision !== base.revision && <p>候选所依据的版本已变化，请特别核对差异。</p>}</div>}
      <fieldset className="form-stack profile-editor-fields" disabled={mutation.isPending}>
        {block.block === 'x' ? <>
          <label>X 账号<input value={draft.handle ?? ''} maxLength={200} onChange={event => update({ handle: event.target.value })} placeholder="@账号" /></label>
          <label>X 账号主页<input type="url" maxLength={4096} value={draft.profileUrl ?? ''} onChange={event => { update({ profileUrl: event.target.value }); setTranslationReviewed(false); }} /></label>
          <label>X 显示名称<input value={draft.displayName ?? ''} maxLength={200} onChange={event => update({ displayName: event.target.value })} /></label>
          <label>X 简介原文<textarea rows={5} maxLength={20000} value={draft.biographyOriginal ?? ''} onChange={event => { update({ biographyOriginal: event.target.value }); setTranslationReviewed(false); }} /></label>
          <label>X 简介中文译文<textarea rows={5} maxLength={20000} value={draft.biographyZh ?? ''} onChange={event => { update({ biographyZh: event.target.value }); setTranslationReviewed(false); }} /></label>
          {originalChanged && <div className="notice"><label className="check-label"><input type="checkbox" checked={translationReviewed} onChange={event => setTranslationReviewed(event.target.checked)} />我已核对当前账号、原文与译文的对应关系</label><p>{translationChanged ? '同时修改账号或原文与译文，需要确认对应关系。' : '账号或原文已经变化。若不保留并核对现有译文，保存时会清除旧译文。'}</p></div>}
          <label>头像来源网址<input type="url" maxLength={4096} value={draft.avatarSourceUrl ?? ''} onChange={event => update({ avatarSourceUrl: event.target.value })} /></label>
          <p className="muted">更改来源会解除旧头像绑定。保存来源后，可在“离线头像”中明确下载。</p>
        </> : block.block === 'basic' ? <>
          <label>公开昵称<input maxLength={200} value={draft.publicName ?? ''} onChange={event => update({ publicName: event.target.value })} /></label>
          <label>整理简介<textarea rows={7} maxLength={20000} value={draft.summary ?? ''} onChange={event => update({ summary: event.target.value })} /></label>
          <label>特殊情况备注<textarea rows={3} maxLength={500} value={draft.remark ?? ''} onChange={event => update({ remark: event.target.value })} placeholder="例如退圈、官方账号删除、只能从存档核实身份；常规作者留空" /></label>
          <label>私人备注<textarea rows={5} maxLength={10000} value={draft.note ?? ''} onChange={event => update({ note: event.target.value })} /></label>
          <p className="muted">备注保存在自己的影库中；具有全库读取权限的 AI 接入也能读取。</p>
        </> : <AccountFields accounts={draft.accounts ?? []} change={accounts => update({ accounts })} />}
        <ProfileEvidenceEditor block={block.block} evidence={evidence} change={setEvidence} />
        {candidate && <label className="check-label"><input type="checkbox" checked={candidateReviewed} onChange={event => setCandidateReviewed(event.target.checked)} />我已对照当前资料与候选，确认保存这份草稿</label>}
        {Object.entries(changes).some(([field, value]) => value === '' || field === 'accounts' && Array.isArray(value) && value.length === 0) && <p className="notice">本次保存包含明确清空：{Object.entries(changes).filter(([, value]) => value === '' || Array.isArray(value) && !value.length).map(([field]) => profileFieldLabels[field as keyof ProfileValues]).join('、')}。</p>}
        <div className="actions">
          <button className="button button--primary" disabled={!dirty || translationNeedsReview || !validEvidence || !!candidate && !candidateReviewed}>{mutation.isPending ? '正在保存…' : '保存资料'}</button>
          <button type="button" className="button" onClick={() => dirty ? setDiscard(true) : close()}>取消</button>
        </div>
      </fieldset>
      <ErrorNotice error={mutation.error} />
      {mutation.isError && <button type="button" className="button" disabled={mutation.isPending} onClick={() => void loadCurrent()}>读取当前资料并对照</button>}
      <ErrorNotice error={readError} />
      {comparison && <div className="notice"><h4>服务器当前修订 {comparison.revision}</h4><ProfileValuesView block={block.block} values={comparison.values} /><ProfileEvidenceList evidence={comparison.evidence} /><button type="button" className="button" onClick={rebase}>已核对，保留本次改动并采用当前版本</button></div>}
      <UnsavedGuard dirty={dirty} />
      {discard && <ConfirmDialog title="放弃未保存的资料？" onCancel={() => setDiscard(false)} onConfirm={close}><p>当前草稿尚未保存，候选记录仍保留在服务器。</p></ConfirmDialog>}
    </form>
  );
}

function AccountFields({ accounts, change }: {
  accounts: Schema<'AuthorProfileAccount'>[]; change: (next: Schema<'AuthorProfileAccount'>[]) => void;
}) {
  const update = (index: number, patch: Partial<Schema<'AuthorProfileAccount'>>) => change(accounts.map((account, at) => at === index ? { ...account, ...patch } : account));
  return <>
    {accounts.map((account, index) => <fieldset className="form-stack profile-account-row" key={index}>
      <legend>公开账号 {index + 1}</legend>
      <label>平台<input required maxLength={80} value={account.platform} onChange={event => update(index, { platform: event.target.value })} placeholder="例如 X、Pixiv" /></label>
      <label>账号标识<input maxLength={200} value={account.identifier} onChange={event => update(index, { identifier: event.target.value })} /></label>
      <label>账号主页<input type="url" maxLength={4096} value={account.url} onChange={event => update(index, { url: event.target.value })} /></label>
      <label>作品发布入口<input type="url" maxLength={4096} value={account.publishingUrl} onChange={event => update(index, { publishingUrl: event.target.value })} /></label>
      <button type="button" className="button button--small" onClick={() => change(accounts.filter((_, at) => at !== index))}>移除此账号</button>
    </fieldset>)}
    <button type="button" className="button" disabled={accounts.length >= 32} onClick={() => change([...accounts, { platform: '', identifier: '', url: '', publishingUrl: '' }])}>添加公开账号</button>
  </>;
}

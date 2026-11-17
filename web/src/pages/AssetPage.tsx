import { useRef, useState } from 'react';
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom';
import { useAsset, useAssetActions, useTags, originalUrl, useAssets, mediaCards } from '../data/media';
import { useDetailSession } from '../data/detail-session';
import type { Asset } from '../data/media';
import { AssetMetadataEditor } from './AssetMetadataEditor';
import { ErrorNotice, Loading, PageHeading, StatusBadge } from '../components/Feedback';
import { Icon } from '../components/Icon';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { ImageViewer } from '../components/ImageViewer';
import { VideoPlayer } from '../components/VideoPlayer';
import { MediaGrid } from '../components/MediaGrid';
import { bytes, dateTime } from '../lib/format';
import { UnsavedGuard } from '../lib/navigation';
import { newId } from '../lib/id';
import { TimelinePanel } from './TimelinePanel';

export function AssetPage() {
  const { id = '' } = useParams();
  const asset = useAsset(id);
  if (!asset.data) return <><PageHeading title="媒体详情" />{asset.isPending ? <Loading text="正在打开媒体…" /> : <ErrorNotice error={asset.error} retry={() => void asset.refetch()} />}</>;
  return <><ErrorNotice error={asset.error} stale retry={() => void asset.refetch()} /><AssetDetail key={`${asset.data.id}:${asset.data.contentVersion}`} asset={asset.data} /></>;
}

function AssetDetail({ asset }: { asset: Asset }) {
  const actions = useAssetActions(asset.id);
  const event = useDetailSession(asset.id, asset.contentVersion);
  const [viewer, setViewer] = useState(false);
  const [editor, setEditor] = useState<'tags' | 'metadata' | 'rename' | null>(null);
  const [confirmTrash, setConfirmTrash] = useState(false);
  const [copyMessage, setCopyMessage] = useState('');
  const [seek, setSeek] = useState<{ id: string; positionMs: number }>();
  const currentPosition = useRef(0);
  const trashKey = useRef(newId());
  const navigate = useNavigate();
  const location = useLocation();
  const src = originalUrl(asset);
  const related = useAssets(asset.collectionId ? { collectionId: asset.collectionId } : { authorId: asset.authorId });
  const relatedItems = (related.data?.pages.flatMap(page => page.items) ?? []).filter(item => item.id !== asset.id);
  const back = () => { if (location.state?.returnTo) navigate(-1); else navigate('/album'); };
  return <>
    <button className="back-link" onClick={back}><Icon name="back" size={18} />返回浏览</button>
    <PageHeading title={asset.fileName} action={<a className="button button--small" href={src} target="_blank" rel="noreferrer">打开原件</a>} />
    <div className="detail-layout"><div className="detail-main">
      {asset.mediaType === 'video' ? <VideoPlayer src={src} title={asset.fileName} initialPosition={asset.positionMs} progressMatches={asset.positionContentVersion === asset.contentVersion} onPlay={event.play} onProgress={event.progress} onPosition={value => { currentPosition.current = value; }} seek={seek} />
        : <button className="image-stage" aria-label="查看原图并缩放" onClick={() => setViewer(true)}><img src={src} alt={asset.fileName} /><span>查看原图</span></button>}
      <div className="detail-actions"><button className={`button ${asset.favorite ? 'button--selected' : ''}`} aria-pressed={asset.favorite} disabled={actions.reaction.isPending} onClick={() => actions.reaction.mutate({ favorite: !asset.favorite, liked: false })}><Icon name="star" size={18} />{asset.favorite ? '已收藏' : '收藏'}</button><button className={`button ${asset.liked ? 'button--selected' : ''}`} aria-pressed={asset.liked} disabled={actions.reaction.isPending || asset.liked} onClick={() => actions.reaction.mutate({ favorite: asset.favorite, liked: true })}><Icon name="heart" size={18} />{asset.liked ? '今日已赞' : '点赞'} · {asset.likeCount}</button><button className="button" onClick={() => setEditor('tags')}>管理标签</button><button className="button" onClick={() => setEditor('metadata')}>编辑资料</button></div>
      <ErrorNotice error={actions.reaction.error} /><ErrorNotice error={event.error} retry={event.retry} />
      {asset.mediaType === 'video' && <TimelinePanel assetId={asset.id} contentVersion={asset.contentVersion} currentPosition={() => currentPosition.current} onSeek={positionMs => setSeek({ id: newId(), positionMs })} />}
      <div className="tag-pool">{asset.tags.map(tag => <Link className="tag" key={tag} to={`/album?tag=${encodeURIComponent(tag)}`}>{tag}</Link>)}</div>
      {editor === 'tags' && <TagEditor key={asset.id} asset={asset} close={() => setEditor(null)} />}{editor === 'metadata' && <AssetMetadataEditor key={asset.id} asset={asset} close={() => setEditor(null)} />}{editor === 'rename' && <RenameEditor asset={asset} close={() => setEditor(null)} />}
      <section className="panel"><div className="section-heading"><h2>详细信息</h2><button className="button button--small" onClick={() => setEditor('rename')}>改名 / 移动</button></div><div className="file-name"><span>{asset.fileName}</span><button className="button button--small" onClick={() => { if (!navigator.clipboard) { setCopyMessage('此浏览器不能直接复制，请选中文件名手动复制。'); return; } void navigator.clipboard.writeText(asset.fileName).then(() => setCopyMessage('已复制文件名'), () => setCopyMessage('复制失败，请选中文件名手动复制。')); }}>复制</button></div>{copyMessage && <p role="status">{copyMessage}</p>}<dl className="facts"><div><dt>原始名称</dt><dd>{asset.originalFileName}</dd></div><div><dt>类型 / 大小</dt><dd>{asset.mimeType} · {bytes(asset.size)}</dd></div><div><dt>入库时间</dt><dd>{dateTime(asset.createdAt)}</dd></div><div><dt>内容版本</dt><dd>{asset.contentVersion}</dd></div><div><dt>作品出处</dt><dd>{asset.source || '未填写'}</dd></div>{asset.collectionId && <div><dt>集合页序</dt><dd><Link to={`/album?collectionId=${encodeURIComponent(asset.collectionId)}`}>查看同组内容{asset.page ? ` · 第 ${asset.page} 项` : ''}</Link></dd></div>}<div><dt>个人备注</dt><dd className="preserve-lines">{asset.note || '未填写'}</dd></div></dl><button className="button button--danger" onClick={() => setConfirmTrash(true)}>移入回收站</button></section>
    </div><aside className="detail-aside"><Link className="panel author-identity" to={`/authors/${encodeURIComponent(asset.authorId)}`}><span className="avatar">{(asset.authorName ?? '其').slice(0, 1)}</span><div><h2>{asset.authorName ?? '其他搁置'}</h2>{asset.authorStatus === 'unknown' ? <StatusBadge tone="warning">归属未确认</StatusBadge> : <span className="muted">作者档案与作品</span>}</div></Link><section className="panel related-panel"><h2>{asset.collectionId ? '同组内容' : '作者的其他作品'}</h2><ErrorNotice error={related.error} retry={() => void related.refetch()} />{related.isPending ? <Loading /> : relatedItems.length ? <div className="related-scroll" tabIndex={0} aria-label="相关媒体"><MediaGrid items={mediaCards(relatedItems)} /></div> : !related.isError && <p className="muted">暂时没有其他内容</p>}{related.hasNextPage && <button className="button" disabled={related.isFetchingNextPage} onClick={() => void related.fetchNextPage()}>加载更多</button>}</section></aside></div>
    {viewer && <ImageViewer src={src} title={asset.fileName} onClose={() => setViewer(false)} />}
    {confirmTrash && <ConfirmDialog title="移入回收站？" danger confirmLabel="移入回收站" busy={actions.trash.isPending} onCancel={() => setConfirmTrash(false)} onConfirm={() => actions.trash.mutate({ revision: asset.revision, key: trashKey.current }, { onSuccess: () => navigate('/album', { replace: true }) })}><p>{asset.fileName}</p><p>媒体将从浏览列表移除，可以在回收站恢复原身份和关联。</p><ErrorNotice error={actions.trash.error} /></ConfirmDialog>}
  </>;
}

function TagEditor({ asset, close }: { asset: Asset; close: () => void }) {
  const vocabulary = useTags();
  const actions = useAssetActions(asset.id);
  const [base] = useState({ revision: asset.tagsRevision, tags: asset.tags });
  const [selected, setSelected] = useState([...asset.tags]);
  const [created, setCreated] = useState<string[]>([]);
  const [newTag, setNewTag] = useState('');
  const [discard, setDiscard] = useState(false);
  const attempt = useRef({ content: '', key: newId() });
  const candidate = [...new Set([...(vocabulary.data?.items ?? []), ...base.tags, ...created])].sort((a, b) => a.localeCompare(b, 'zh-CN', { numeric: true }));
  const dirty = selected.length !== base.tags.length || selected.some(tag => !base.tags.includes(tag));
  const save = () => {
    const content = JSON.stringify(selected);
    if (attempt.current.content !== content) attempt.current = { content, key: newId() };
    actions.tags.mutate({ key: attempt.current.key, body: { expectedRevision: base.revision, tags: selected } }, { onSuccess: close });
  };
  return <section className="panel form-stack"><div className="section-heading"><h2>管理标签</h2><span className="muted">已选 {selected.length} 项</span></div><p className="muted">选择不会改变候选位置；清空后保存也是有效修改。</p><ErrorNotice error={vocabulary.error} retry={() => void vocabulary.refetch()} /><div className="tag-pool tag-pool--scroll">{candidate.map(tag => <button key={tag} className="tag" type="button" disabled={actions.tags.isPending} aria-pressed={selected.includes(tag)} onClick={() => setSelected(current => current.includes(tag) ? current.filter(value => value !== tag) : [...current, tag])}>{tag}</button>)}</div><form className="inline-form" onSubmit={event => { event.preventDefault(); const tag = newTag.trim(); if (!tag) return; setCreated(values => values.includes(tag) ? values : [...values, tag]); setSelected(values => values.includes(tag) ? values : [...values, tag]); setNewTag(''); }}><input aria-label="新标签名称" maxLength={100} disabled={actions.tags.isPending} value={newTag} onChange={event => setNewTag(event.target.value)} placeholder="新建标签" /><button className="button" disabled={!newTag.trim() || actions.tags.isPending}>加入</button></form><ErrorNotice error={actions.tags.error} /><div className="actions"><button className="button button--primary" disabled={!dirty || actions.tags.isPending || !vocabulary.data} onClick={save}>{actions.tags.isPending ? '正在保存…' : '保存标签'}</button><button className="button" disabled={actions.tags.isPending} onClick={() => setSelected([])}>清空选择</button><button className="button" disabled={actions.tags.isPending} onClick={() => dirty ? setDiscard(true) : close()}>取消</button></div><UnsavedGuard dirty={dirty} />{discard && <ConfirmDialog title="放弃标签修改？" onCancel={() => setDiscard(false)} onConfirm={close}><p>尚未保存的选择会被放弃。</p></ConfirmDialog>}</section>;
}

function RenameEditor({ asset, close }: { asset: Asset; close: () => void }) {
  const [base] = useState(asset);
  const [path, setPath] = useState(asset.relativePath);
  const actions = useAssetActions(asset.id);
  const attempt = useRef({ path: '', key: newId() });
  const dirty = path !== base.relativePath;
  return <form className="panel form-stack" onSubmit={event => { event.preventDefault(); if (attempt.current.path !== path) attempt.current = { path, key: newId() }; actions.move.mutate({ relativePath: path, revision: base.revision, key: attempt.current.key }, { onSuccess: close }); }}><h2>精确改名 / 移动</h2><p className="muted">填写库内相对路径和完整文件名。保持资产身份、收藏和历史；重名会提示冲突。</p><label>目标路径<input value={path} disabled={actions.move.isPending} maxLength={500} onChange={event => setPath(event.target.value)} required /></label><ErrorNotice error={actions.move.error} /><div className="actions"><button className="button button--primary" disabled={!dirty || actions.move.isPending}>保存位置</button><button type="button" className="button" disabled={actions.move.isPending} onClick={close}>取消</button></div><UnsavedGuard dirty={dirty} /></form>;
}

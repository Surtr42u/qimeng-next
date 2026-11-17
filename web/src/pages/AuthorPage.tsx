import { useState } from 'react';
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom';
import { useAuthor, useUpdateAuthor } from '../data/manage';
import { useAssets, mediaCards } from '../data/media';
import { useAuthorProfile } from '../data/author-profiles';
import { ErrorNotice, Loading } from '../components/Feedback';
import { MediaGrid } from '../components/MediaGrid';
import { LegacyFacetFilter } from '../components/LegacyFacetFilter';
import { useAssetFacets } from '../data/browse';
import { useBrowseSelection, BrowseTail } from './BrowsePage';
import { AuthorAvatar, ProfileLink } from './ProfilePresentation';
import { ProfileDetails } from './ProfileDetails';

export function AuthorPage() {
  const { id = '' } = useParams();
  const location = useLocation();
  const [params, setParams] = useSearchParams();
  const author = useAuthor(id);
  const profile = useAuthorProfile(id);
  const [chinese, setChinese] = useState(true);
  const { filters, change } = useBrowseSelection({ authorId: id });
  const assets = useAssets(filters);
  const roles = useAssetFacets(filters, 'roles');
  const types = useAssetFacets(filters, 'types');
  const follow = useUpdateAuthor(id);
  const items = assets.data?.pages.flatMap(page => page.items) ?? [];
  const fileCount = author.data?.fileCount ?? assets.data?.pages[0]?.totalMatched ?? '—';
  const x = profile.data?.blocks.find(block => block.block === 'x');
  const accounts = profile.data?.blocks.find(block => block.block === 'accounts')?.values.accounts ?? [];
  const showUpdates = params.get('section') === 'updates';
  const setParam = (key: string, value: string) => setParams(current => { const next = new URLSearchParams(current); value ? next.set(key, value) : next.delete(key); return next; }, { replace: true });
  if (!author.data) return <div className="page" id="page-collection"><div className="page-head"><h2 data-page-heading tabIndex={-1}>作者主页</h2></div>{author.isPending ? <Loading /> : <ErrorNotice error={author.error} retry={() => void author.refetch()} />}</div>;
  const current = author.data;
  const original = x?.values.biographyOriginal;
  const translated = x?.values.biographyZh;
  const showChinese = chinese && !!translated;
  return <div className="page" id="page-collection">
    <div className="page-head"><h2 data-page-heading tabIndex={-1}>作者主页</h2><p>作者 · {fileCount} 个文件</p></div>
    <ErrorNotice error={author.error} stale retry={() => void author.refetch()} />
    <section className="rank-card creator-profile" aria-label="作者档案">
      <div className="creator-profile-identity"><AuthorAvatar name={current.name} avatar={x?.avatar} className="creator-profile-avatar" /><div className="creator-profile-name"><h2>{current.name}</h2><p>{current.kind === 'cos' ? 'COS 作者' : '创作者'} · {fileCount} 个本地文件</p></div><button type="button" className="pill" disabled={follow.isPending} onClick={() => follow.mutate({ expectedRevision: current.revision, following: !current.following })}>{current.following ? '已关注' : '关注'}</button></div>
      {original && <p className="creator-profile-bio" aria-label={showChinese ? 'X 简介中文' : 'X 简介原文'} lang={showChinese ? 'zh-CN' : undefined}>{showChinese ? translated : original}</p>}
      <div className="creator-profile-actions">
        {x?.values.profileUrl && <ProfileLink value={x.values.profileUrl}>前往 X ↗</ProfileLink>}
        <Link className="pill" to={`/authors/${encodeURIComponent(id)}/profile`}>详细资料</Link>
        {translated && <div className="creator-language" role="group" aria-label="简介显示语言"><button type="button" aria-pressed={chinese} onClick={() => setChinese(true)}>中文</button><button type="button" aria-pressed={!chinese} onClick={() => setChinese(false)}>原文</button></div>}
      </div>
      {profile.isPending && <p role="status">正在读取作者资料…</p>}
      <ErrorNotice error={profile.error} stale={!!profile.data} retry={() => void profile.refetch()} /><ErrorNotice error={follow.error} />
    </section>
    {location.state?.mergeSummary && <p role="status" className="notice">作者合并已完成，影响 {location.state.mergeSummary.affected} 份媒体，保留 {location.state.mergeSummary.conflicts} 项档案冲突记录。<Link to={`/authors/${encodeURIComponent(id)}/archives`}>查看保留的档案</Link></p>}
    <div className="creator-content-tabs" role="group" aria-label="作者内容分区"><button className={`pill${showUpdates ? '' : ' active'}`} aria-pressed={!showUpdates} onClick={() => setParam('section', '')}>本地作品</button><button className={`pill${showUpdates ? ' active' : ''}`} aria-pressed={showUpdates} onClick={() => setParam('section', 'updates')}>作者新作</button></div>
    {showUpdates ? <section className="rank-card creator-updates" aria-label="作者新作"><h3>作者新作</h3>{profile.isPending ? <Loading /> : profile.error ? <ErrorNotice error={profile.error} retry={() => void profile.refetch()} /> : <p>尚未接入可用的作品发布订阅，可前往作者发布页查看新作。</p>}<div className="creator-profile-actions">{accounts.filter(account => account.publishingUrl).map((account, index) => <ProfileLink key={index} value={account.publishingUrl}>{account.platform} 发布入口 ↗</ProfileLink>)}{x?.values.profileUrl && <ProfileLink value={x.values.profileUrl}>查看 X 发布 ↗</ProfileLink>}<Link className="pill" to={`/authors/${encodeURIComponent(id)}/profile`}>发布来源与详细资料</Link></div></section> : <>
      <LegacyFacetFilter filters={filters} dimensions={[{ row: 'roles', label: current.kind === 'cos' ? '作品' : '角色', query: roles }, { row: 'types', label: '类型', query: types }]} onChange={change} />
      <ErrorNotice error={assets.error} stale={items.length > 0} retry={() => void assets.refetch()} />
      {assets.isPending ? <Loading /> : items.length ? <MediaGrid items={mediaCards(items)} /> : !assets.isError && <p className="grid-empty">该作者下暂无内容。</p>}
      <BrowseTail query={assets} count={items.length} />
    </>}
  </div>;
}

export function AuthorProfilePage() {
  const { id = '' } = useParams();
  const author = useAuthor(id);
  return <div className="page" id="page-author-profile">
    <header className="manage-page-head"><Link className="manage-back" to={`/authors/${encodeURIComponent(id)}`}>← 返回作者主页</Link><div className="manage-heading"><div><h2 data-page-heading tabIndex={-1}>作者资料</h2><p>{author.data?.name ?? '账号、平台与核验信息'}</p></div></div></header>
    {author.isPending ? <Loading /> : author.data ? <ProfileDetails author={author.data} /> : <ErrorNotice error={author.error} retry={() => void author.refetch()} />}
  </div>;
}

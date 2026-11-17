import { Link, useLocation } from 'react-router-dom';
import { useState } from 'react';
import { Icon } from './Icon';

export type MediaCardModel = { id: string; title: string; caption: string; type: string; src?: string; previewHint?: string; favorite: boolean; liked: boolean };
export function MediaGrid({ items, search = false }: { items: readonly MediaCardModel[]; search?: boolean }) {
  const location = useLocation();
  return <div className="media-grid">{items.map(item => <Link key={item.id} to={`/asset/${encodeURIComponent(item.id)}`} state={{ returnTo: location.pathname + location.search }} className="media-card" aria-label={`打开 ${item.title}`} onKeyDown={event => { if (event.key === ' ') { event.preventDefault(); event.currentTarget.click(); } }}>
    <div className="media-cover"><Preview item={item} />
      {item.type !== 'image' && <span className="media-type">{item.type === 'video' ? '视频' : '动图'}</span>}
      {(item.favorite || item.liked) && <span className="media-reaction"><Icon name={item.favorite ? 'star' : 'heart'} size={15} /></span>}
    </div>
    <h3 className={search ? 'one-line' : ''} title={item.title}>{item.title}</h3><p>{item.caption}</p>
  </Link>)}</div>;
}
function Preview({ item }: { item: MediaCardModel }) {
  const [failedUrl, setFailedUrl] = useState<string>();
  return item.src && failedUrl !== item.src ? <img src={item.src} alt="" loading="lazy" decoding="async" onError={() => setFailedUrl(item.src)} /> : <span className="media-placeholder"><Icon name={item.type === 'video' ? 'play' : 'image'} size={36} /><small>{failedUrl === item.src && item.src ? '预览读取失败' : item.previewHint ?? '暂无预览'}</small></span>;
}

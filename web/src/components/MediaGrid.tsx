import { Link, useLocation } from "react-router-dom";
import { useState } from "react";
import { Icon } from "./Icon";
import { useThumbnailURL } from '../data/media';

export type MediaCardModel = {
  id: string;
  title: string;
  caption: string;
  date?: string;
  duration?: string;
  type: string;
  src?: string;
  previewHint?: string;
  favorite: boolean;
  liked: boolean;
};
export function MediaGrid({
  items,
  search = false,
}: {
  items: readonly MediaCardModel[];
  search?: boolean;
}) {
  const location = useLocation();
  return (
    <div className="media-grid">
      {items.map((item) => (
        <Link
          key={item.id}
          to={`/asset/${encodeURIComponent(item.id)}`}
          state={{ returnTo: location.pathname + location.search }}
          className="card media-card"
          aria-label={`打开 ${item.title}`}
          onKeyDown={(event) => {
            if (event.key === " ") {
              event.preventDefault();
              event.currentTarget.click();
            }
          }}
        >
          <div className="card--cover">
            <Preview item={item} />
            {item.duration && <span className="card--duration">{item.duration}</span>}
          </div>
          <div className={`card--title${search ? ' one-line' : ''}`} title={item.title}>
            {item.title}
          </div>
          <div className="card--meta"><span className="card--up">{item.caption}</span>{item.date && <span className="card--date">{item.date}</span>}</div>
        </Link>
      ))}
    </div>
  );
}
export function Preview({ item }: { item: MediaCardModel }) {
  const [failedUrl, setFailedUrl] = useState<string>();
  const src = useThumbnailURL(item.src);
  return src && failedUrl !== src ? (
    <img
      src={src}
      alt=""
      loading="lazy"
      decoding="async"
      onError={() => setFailedUrl(src)}
    />
  ) : (
    <span className="media-placeholder">
      <Icon name={item.type === "video" ? "play" : "image"} size={36} />
      <small>
        {failedUrl === src && src
          ? "预览读取失败"
          : (item.previewHint ?? "暂无预览")}
      </small>
    </span>
  );
}

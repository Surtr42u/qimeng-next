import { Link, useLocation } from "react-router-dom";
import { useMemo, useState } from "react";
import { listNavigation } from "../lib/detail-navigation";
import { Icon } from "./Icon";
import { useThumbnailURL } from "../data/media";

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
  navigation,
}: {
  items: readonly MediaCardModel[];
  search?: boolean;
  navigation?: ReturnType<typeof listNavigation>;
}) {
  const location = useLocation();
  const order = useMemo(() => navigation ?? listNavigation(items), [navigation, items]);
  return (
    <div className="media-grid">
      {items.map((item) => (
        <Link
          key={item.id}
          to={`/asset/${encodeURIComponent(item.id)}`}
          state={{ returnTo: location.pathname + location.search, ids: order.ids, index: order.positions.get(item.id) }}
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
            {item.duration && (
              <span className="card--duration">{item.duration}</span>
            )}
          </div>
          <div
            className={`card--title${search ? " one-line" : ""}`}
            title={item.title}
          >
            {item.title}
          </div>
          <div className="card--meta">
            <span className="card--up">{item.caption}</span>
            {item.date && <span className="card--date">{item.date}</span>}
          </div>
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

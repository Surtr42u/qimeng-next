import { useState } from "react";

export function ImageStage({
  src,
  title,
  onOpen,
}: {
  src: string;
  title: string;
  onOpen: () => void;
}) {
  const [failed, setFailed] = useState<string>();
  const [attempt, setAttempt] = useState(0);
  return (
    <div className="asset-image-content">
      <button
        className="asset-img-open"
        aria-label="查看原图并缩放"
        title="查看大图"
        disabled={failed === src}
        onClick={onOpen}
      >
        <img
          key={`${src}:${attempt}`}
          src={src}
          alt={title}
          onError={() => setFailed(src)}
          onLoad={() => setFailed(undefined)}
        />
      </button>
      {failed === src && (
        <div role="alert" className="player-failure">
          <p>
            未能读取原件。可以重试；如果内容刚被替换，请刷新详情取得当前版本。
          </p>
          <button
            className="button button--small"
            onClick={() => {
              setFailed(undefined);
              setAttempt((value) => value + 1);
            }}
          >
            重试原图
          </button>
        </div>
      )}
    </div>
  );
}

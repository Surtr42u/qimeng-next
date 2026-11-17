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
    <div>
      <button
        className="image-stage"
        aria-label="查看原图并缩放"
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
        <span>{failed === src ? "原图读取失败" : "查看原图"}</span>
      </button>
      {failed === src && (
        <div role="alert" className="notice notice--error">
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

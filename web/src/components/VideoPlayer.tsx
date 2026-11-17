import { useEffect, useRef, useState } from "react";
import { duration } from "../lib/format";

export function VideoPlayer({
  src,
  title,
  initialPosition,
  progressMatches,
  onProgress,
  onPlay,
  onPosition,
  seek,
}: {
  src: string;
  title: string;
  initialPosition: number;
  progressMatches: boolean;
  onProgress: (positionMs: number) => void;
  onPlay: () => void;
  onPosition?: (positionMs: number) => void;
  seek?: { id: string; positionMs: number };
}) {
  const stage = useRef<HTMLDivElement>(null);
  const video = useRef<HTMLVideoElement>(null);
  const [webFullscreen, setWebFullscreen] = useState(false);
  const [error, setError] = useState("");
  const [resume, setResume] = useState(progressMatches && initialPosition > 0);
  const lastSent = useRef(0);
  const interacted = useRef(false);
  const lastPosition = useRef<number | undefined>(undefined);
  const progressCallback = useRef(onProgress);
  progressCallback.current = onProgress;
  const report = (position: number) => {
    if (!interacted.current || lastPosition.current === position) return;
    lastPosition.current = position;
    progressCallback.current(position);
  };
  useEffect(() => {
    const element = video.current;
    return () => {
      if (element && interacted.current) {
        const position = Math.round(element.currentTime * 1000);
        if (Number.isFinite(position) && position !== lastPosition.current) progressCallback.current(position);
      }
    };
  }, [src]);
  useEffect(() => {
    if (!seek || !video.current) return;
    const element = video.current;
    const apply = () => {
      element.currentTime = seek.positionMs / 1000;
      setResume(false);
    };
    if (element.readyState > 0) apply();
    else element.addEventListener("loadedmetadata", apply, { once: true });
    return () => element.removeEventListener("loadedmetadata", apply);
  }, [seek]);
  useEffect(() => {
    const handle = (event: KeyboardEvent) => {
      if (event.key === "Escape") setWebFullscreen(false);
    };
    document.addEventListener("keydown", handle);
    return () => document.removeEventListener("keydown", handle);
  }, []);
  useEffect(() => {
    if (!webFullscreen) return;
    const original = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = original;
    };
  }, [webFullscreen]);
  return (
    <div
      ref={stage}
      className={`video-stage ${webFullscreen ? "video-stage--fullscreen" : ""}`}
    >
      <button
        className="button button--small video-exit"
        onClick={() => {
          setWebFullscreen(false);
          if (document.fullscreenElement) void document.exitFullscreen().catch(() => setError('未能退出系统全屏，请按 Esc 重试。'));
        }}
      >退出全屏</button>
      <video
        ref={video}
        src={src}
        controls
        playsInline
        preload="metadata"
        aria-label={title}
        onPlay={() => { interacted.current = true; onPlay(); }}
        onSeeked={event => { interacted.current = true; report(Math.round(event.currentTarget.currentTime * 1000)); }}
        onError={() =>
          setError(
            "此浏览器无法播放该文件或原件不可读取。可打开原件使用系统播放器。",
          )
        }
        onPause={(event) =>
          report(Math.round(event.currentTarget.currentTime * 1000))
        }
        onTimeUpdate={(event) => {
          const position = Math.round(event.currentTarget.currentTime * 1000);
          onPosition?.(position);
          if (
            !event.currentTarget.paused &&
            Date.now() - lastSent.current >= 10000
          ) {
            lastSent.current = Date.now();
            report(position);
          }
        }}
      />
      {resume && (
        <div className="resume-prompt qm-glass">
          <span>上次播放到 {duration(initialPosition)}</span>
          <button
            className="button button--small"
            onClick={() => {
              if (video.current) {
                video.current.currentTime = initialPosition / 1000;
                void video.current
                  .play()
                  .catch(() =>
                    setError("浏览器未允许自动播放，请点击播放按钮。"),
                  );
              }
              setResume(false);
            }}
          >
            继续播放
          </button>
          <button
            className="button button--small"
            onClick={() => setResume(false)}
          >
            从头开始
          </button>
        </div>
      )}
      {!progressMatches && initialPosition > 0 && (
        <p className="notice">记录来自旧内容版本，未自动套用到当前视频。</p>
      )}
      {error && (
        <p role="alert" className="notice notice--error">
          {error}{" "}
          <a href={src} target="_blank" rel="noreferrer">
            打开原件
          </a>
        </p>
      )}
      <div className="video-actions">
        <label>
          速度
          <select
            defaultValue="1"
            aria-label="播放速度"
            onChange={(event) => {
              if (video.current)
                video.current.playbackRate = Number(event.target.value);
            }}
          >
            {[0.5, 0.75, 1, 1.25, 1.5, 2].map((rate) => (
              <option key={rate} value={rate}>
                {rate}×
              </option>
            ))}
          </select>
        </label>
        <button
          className="button button--small"
          onClick={() => setWebFullscreen(!webFullscreen)}
        >
          {webFullscreen ? "退出网页全屏" : "网页全屏"}
        </button>
        <button
          className="button button--small"
          onClick={() => {
            if (document.fullscreenElement) {
              void document.exitFullscreen().catch(() => setError('未能退出系统全屏，请按 Esc 重试。'));
            } else if (stage.current?.requestFullscreen) {
              void stage.current.requestFullscreen().catch(() => setError("当前浏览器没有允许全屏，请使用网页全屏。"));
            } else {
              setWebFullscreen(true);
              setError('此浏览器不支持系统全屏，已切换为网页全屏。');
            }
          }}
        >
          全屏 / 退出
        </button>
      </div>
    </div>
  );
}

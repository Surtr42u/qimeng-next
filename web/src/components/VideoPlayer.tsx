import Artplayer from "artplayer";
import { useEffect, useRef, useState } from "react";
import { attachFullscreenChrome } from "../lib/player-fullscreen";
import { attachProgressSeekFix } from "../lib/player-seek";
import { attachSelectorBehavior, playerControls, playerSettings, PLAYBACK_RATES, updateHighlights, updateVideoTrackControls } from "../lib/player-controls";
import type { PlayerHighlight } from "../lib/player-controls";
import { reportClientProblem } from "../lib/client-problems";

export function VideoPlayer({
  src, title, poster, highlights = [], initialPosition, progressMatches,
  onProgress, onPlay, onPosition, seek,
}: {
  src: string;
  title: string;
  poster?: string;
  highlights?: PlayerHighlight[];
  initialPosition: number;
  progressMatches: boolean;
  onProgress: (positionMs: number) => void;
  onPlay: () => void;
  onPosition?: (positionMs: number) => void;
  seek?: { id: string; positionMs: number };
}) {
  const container = useRef<HTMLDivElement>(null);
  const player = useRef<Artplayer | undefined>(undefined);
  const [initial] = useState({ src, title, poster, initialPosition, progressMatches });
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const callbacks = useRef({ onProgress, onPlay, onPosition });
  const marks = useRef(highlights);
  useEffect(() => { callbacks.current = { onProgress, onPlay, onPosition }; });
  useEffect(() => {
    marks.current = highlights;
    if (player.current) updateHighlights(player.current, highlights);
  }, [highlights]);
  useEffect(() => {
    if (!container.current) return;
    const primary = () => getComputedStyle(document.documentElement).getPropertyValue("--qm-primary").trim();
    Artplayer.PLAYBACK_RATE = [...PLAYBACK_RATES];
    const art = new Artplayer({
      container: container.current, url: initial.src, poster: initial.poster,
      theme: primary(), lang: "zh-cn", volume: 0.7, muted: true,
      setting: true, settings: playerSettings(), controls: playerControls(),
      fullscreen: true, fullscreenWeb: true,
      // Keep labels out of ArtPlayer's HTML interpolation; safe DOM binding follows metadata.
      highlight: [],
      moreVideoAttr: { playsInline: true, preload: "metadata" },
    });
    player.current = art;
    art.video.setAttribute("aria-label", initial.title);
    let interacted = false;
    let disposed = false;
    let lastSentAt = 0;
    let lastPosition: number | undefined;
    let resumed = false;
    const report = () => {
      if (!interacted || disposed) return;
      const position = Math.round(art.currentTime * 1000);
      if (Number.isFinite(position) && position !== lastPosition) {
        lastPosition = position;
        callbacks.current.onProgress(position);
      }
    };
    const renderMarks = () => queueMicrotask(() => {
      // ArtPlayer's progress control installs its own metadata listener during
      // mounting and clears the container. Publish after all its listeners run.
      if (!disposed && player.current === art) updateHighlights(art, marks.current);
    });
    const metadata = () => {
      updateVideoTrackControls(art);
      renderMarks();
      if (!resumed && initial.progressMatches && initial.initialPosition > 0) {
        resumed = true;
        // A saved position belongs only to this exact content version.
        const seconds = initial.initialPosition / 1000;
        if (Number.isFinite(art.duration) && seconds < art.duration) art.seek = seconds;
      }
    };
    art.on("video:loadedmetadata", metadata);
    art.on("video:durationchange", renderMarks);
    art.on("ready", renderMarks);
    const tracksChanged = () => updateVideoTrackControls(art);
    art.video.textTracks.addEventListener("addtrack", tracksChanged);
    art.video.textTracks.addEventListener("removetrack", tracksChanged);
    art.on("video:play", () => { interacted = true; callbacks.current.onPlay(); });
    art.on("video:timeupdate", () => {
      callbacks.current.onPosition?.(Math.round(art.currentTime * 1000));
      if (!art.video.paused && Date.now() - lastSentAt >= 10000) { lastSentAt = Date.now(); report(); }
    });
    art.on("video:pause", report);
    art.on("video:ended", report);
    art.on("video:seeked", report);
    art.on("video:ratechange", () => {
      const value = art.template.$player.querySelector(".art-control-playbackRate .art-selector-value");
      if (value) value.textContent = art.playbackRate === 1 ? "倍速" : `${art.playbackRate}x`;
    });
    const failure = () => {
      if (!disposed) { setError("此浏览器无法播放该文件或原件不可读取。"); reportClientProblem("playback_failed"); }
    };
    art.on("video:error", failure);
    art.on("error", failure);
    art.on("video:canplay", () => { if (!disposed) setError(""); });
    art.on("fullscreenError", () => { if (!disposed) setError("当前浏览器没有允许系统全屏，请使用网页全屏。"); });
    const hidden = () => { if (document.visibilityState !== "visible") report(); };
    document.addEventListener("visibilitychange", hidden);
    window.addEventListener("pagehide", report);
    const theme = new MutationObserver(() => { art.theme = primary(); });
    theme.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
    const detachSeek = attachProgressSeekFix(container.current, art);
    const detachSelectors = attachSelectorBehavior(art);
    const detachFullscreen = attachFullscreenChrome(art);
    return () => {
      report();
      disposed = true;
      document.removeEventListener("visibilitychange", hidden);
      window.removeEventListener("pagehide", report);
      art.video.textTracks.removeEventListener("addtrack", tracksChanged);
      art.video.textTracks.removeEventListener("removetrack", tracksChanged);
      theme.disconnect();
      detachSeek();
      detachSelectors();
      detachFullscreen();
      if (player.current === art) player.current = undefined;
      art.destroy();
    };
  }, [initial, attempt]);

  useEffect(() => { player.current?.video.setAttribute("aria-label", title); }, [title]);

  useEffect(() => {
    const art = player.current;
    if (!seek || !art) return;
    const apply = () => { art.seek = seek.positionMs / 1000; };
    if (art.video.readyState > 0) apply();
    else art.video.addEventListener("loadedmetadata", apply, { once: true });
    return () => art.video.removeEventListener("loadedmetadata", apply);
  }, [seek, attempt]);

  return (
    <div className="video-player-box">
      <div className="video-player-mount" ref={container} />
      {error && (
        <div className="player-failure" role="alert">
          <p>{error}</p>
          <button className="pill" onClick={() => { setError(""); setAttempt(value => value + 1); }}>重试播放</button>
          <a className="pill" href={src} target="_blank" rel="noreferrer">打开原件</a>
        </div>
      )}
      {!progressMatches && initialPosition > 0 && <span className="sr-only" role="status">记录来自旧内容版本，未自动套用到当前视频。</span>}
    </div>
  );
}

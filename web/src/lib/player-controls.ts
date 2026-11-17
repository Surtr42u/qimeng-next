import type Artplayer from "artplayer";

// The existing ArtPlayer controls, labels and selectors are presentation only.
export const PLAYBACK_RATES = [0.5, 0.75, 1, 1.25, 1.5, 2] as const;
export function qualityLabel(height: number) {
  if (!Number.isFinite(height) || height <= 0) return "原画";
  const h = Math.round(height);
  if (h >= 2160) return "4K 超清";
  if (h >= 1440) return `${h}P 超清`;
  if (h >= 1080) return "1080P 高清";
  if (h >= 720) return "720P 高清";
  if (h >= 480) return "480P 标清";
  return `${h}P`;
}

function closeSelector(art: Artplayer, name: string) {
  const element = art.template.$player.querySelector(`.art-control-${name}`);
  element?.classList.remove("art-selector-show");
  element?.classList.add("art-selector-closed");
}

export function playerControls(): NonNullable<Artplayer["option"]["controls"]> {
  return [
    {
      name: "quality", position: "right", index: 10, html: "原画",
      selector: [{ default: true, html: "原画", value: "orig" }],
      onSelect: function (item) { closeSelector(this, "quality"); return item.html; },
    },
    {
      name: "playbackRate", position: "right", index: 12, html: "倍速",
      selector: PLAYBACK_RATES.map(rate => ({ value: rate, default: rate === 1, html: rate === 1 ? "1.0x 正常" : `${rate}x` })),
      onSelect: function (item) {
        const rate = Number(item.value);
        this.playbackRate = rate;
        closeSelector(this, "playbackRate");
        return rate === 1 ? "倍速" : `${rate}x`;
      },
    },
  ];
}

export function playerSettings(): NonNullable<Artplayer["option"]["settings"]> {
  return [
    {
      html: "画面比例", icon: "",
      selector: [
        { default: true, html: "默认", value: "default" },
        { html: "16:9", value: "16:9" },
        { html: "4:3", value: "4:3" },
        { html: "拉伸铺满", value: "fill" },
      ],
      onSelect: function (item) {
        // Use the player's public aspect ratio setter; objectFit alone never changed 16:9/4:3.
        this.aspectRatio = item.value === "16:9" || item.value === "4:3" ? item.value : "default";
        this.video.style.objectFit = item.value === "fill" ? "fill" : "contain";
        this.setting.show = false;
        return item.html;
      },
    },
    {
      html: "单片循环", switch: false,
      onSwitch: function (item) { this.video.loop = !item.switch; return !item.switch; },
    },
  ];
}

export function updateVideoTrackControls(art: Artplayer) {
  const video = art.video;
  const label = qualityLabel(video.videoHeight);
  const detail = document.createElement("span");
  detail.append(document.createTextNode(`原画 · ${label}`), document.createElement("br"));
  const dimensions = document.createElement("span");
  dimensions.style.cssText = "font-size:11px;opacity:0.72";
  dimensions.textContent = `${video.videoWidth}×${video.videoHeight}`;
  detail.append(dimensions);
  art.controls.update({
    name: "quality", position: "right", index: 10, html: label,
    selector: [{ default: true, html: detail, value: "orig" }],
    onSelect: function () { closeSelector(this, "quality"); return label; },
  });
  const tracks = Array.from(video.textTracks);
  art.controls.remove("subtitle");
  if (!tracks.length) return;
  art.controls.add({
    name: "subtitle", position: "right", index: 14, html: "字幕",
    selector: [
      { default: !tracks.some(track => track.mode === "showing"), html: "关闭", value: 0 },
      ...tracks.map((track, index) => {
        // Track labels are external media text, never HTML strings.
        const text = document.createElement("span");
        text.textContent = track.label || `字幕 ${index + 1}`;
        return { default: track.mode === "showing", html: text, value: index + 1 };
      }),
    ],
    onSelect: function (item) {
      tracks.forEach((track, index) => { track.mode = Number(item.value) === index + 1 ? "showing" : "hidden"; });
      closeSelector(this, "subtitle");
      return item.html;
    },
  });
}

export type PlayerHighlight = { time: number; text: string };
export function updateHighlights(art: Artplayer, highlights: readonly PlayerHighlight[]) {
  const host = art.template.$player.querySelector(".art-progress-highlight");
  if (!host) return;
  const elements = Number.isFinite(art.duration) && art.duration > 0 ? highlights.map(mark => {
    const element = document.createElement("span");
    // ArtPlayer 5.4 interpolates option.highlight text into HTML. Retain its DOM contract
    // while constructing attributes safely; edited marks update without remounting playback.
    element.dataset.text = mark.text;
    element.dataset.time = String(mark.time);
    element.style.left = `${Math.min(art.duration, Math.max(0, mark.time)) / art.duration * 100}%`;
    return element;
  }) : [];
  host.replaceChildren(...elements);
}

export function attachSelectorBehavior(art: Artplayer) {
  const root = art.template.$player;
  const controls = () => root.querySelectorAll<HTMLElement>(".art-control-selector");
  const close = (element: HTMLElement) => {
    element.classList.remove("art-selector-show");
    element.classList.add("art-selector-closed");
  };
  const click = (event: MouseEvent) => {
    const target = event.target instanceof Element ? event.target : undefined;
    const value = target?.closest(".art-selector-value");
    const selector = value?.closest<HTMLElement>(".art-control-selector");
    if (selector) {
      const open = !selector.classList.contains("art-selector-closed") && (selector.classList.contains("art-selector-show") || selector.matches(":hover"));
      controls().forEach(close);
      art.setting.show = false;
      if (!open) { selector.classList.remove("art-selector-closed"); selector.classList.add("art-selector-show"); }
    } else if (target?.closest(".art-selector-item")) {
      controls().forEach(close);
    } else if (!target?.closest(".art-selector-list")) {
      controls().forEach(close);
      if (!target?.closest(".art-settings, .art-control-setting")) art.setting.show = false;
    }
  };
  const move = (event: MouseEvent) => {
    const from = event.relatedTarget instanceof Element ? event.relatedTarget.closest<HTMLElement>(".art-control-selector") : null;
    const to = event.target instanceof Element ? event.target.closest<HTMLElement>(".art-control-selector") : null;
    if (from !== to) {
      from?.classList.remove("art-selector-show", "art-selector-closed");
      to?.classList.remove("art-selector-closed");
    }
  };
  const leave = (event: MouseEvent) => {
    const from = event.target instanceof Element ? event.target.closest<HTMLElement>(".art-control-selector") : null;
    const to = event.relatedTarget instanceof Element ? event.relatedTarget.closest<HTMLElement>(".art-control-selector") : null;
    if (from !== to) from?.classList.remove("art-selector-show", "art-selector-closed");
  };
  root.addEventListener("click", click);
  root.addEventListener("mouseover", move);
  root.addEventListener("mouseout", leave);
  const outside = (event: MouseEvent) => { if (event.target instanceof Node && !root.contains(event.target)) { controls().forEach(close); art.setting.show = false; } };
  document.addEventListener("click", outside);
  return () => {
    root.removeEventListener("click", click);
    root.removeEventListener("mouseover", move);
    root.removeEventListener("mouseout", leave);
    document.removeEventListener("click", outside);
  };
}

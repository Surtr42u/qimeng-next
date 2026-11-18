(() => {
  "use strict";
  Object.defineProperty(window, "__QIMENG_DESKTOP__", { value: true });
  const send = action => window.__TAURI__.core.invoke("window_action", { action }).catch(() => {
    window.dispatchEvent(new Event("qimeng-window-action-error"));
  });
  const titlebar = "[data-qimeng-titlebar]";
  const interactive = "a,button,input,select,textarea,label,[role=button],[role=tab],[contenteditable=true]";
  let lastPress;
  document.addEventListener("click", event => {
    if (!event.isTrusted || !(event.target instanceof Element)) return;
    const link = event.target.closest('a[target="_blank"][href]:not([download])');
    if (link && event.button === 0 && !event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey) {
      const address = new URL(link.href);
      if (address.origin === location.origin && ["http:", "https:"].includes(address.protocol)) {
        event.preventDefault();
        window.__TAURI__.core.invoke("open_original", { address: address.href }).catch(() => {
          window.dispatchEvent(new Event("qimeng-original-open-error"));
        });
        return;
      }
    }
    const button = event.target.closest(titlebar + " button[data-window-action]");
    if (button && ["minimize", "toggleMaximize", "close"].includes(button.dataset.windowAction)) {
      event.preventDefault(); send(button.dataset.windowAction);
    }
  }, true);
  document.addEventListener("mousedown", event => {
    if (!event.isTrusted || event.button !== 0 || document.fullscreenElement || !(event.target instanceof Element)
        || !event.target.closest(titlebar) || event.target.closest(interactive)) { lastPress = undefined; return; }
    const now = performance.now();
    const twice = lastPress && event.detail === 2 && now - lastPress.time < 550
      && Math.abs(lastPress.x - event.screenX) <= 5 && Math.abs(lastPress.y - event.screenY) <= 5;
    lastPress = twice ? undefined : { x: event.screenX, y: event.screenY, time: now };
    event.preventDefault(); send(twice ? "toggleMaximize" : "drag");
  }, true);
})();

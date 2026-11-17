import { useSyncExternalStore } from "react";

export type ThumbnailPreference = "auto" | "sm" | "lg";
const key = "qimeng.thumbnail-size";
const listeners = new Set<() => void>();
let memory: ThumbnailPreference | undefined;
function read(): ThumbnailPreference {
  if (memory) return memory;
  try {
    const value = localStorage.getItem(key);
    return value === "sm" || value === "lg" ? value : "auto";
  } catch {
    return "auto";
  }
}
function external(event: StorageEvent) {
  if (event.key !== key) return;
  memory = undefined;
  for (const listener of listeners) listener();
}
function subscribe(listener: () => void) {
  listeners.add(listener);
  if (listeners.size === 1) window.addEventListener("storage", external);
  return () => {
    listeners.delete(listener);
    if (!listeners.size) window.removeEventListener("storage", external);
  };
}
export function useThumbnailPreference() {
  const preference = useSyncExternalStore(subscribe, read);
  return [
    preference,
    (value: ThumbnailPreference) => {
      memory = value;
      let stored = true;
      try {
        localStorage.setItem(key, value);
      } catch {
        stored = false;
      }
      for (const listener of listeners) listener();
      return stored;
    },
  ] as const;
}

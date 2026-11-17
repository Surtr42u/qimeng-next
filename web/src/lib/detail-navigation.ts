export type DetailNavigation = {
  returnTo: string;
  ids: string[];
  index: number;
};

// A history entry carries the visible list order, not a reconstructed server query.
export function readDetailNavigation(value: unknown, assetId: string): DetailNavigation | undefined {
  if (!value || typeof value !== "object") return;
  const state = value as Partial<DetailNavigation>;
  if (!Array.isArray(state.ids) || !state.ids.every(id => typeof id === "string")) return;
  if (typeof state.index !== "number" || !Number.isInteger(state.index) || state.ids[state.index] !== assetId) return;
  if (typeof state.returnTo !== "string" || !state.returnTo.startsWith("/") || state.returnTo.startsWith("//")) return;
  return state as DetailNavigation;
}

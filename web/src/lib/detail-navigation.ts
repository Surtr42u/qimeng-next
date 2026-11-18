export type DetailNavigation = {
  returnTo: string;
  ids: string[];
  index: number;
};
export function listNavigation(items: readonly { id: string }[]) {
  const ids = items.map(item => item.id);
  const positions = new Map<string, number>();
  ids.forEach((id, index) => { if (!positions.has(id)) positions.set(id, index); });
  return { ids, positions };
}

export const isAssetDetail = (path: string) => /^\/asset\/[^/]+$/.test(path);
export function backdropLocation(value: unknown) {
  const address = value && typeof value === "object" && "returnTo" in value ? value.returnTo : undefined;
  if (typeof address === "string" && /^(?:\/|\/album)(?:\?[^#]*)?$/.test(address)) {
    const boundary = address.indexOf("?");
    return { pathname: boundary === -1 ? address : address.slice(0, boundary), search: boundary === -1 ? "" : address.slice(boundary) };
  }
  return { pathname: "/", search: "" };
}

// A history entry carries the visible list order, not a reconstructed server query.
export function readDetailNavigation(value: unknown, assetId: string): DetailNavigation | undefined {
  if (!value || typeof value !== "object") return;
  const state = value as Partial<DetailNavigation>;
  if (!Array.isArray(state.ids) || !state.ids.every(id => typeof id === "string")) return;
  if (typeof state.index !== "number" || !Number.isInteger(state.index) || state.ids[state.index] !== assetId) return;
  if (typeof state.returnTo !== "string" || !state.returnTo.startsWith("/") || state.returnTo.startsWith("//") || state.returnTo.includes("\\")) return;
  return state as DetailNavigation;
}

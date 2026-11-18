import type { components } from "../api/generated/schema";

type Report = components["schemas"]["ClientErrorReport"];
type Listener = (code: Report["code"], httpStatus?: number) => void;
const listeners = new Set<Listener>();
export function reportClientProblem(code: Report["code"], httpStatus?: number) {
  for (const listener of listeners) listener(code, httpStatus);
}
export function onClientProblem(listener: Listener) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}
export function diagnosticScreen(path: string): Report["screen"] {
  const [, area, task] = path.split("/", 3);
  if (!area) return "home";
  if (area === "asset") return "detail";
  if (area === "authors") return "author";
  if (area === "manage" && task === "upload") return "upload";
  if (area === "manage" && task === "backup") return "backup";
  if (area === "album" || area === "search" || area === "settings" || area === "maintenance") return area;
  return "other";
}

import { useLayoutEffect } from "react";
import { api, read } from "./client";
import { diagnosticScreen, onClientProblem, reportClientProblem } from "../lib/client-problems";

export function ClientDiagnostics() {
  useLayoutEffect(() => {
    const controller = new AbortController();
    const seen = new Map<string, number>();
    let lostConnection = false;
    const unsubscribe = onClientProblem((code, status) => {
      if (!navigator.onLine) { lostConnection = true; return; }
      const httpStatus = status !== undefined && Number.isInteger(status) && status >= 400 && status <= 599 ? status : undefined;
      const screen = diagnosticScreen(window.location.pathname);
      const now = Date.now();
      const fingerprint = [code, screen, httpStatus].join("/");
      for (const [key, sent] of seen) if (now - sent >= 60000) seen.delete(key);
      if (seen.has(fingerprint) || seen.size >= 64) return;
      seen.set(fingerprint, now);
      const level = code === "storage_unavailable" || code === "connection_lost" ? "warning" : "error";
      // Only fixed categories and a status number cross this boundary. A failed report is never reported recursively.
      void read(api.POST("/system/client-errors", { body: { level, code, screen, httpStatus }, signal: controller.signal })).catch(() => {});
    });
    const offline = () => { lostConnection = true; };
    const online = () => { if (lostConnection) { lostConnection = false; reportClientProblem("connection_lost"); } };
    const crashed = (event: ErrorEvent) => { if (event.error) reportClientProblem("render_failed"); };
    window.addEventListener("error", crashed);
    window.addEventListener("offline", offline);
    window.addEventListener("online", online);
    return () => {
      unsubscribe(); controller.abort();
      window.removeEventListener("error", crashed);
      window.removeEventListener("offline", offline);
      window.removeEventListener("online", online);
    };
  }, []);
  return null;
}

import { startTransition, use, useEffect, useState } from "react";
import type { ComponentType } from "react";
import { ErrorNotice } from "../components/Feedback";

type PageResult = { Page: ComponentType } | { error: Error };

// The authenticated outlet starts the import. The shared Suspense boundary keeps
// an existing page and its focused control mounted while navigation is pending.
export function onDemandPage(importPage: () => Promise<ComponentType>) {
  let request: Promise<PageResult> | undefined;
  const load = () => request ??= importPage().then(
    Page => ({ Page }),
    cause => ({ error: new Error("页面文件未能载入。恢复连接后可重新打开此页。", { cause }) }),
  );

  return function OnDemandPage() {
    const [attempt, setAttempt] = useState(load);
    const result = use(attempt);
    useEffect(() => {
      if (!("error" in result)) return;
      // A failed module must be retried on re-entry or when this page reconnects.
      if (request === attempt) request = undefined;
      const retry = () => startTransition(() => setAttempt(load()));
      window.addEventListener("online", retry);
      return () => window.removeEventListener("online", retry);
    }, [attempt, result]);
    if ("error" in result) {
      // A fresh document also recovers browsers that retain a failed module graph.
      return <ErrorNotice error={result.error} retry={() => window.location.reload()} />;
    }
    return <result.Page />;
  };
}

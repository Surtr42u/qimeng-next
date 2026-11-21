import { useEffect } from "react";
import { queryClient } from "./query";
import {
  EVENTS_PATH,
  refreshTargets,
  subscribeServerEvents,
} from "./server-events";

/**
 * Holds one stream for the authenticated session and turns frames into cache
 * invalidations. Rendered once inside the session provider; the connection
 * lives exactly as long as that session key.
 */
export function ServerEventBridge({ instance, session }: { instance: string; session: string }) {
  useEffect(() => {
    const root = ["remote", instance, session] as const;
    const refresh = () => {
      void queryClient.invalidateQueries({ queryKey: root });
    };
    return subscribeServerEvents({
      url: new URL(EVENTS_PATH, window.location.origin).toString(),
      // A reconnect may have missed frames; one authoritative reload covers
      // the whole gap, and an extra reload is always safe.
      onOpen: refresh,
      onMessage: message => {
        if (refreshTargets(message.event).length > 0) refresh();
      },
      onAuthFailure: () => {
        window.dispatchEvent(new Event("qimeng-session-expired"));
      },
    });
  }, [instance, session]);

  return null;
}

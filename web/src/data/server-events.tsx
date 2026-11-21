import { useEffect } from "react";
import { queryClient } from "./query";
import {
  EVENTS_PATH,
  refreshTargets,
  subscribeServerEvents,
} from "./server-events-core";

/**
 * Holds one stream for the authenticated session and turns frames into cache
 * invalidations. Rendered once inside the session provider; the connection
 * lives exactly as long as that session key.
 */
export function ServerEventBridge({ instance, session }: { instance: string; session: string }) {
  useEffect(() => {
    const root = ["remote", instance, session] as const;
    // 只标脏、不强制重取。旧的写法是 `invalidateQueries({ queryKey: root })`，它会
    // 立刻重取所有挂着的查询：正在播放的视频会被重挂（e2e「无法回退播放」、加载中
    // 元素消失即此），列表也会被整体替换掉滚动位置。标脏后由既有的 30s staleTime、
    // 窗口重新聚焦（refetchOnWindowFocus）或下次进入页面各取一次——这与旧版把 SSE
    // 当「数据收敛闸门」而不是「强制刷新」的口径一致。
    const markStale = () => {
      void queryClient.invalidateQueries({ queryKey: root, refetchType: "none" });
    };
    return subscribeServerEvents({
      url: new URL(EVENTS_PATH, window.location.origin).toString(),
      // 断线重连可能漏帧：把整棵缓存标脏，下一次读取自然是权威值。
      onOpen: markStale,
      onMessage: message => {
        if (refreshTargets(message.event).length > 0) markStale();
      },
      onAuthFailure: () => {
        window.dispatchEvent(new Event("qimeng-session-expired"));
      },
    });
  }, [instance, session]);

  return null;
}

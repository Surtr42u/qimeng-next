import { useMutation, useQuery } from "@tanstack/react-query";
import { api, operationHeaders, read } from "./client";
import { useRemoteKey } from "./session";
import { queryClient } from "./query";

export function useLocalSyncStatus(expanded: boolean) {
  const scope = useRemoteKey();
  return useQuery({ queryKey: [...scope, "local-sync"], gcTime: 0,
    queryFn: ({ signal }) => read(api.GET("/local-sync/status", { signal })),
    refetchInterval: expanded ? 5000 : false,
  });
}
export function useTriggerLocalSync() {
  const scope = useRemoteKey();
  return useMutation({ gcTime: 0,
    mutationFn: (key: string) => read(api.POST("/local-sync/trigger", { params: { header: operationHeaders(key) } })),
    onSuccess: data => { queryClient.setQueryData([...scope, "local-sync"], data); },
  });
}

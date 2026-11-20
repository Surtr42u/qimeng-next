import { useInfiniteQuery, useMutation, useQuery } from "@tanstack/react-query";
import { api, ApiError, read } from "./client";
import type { Schema } from "./client";
import { useRemoteKey } from "./session";
import { queryClient } from "./query";

export function useResearchProviderContext(enabled: boolean) {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, "research-provider-context"],
    queryFn: ({ signal }) => read(api.GET("/research/context", { signal })),
    enabled,
  });
}

export function useAIDelegations(
  state: Schema<"AIDelegation">["state"] | "all" = "active",
) {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, "ai-delegations", state],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      read(
        api.GET("/ai/delegations", {
          params: { query: { state, cursor: pageParam, limit: 30 } },
          signal,
        }),
      ),
    getNextPageParam: (page) => page.nextCursor || undefined,
  });
}

// Issuance is not replayable: a failed transport or missing 201 body can hide a committed grant.
export function isAICreationUnconfirmed(error: unknown) {
  return !(error instanceof ApiError) || error.status < 400 || error.status === 408 || error.status >= 500;
}

export function useCreateAIDelegation(
  receive: (delegation: Schema<"AIDelegation">, token: string) => void,
  requireReconciliation: () => void,
) {
  const key = useRemoteKey();
  return useMutation({
    gcTime: 0,
    // Try the selected endpoint immediately, including local services, but never queue issuance until reconnection.
    networkMode: "always",
    retry: false,
    mutationFn: async (body: Schema<"AIDelegationPolicy">) => {
      const result = await read(api.POST("/ai/delegations", { body }));
      const secret = result.credential.token;
      if (!secret)
        throw new Error(
          "授权可能已建立，但没有收到执行器凭据。请刷新授权列表，撤销未配置的接入后重新创建。",
        );
      // Secrets never enter the query/mutation result cache, browser storage or a URL.
      receive(result.delegation, secret);
      return result.delegation;
    },
    onError: error => {
      if (isAICreationUnconfirmed(error)) requireReconciliation();
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: [...key, "ai-delegations"] }),
  });
}

export function useRevokeAIDelegation() {
  const key = useRemoteKey();
  return useMutation({
    // An explicit revocation is also scoped to this moment, not background reconnect.
    networkMode: "always",
    retry: false,
    mutationFn: (id: string) =>
      read(api.DELETE("/ai/delegations/{id}", { params: { path: { id } } })),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: [...key, "ai-delegations"] }),
  });
}

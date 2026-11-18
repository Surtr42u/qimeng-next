import { useMutation, useQuery } from "@tanstack/react-query";
import { api, operationHeaders, read, type Schema } from "./client";
import { queryClient } from "./query";
import { useRemoteKey } from "./session";

export function useSourceRules() {
  const scope = useRemoteKey();
  const key = [...scope, "source-rules"];
  const query = useQuery({ queryKey: key, queryFn: ({ signal }) => read(api.GET("/sources/custom-groups", { signal })),
    refetchInterval: state => ["pending", "running"].includes(state.state.data?.rebuildState ?? "") ? 2000 : false });
  const save = useMutation({ gcTime: 0,
    mutationFn: ({ body, id }: { body: Schema<"SourceRuleUpdate">; id: string }) => read(api.PUT("/sources/custom-groups", { params: { header: operationHeaders(id) }, body })),
    onSuccess: value => { queryClient.setQueryData(key, value); void queryClient.invalidateQueries({ queryKey: key }); } });
  return { query, save };
}

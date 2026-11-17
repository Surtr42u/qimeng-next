import { useMutation, useQuery } from "@tanstack/react-query";
import { api, operationHeaders, read } from "./client";
import { useRemoteKey } from "./session";
import { queryClient } from "./query";

export function useAcquisitionVocabulary() {
  const remote = useRemoteKey();
  return useQuery({
    queryKey: [...remote, "acquisition-vocabulary"],
    queryFn: ({ signal }) =>
      read(api.GET("/acquisition-vocabulary", { signal })),
  });
}
export function useUpdateAcquisitionVocabulary() {
  const remote = useRemoteKey();
  return useMutation({
    mutationFn: ({
      expectedRevision,
      sources,
      key,
    }: {
      expectedRevision: number;
      sources: string[];
      key: string;
    }) =>
      read(
        api.PUT("/acquisition-vocabulary", {
          body: { expectedRevision, sources },
          params: { header: operationHeaders(key) },
        }),
      ),
    onSuccess: (data) => {
      queryClient.setQueryData([...remote, "acquisition-vocabulary"], data);
    },
  });
}

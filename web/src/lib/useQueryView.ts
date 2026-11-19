import { useQuery } from "@tanstack/react-query";

// Each view owns the queries for its records. A cached record therefore always
// has its renderer in this document, including when the connection drops.
export function useQueryView<T>(name: string, load: () => Promise<T>) {
  return useQuery({
    queryKey: ["view", name],
    queryFn: async () => {
      try {
        return await load();
      } catch (cause) {
        throw new Error("页面文件未能载入。可在恢复连接后重试。", { cause });
      }
    },
    staleTime: Infinity,
    gcTime: Infinity,
    retry: false,
  });
}

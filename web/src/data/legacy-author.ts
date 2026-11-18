import { useQuery } from "@tanstack/react-query";
import { api, read } from "./client";
import { useRemoteKey } from "./session";

export function useExactAuthorBookmark(name: string) {
  const scope = useRemoteKey();
  return useQuery({ queryKey: [...scope, "author-name-bookmark", name], enabled: !!name,
    queryFn: async ({ signal }) => {
      const names = [...new Set([name, name.endsWith(" ·COS") ? name.slice(0, -5) : name])];
      const pages = await Promise.all(names.map(exactName => read(api.GET("/authors", { params: { query: { exactName, limit: 2 } }, signal }))));
      return [...new Set(pages.flatMap(page => page.items.map(author => author.id)))];
    } });
}

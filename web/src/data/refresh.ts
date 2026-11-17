import { useRemoteKey } from './session';
import { queryClient } from './query';

export function useRefreshLibrary() {
  const key = useRemoteKey();
  return (rankingsOnly = false) => rankingsOnly
    ? queryClient.resetQueries({ queryKey: [...key, 'rankings'] })
    : queryClient.invalidateQueries({ queryKey: key });
}

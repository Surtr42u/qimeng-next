import { useMutation } from '@tanstack/react-query';
import { api, operationHeaders, read } from './client';
import type { Schema } from './client';
import { useRemoteKey } from './session';
import { queryClient } from './query';

export function useSaveSettings() {
  const remote = useRemoteKey();
  return useMutation({
    mutationFn: ({ body, key }: { body: Schema<'SettingsUpdate'>; key: string }) => read(api.PUT('/settings', { body, params: { header: operationHeaders(key) } })),
    onSuccess: data => { queryClient.setQueryData([...remote, 'settings'], data); },
  });
}

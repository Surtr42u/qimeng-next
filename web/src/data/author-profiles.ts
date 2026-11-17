import { useInfiniteQuery, useMutation, useQuery } from '@tanstack/react-query';
import { useRef } from 'react';
import { api, operationHeaders, read } from './client';
import type { Schema } from './client';
import { useRemoteKey } from './session';
import { queryClient } from './query';
import { newId } from '../lib/id';

export type ProfileBlockName = Schema<'AuthorProfileBlock'>['block'];

function useProfileInvalidation(id: string) {
  const remote = useRemoteKey();
  return () => Promise.all([
    queryClient.invalidateQueries({ queryKey: [...remote, 'author-profile', id] }),
    queryClient.invalidateQueries({ queryKey: [...remote, 'author-profile-summary', id] }),
    queryClient.invalidateQueries({ queryKey: [...remote, 'author-profile-candidates', id] }),
  ]);
}

function useAttemptKey() {
  const attempt = useRef({ signature: '', key: newId() });
  return (input: unknown) => {
    const signature = JSON.stringify(input);
    if (signature !== attempt.current.signature) attempt.current = { signature, key: newId() };
    return attempt.current.key;
  };
}

export function useAuthorProfile(id: string, enabled = true) {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, 'author-profile', id],
    enabled: !!id && enabled,
    queryFn: ({ signal }) => read(api.GET('/authors/{id}/profile', { params: { path: { id } }, signal })),
  });
}

export function useAuthorProfileSummary(id: string, enabled = true) {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, 'author-profile-summary', id],
    enabled: !!id && enabled,
    queryFn: ({ signal }) => read(api.GET('/authors/{id}/profile-summary', { params: { path: { id } }, signal })),
  });
}

export function useUpdateAuthorProfile(id: string, block: ProfileBlockName) {
  const attempt = useAttemptKey();
  const invalidate = useProfileInvalidation(id);
  return useMutation({
    mutationFn: (body: Schema<'AuthorProfileWrite'>) => read(api.PATCH('/authors/{id}/profile/{block}', {
      params: { path: { id, block }, header: operationHeaders(attempt([id, block, body])) }, body,
    })),
    onSuccess: invalidate,
  });
}

export function useAuthorProfileCandidates(id: string, archiveId?: string, enabled = true) {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, 'author-profile-candidates', id, archiveId],
    enabled,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => read(api.GET('/authors/{id}/profile/candidates', {
      params: { path: { id }, query: { archiveId, cursor: pageParam, limit: 20 } }, signal,
    })),
    getNextPageParam: last => last.nextCursor || undefined,
  });
}

export function useDismissProfileCandidate(id: string) {
  const attempt = useAttemptKey();
  const invalidate = useProfileInvalidation(id);
  return useMutation({
    mutationFn: (candidateId: string) => read(api.POST('/authors/{id}/profile/candidates/{candidateId}/dismiss', {
      params: { path: { id, candidateId }, header: operationHeaders(attempt([id, candidateId])) },
    })),
    onSuccess: invalidate,
  });
}

export function useProfileAvatarMutation(id: string) {
  const attempt = useAttemptKey();
  const invalidate = useProfileInvalidation(id);
  return useMutation({
    mutationFn: (input:
      | { kind: 'https'; body: Schema<'AuthorAvatarFetch'> }
      | { kind: 'upload'; file: File; revision: number; key: string }
      | { kind: 'clear'; revision: number }
    ) => {
      if (input.kind === 'https') return read(api.POST('/authors/{id}/profile/avatar-from-url', {
        params: { path: { id }, header: operationHeaders(attempt([id, input])) }, body: input.body,
      }));
      if (input.kind === 'upload') return read(api.PUT('/authors/{id}/profile/avatar', {
        params: { path: { id }, query: { expectedRevision: input.revision }, header: operationHeaders(input.key) },
        body: '' as string,
        bodySerializer: () => input.file,
        headers: { 'Content-Type': 'application/octet-stream' },
      }));
      return read(api.DELETE('/authors/{id}/profile/avatar', {
        params: { path: { id }, query: { expectedRevision: input.revision }, header: operationHeaders(attempt([id, input])) },
      }));
    },
    onSuccess: invalidate,
  });
}

export function useAuthorProfileArchives(id: string) {
  const key = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...key, 'author-profile-archives', id],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => read(api.GET('/authors/{id}/profile/archives', {
      params: { path: { id }, query: { cursor: pageParam, limit: 20 } }, signal,
    })),
    getNextPageParam: last => last.nextCursor || undefined,
  });
}

export function useAuthorProfileArchive(id: string, archiveId: string) {
  const key = useRemoteKey();
  return useQuery({
    queryKey: [...key, 'author-profile-archive', id, archiveId],
    enabled: !!archiveId,
    queryFn: ({ signal }) => read(api.GET('/authors/{id}/profile/archives/{archiveId}', {
      params: { path: { id, archiveId } }, signal,
    })),
  });
}

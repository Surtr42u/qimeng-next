import { useQuery } from '@tanstack/react-query';
import { api, read } from './client';
import type { Schema } from './client';
import { useRemoteKey } from './session';

type Context = { libraryId: string; directory: string; family: string; mediaType: Schema<'Asset'>['mediaType']; collectionId?: string };
export function useNamingContext(context: Context, enabled: boolean) {
  const key = useRemoteKey();
  const { libraryId, ...query } = context;
  return useQuery({ queryKey: [...key, 'naming-context', context], enabled: enabled && !!libraryId && (!!context.family.trim() || !!context.collectionId), queryFn: ({ signal }) => read(api.GET('/libraries/{id}/naming/context', { params: { path: { id: libraryId }, query }, signal })) });
}
export function useNameSuggestions(libraryId: string, directory: string, q: string, mediaType: Schema<'Asset'>['mediaType'], enabled: boolean) {
  const key = useRemoteKey();
  return useQuery({ queryKey: [...key, 'name-suggestions', libraryId, directory, q, mediaType], enabled: enabled && !!libraryId && !!q.trim(), queryFn: ({ signal }) => read(api.GET('/libraries/{id}/naming/suggestions', { params: { path: { id: libraryId }, query: { directory, q, mediaType } }, signal })) });
}

export type NamingFilePreview = Schema<'NamingPreviewItem'> & { sourceName: string };
export async function previewNamingFiles(files: readonly File[], target: { libraryId: string; relativeDirectory?: string; family?: string; collectionId?: string; note?: string }, collectionKind?: Schema<'Collection'>['kind']): Promise<NamingFilePreview[]> {
  const groups = new Map<string, { file: File; index: number }[]>();
  files.forEach((file, index) => {
    const relative = file.webkitRelativePath ?? '';
    const sourceDirectory = relative.includes('/') ? relative.slice(0, relative.lastIndexOf('/')) : '';
    const directory = target.collectionId ? target.relativeDirectory ?? '' : [target.relativeDirectory, sourceDirectory].filter(Boolean).join('/');
    groups.set(directory, [...(groups.get(directory) ?? []), { file, index }]);
  });
  const results: { index: number; preview: NamingFilePreview }[] = [];
  for (const [directory, inputs] of groups) {
    const result = await read(api.POST('/libraries/{id}/naming/preview', {
      params: { path: { id: target.libraryId } },
      body: { intent: target.collectionId ? collectionKind === 'short_drama' ? 'append_drama' : 'append_pages' : 'new_work', directory, family: target.family, collectionId: target.collectionId, items: inputs.map(item => ({ fileName: item.file.name, note: target.note })) },
    }));
    if (result.items.length !== inputs.length) throw new Error('命名预览未覆盖所有选定文件，未开始本次上传。');
    result.items.forEach((item, index) => {
      const source = inputs[index]!;
      results.push({ index: source.index, preview: { ...item, sourceName: source.file.webkitRelativePath || source.file.name } });
    });
  }
  return results.sort((left, right) => left.index - right.index).map(result => result.preview);
}

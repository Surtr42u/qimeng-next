import { useEffect, useRef, useState } from 'react';
import { api, operationHeaders, read } from './client';
import type { Schema } from './client';
import { invalidateLibrary } from './query';
import { ApiError } from '../lib/errors';
import { newId } from '../lib/id';

export type FileTarget = Pick<Schema<'Asset'>, 'id' | 'revision' | 'fileName'>;
export type FileAction = { kind: 'trash' } | { kind: 'move'; directory: string; fileName?: string };
export type FileJob = { target: FileTarget; action: FileAction; key: string; state: 'queued' | 'running' | 'succeeded' | 'failed' | 'uncertain' | 'cancelled'; message?: string };

export function useFileActions() {
  const [jobs, setJobs] = useState<FileJob[]>([]);
  const [running, setRunning] = useState(false);
  const controller = useRef<AbortController | undefined>(undefined);
  const current = useRef<FileJob[]>([]);
  useEffect(() => () => controller.current?.abort(), []);

  const publish = () => setJobs(current.current.map(job => ({ ...job })));
  const execute = async (plan: FileJob[]) => {
    if (controller.current) return;
    const abort = new AbortController();
    controller.current = abort;
    current.current = plan;
    setRunning(true);
    publish();
    try {
      for (const job of current.current) {
        if (job.state === 'succeeded') continue;
        if (abort.signal.aborted) { job.state = 'cancelled'; job.message = '未执行'; continue; }
        job.state = 'running';
        job.message = undefined;
        publish();
        try {
          const params = { path: { id: job.target.id }, header: operationHeaders(job.key) };
          if (job.action.kind === 'trash') {
            await read(api.DELETE('/assets/{id}', { params: { ...params, query: { expectedRevision: job.target.revision } }, signal: abort.signal }));
          } else {
            const fileName = job.action.fileName ?? job.target.fileName;
            const relativePath = job.action.directory ? `${job.action.directory}/${fileName}` : fileName;
            await read(api.POST('/assets/{id}/move', { params, body: { expectedRevision: job.target.revision, relativePath }, signal: abort.signal }));
          }
          job.state = 'succeeded';
        } catch (error) {
          const uncertain = error instanceof ApiError && error.status === 0 || error instanceof DOMException && error.name === 'AbortError';
          job.state = uncertain ? 'uncertain' : 'failed';
          job.message = uncertain ? '结果尚未确认；重试会沿用同一操作身份核对' : error instanceof Error ? error.message : '操作失败';
        }
        publish();
      }
    } finally {
      controller.current = undefined;
      setRunning(false);
      publish();
      await invalidateLibrary();
    }
  };
  return {
    jobs, running,
    start: (targets: FileTarget[], action: FileAction) => execute(targets.map<FileJob>(target => ({ target: { id: target.id, revision: target.revision, fileName: target.fileName }, action: { ...action }, key: newId(), state: 'queued' }))),
    retry: () => execute(current.current.map<FileJob>(job => job.state === 'succeeded' ? job : { ...job, state: 'queued', message: undefined })),
    cancel: () => controller.current?.abort(),
    reset: () => { if (!controller.current) { current.current = []; setJobs([]); } },
  };
}

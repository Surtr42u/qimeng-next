export function fileDigest(file: File, signal: AbortSignal, progress: (value: number) => void): Promise<string> {
  return new Promise((resolve, reject) => {
    const worker = new Worker(new URL('./hash-worker.ts', import.meta.url), { type: 'module' });
    const stop = () => { worker.terminate(); signal.removeEventListener('abort', abort); };
    const abort = () => { stop(); reject(new DOMException('Cancelled', 'AbortError')); };
    if (signal.aborted) { abort(); return; }
    signal.addEventListener('abort', abort, { once: true });
    worker.onerror = () => { stop(); reject(new Error('文件核验线程启动失败，请重试。')); };
    worker.onmessage = (event: MessageEvent<{ progress?: number; digest?: string; error?: string }>) => {
      if (event.data.progress !== undefined) progress(event.data.progress);
      if (event.data.digest) { stop(); resolve(event.data.digest); }
      if (event.data.error) { stop(); reject(new Error(event.data.error)); }
    };
    worker.postMessage(file);
  });
}

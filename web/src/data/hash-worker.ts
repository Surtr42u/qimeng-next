import { sha256 } from '@noble/hashes/sha2.js';
self.onmessage = async (event: MessageEvent<File>) => {
  try {
    const file = event.data;
    const hash = sha256.create();
    for (let offset = 0; offset < file.size; offset += 4 * 1024 * 1024) {
      hash.update(new Uint8Array(await file.slice(offset, offset + 4 * 1024 * 1024).arrayBuffer()));
      self.postMessage({ progress: Math.min(file.size, offset + 4 * 1024 * 1024) });
    }
    self.postMessage({ digest: Array.from(hash.digest(), b => b.toString(16).padStart(2, '0')).join('') });
  } catch { self.postMessage({ error: '无法完整读取所选文件，请确认文件仍可访问。' }); }
};

// Record only the method, successful bound identity/revision and a digest of the synthetic
// biography. All response bytes travel unchanged from the isolated Go process to the MCP executor.
import { createHash } from 'node:crypto';
import { once } from 'node:events';
import { createServer, request } from 'node:http';
import { goOrigin } from './fixtures.mjs';

export function authorDigest(author) {
  if (!author || typeof author.id !== 'string' || !Number.isSafeInteger(author.revision) || typeof author.biography !== 'string') return null;
  return createHash('sha256').update(JSON.stringify([author.id, author.revision, author.biography])).digest('hex');
}

export async function observeAuthorTraffic(token, authorId) {
  const events = [];
  const path = '/api/v1/authors/' + encodeURIComponent(authorId);
  const server = createServer((incoming, outgoing) => {
    if (!incoming.url?.startsWith('/api/v1/') || incoming.headers.authorization !== 'Bearer ' + token) {
      outgoing.writeHead(403).end();
      return;
    }
    const headers = Object.fromEntries(['authorization', 'content-type', 'content-length', 'idempotency-key', 'accept']
      .filter(name => incoming.headers[name] !== undefined).map(name => [name, incoming.headers[name]]));
    const upstream = request(goOrigin + incoming.url, { method: incoming.method, headers }, response => {
      outgoing.writeHead(response.statusCode, response.headers);
      if (incoming.url === path && ['PATCH', 'GET'].includes(incoming.method)) {
        const chunks = [];
        let size = 0;
        response.on('data', chunk => {
          size += chunk.length;
          if (size <= 1024 * 1024) chunks.push(chunk);
        });
        response.on('end', () => {
          let value;
          if (size <= 1024 * 1024) {
            try { value = JSON.parse(Buffer.concat(chunks).toString('utf8')); } catch { /* No readable author response. */ }
          }
          events.push({ method: incoming.method, status: response.statusCode, matchesBoundAuthor: value?.id === authorId,
            revision: Number.isSafeInteger(value?.revision) ? value.revision : null, digest: authorDigest(value) });
        });
      }
      response.on('error', () => outgoing.destroy());
      response.pipe(outgoing);
    });
    upstream.on('error', () => {
      if (!outgoing.headersSent) outgoing.writeHead(502);
      outgoing.end();
    });
    incoming.on('error', () => upstream.destroy());
    incoming.pipe(upstream);
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  return { events, origin: 'http://127.0.0.1:' + server.address().port, close: () => new Promise(resolve => server.close(resolve)) };
}

// Only cache public application files. Authenticated API and media never enter this cache.
const CACHE = 'qimeng-shell-v1';
const SHELL = ['/', '/icon.svg', '/theme.js', '/manifest.webmanifest'];
self.addEventListener('install', event => event.waitUntil(caches.open(CACHE).then(cache => cache.addAll(SHELL))));
self.addEventListener('activate', event => event.waitUntil(
  caches.keys().then(keys => Promise.all(keys.filter(key => key.startsWith('qimeng-shell-') && key !== CACHE).map(key => caches.delete(key))))
));
self.addEventListener('fetch', event => {
  const url = new URL(event.request.url);
  if (event.request.method !== 'GET' || url.origin !== self.location.origin || url.pathname.startsWith('/api/') || url.pathname === '/healthz') return;
  if (event.request.mode === 'navigate') {
    event.respondWith(fetch(event.request).then(async response => {
      if (response.ok && response.headers.get('content-type')?.includes('text/html')) (await caches.open(CACHE)).put('/', response.clone());
      return response;
    }).catch(async () => (await caches.match('/')) || Response.error()));
    return;
  }
  if (url.pathname.startsWith('/assets/') || SHELL.includes(url.pathname)) {
    event.respondWith(caches.match(event.request).then(cached => cached || fetch(event.request).then(async response => {
      if (response.ok) (await caches.open(CACHE)).put(event.request, response.clone());
      return response;
    })));
  }
});

// Service worker for SUPER CLI MARIO: the whole site is static, so
// same-origin GETs are cache-first with a network fallback (successful
// responses go into the cache). Everything else — POSTs, cross-origin
// traffic like the Supabase leaderboard — passes straight through.
const CACHE = 'mario-v0.2.0';
const ASSETS = [
  './',
  './index.html',
  './mario.wasm',
  './wasm_exec.js',
  './boot.js',
  './manifest.webmanifest',
  './icons/icon-192.png',
  './icons/icon-512.png',
  './icons/icon-maskable-512.png',
];

self.addEventListener('install', (e) => {
  // One failed fetch must not brick the install: tolerate each entry.
  e.waitUntil((async () => {
    const cache = await caches.open(CACHE);
    await Promise.all(ASSETS.map((a) => cache.add(a).catch(() => {})));
    await self.skipWaiting();
  })());
});

self.addEventListener('activate', (e) => {
  // Drop every cache from older versions, then take over clients.
  e.waitUntil((async () => {
    const names = await caches.keys();
    await Promise.all(names.filter((n) => n !== CACHE).map((n) => caches.delete(n)));
    await self.clients.claim();
  })());
});

self.addEventListener('fetch', (e) => {
  const req = e.request;
  if (req.method !== 'GET') return; // leaderboard writes go to the network
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return; // supabase & friends pass through
  e.respondWith((async () => {
    const cache = await caches.open(CACHE);
    // Navigations (any depth) resolve to the shell page.
    const key = req.mode === 'navigate' ? './index.html' : req;
    const hit = await cache.match(key);
    if (hit) return hit;
    try {
      const res = await fetch(req);
      if (res && res.ok) cache.put(req, res.clone());
      return res;
    } catch {
      return Response.error();
    }
  })());
});

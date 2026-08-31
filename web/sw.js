// Service worker for SUPER CLI MARIO: same-origin GETs are network-first
// with a cache fallback (offline play keeps working). The revalidation is
// forced ('no-cache') so a release goes live on the visitor's very next
// page load — the browser's HTTP cache would otherwise keep serving a
// fresh-looking (max-age) old shell. Everything else — POSTs, cross-origin
// traffic like the Supabase leaderboard — passes straight through.
const CACHE = 'mario-v0.2.0';
// Precache manifest. This hand-maintained line is only the fallback for
// serving web/ directly in dev; `make web` REWRITES this exact line in
// the dist/web copy from the actual built file listing (see the web
// target), so nothing the build ships — SECURITY.md,
// icons/apple-touch-icon.png, future additions — can be missing here.
// Keep it roughly in sync anyway; the grep after the stamp fails the
// build if the rewrite misses.
const ASSETS = ['./', './index.html', './mario.wasm', './wasm_exec.js', './boot.js', './manifest.webmanifest', './icons/icon-192.png', './icons/icon-512.png', './icons/icon-maskable-512.png', './icons/apple-touch-icon.png', './SECURITY.md'];

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
  // Navigations (any depth) resolve to the shell page.
  const navigate = req.mode === 'navigate';
  e.respondWith((async () => {
    const cache = await caches.open(CACHE);
    const key = navigate ? './index.html' : req;
    // Network-first: a cache-first SW serves the previous release for a
    // whole visit after every deploy (the new worker only installs in the
    // background). 'no-cache' costs one conditional GET per asset (nginx
    // answers 304 on the etag) instead of a full download.
    let net = null;
    try {
      net = navigate
        ? await fetch(new Request('./index.html', { cache: 'no-cache' }))
        : await fetch(new Request(req, { cache: 'no-cache' }));
    } catch {}
    if (net && net.ok) {
      cache.put(key, net.clone());
      return net;
    }
    const hit = await cache.match(key);
    return hit || net || Response.error();
  })());
});

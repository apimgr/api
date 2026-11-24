// The server renders the full build stamp into this value.
const APP_NAME = '__APP_NAME__';
const CACHE_VERSION = '__ASSET_STAMP__';
const CACHE_NAME = `${APP_NAME}-cache-${CACHE_VERSION}`;
const NAV_TIMEOUT_MS = 8000;
const APP_BASE_PATH = new URL(self.registration.scope).pathname.replace(/\/+$/, '');

function appPath(path) {
  return `${APP_BASE_PATH}${path}`;
}

// Versioned URLs match the asset helper used by rendered pages. HTML is never
// precached; navigations always check the network first.
const PRECACHE_ASSETS = [
  '/static/css/common.css?v=__ASSET_STAMP__',
  '/static/css/components.css?v=__ASSET_STAMP__',
  '/static/css/public.css?v=__ASSET_STAMP__',
  '/static/js/theme.js?v=__ASSET_STAMP__',
  '/static/js/app.js?v=__ASSET_STAMP__',
  '/static/images/icons/icon-192.png?v=__ASSET_STAMP__',
  '/static/images/icons/icon-512.png?v=__ASSET_STAMP__',
  '/static/images/icons/icon-192-maskable.png?v=__ASSET_STAMP__',
  '/static/images/icons/icon-512-maskable.png?v=__ASSET_STAMP__',
  '/static/images/apple-touch-icon.png?v=__ASSET_STAMP__',
  '/offline.html',
].map(appPath);

self.addEventListener('install', event => {
  event.waitUntil(caches.open(CACHE_NAME).then(cache => cache.addAll(PRECACHE_ASSETS)));
});

self.addEventListener('activate', event => {
  event.waitUntil(
    caches.keys()
      .then(keys => Promise.all(keys
        .filter(key => key.startsWith(`${APP_NAME}-cache-`) && key !== CACHE_NAME)
        .map(key => caches.delete(key))))
      .then(() => self.clients.claim())
  );
});

function withTimeout(promise, ms) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('navigation timeout')), ms);
    promise.then(
      value => { clearTimeout(timer); resolve(value); },
      error => { clearTimeout(timer); reject(error); }
    );
  });
}

function offlineResponse(status, message) {
  return new Response(message, {
    status,
    headers: { 'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'no-store' },
  });
}

self.addEventListener('fetch', event => {
  const request = event.request;
  const url = new URL(request.url);
  if (request.method !== 'GET' || url.origin !== self.location.origin) return;
  if (url.pathname.startsWith(appPath('/api/'))) return;

  if (request.mode === 'navigate') {
    event.respondWith(
      withTimeout(fetch(request), NAV_TIMEOUT_MS)
        .then(async response => {
          const cache = await caches.open(CACHE_NAME);
          if (response.redirected) {
            try { await cache.delete(request); } catch (_) { /* Cache is optional. */ }
            throw new Error('redirected navigation');
          }
          if (response.ok && response.headers.get('X-Offline-Cache') === 'public') {
            try { await cache.put(request, response.clone()); } catch (_) { /* Cache is optional. */ }
          } else {
            try { await cache.delete(request); } catch (_) { /* Cache is optional. */ }
          }
          return response;
        })
        .catch(async () => {
          try {
            const cached = await caches.match(request);
            if (cached) return cached;
            const offline = await caches.match(appPath('/offline.html'));
            if (offline) return offline;
          } catch (_) { /* Use the synthesized response below. */ }
          return new Response(
            '<!doctype html><html lang="en"><head><meta charset="utf-8">' +
            '<meta name="viewport" content="width=device-width,initial-scale=1">' +
            '<title>Offline</title></head><body><main><h1>You are offline</h1>' +
            '<p>This page could not be loaded and no cached copy is available. ' +
            'Check your connection and try again.</p></main></body></html>',
            { status: 503, headers: { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' } }
          );
        })
    );
    return;
  }

  if (url.pathname.startsWith(appPath('/static/'))) {
    event.respondWith((async () => {
      try {
        const cached = await caches.match(request);
        if (cached) return cached;
        const response = await fetch(request);
        if (response.ok && !response.redirected) {
          try { await (await caches.open(CACHE_NAME)).put(request, response.clone()); } catch (_) { /* Cache is optional. */ }
        }
        return response.redirected ? offlineResponse(504, 'Resource unavailable.') : response;
      } catch (_) {
        return offlineResponse(504, 'Resource unavailable offline.');
      }
    })());
    return;
  }

  event.respondWith((async () => {
    try {
      const response = await fetch(request);
      if (response.ok && !response.redirected) {
        try { await (await caches.open(CACHE_NAME)).put(request, response.clone()); } catch (_) { /* Cache is optional. */ }
      }
      return response.redirected ? offlineResponse(504, 'Resource unavailable.') : response;
    } catch (_) {
      try {
        const cached = await caches.match(request);
        if (cached) return cached;
      } catch (_) { /* Return a real terminal response. */ }
      return offlineResponse(504, 'Resource unavailable offline.');
    }
  })());
});

self.addEventListener('message', event => {
  if (event.data && event.data.type === 'SKIP_WAITING') self.skipWaiting();
});

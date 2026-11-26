// The server renders the full build stamp into this value.
const APP_NAME = '__APP_NAME__';
const CACHE_VERSION = '__ASSET_STAMP__';
const CACHE_NAME = `${APP_NAME}-cache-${CACHE_VERSION}`;
const NAV_TIMEOUT_MS = 8000;
const MAX_STATIC_CACHE_BYTES = 50 * 1024 * 1024;
const LARGE_CACHE_ENTRY_BYTES = 1024 * 1024;
const STATIC_CACHE_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000;
const CACHE_STORED_AT_HEADER = 'X-Offline-Cached-At';
const CACHE_SIZE_HEADER = 'X-Offline-Cache-Bytes';
const APP_BASE_PATH = new URL(self.registration.scope).pathname.replace(/\/+$/, '');
const DEFAULT_STRINGS = {
  lang: 'en',
  dir: 'ltr',
  title: 'Offline',
  heading: 'You are offline',
  message: 'This page could not be loaded and no cached copy is available. Check your connection and try again.',
};

function appPath(path) {
  return `${APP_BASE_PATH}${path}`;
}

// Versioned URLs match the asset helper used by rendered pages. HTML is never
// precached; navigations always check the network first.
const PRECACHE_ASSETS = [
  '/static/css/common.css?v=__ASSET_STAMP__',
  '/static/css/components.css?v=__ASSET_STAMP__',
  '/static/css/public.css?v=__ASSET_STAMP__',
  '/static/js/app.js?v=__ASSET_STAMP__',
  '/static/images/icons/icon-192.png?v=__ASSET_STAMP__',
  '/static/images/icons/icon-512.png?v=__ASSET_STAMP__',
  '/static/images/icons/icon-192-maskable.png?v=__ASSET_STAMP__',
  '/static/images/icons/icon-512-maskable.png?v=__ASSET_STAMP__',
  '/static/images/apple-touch-icon.png?v=__ASSET_STAMP__',
].map(appPath);

self.addEventListener('install', event => {
  event.waitUntil((async () => {
    const cache = await caches.open(CACHE_NAME);
    for (const asset of PRECACHE_ASSETS) {
      const response = await fetch(asset);
      if (!response.ok || response.redirected) throw new Error(`Unable to precache ${asset}`);
      if (new URL(asset).pathname.startsWith(appPath('/static/'))) {
        await cacheStaticAsset(cache, asset, response);
      } else {
        await cacheBoundedResponse(cache, asset, response);
      }
    }
  })());
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

function escapeHTML(value) {
  return String(value).replace(/[&<>"']/g, character => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  })[character]);
}

async function offlineFallbackResponse() {
  const strings = { ...DEFAULT_STRINGS };
  try {
    const cache = await caches.open(`${APP_NAME}-offline-strings`);
    const response = await cache.match(appPath('/offline-strings.json'));
    if (response) {
      const stored = await response.json();
      for (const key of Object.keys(strings)) {
        if (typeof stored[key] === 'string' && stored[key]) strings[key] = stored[key];
      }
    }
  } catch (_) { /* The English first-use fallback remains available. */ }

  return new Response(
    '<!doctype html><html lang="' + escapeHTML(strings.lang) + '" dir="' + escapeHTML(strings.dir) + '"><head><meta charset="utf-8">' +
      '<meta name="viewport" content="width=device-width,initial-scale=1">' +
      '<title>' + escapeHTML(strings.title) + '</title></head><body><main>' +
      '<h1>' + escapeHTML(strings.heading) + '</h1><p>' + escapeHTML(strings.message) + '</p>' +
      '</main></body></html>',
    { status: 503, statusText: 'Service Unavailable', headers: { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' } }
  );
}

async function responseSize(response) {
  const contentLength = Number(response.headers.get('Content-Length'));
  if (Number.isFinite(contentLength) && contentLength >= 0) return contentLength;
  if (!response.body) return 0;

  const reader = response.clone().body.getReader();
  let size = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) return size;
      size += value.byteLength;
      if (size > MAX_STATIC_CACHE_BYTES) {
        await reader.cancel();
        return size;
      }
    }
  } finally {
    reader.releaseLock();
  }
}

async function cacheEntries(cache) {
  const entries = [];
  for (const request of await cache.keys()) {
    const response = await cache.match(request);
    if (!response) continue;
    const storedAt = Number(response.headers.get(CACHE_STORED_AT_HEADER));
    const isStaticAsset = new URL(request.url).pathname.startsWith(appPath('/static/'));
    if (isStaticAsset && (!Number.isFinite(storedAt) || Date.now() - storedAt > STATIC_CACHE_MAX_AGE_MS)) {
      await cache.delete(request);
      continue;
    }
    const cachedSize = Number(response.headers.get(CACHE_SIZE_HEADER));
    const size = Number.isFinite(cachedSize) && cachedSize >= 0 ? cachedSize : await responseSize(response);
    entries.push({ request, storedAt: Number.isFinite(storedAt) ? storedAt : 0, size });
  }
  return entries;
}

async function storageAllowsLargeEntry(size) {
  if (size < LARGE_CACHE_ENTRY_BYTES || !navigator.storage || !navigator.storage.estimate) return true;
  try {
    const estimate = await navigator.storage.estimate();
    if (!Number.isFinite(estimate.quota) || !Number.isFinite(estimate.usage)) return true;
    return estimate.usage + size <= estimate.quota * 0.8;
  } catch (_) {
    return true;
  }
}

async function cacheBoundedResponse(cache, request, response) {
  const size = await responseSize(response);
  if (size > MAX_STATIC_CACHE_BYTES || !(await storageAllowsLargeEntry(size))) return false;

  const entries = await cacheEntries(cache);
  const existing = entries.find(entry => entry.request.url === new URL(request.url).href);
  let total = entries.reduce((sum, entry) => sum + entry.size, 0) - (existing ? existing.size : 0);
  const evictionCandidates = entries.filter(entry => !existing || entry.request.url !== existing.request.url);
  while (total + size > MAX_STATIC_CACHE_BYTES && evictionCandidates.length > 0) {
    evictionCandidates.sort((a, b) => a.storedAt - b.storedAt);
    const oldest = evictionCandidates.shift();
    await cache.delete(oldest.request);
    total -= oldest.size;
  }
  if (total + size > MAX_STATIC_CACHE_BYTES) return false;

  const headers = new Headers(response.headers);
  headers.set(CACHE_STORED_AT_HEADER, String(Date.now()));
  headers.set(CACHE_SIZE_HEADER, String(size));
  await cache.put(request, new Response(response.clone().body, {
    status: response.status,
    statusText: response.statusText,
    headers,
  }));
  return true;
}

async function cacheStaticAsset(cache, request, response) {
  return cacheBoundedResponse(cache, request, response);
}

async function cachePublicPage(cache, request, response) {
  return cacheBoundedResponse(cache, request, response);
}

function withoutCacheMetadata(response) {
  const headers = new Headers(response.headers);
  headers.delete(CACHE_STORED_AT_HEADER);
  headers.delete(CACHE_SIZE_HEADER);
  return new Response(response.body, {
    status: response.status,
    statusText: response.statusText,
    headers,
  });
}

async function cachedStaticAsset(cache, request) {
  const response = await cache.match(request);
  if (!response) return null;
  const storedAt = Number(response.headers.get(CACHE_STORED_AT_HEADER));
  if (!Number.isFinite(storedAt) || Date.now() - storedAt > STATIC_CACHE_MAX_AGE_MS) {
    await cache.delete(request);
    return null;
  }
  return withoutCacheMetadata(response);
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
          if (response.redirected) {
            try {
              const cache = await caches.open(CACHE_NAME);
              await cache.delete(request);
            } catch (_) { /* Cache is optional. */ }
            return response;
          }
          const cache = await caches.open(CACHE_NAME);
          if (response.ok && response.headers.get('X-Offline-Cache') === 'public') {
            try { await cachePublicPage(cache, request, response); } catch (_) { /* Cache is optional. */ }
          } else {
            try { await cache.delete(request); } catch (_) { /* Cache is optional. */ }
          }
          return response;
        })
        .catch(async () => {
          try {
            const cached = await caches.match(request);
            if (cached) return withoutCacheMetadata(cached);
          } catch (_) { /* Use the synthesized response below. */ }
          return offlineFallbackResponse();
        })
    );
    return;
  }

  if (url.pathname.startsWith(appPath('/static/'))) {
    event.respondWith((async () => {
      const cache = await caches.open(CACHE_NAME);
      try {
        const cached = await cachedStaticAsset(cache, request);
        if (cached) return cached;
        const response = await fetch(request);
        if (response.ok && !response.redirected) {
          try { await cacheStaticAsset(cache, request, response); } catch (_) { /* Cache is optional. */ }
        }
        return response.redirected ? offlineResponse(504, 'Resource unavailable.') : response;
      } catch (_) {
        try {
          const cached = await cachedStaticAsset(cache, request);
          if (cached) return cached;
        } catch (_) { /* Cache is optional. */ }
        return offlineResponse(504, 'Resource unavailable offline.');
      }
    })());
    return;
  }
});

self.addEventListener('message', event => {
  if (event.data && event.data.type === 'SKIP_WAITING') self.skipWaiting();
});

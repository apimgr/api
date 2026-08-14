// sw.js - Service Worker per AI.md PART 16 "PWA Support"
// Lifecycle: install (pre-cache) -> activate (purge old caches) -> fetch
// (serve per resource-type strategy). Cache name embeds the app version so
// activate can delete every cache from a prior version.
const CACHE_VERSION = 'v0.0.1';
const CACHE_NAME = `castools-cache-${CACHE_VERSION}`;

// Assets to pre-cache on install
const PRECACHE_ASSETS = [
  '/',
  '/static/css/common.css',
  '/static/css/components.css',
  '/static/css/public.css',
  '/static/js/app.js',
  '/static/images/icons/icon-192.png',
  '/static/images/icons/icon-512.png',
  '/offline.html',
];

// INSTALL - pre-cache static assets, then activate without waiting for old
// tabs to close (App Update Notification flow controls the user-visible
// prompt instead)
self.addEventListener('install', function(event) {
  event.waitUntil(
    caches.open(CACHE_NAME)
      .then(function(cache) { return cache.addAll(PRECACHE_ASSETS); })
      .then(function() { return self.skipWaiting(); })
  );
});

// ACTIVATE - delete every cache from a previous version
self.addEventListener('activate', function(event) {
  event.waitUntil(
    caches.keys()
      .then(function(keys) {
        return Promise.all(
          keys
            .filter(function(key) { return key.startsWith('castools-cache-') && key !== CACHE_NAME; })
            .map(function(key) { return caches.delete(key); })
        );
      })
      .then(function() { return self.clients.claim(); })
  );
});

// FETCH - cache-first for static assets, network-first w/ cache fallback
// for HTML pages, network-only (no caching) for API calls
self.addEventListener('fetch', function(event) {
  const request = event.request;
  const url = new URL(request.url);

  // Only GET requests are cacheable; everything else goes straight to network
  if (request.method !== 'GET') {
    return;
  }

  // API calls: network-only, never cached, never served stale
  if (url.pathname.startsWith('/api/')) {
    return;
  }

  // Static assets: cache-first
  if (url.pathname.startsWith('/static/')) {
    event.respondWith(
      caches.match(request).then(function(cached) {
        return cached || fetch(request).then(function(response) {
          const clone = response.clone();
          caches.open(CACHE_NAME).then(function(cache) { cache.put(request, clone); });
          return response;
        });
      })
    );
    return;
  }

  // HTML pages: network-first, cache fallback, offline page as last resort
  if ((request.headers.get('accept') || '').includes('text/html')) {
    event.respondWith(
      fetch(request)
        .then(function(response) {
          const clone = response.clone();
          caches.open(CACHE_NAME).then(function(cache) { cache.put(request, clone); });
          return response;
        })
        .catch(function() {
          return caches.match(request).then(function(cached) {
            return cached || caches.match('/offline.html');
          });
        })
    );
    return;
  }

  // Default: network-first, cache fallback
  event.respondWith(
    fetch(request).catch(function() { return caches.match(request); })
  );
});

// Allow the page to force this worker to activate immediately, used by the
// "Update Now" banner in app.js
self.addEventListener('message', function(event) {
  if (event.data && event.data.type === 'SKIP_WAITING') {
    self.skipWaiting();
  }
});

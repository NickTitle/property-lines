const appShellCacheName = "property-lines-app-shell-v1";
const tileCacheName = "property-lines-tile-cache-v1";
const appShellAssets = [
  "/",
  "/index.html",
  "/styles.css",
  "/app.js",
  "/manifest.webmanifest",
  "/vendor/leaflet/leaflet.css",
  "/vendor/leaflet/leaflet.js",
  "/vendor/leaflet/images/layers.png",
  "/vendor/leaflet/images/layers-2x.png",
  "/vendor/leaflet/images/marker-icon.png",
  "/vendor/leaflet/images/marker-icon-2x.png",
  "/vendor/leaflet/images/marker-shadow.png",
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    (async () => {
      const cache = await caches.open(appShellCacheName);
      await cache.addAll(appShellAssets);
      await self.skipWaiting();
    })(),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    (async () => {
      const cacheNames = await caches.keys();
      await Promise.all(
        cacheNames
          .filter((name) => name.startsWith("property-lines-app-shell-") && name !== appShellCacheName)
          .map((name) => caches.delete(name)),
      );
      await self.clients.claim();
    })(),
  );
});

self.addEventListener("fetch", (event) => {
  if (event.request.method !== "GET") {
    return;
  }

  const url = new URL(event.request.url);

  if (isAppShellRequest(url)) {
    event.respondWith(handleAppShellRequest(event.request, url));
    return;
  }

  if (isTileRequest(url)) {
    event.respondWith(handleTileRequest(event.request));
  }
});

function isAppShellRequest(url) {
  if (url.origin !== self.location.origin) {
    return false;
  }

  if (url.pathname.startsWith("/api/") || url.pathname === "/healthz") {
    return false;
  }

  return true;
}

function isTileRequest(url) {
  return url.hostname === "tile.openstreetmap.org" || url.hostname.endsWith(".tile.openstreetmap.org");
}

function appShellCacheKey(url, request) {
  if (request.mode === "navigate" || url.pathname === "/") {
    return "/index.html";
  }

  return url.pathname;
}

async function handleAppShellRequest(request, url) {
  const cache = await caches.open(appShellCacheName);
  const key = appShellCacheKey(url, request);
  const cached = await cache.match(key, { ignoreSearch: true });

  if (request.mode === "navigate") {
    try {
      const response = await fetch(request);
      if (response.ok) {
        await cache.put("/index.html", response.clone()).catch(() => undefined);
      }
      return response;
    } catch {
      return cached || Response.error();
    }
  }

  if (cached) {
    void refreshAppShellAsset(cache, key, request);
    return cached;
  }

  const response = await fetch(request);
  if (response.ok) {
    await cache.put(key, response.clone()).catch(() => undefined);
  }
  return response;
}

async function refreshAppShellAsset(cache, key, request) {
  try {
    const response = await fetch(request);
    if (response.ok) {
      await cache.put(key, response.clone()).catch(() => undefined);
    }
  } catch {
    // Ignore refresh failures and keep serving the cached shell.
  }
}

async function handleTileRequest(request) {
  const cache = await caches.open(tileCacheName);
  const cached = await cache.match(request, { ignoreVary: true });

  if (cached) {
    return cached;
  }

  const response = await fetch(request);
  if (response.ok || response.type === "opaque") {
    await cache.put(request, response.clone()).catch(() => undefined);
  }
  return response;
}

const nysLayerUrl =
  "https://gisservices.its.ny.gov/arcgis/rest/services/NYS_Tax_Parcels_Public/MapServer/1";
const demoLayerUrl =
  "https://gis.franklincountyohio.gov/hosting/rest/services/ParcelFeatures/Parcel_Features/FeatureServer/0";
const serviceUrlStorageKey = "property-lines:serviceUrl";
const basemapStorageKey = "property-lines:basemap";
const areaCachePrefix = "property-lines:area-cache:";
const areaCacheIndexKey = "property-lines:area-cache-index";
const maxAreaCacheEntries = 8;
const tileCacheName = "property-lines-tile-cache-v1";
const tileUrlTemplate = "https://a.tile.openstreetmap.org/{z}/{x}/{y}.png";
const satelliteTileUrlTemplate =
  "https://basemap.nationalmap.gov/arcgis/rest/services/USGSImageryOnly/MapServer/tile/{z}/{y}/{x}";
const maxTileDownloadCount = 250;
const defaultPoint = { latitude: 41.933, longitude: -74.0186, source: "New York sample start" };
const basemaps = {
  street: {
    id: "street",
    label: "Streets",
    maxZoom: 20,
    tileUrlTemplate,
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
  },
  satellite: {
    id: "satellite",
    label: "Satellite",
    maxNativeZoom: 16,
    maxZoom: 20,
    tileUrlTemplate: satelliteTileUrlTemplate,
    attribution: 'Imagery: <a href="https://www.usgs.gov/">U.S. Geological Survey</a> / The National Map',
  },
};
const automaticParcelSources = [
  {
    name: "NYS Tax Parcels Public",
    serviceUrl: nysLayerUrl,
    bounds: {
      north: 45.1,
      south: 40.45,
      east: -71.75,
      west: -79.8,
    },
  },
];

const dom = {
  accuracyText: document.querySelector("#accuracyText"),
  attributeTable: document.querySelector("#attributeTable"),
  cacheAreaButton: document.querySelector("#cacheAreaButton"),
  cacheCountInput: document.querySelector("#cacheCountInput"),
  cacheLimitInput: document.querySelector("#cacheLimitInput"),
  cacheStatus: document.querySelector("#cacheStatus"),
  cacheTilesButton: document.querySelector("#cacheTilesButton"),
  centerButton: document.querySelector("#centerButton"),
  clearCacheButton: document.querySelector("#clearCacheButton"),
  clearTileCacheButton: document.querySelector("#clearTileCacheButton"),
  demoButton: document.querySelector("#demoButton"),
  latitudeInput: document.querySelector("#latitudeInput"),
  limitInput: document.querySelector("#limitInput"),
  loadCacheButton: document.querySelector("#loadCacheButton"),
  locateButton: document.querySelector("#locateButton"),
  longitudeInput: document.querySelector("#longitudeInput"),
  nysButton: document.querySelector("#nysButton"),
  parcelSummary: document.querySelector("#parcelSummary"),
  queryButton: document.querySelector("#queryButton"),
  satelliteLayerButton: document.querySelector("#satelliteLayerButton"),
  serviceUrlInput: document.querySelector("#serviceUrlInput"),
  sourceStatus: document.querySelector("#sourceStatus"),
  streetLayerButton: document.querySelector("#streetLayerButton"),
  status: document.querySelector("#status"),
  tileCacheCountInput: document.querySelector("#tileCacheCountInput"),
  tileCacheStatus: document.querySelector("#tileCacheStatus"),
  tileZoomLevelsInput: document.querySelector("#tileZoomLevelsInput"),
};

let baseMapLayer = null;
let gpsMarker = null;
let accuracyCircle = null;
let parcelLayer = null;
let selectedLayer = null;
let gpsWatchId = null;
let lastGpsFix = null;
let lastGpsHeading = null;
let currentBasemapId = readStoredBasemapId();
let currentPoint = {
  latitude: defaultPoint.latitude,
  longitude: defaultPoint.longitude,
  source: defaultPoint.source,
};

const pointIcon = L.divIcon({
  className: "gps-marker",
  html: `
    <span class="gps-marker__wrap">
      <svg class="gps-marker__cone" viewBox="0 0 56 56" aria-hidden="true">
        <path d="M28 28 L 41 8 A 24 24 0 0 0 15 8 Z"></path>
      </svg>
      <span class="gps-marker__dot"></span>
    </span>
  `,
  iconSize: [56, 56],
  iconAnchor: [28, 28],
});

const map = L.map("map", { zoomControl: false }).setView(
  [currentPoint.latitude, currentPoint.longitude],
  17,
);

L.control.zoom({ position: "topleft" }).addTo(map);
setBasemap(currentBasemapId, { persist: false });

function init() {
  const storedServiceUrl = window.localStorage.getItem(serviceUrlStorageKey) || "";
  if (isLegacyDemoServiceUrl(storedServiceUrl)) {
    window.localStorage.removeItem(serviceUrlStorageKey);
    dom.serviceUrlInput.value = "";
  } else {
    dom.serviceUrlInput.value = storedServiceUrl;
  }
  dom.limitInput.value = window.localStorage.getItem("property-lines:limit") || "300";
  dom.cacheLimitInput.value = window.localStorage.getItem("property-lines:cacheLimit") || "300";
  dom.tileZoomLevelsInput.value = window.localStorage.getItem("property-lines:tileZoomLevels") || "2";

  updatePoint(currentPoint.latitude, currentPoint.longitude, defaultPoint.source, null, false);
  updateCacheStatus();
  updateSourceStatus();
  updateBasemapControls();
  void updateTileCacheStatus().catch(() => setTileCacheStatus("Map tile cache status is unavailable."));
  void registerServiceWorker();

  dom.locateButton.addEventListener("click", locate);
  dom.centerButton.addEventListener("click", useMapCenter);
  dom.queryButton.addEventListener("click", queryParcels);
  dom.cacheAreaButton.addEventListener("click", cacheVisibleArea);
  dom.loadCacheButton.addEventListener("click", loadCachedArea);
  dom.clearCacheButton.addEventListener("click", clearAreaCache);
  dom.cacheTilesButton.addEventListener("click", cacheVisibleTiles);
  dom.clearTileCacheButton.addEventListener("click", clearTileCache);
  dom.demoButton.addEventListener("click", useDemoLayer);
  dom.nysButton.addEventListener("click", useNysParcels);
  dom.streetLayerButton.addEventListener("click", () => setBasemap("street"));
  dom.satelliteLayerButton.addEventListener("click", () => setBasemap("satellite"));
  dom.latitudeInput.addEventListener("change", useInputCoordinates);
  dom.longitudeInput.addEventListener("change", useInputCoordinates);
  dom.serviceUrlInput.addEventListener("change", () => {
    persistSettings();
    updateSourceStatus();
  });
  dom.limitInput.addEventListener("change", persistSettings);
  dom.cacheLimitInput.addEventListener("change", persistSettings);
  dom.tileZoomLevelsInput.addEventListener("change", persistSettings);

  map.on("click", (event) => {
    stopGpsTracking();
    updatePoint(event.latlng.lat, event.latlng.lng, "Map click", null, true);
  });

  window.addEventListener("beforeunload", stopGpsTracking);
}

function readStoredBasemapId() {
  const stored = window.localStorage.getItem(basemapStorageKey);
  return Object.prototype.hasOwnProperty.call(basemaps, stored) ? stored : "street";
}

function getBasemap(id = currentBasemapId) {
  return basemaps[id] || basemaps.street;
}

function setBasemap(id, { persist = true } = {}) {
  const basemap = getBasemap(id);
  currentBasemapId = basemap.id;

  if (baseMapLayer) {
    baseMapLayer.remove();
  }

  baseMapLayer = L.tileLayer(basemap.tileUrlTemplate, {
    attribution: basemap.attribution,
    maxNativeZoom: basemap.maxNativeZoom,
    maxZoom: basemap.maxZoom,
  }).addTo(map);

  if (persist) {
    window.localStorage.setItem(basemapStorageKey, basemap.id);
  }

  updateBasemapControls();
}

function updateBasemapControls() {
  dom.streetLayerButton.classList.toggle("is-active", currentBasemapId === "street");
  dom.satelliteLayerButton.classList.toggle("is-active", currentBasemapId === "satellite");
  dom.streetLayerButton.setAttribute("aria-pressed", currentBasemapId === "street" ? "true" : "false");
  dom.satelliteLayerButton.setAttribute("aria-pressed", currentBasemapId === "satellite" ? "true" : "false");
}

function setStatus(message, tone = "") {
  dom.status.textContent = message;
  dom.status.classList.toggle("error", tone === "error");
}

function setBusy(isBusy) {
  dom.queryButton.disabled = isBusy;
  dom.cacheAreaButton.disabled = isBusy;
  dom.cacheTilesButton.disabled = isBusy;
  dom.centerButton.disabled = isBusy;
}

function persistSettings() {
  const serviceUrl = dom.serviceUrlInput.value.trim();
  if (serviceUrl) {
    window.localStorage.setItem(serviceUrlStorageKey, serviceUrl);
  } else {
    window.localStorage.removeItem(serviceUrlStorageKey);
  }
  window.localStorage.setItem("property-lines:limit", dom.limitInput.value);
  window.localStorage.setItem("property-lines:cacheLimit", dom.cacheLimitInput.value);
  window.localStorage.setItem("property-lines:tileZoomLevels", dom.tileZoomLevelsInput.value);
}

function isLegacyDemoServiceUrl(value) {
  return normalizeServiceUrl(value || "") === normalizeServiceUrl(demoLayerUrl);
}

async function registerServiceWorker() {
  if (!("serviceWorker" in navigator)) {
    setTileCacheStatus("Service worker unavailable; offline app and map tiles cannot be served.");
    return;
  }

  try {
    await navigator.serviceWorker.register("/sw.js", { scope: "/" });
    await navigator.serviceWorker.ready;
  } catch {
    setTileCacheStatus("Service worker registration failed; offline app and map tiles cannot be served.");
  }
}

function useDemoLayer() {
  stopGpsTracking();
  dom.serviceUrlInput.value = demoLayerUrl;
  persistSettings();
  map.setView([39.9612, -82.9988], 17);
  updatePoint(39.9612, -82.9988, "Demo", null, false);
  updateSourceStatus();
  setStatus("Ohio demo layer selected. Use NYS parcels to return to automatic local lookup.");
}

function useNysParcels() {
  stopGpsTracking();
  dom.serviceUrlInput.value = "";
  persistSettings();
  map.setView([defaultPoint.latitude, defaultPoint.longitude], 17);
  updatePoint(defaultPoint.latitude, defaultPoint.longitude, defaultPoint.source, null, false);
  updateSourceStatus();
  setStatus("Automatic NYS parcel lookup selected. Use GPS for your exact position.");
}

function useMapCenter() {
  stopGpsTracking();
  const center = map.getCenter();
  updatePoint(center.lat, center.lng, "Map center", null, false);
}

function useInputCoordinates() {
  stopGpsTracking();
  const latitude = Number(dom.latitudeInput.value);
  const longitude = Number(dom.longitudeInput.value);

  if (!Number.isFinite(latitude) || !Number.isFinite(longitude)) {
    setStatus("Latitude and longitude must be valid numbers.", "error");
    return;
  }

  updatePoint(latitude, longitude, "Manual", null, true);
}

function locate() {
  if (!navigator.geolocation) {
    setStatus("This browser does not expose GPS location.", "error");
    return;
  }

  if (gpsWatchId !== null) {
    stopGpsTracking();
    setStatus("GPS tracking paused.");
    return;
  }

  lastGpsFix = null;
  lastGpsHeading = null;
  setGpsTrackingState(true);
  setStatus("Starting GPS tracking...");
  gpsWatchId = navigator.geolocation.watchPosition(
    (position) => {
      const heading = resolveGpsHeading(position, lastGpsFix, lastGpsHeading);
      lastGpsFix = position;
      lastGpsHeading = heading;
      updatePoint(
        position.coords.latitude,
        position.coords.longitude,
        "GPS",
        position.coords.accuracy,
        true,
        heading,
      );
      setStatus(
        `GPS tracking active. Accuracy about ${Math.round(position.coords.accuracy)} m${heading === null ? "" : `, heading ${formatHeading(heading)}`}.`,
      );
    },
    (error) => {
      const hadFix = lastGpsFix !== null;
      stopGpsTracking();
      if (hadFix) {
        setStatus(error.message || "GPS tracking stopped.", "error");
        return;
      }
      setStatus(error.message || "GPS location failed.", "error");
    },
    {
      enableHighAccuracy: true,
      maximumAge: 5_000,
      timeout: 15_000,
    },
  );
}

function stopGpsTracking() {
  if (gpsWatchId !== null && navigator.geolocation) {
    navigator.geolocation.clearWatch(gpsWatchId);
  }
  gpsWatchId = null;
  lastGpsFix = null;
  lastGpsHeading = null;
  setGpsTrackingState(false);
}

function setGpsTrackingState(isTracking) {
  dom.locateButton.textContent = isTracking ? "Stop GPS" : "Use GPS";
  dom.locateButton.classList.toggle("active", isTracking);
}

function updatePoint(latitude, longitude, source, accuracy, moveMap, heading = null) {
  currentPoint = { latitude, longitude, source };
  dom.latitudeInput.value = latitude.toFixed(6);
  dom.longitudeInput.value = longitude.toFixed(6);
  dom.accuracyText.textContent = formatLocationMeta(source, accuracy, heading);

  const latLng = [latitude, longitude];
  if (!gpsMarker) {
    gpsMarker = L.marker(latLng, { icon: pointIcon, zIndexOffset: 1000 }).addTo(map);
  } else {
    gpsMarker.setLatLng(latLng);
  }
  updateGpsHeadingVisual(heading);

  if (accuracy) {
    if (!accuracyCircle) {
      accuracyCircle = L.circle(latLng, {
        radius: accuracy,
        color: "#1d6f4c",
        fillColor: "#1d6f4c",
        fillOpacity: 0.08,
        weight: 1,
      }).addTo(map);
    } else {
      accuracyCircle.setLatLng(latLng);
      accuracyCircle.setRadius(accuracy);
    }
  } else if (accuracyCircle) {
    accuracyCircle.remove();
    accuracyCircle = null;
  }

  if (moveMap) {
    map.setView(latLng, Math.max(map.getZoom(), 17));
  }

  updateSourceStatus();
}

function updateGpsHeadingVisual(heading) {
  const cone = gpsMarker?.getElement()?.querySelector(".gps-marker__cone");
  if (!cone) {
    return;
  }

  if (heading === null) {
    cone.style.opacity = "0";
    cone.style.transform = "rotate(0deg)";
    return;
  }

  cone.style.opacity = "1";
  cone.style.transform = `rotate(${heading}deg)`;
}

function formatLocationMeta(source, accuracy, heading) {
  const parts = [`${source} point.`];

  if (accuracy) {
    parts.push(`Browser accuracy is about ${Math.round(accuracy)} m.`);
  }

  if (heading !== null) {
    parts.push(`Heading ${formatHeading(heading)}.`);
  }

  return parts.join(" ");
}

function formatHeading(heading) {
  return `${Math.round(normalizeHeading(heading))}\u00b0`;
}

function resolveGpsHeading(position, previousFix, previousHeading) {
  const reportedHeading = normalizeHeadingValue(position?.coords?.heading);
  const speed = Number(position?.coords?.speed);

  if (reportedHeading !== null && (!Number.isFinite(speed) || speed >= 0.75)) {
    return reportedHeading;
  }

  if (!previousFix) {
    return previousHeading;
  }

  const distance = metersBetweenPoints(
    previousFix.coords.latitude,
    previousFix.coords.longitude,
    position.coords.latitude,
    position.coords.longitude,
  );

  if (distance < 2) {
    return previousHeading;
  }

  return bearingBetweenPoints(
    previousFix.coords.latitude,
    previousFix.coords.longitude,
    position.coords.latitude,
    position.coords.longitude,
  );
}

function normalizeHeadingValue(value) {
  return Number.isFinite(value) && value >= 0 ? normalizeHeading(value) : null;
}

function normalizeHeading(value) {
  return (value % 360 + 360) % 360;
}

function bearingBetweenPoints(startLatitude, startLongitude, endLatitude, endLongitude) {
  const startLat = startLatitude * Math.PI / 180;
  const endLat = endLatitude * Math.PI / 180;
  const deltaLon = (endLongitude - startLongitude) * Math.PI / 180;
  const y = Math.sin(deltaLon) * Math.cos(endLat);
  const x =
    Math.cos(startLat) * Math.sin(endLat) -
    Math.sin(startLat) * Math.cos(endLat) * Math.cos(deltaLon);

  return normalizeHeading(Math.atan2(y, x) * 180 / Math.PI);
}

function metersBetweenPoints(startLatitude, startLongitude, endLatitude, endLongitude) {
  const earthRadiusMeters = 6_371_000;
  const startLat = startLatitude * Math.PI / 180;
  const endLat = endLatitude * Math.PI / 180;
  const deltaLat = (endLatitude - startLatitude) * Math.PI / 180;
  const deltaLon = (endLongitude - startLongitude) * Math.PI / 180;
  const a =
    Math.sin(deltaLat / 2) * Math.sin(deltaLat / 2) +
    Math.cos(startLat) * Math.cos(endLat) * Math.sin(deltaLon / 2) * Math.sin(deltaLon / 2);

  return 2 * earthRadiusMeters * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
}

async function queryParcels() {
  persistSettings();
  clearSelectedParcel();
  setBusy(true);
  setStatus("Loading parcels for the visible map...");

  try {
    const bounds = map.getBounds();
    const serviceUrl = dom.serviceUrlInput.value.trim();
    const limit = clampNumber(Number(dom.limitInput.value), 1, 500);
    const response = await fetch("/api/parcels/area", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        serviceUrl,
        north: bounds.getNorth(),
        south: bounds.getSouth(),
        east: bounds.getEast(),
        west: bounds.getWest(),
        limit,
      }),
    });
    const body = await response.json();

    if (!response.ok) {
      throw new Error(body.error || "Visible parcel query failed.");
    }

    renderParcels(body.geojson, { fitBounds: false, autoSelectFirst: false });
    applySourceResponse(body);
    const count = body.geojson?.features?.length || 0;
    if (!count) {
      setStatus("No parcel features matched this view.");
    } else if (count >= limit) {
      setStatus(`Loaded ${count} visible parcel features. Zoom in or raise the parcel cap if this view is clipped.`);
    } else {
      setStatus(`${count} visible parcel feature${count === 1 ? "" : "s"} loaded. Tap a parcel for details.`);
    }
  } catch (error) {
    if (!navigator.onLine) {
      const cachedEntry = findBestAreaCache();
      if (cachedEntry && loadCachedAreaEntry(cachedEntry, "Offline. Loaded cached parcels for this view.")) {
        return;
      }

      clearParcelLayer();
      setStatus("Offline and no cached parcel area matches this view. Cache the area before airplane mode.", "error");
      return;
    }

    clearParcelLayer();
    setStatus(error instanceof Error ? error.message : "Visible parcel query failed.", "error");
  } finally {
    setBusy(false);
  }
}

async function cacheVisibleArea() {
  persistSettings();
  clearSelectedParcel();
  setBusy(true);
  setStatus("Downloading parcels for visible area...");

  try {
    const bounds = map.getBounds();
    const center = bounds.getCenter();
    const serviceUrl = dom.serviceUrlInput.value.trim();
    const effectiveServiceUrl = getEffectiveServiceUrl(center.lat, center.lng);
    const response = await fetch("/api/parcels/area", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        serviceUrl,
        north: bounds.getNorth(),
        south: bounds.getSouth(),
        east: bounds.getEast(),
        west: bounds.getWest(),
        limit: Number(dom.cacheLimitInput.value),
      }),
    });
    const body = await response.json();

    if (!response.ok) {
      throw new Error(body.error || "Area cache query failed.");
    }

    renderParcels(body.geojson, { fitBounds: false, autoSelectFirst: false });
    applySourceResponse(body);
    const entry = saveAreaCache({
      bounds: body.bounds || boundsToObject(bounds),
      geojson: body.geojson,
      serviceUrl: body.serviceUrl || effectiveServiceUrl,
    });
    const count = body.geojson?.features?.length || 0;
    setStatus(`${count} parcel feature${count === 1 ? "" : "s"} cached for this view.`);
    setCacheStatus(`Saved ${entry.featureCount} parcels. Cache size ${formatBytes(entry.bytes)}.`);
  } catch (error) {
    setStatus(error instanceof Error ? error.message : "Area cache failed.", "error");
  } finally {
    setBusy(false);
    updateCacheStatus();
  }
}

function loadCachedArea() {
  const entry = findBestAreaCache();

  if (!entry) {
    setStatus("No cached area matches this parcel source and map location.", "error");
    return;
  }

  if (!loadCachedAreaEntry(entry)) {
    setStatus("Cached area could not be loaded.", "error");
  }
}

function clearAreaCache() {
  const index = getAreaCacheIndex();
  for (const entry of index) {
    window.localStorage.removeItem(entry.key);
  }
  window.localStorage.removeItem(areaCacheIndexKey);
  updateCacheStatus();
  setStatus("Area cache cleared.");
}

async function cacheVisibleTiles() {
  persistSettings();

  if (!("caches" in window)) {
    setStatus("Cache Storage is not available in this browser.", "error");
    return;
  }

  setBusy(true);
  setStatus(`Downloading ${getBasemap().label.toLowerCase()} tiles for visible area...`);

  try {
    await registerServiceWorker();
    const urls = visibleTileUrls();

    if (urls.length > maxTileDownloadCount) {
      throw new Error(
        `Visible map needs ${urls.length} tiles. Zoom in or reduce zoom levels below ${maxTileDownloadCount} tiles.`,
      );
    }

    const cache = await caches.open(tileCacheName);
    let cached = 0;
    let skipped = 0;
    let failed = 0;

    for (const [index, url] of urls.entries()) {
      const request = new Request(url, { mode: "no-cors" });
      const existing = await cache.match(request, { ignoreVary: true });

      if (existing) {
        skipped += 1;
      } else {
        try {
          const response = await fetch(request);
          if (response.ok || response.type === "opaque") {
            await cache.put(request, response);
            cached += 1;
          } else {
            failed += 1;
          }
        } catch {
          failed += 1;
        }
      }

      if (index % 10 === 0 || index === urls.length - 1) {
        setTileCacheStatus(`Tile cache progress: ${index + 1} / ${urls.length}.`);
      }
    }

    await updateTileCacheStatus();
    setStatus(`${getBasemap().label} tiles cached. New ${cached}, already cached ${skipped}, failed ${failed}.`, failed ? "error" : "");
  } catch (error) {
    setStatus(error instanceof Error ? error.message : "Map tile cache failed.", "error");
  } finally {
    setBusy(false);
  }
}

async function clearTileCache() {
  if (!("caches" in window)) {
    setStatus("Cache Storage is not available in this browser.", "error");
    return;
  }

  await caches.delete(tileCacheName);
  await updateTileCacheStatus();
  setStatus("Map tile cache cleared.");
}

function renderParcels(geojson, { fitBounds = true, autoSelectFirst = true } = {}) {
  clearParcelLayer();

  parcelLayer = L.geoJSON(geojson, {
    style: baseParcelStyle,
    onEachFeature(feature, layer) {
      layer.on("click", () => selectParcel(feature, layer));
      const summary = getParcelSummary(feature.properties || {});
      layer.bindPopup(`<p class="popup-title">${escapeHtml(summary.title)}</p><p class="popup-meta">${escapeHtml(summary.subtitle)}</p>`);
    },
  }).addTo(map);

  if (parcelLayer.getLayers().length > 0) {
    if (fitBounds) {
      map.fitBounds(parcelLayer.getBounds(), { padding: [24, 24], maxZoom: 19 });
    }
    if (autoSelectFirst) {
      const firstLayer = parcelLayer.getLayers()[0];
      selectParcel(firstLayer.feature, firstLayer);
    }
  }
}

function loadCachedAreaEntry(entry, statusMessage = "") {
  try {
    const cached = readCachedAreaEntry(entry);
    renderParcels(cached.geojson, { fitBounds: false, autoSelectFirst: false });
    setStatus(
      statusMessage || `Loaded ${entry.featureCount} cached parcel feature${entry.featureCount === 1 ? "" : "s"}.`,
    );
    setCacheStatus(`Loaded cache from ${new Date(entry.fetchedAt).toLocaleString()}.`);
    return true;
  } catch {
    return false;
  }
}

function readCachedAreaEntry(entry) {
  const cached = JSON.parse(window.localStorage.getItem(entry.key) || "null");

  if (!cached?.geojson) {
    throw new Error("Cached area is missing parcel data.");
  }

  return cached;
}

function saveAreaCache({ bounds, geojson, serviceUrl }) {
  const normalizedBounds = normalizeBounds(bounds);
  const data = {
    bounds: normalizedBounds,
    fetchedAt: new Date().toISOString(),
    geojson,
    schemaVersion: 1,
    serviceUrl: normalizeServiceUrl(serviceUrl),
  };
  const serialized = JSON.stringify(data);
  const key = `${areaCachePrefix}${hashString(`${data.serviceUrl}:${JSON.stringify(normalizedBounds)}`)}`;
  const entry = {
    bounds: normalizedBounds,
    bytes: serialized.length,
    featureCount: Array.isArray(geojson?.features) ? geojson.features.length : 0,
    fetchedAt: data.fetchedAt,
    key,
    serviceUrl: data.serviceUrl,
  };

  const write = () => {
    window.localStorage.setItem(key, serialized);
    setAreaCacheIndex([entry, ...getAreaCacheIndex().filter((item) => item.key !== key)]);
  };

  try {
    write();
  } catch (error) {
    pruneAreaCache(2);
    write();
  }

  pruneAreaCache();
  return entry;
}

function getAreaCacheIndex() {
  try {
    const parsed = JSON.parse(window.localStorage.getItem(areaCacheIndexKey) || "[]");
    return Array.isArray(parsed) ? parsed.filter(isValidCacheEntry) : [];
  } catch {
    return [];
  }
}

function setAreaCacheIndex(index) {
  window.localStorage.setItem(areaCacheIndexKey, JSON.stringify(index.slice(0, maxAreaCacheEntries)));
}

function pruneAreaCache(extraEntries = 0) {
  const index = getAreaCacheIndex().sort((first, second) => second.fetchedAt.localeCompare(first.fetchedAt));
  const keepCount = Math.max(0, maxAreaCacheEntries - extraEntries);
  const keep = index.slice(0, keepCount);
  const remove = index.slice(keepCount);

  for (const entry of remove) {
    window.localStorage.removeItem(entry.key);
  }

  setAreaCacheIndex(keep);
}

function findBestAreaCache() {
  const point = map.getCenter();
  const serviceUrl = normalizeServiceUrl(getEffectiveServiceUrl(point.lat, point.lng));
  if (!serviceUrl) {
    return null;
  }

  const entries = getAreaCacheIndex()
    .filter((entry) => entry.serviceUrl === serviceUrl)
    .filter((entry) => containsPoint(entry.bounds, point.lat, point.lng))
    .sort((first, second) => {
      const areaDelta = boundsArea(first.bounds) - boundsArea(second.bounds);
      return areaDelta || second.fetchedAt.localeCompare(first.fetchedAt);
    });

  return entries[0] || null;
}

function updateCacheStatus() {
  const index = getAreaCacheIndex();
  const totalBytes = index.reduce((total, entry) => total + Number(entry.bytes || 0), 0);
  const totalFeatures = index.reduce((total, entry) => total + Number(entry.featureCount || 0), 0);
  dom.cacheCountInput.value = String(index.length);
  dom.cacheStatus.textContent = index.length
    ? `${index.length} area${index.length === 1 ? "" : "s"} cached, ${totalFeatures} parcels, ${formatBytes(totalBytes)}.`
    : "No cached areas yet.";
}

async function updateTileCacheStatus() {
  if (!("caches" in window)) {
    setTileCacheStatus("Cache Storage is not available in this browser.");
    return;
  }

  const cache = await caches.open(tileCacheName);
  const keys = await cache.keys();
  dom.tileCacheCountInput.value = String(keys.length);
  setTileCacheStatus(
    keys.length ? `${keys.length} basemap tile${keys.length === 1 ? "" : "s"} cached.` : "No map tiles cached yet.",
  );
}

function setTileCacheStatus(message) {
  dom.tileCacheStatus.textContent = message;
}

function getEffectiveServiceUrl(latitude = currentPoint.latitude, longitude = currentPoint.longitude) {
  const manualServiceUrl = dom.serviceUrlInput.value.trim();
  if (manualServiceUrl) {
    return manualServiceUrl;
  }

  return resolveAutomaticParcelSource(latitude, longitude)?.serviceUrl || "";
}

function updateSourceStatus() {
  const manualServiceUrl = dom.serviceUrlInput.value.trim();
  if (manualServiceUrl) {
    dom.sourceStatus.textContent = "Manual parcel source selected.";
    return;
  }

  const source = resolveAutomaticParcelSource(currentPoint.latitude, currentPoint.longitude);
  dom.sourceStatus.textContent = source
    ? `Auto source: ${source.name}.`
    : "No automatic source for this point. Paste a public ArcGIS parcel layer URL.";
}

function applySourceResponse(body) {
  if (body?.sourceName) {
    dom.sourceStatus.textContent = `Using ${body.sourceName}.`;
  }
}

function resolveAutomaticParcelSource(latitude, longitude) {
  return automaticParcelSources.find((source) => containsPoint(source.bounds, latitude, longitude)) || null;
}

function isValidCacheEntry(entry) {
  return (
    entry &&
    typeof entry.key === "string" &&
    entry.key.startsWith(areaCachePrefix) &&
    typeof entry.serviceUrl === "string" &&
    typeof entry.fetchedAt === "string" &&
    entry.bounds &&
    Number.isFinite(entry.bounds.north) &&
    Number.isFinite(entry.bounds.south) &&
    Number.isFinite(entry.bounds.east) &&
    Number.isFinite(entry.bounds.west)
  );
}

function boundsToObject(bounds) {
  return {
    east: bounds.getEast(),
    north: bounds.getNorth(),
    south: bounds.getSouth(),
    west: bounds.getWest(),
  };
}

function normalizeBounds(bounds) {
  return {
    east: roundCoordinate(Number(bounds.east)),
    north: roundCoordinate(Number(bounds.north)),
    south: roundCoordinate(Number(bounds.south)),
    west: roundCoordinate(Number(bounds.west)),
  };
}

function roundCoordinate(value) {
  return Math.round(value * 100000) / 100000;
}

function normalizeServiceUrl(value) {
  return value.trim().replace(/\/query\/?$/i, "").replace(/\/+$/, "");
}

function containsPoint(bounds, latitude, longitude) {
  return latitude <= bounds.north && latitude >= bounds.south && longitude <= bounds.east && longitude >= bounds.west;
}

function boundsArea(bounds) {
  return Math.abs((bounds.north - bounds.south) * (bounds.east - bounds.west));
}

function formatBytes(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return "0 B";
  }

  if (bytes < 1024) {
    return `${bytes} B`;
  }

  const kib = bytes / 1024;
  if (kib < 1024) {
    return `${kib.toFixed(1)} KiB`;
  }

  return `${(kib / 1024).toFixed(1)} MiB`;
}

function visibleTileUrls() {
  const basemap = getBasemap();
  const zoom = Math.round(map.getZoom());
  const zoomLevels = clampNumber(Number(dom.tileZoomLevelsInput.value), 1, 3);
  const bounds = map.getBounds();
  const urls = [];

  for (let currentZoom = zoom; currentZoom < zoom + zoomLevels && currentZoom <= basemap.maxZoom; currentZoom += 1) {
    const tileZoom = Math.min(currentZoom, basemap.maxNativeZoom || currentZoom);
    const northwest = latLngToTile(bounds.getNorth(), bounds.getWest(), tileZoom);
    const southeast = latLngToTile(bounds.getSouth(), bounds.getEast(), tileZoom);
    const minX = Math.min(northwest.x, southeast.x);
    const maxX = Math.max(northwest.x, southeast.x);
    const minY = Math.min(northwest.y, southeast.y);
    const maxY = Math.max(northwest.y, southeast.y);

    for (let x = minX; x <= maxX; x += 1) {
      for (let y = minY; y <= maxY; y += 1) {
        urls.push(
          basemap.tileUrlTemplate
            .replace("{z}", String(tileZoom))
            .replace("{x}", String(x))
            .replace("{y}", String(y)),
        );
      }
    }
  }

  return Array.from(new Set(urls));
}

function latLngToTile(latitude, longitude, zoom) {
  const latRad = latitude * Math.PI / 180;
  const tileCount = 2 ** zoom;
  const x = Math.floor((longitude + 180) / 360 * tileCount);
  const y = Math.floor((1 - Math.log(Math.tan(latRad) + 1 / Math.cos(latRad)) / Math.PI) / 2 * tileCount);

  return {
    x: clampNumber(x, 0, tileCount - 1),
    y: clampNumber(y, 0, tileCount - 1),
  };
}

function clampNumber(value, minimum, maximum) {
  return Math.min(Math.max(Number.isFinite(value) ? value : minimum, minimum), maximum);
}

function hashString(value) {
  let hash = 2166136261;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0).toString(36);
}

function clearParcelLayer() {
  selectedLayer = null;
  if (parcelLayer) {
    parcelLayer.remove();
    parcelLayer = null;
  }
}

function clearSelectedParcel() {
  selectedLayer = null;
  dom.parcelSummary.classList.add("empty");
  dom.parcelSummary.textContent = "No parcel selected.";
  dom.attributeTable.replaceChildren();
}

function baseParcelStyle() {
  return {
    color: "#1d6f4c",
    fillColor: "#b7e57c",
    fillOpacity: 0.18,
    weight: 2,
  };
}

function selectedParcelStyle() {
  return {
    color: "#0f4f34",
    fillColor: "#b7e57c",
    fillOpacity: 0.32,
    weight: 4,
  };
}

function selectParcel(feature, layer) {
  if (selectedLayer) {
    selectedLayer.setStyle(baseParcelStyle());
  }

  selectedLayer = layer;
  selectedLayer.setStyle(selectedParcelStyle());
  selectedLayer.bringToFront();
  renderParcelDetails(feature.properties || {});
}

function renderParcelDetails(properties) {
  const summary = getParcelSummary(properties);
  dom.parcelSummary.classList.remove("empty");
  dom.parcelSummary.replaceChildren(
    summaryRow("Parcel", summary.parcelId),
    summaryRow("Owner", summary.owner),
    summaryRow("Address", summary.address),
    summaryRow("Use", summary.use),
  );

  dom.attributeTable.replaceChildren(
    ...Object.entries(properties)
      .filter(([, value]) => value !== null && value !== undefined && value !== "")
      .sort(([first], [second]) => first.localeCompare(second))
      .map(([key, value]) => {
        const row = document.createElement("div");
        row.className = "attribute-row";

        const keyEl = document.createElement("div");
        keyEl.className = "attribute-key";
        keyEl.textContent = key;

        const valueEl = document.createElement("div");
        valueEl.className = "attribute-value";
        valueEl.textContent = String(value);

        row.append(keyEl, valueEl);
        return row;
      }),
  );
}

function summaryRow(label, value) {
  const row = document.createElement("div");
  row.className = "summary-row";

  const labelEl = document.createElement("div");
  labelEl.className = "summary-label";
  labelEl.textContent = label;

  const valueEl = document.createElement("div");
  valueEl.className = "summary-value";
  valueEl.textContent = value || "Unknown";

  row.append(labelEl, valueEl);
  return row;
}

function getParcelSummary(properties) {
  const parcelId = pickField(properties, ["PARCELID", "PARCEL_ID", "APN", "PIN", "PID", "PARCEL", "LOWPARCELID", "ACCOUNT"]);
  const owner = pickField(properties, [
    "OWNERNME1",
    "OWNERNAME",
    "OWNER_NAME",
    "OWNER",
    "OWNNAME",
    "CNVYNAME",
    "MAILNME1",
  ]);
  const address = pickField(properties, [
    "SITEADDRESS",
    "SITUSADDRESS",
    "SITUS_ADDR",
    "PROPERTYADDRESS",
    "ADDRESS",
    "PROPADDR",
    "PRPRTYDSCRP",
  ]);
  const use = pickField(properties, [
    "CLASSDSCRP",
    "PCLASSDSCRP",
    "LANDUSE",
    "LAND_USE",
    "USE_DESC",
    "ZONING",
    "ZONE",
  ]);

  return {
    address,
    owner,
    parcelId,
    title: parcelId || owner || "Parcel",
    subtitle: [owner, address].filter(Boolean).join(" / ") || "Open details in the side panel.",
    use,
  };
}

function pickField(properties, candidates) {
  const entries = Object.entries(properties);

  for (const candidate of candidates) {
    const exact = entries.find(([key]) => key.toLowerCase() === candidate.toLowerCase());
    if (exact && exact[1] !== null && exact[1] !== undefined && exact[1] !== "") {
      return String(exact[1]);
    }
  }

  const fuzzy = entries.find(([key, value]) => {
    const lower = key.toLowerCase();
    return candidates.some((candidate) => lower.includes(candidate.toLowerCase())) && value;
  });

  return fuzzy ? String(fuzzy[1]) : "";
}

function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

init();

# Property Lines

A small GPS-first parcel viewer for checking approximate property boundaries.

The app uses browser geolocation, OpenStreetMap base tiles, and a server-side
ArcGIS FeatureServer/MapServer parcel-layer adapter. It can choose a known
public parcel layer from the current location, or query a manually supplied
county or city parcel layer through the local server and display the matching
parcel geometry and attributes.

Visible map areas can be downloaded into browser `localStorage` for later use.
The cache stores the parcel GeoJSON returned by the active ArcGIS source and is
kept to a small rolling set of areas because browser storage is limited.

OpenStreetMap basemap tiles can also be cached for the visible map. Tile
caching uses the browser Cache Storage API plus a service worker, which lets the
app serve cached tiles when the network is unavailable.

The app shell itself is also precached by the service worker, so after one
successful online load the UI can reopen with no network, including airplane
mode.

This is not a survey tool. Parcel GIS layers can be stale, generalized, or
misaligned with field conditions. Confirm legal boundaries with the county
recorder, assessor, recorded plat, deed, or a licensed surveyor.

## Local Development

```bash
go build -o bin/property-lines ./cmd/property-lines
PORT=4190 ./bin/property-lines
```

Open `http://127.0.0.1:4190/`.

## Map Render Check

Run the screenshot smoke check before shipping map changes:

```bash
./scripts/check-map-render.sh
```

It starts a local test server, opens headless Chromium, captures desktop and
mobile screenshots, exercises the parcel query route against a mock server
response, then reloads once in offline mode. It fails if the Leaflet stylesheet
drops out, the map collapses back into blank blocks, the parcel query path
regresses, or the shell cannot reopen without network. The test logs screenshot
directories under `/tmp/property-lines-map-check/...`,
`/tmp/property-lines-parcel-query-check/...`, and
`/tmp/property-lines-offline-check/...`.

## Deployment

Browser GPS requires a secure context, so serve the app over HTTPS for phones
and tablets.

This repo includes generic deployment templates:

- `Caddyfile`: example reverse-proxy config
- `property-lines.service`: example systemd unit

Adjust the hostname, user, paths, and TLS configuration for your host before
using them directly.

## Parcel Sources

For New York locations, the app defaults to NYS Tax Parcels Public:

```text
https://gisservices.its.ny.gov/arcgis/rest/services/NYS_Tax_Parcels_Public/MapServer/1
```

For other places, paste a public ArcGIS parcel layer URL such as:

```text
https://gis.franklincountyohio.gov/hosting/rest/services/ParcelFeatures/Parcel_Features/FeatureServer/0
```

For broad nationwide coverage, wire in a provider such as Regrid later. The
current server route is intentionally a proxy so API keys can stay server-side.

## Area Cache

1. Zoom into the area you want to keep.
2. Click `Cache Visible Area`.
3. Later, pan back into that area and click `Load Cached Area`.

The server rejects visible-area cache requests over 5 km wide or tall to avoid
accidentally pulling huge parcel datasets into browser storage.

## Basemap Tile Cache

1. Zoom into the area you want to keep.
2. Pick how many zoom levels to cache.
3. Click `Cache Map Tiles`.

The app caps explicit tile downloads to avoid accidentally bulk-downloading map
tiles. Browsed OpenStreetMap tiles are also cached opportunistically by the
service worker.

## Airplane Mode

After one online visit, the app shell should reopen offline because the service
worker precaches the HTML, CSS, JS, manifest, and Leaflet assets.

For parcel data and basemap content, offline use still depends on what you saved
ahead of time:

1. Open the app once while online so the shell installs into the browser cache.
2. Use `Cache Visible Area` anywhere you want parcel geometry available offline.
3. Use `Cache Map Tiles` for the same area if you want the basemap visible too.
4. In airplane mode, the app can reopen and `Load Parcels` will fall back to
   the best cached area for the current view when live requests are unavailable.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

type viewportSpec struct {
	Name            string
	Width           int64
	Height          int64
	DeviceScale     float64
	Mobile          bool
	MinVisibleTiles int
}

type rectSnapshot struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type viewportSnapshot struct {
	Viewport struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"viewport"`
	StyleSheetCount        int           `json:"styleSheetCount"`
	LeafletSheetLoaded     bool          `json:"leafletSheetLoaded"`
	MapRect                *rectSnapshot `json:"mapRect"`
	TileContainerRect      *rectSnapshot `json:"tileContainerRect"`
	FirstTilePosition      string        `json:"firstTilePosition"`
	VisibleLoadedTileCount int           `json:"visibleLoadedTileCount"`
	VisibleBrokenTileCount int           `json:"visibleBrokenTileCount"`
	TotalTileCount         int           `json:"totalTileCount"`
}

func TestMapRenderSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("screenshot smoke test is disabled in short mode")
	}

	browserPath, err := findBrowserBinary()
	if err != nil {
		t.Skipf("screenshot smoke test skipped: %v", err)
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	restoreCWD, err := chdir(repoRoot)
	if err != nil {
		t.Fatalf("change directory: %v", err)
	}
	defer restoreCWD()

	server := httptest.NewServer(newTestMux())
	defer server.Close()

	artifactDir := filepath.Join(
		os.TempDir(),
		"property-lines-map-check",
		time.Now().UTC().Format("20060102-150405"),
	)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("create artifact dir: %v", err)
	}
	t.Logf("map screenshots: %s", artifactDir)

	viewports := []viewportSpec{
		{
			Name:            "desktop",
			Width:           1440,
			Height:          900,
			DeviceScale:     1,
			Mobile:          false,
			MinVisibleTiles: 6,
		},
		{
			Name:            "mobile",
			Width:           430,
			Height:          932,
			DeviceScale:     3,
			Mobile:          true,
			MinVisibleTiles: 4,
		},
	}

	for _, viewport := range viewports {
		viewport := viewport
		t.Run(viewport.Name, func(t *testing.T) {
			snapshot, screenshotPath, err := runViewportCheck(server.URL, browserPath, artifactDir, viewport)
			if err != nil {
				t.Fatalf("%s map check failed: %v", viewport.Name, err)
			}

			if !snapshot.LeafletSheetLoaded {
				t.Fatalf("%s Leaflet stylesheet was not loaded; screenshot: %s", viewport.Name, screenshotPath)
			}
			if snapshot.FirstTilePosition != "absolute" {
				t.Fatalf("%s tile positioning regressed to %q; screenshot: %s", viewport.Name, snapshot.FirstTilePosition, screenshotPath)
			}
			if snapshot.MapRect == nil || snapshot.MapRect.Width < 100 || snapshot.MapRect.Height < 100 {
				t.Fatalf("%s map rect is invalid: %+v; screenshot: %s", viewport.Name, snapshot.MapRect, screenshotPath)
			}
			if snapshot.MapRect.Height > snapshot.Viewport.Height*1.6 {
				t.Fatalf("%s map height exploded to %.1f for viewport %.1f; screenshot: %s", viewport.Name, snapshot.MapRect.Height, snapshot.Viewport.Height, screenshotPath)
			}
			if snapshot.VisibleLoadedTileCount < viewport.MinVisibleTiles {
				t.Fatalf("%s only rendered %d visible tiles; screenshot: %s", viewport.Name, snapshot.VisibleLoadedTileCount, screenshotPath)
			}
		})
	}
}

func TestSatelliteBasemapSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("satellite basemap smoke test is disabled in short mode")
	}

	browserPath, err := findBrowserBinary()
	if err != nil {
		t.Skipf("satellite basemap smoke test skipped: %v", err)
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	restoreCWD, err := chdir(repoRoot)
	if err != nil {
		t.Fatalf("change directory: %v", err)
	}
	defer restoreCWD()

	server := httptest.NewServer(newTestMux())
	defer server.Close()

	artifactDir := filepath.Join(
		os.TempDir(),
		"property-lines-satellite-check",
		time.Now().UTC().Format("20060102-150405"),
	)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("create artifact dir: %v", err)
	}
	t.Logf("satellite screenshots: %s", artifactDir)

	viewport := viewportSpec{
		Name:            "satellite",
		Width:           1440,
		Height:          900,
		DeviceScale:     1,
		Mobile:          false,
		MinVisibleTiles: 6,
	}

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("mute-audio", true),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	defer cancelAlloc()

	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	defer cancelCtx()

	ctx, cancelTimeout := context.WithTimeout(ctx, 45*time.Second)
	defer cancelTimeout()

	url := fmt.Sprintf("%s/?satellitecheck=%d", server.URL, time.Now().UnixNano())
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(viewport.Width, viewport.Height, viewport.DeviceScale, viewport.Mobile),
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#map`, chromedp.ByID),
		chromedp.Click(`#satelliteLayerButton`, chromedp.ByID),
	); err != nil {
		t.Fatalf("satellite basemap setup failed: %v", err)
	}

	loaded, broken, sampleSrc, err := waitForSatelliteTiles(ctx, viewport.MinVisibleTiles)
	if err != nil {
		t.Fatalf("satellite basemap failed: %v", err)
	}
	if broken > 0 {
		t.Fatalf("satellite basemap has %d broken visible tiles; sample: %s", broken, sampleSrc)
	}
	if !strings.Contains(sampleSrc, "/tile/16/") {
		t.Fatalf("satellite basemap did not use overzoomed native level 16 tiles; sample: %s", sampleSrc)
	}
	if loaded < viewport.MinVisibleTiles {
		t.Fatalf("satellite basemap only loaded %d visible tiles; sample: %s", loaded, sampleSrc)
	}

	var pngBytes []byte
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		pngBytes, err = page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatPng).Do(ctx)
		return err
	})); err != nil {
		t.Fatalf("capture satellite screenshot failed: %v", err)
	}

	screenshotPath := filepath.Join(artifactDir, "satellite.png")
	if err := os.WriteFile(screenshotPath, pngBytes, 0o644); err != nil {
		t.Fatalf("write satellite screenshot failed: %v", err)
	}
}

func TestOfflineShellSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("offline shell smoke test is disabled in short mode")
	}

	browserPath, err := findBrowserBinary()
	if err != nil {
		t.Skipf("offline shell smoke test skipped: %v", err)
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	restoreCWD, err := chdir(repoRoot)
	if err != nil {
		t.Fatalf("change directory: %v", err)
	}
	defer restoreCWD()

	server := httptest.NewServer(newTestMux())
	defer server.Close()

	artifactDir := filepath.Join(
		os.TempDir(),
		"property-lines-offline-check",
		time.Now().UTC().Format("20060102-150405"),
	)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("create artifact dir: %v", err)
	}
	t.Logf("offline screenshots: %s", artifactDir)

	viewport := viewportSpec{
		Name:            "offline-desktop",
		Width:           1440,
		Height:          900,
		DeviceScale:     1,
		Mobile:          false,
		MinVisibleTiles: 6,
	}

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("mute-audio", true),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	defer cancelAlloc()

	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	defer cancelCtx()

	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	url := fmt.Sprintf("%s/?offline-check=%d", server.URL, time.Now().UnixNano())
	if err := chromedp.Run(ctx,
		network.Enable(),
		emulation.SetDeviceMetricsOverride(viewport.Width, viewport.Height, viewport.DeviceScale, viewport.Mobile),
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#map`, chromedp.ByID),
	); err != nil {
		t.Fatalf("initial online load failed: %v", err)
	}

	var snapshot viewportSnapshot
	if err := waitForSnapshot(ctx, viewport, &snapshot); err != nil {
		t.Fatalf("initial online snapshot failed: %v", err)
	}

	if err := chromedp.Run(ctx,
		chromedp.Sleep(750*time.Millisecond),
		chromedp.Reload(),
		chromedp.WaitVisible(`#map`, chromedp.ByID),
	); err != nil {
		t.Fatalf("controlled online reload failed: %v", err)
	}
	if err := waitForServiceWorkerControl(ctx); err != nil {
		t.Fatalf("service worker did not take control: %v", err)
	}
	if err := waitForSnapshot(ctx, viewport, &snapshot); err != nil {
		t.Fatalf("controlled online snapshot failed: %v", err)
	}

	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return network.EmulateNetworkConditions(true, 0, 0, 0).
				WithConnectionType(network.ConnectionTypeNone).
				Do(ctx)
		}),
		chromedp.Navigate(fmt.Sprintf("%s/?offline-check=%d&airplane=1", server.URL, time.Now().UnixNano())),
		chromedp.WaitVisible(`#map`, chromedp.ByID),
	); err != nil {
		t.Fatalf("offline reload failed: %v", err)
	}

	if err := waitForSnapshot(ctx, viewport, &snapshot); err != nil {
		t.Fatalf("offline snapshot failed: %v", err)
	}

	if !snapshot.LeafletSheetLoaded {
		t.Fatalf("offline shell lost the Leaflet stylesheet")
	}
	if snapshot.FirstTilePosition != "absolute" {
		t.Fatalf("offline tile positioning regressed to %q", snapshot.FirstTilePosition)
	}
	if snapshot.VisibleLoadedTileCount < viewport.MinVisibleTiles {
		t.Fatalf("offline view only rendered %d visible tiles", snapshot.VisibleLoadedTileCount)
	}

	var pngBytes []byte
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		pngBytes, err = page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatPng).Do(ctx)
		return err
	})); err != nil {
		t.Fatalf("capture offline screenshot failed: %v", err)
	}

	screenshotPath := filepath.Join(artifactDir, "offline-desktop.png")
	if err := os.WriteFile(screenshotPath, pngBytes, 0o644); err != nil {
		t.Fatalf("write offline screenshot failed: %v", err)
	}

	snapshotPath := filepath.Join(artifactDir, "offline-desktop.json")
	prettySnapshot, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("marshal offline snapshot failed: %v", err)
	}
	if err := os.WriteFile(snapshotPath, prettySnapshot, 0o644); err != nil {
		t.Fatalf("write offline snapshot failed: %v", err)
	}
}

func TestBrowserParcelQuerySmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("browser parcel-query smoke test is disabled in short mode")
	}

	browserPath, err := findBrowserBinary()
	if err != nil {
		t.Skipf("browser parcel-query smoke test skipped: %v", err)
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	restoreCWD, err := chdir(repoRoot)
	if err != nil {
		t.Fatalf("change directory: %v", err)
	}
	defer restoreCWD()

	server := httptest.NewServer(newTestMux())
	defer server.Close()

	artifactDir := filepath.Join(
		os.TempDir(),
		"property-lines-parcel-query-check",
		time.Now().UTC().Format("20060102-150405"),
	)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("create artifact dir: %v", err)
	}
	t.Logf("parcel query screenshots: %s", artifactDir)

	viewport := viewportSpec{
		Name:            "parcel-query",
		Width:           1440,
		Height:          900,
		DeviceScale:     1,
		Mobile:          false,
		MinVisibleTiles: 6,
	}

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("mute-audio", true),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	defer cancelAlloc()

	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	defer cancelCtx()

	ctx, cancelTimeout := context.WithTimeout(ctx, 45*time.Second)
	defer cancelTimeout()

	url := fmt.Sprintf("%s/?parcelcheck=%d", server.URL, time.Now().UnixNano())
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(viewport.Width, viewport.Height, viewport.DeviceScale, viewport.Mobile),
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#map`, chromedp.ByID),
		chromedp.Click(`#queryButton`, chromedp.ByID),
	); err != nil {
		t.Fatalf("browser parcel-query setup failed: %v", err)
	}

	status, parcelPathCount, err := waitForParcelQuery(ctx)
	if err != nil {
		t.Fatalf("browser parcel-query failed: %v", err)
	}
	if parcelPathCount < 1 {
		t.Fatalf("browser parcel-query rendered no parcel paths; status: %s", status)
	}

	var pngBytes []byte
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		pngBytes, err = page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatPng).Do(ctx)
		return err
	})); err != nil {
		t.Fatalf("capture parcel-query screenshot failed: %v", err)
	}

	screenshotPath := filepath.Join(artifactDir, "parcel-query.png")
	if err := os.WriteFile(screenshotPath, pngBytes, 0o644); err != nil {
		t.Fatalf("write parcel-query screenshot failed: %v", err)
	}
}

func newTestMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("POST /api/parcels/area", handleMockParcelArea)
	mux.Handle("GET /", staticHandler())
	return mux
}

func handleMockParcelArea(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{
		"sourceName": "Mock Parcel API",
		"serviceUrl": "https://example.com/mock/parcel-layer/0",
		"bounds": {
			"north": 41.9342,
			"south": 41.9328,
			"east": -74.0179,
			"west": -74.0195
		},
		"geojson": {
			"type": "FeatureCollection",
			"features": [
				{
					"type": "Feature",
					"properties": {
						"OWNER": "Mock Owner One",
						"PARCELID": "PL-1001",
						"ADDRESS": "101 Sample Lane"
					},
					"geometry": {
						"type": "Polygon",
						"coordinates": [[
							[-74.01920, 41.93355],
							[-74.01885, 41.93355],
							[-74.01885, 41.93325],
							[-74.01920, 41.93325],
							[-74.01920, 41.93355]
						]]
					}
				},
				{
					"type": "Feature",
					"properties": {
						"OWNER": "Mock Owner Two",
						"PARCELID": "PL-1002",
						"ADDRESS": "103 Sample Lane"
					},
					"geometry": {
						"type": "Polygon",
						"coordinates": [[
							[-74.01882, 41.93352],
							[-74.01848, 41.93352],
							[-74.01848, 41.93323],
							[-74.01882, 41.93323],
							[-74.01882, 41.93352]
						]]
					}
				}
			]
		}
	}`))
}

func runViewportCheck(baseURL, browserPath, artifactDir string, viewport viewportSpec) (viewportSnapshot, string, error) {
	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("mute-audio", true),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	defer cancelAlloc()

	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	defer cancelCtx()

	ctx, cancelTimeout := context.WithTimeout(ctx, 45*time.Second)
	defer cancelTimeout()

	url := fmt.Sprintf("%s/?mapcheck=%d&viewport=%s", baseURL, time.Now().UnixNano(), viewport.Name)
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(viewport.Width, viewport.Height, viewport.DeviceScale, viewport.Mobile),
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#map`, chromedp.ByID),
	); err != nil {
		return viewportSnapshot{}, "", err
	}

	var snapshot viewportSnapshot
	if err := waitForSnapshot(ctx, viewport, &snapshot); err != nil {
		return viewportSnapshot{}, "", err
	}

	var pngBytes []byte
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		pngBytes, err = page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatPng).Do(ctx)
		return err
	})); err != nil {
		return viewportSnapshot{}, "", err
	}

	screenshotPath := filepath.Join(artifactDir, viewport.Name+".png")
	if err := os.WriteFile(screenshotPath, pngBytes, 0o644); err != nil {
		return viewportSnapshot{}, "", err
	}

	snapshotPath := filepath.Join(artifactDir, viewport.Name+".json")
	prettySnapshot, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return viewportSnapshot{}, screenshotPath, err
	}
	if err := os.WriteFile(snapshotPath, prettySnapshot, 0o644); err != nil {
		return viewportSnapshot{}, screenshotPath, err
	}

	return snapshot, screenshotPath, nil
}

func waitForSnapshot(ctx context.Context, viewport viewportSpec, snapshot *viewportSnapshot) error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		current, err := captureSnapshot(ctx)
		if err != nil {
			return err
		}
		*snapshot = current
		if snapshotReady(current, viewport) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}

	return fmt.Errorf("map did not settle: %+v", *snapshot)
}

func waitForSatelliteTiles(ctx context.Context, minVisibleTiles int) (int, int, string, error) {
	type satelliteSnapshot struct {
		Loaded    int    `json:"loaded"`
		Broken    int    `json:"broken"`
		SampleSrc string `json:"sampleSrc"`
	}

	deadline := time.Now().Add(15 * time.Second)
	last := satelliteSnapshot{}
	for time.Now().Before(deadline) {
		const expression = `JSON.stringify((() => {
			const visibleTiles = Array.from(document.querySelectorAll(".leaflet-tile"))
				.filter((img) => img.src.includes("basemap.nationalmap.gov"))
				.filter((img) => {
					const rect = img.getBoundingClientRect();
					return rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
				});
			return {
				loaded: visibleTiles.filter((img) => img.naturalWidth > 0).length,
				broken: visibleTiles.filter((img) => img.complete && img.naturalWidth === 0).length,
				sampleSrc: visibleTiles[0]?.src || "",
			};
		})())`

		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &raw)); err != nil {
			return 0, 0, "", err
		}
		if err := json.Unmarshal([]byte(raw), &last); err != nil {
			return 0, 0, "", err
		}
		if last.Loaded >= minVisibleTiles {
			return last.Loaded, last.Broken, last.SampleSrc, nil
		}

		time.Sleep(250 * time.Millisecond)
	}

	return last.Loaded, last.Broken, last.SampleSrc, fmt.Errorf(
		"satellite tiles did not settle (loaded=%d, broken=%d, sample=%q)",
		last.Loaded,
		last.Broken,
		last.SampleSrc,
	)
}

func waitForServiceWorkerControl(ctx context.Context) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var controlled bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(`Boolean(navigator.serviceWorker && navigator.serviceWorker.controller)`, &controlled)); err != nil {
			return err
		}
		if controlled {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}

	return fmt.Errorf("service worker controller never became active")
}

func waitForParcelQuery(ctx context.Context) (string, int, error) {
	deadline := time.Now().Add(15 * time.Second)
	lastStatus := ""
	lastParcelPathCount := 0
	for time.Now().Before(deadline) {
		var status string
		var parcelPathCount int
		if err := chromedp.Run(ctx,
			chromedp.Text(`#status`, &status, chromedp.ByID),
			chromedp.Evaluate(`document.querySelectorAll(".leaflet-overlay-pane svg path").length`, &parcelPathCount),
		); err != nil {
			return "", 0, err
		}
		lastStatus = status
		lastParcelPathCount = parcelPathCount

		normalizedStatus := strings.ToLower(strings.TrimSpace(status))
		if strings.Contains(normalizedStatus, "loaded") && parcelPathCount > 0 {
			return status, parcelPathCount, nil
		}
		if strings.Contains(normalizedStatus, "failed") || strings.Contains(normalizedStatus, "error") {
			return status, parcelPathCount, fmt.Errorf(status)
		}

		time.Sleep(250 * time.Millisecond)
	}

	return lastStatus, lastParcelPathCount, fmt.Errorf(
		"parcel query did not settle (status=%q, parcelPaths=%d)",
		lastStatus,
		lastParcelPathCount,
	)
}

func captureSnapshot(ctx context.Context) (viewportSnapshot, error) {
	const expression = `JSON.stringify((() => {
		const sheets = Array.from(document.styleSheets).map((sheet) => ({
			href: sheet.href || "",
			rules: (() => {
				try { return sheet.cssRules.length; } catch { return -1; }
			})(),
		}));
		const visibleTiles = Array.from(document.querySelectorAll(".leaflet-tile")).filter((img) => {
			const rect = img.getBoundingClientRect();
			return rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
		});
		const mapRect = document.querySelector("#map")?.getBoundingClientRect() || null;
		const tileContainerRect = document.querySelector(".leaflet-tile-container")?.getBoundingClientRect() || null;
		const firstVisibleTile = visibleTiles[0] || document.querySelector(".leaflet-tile");
		return {
			viewport: { width: innerWidth, height: innerHeight },
			styleSheetCount: sheets.length,
			leafletSheetLoaded: sheets.some((sheet) => sheet.href.endsWith("/vendor/leaflet/leaflet.css") && sheet.rules > 0),
			mapRect: mapRect ? { left: mapRect.left, top: mapRect.top, width: mapRect.width, height: mapRect.height } : null,
			tileContainerRect: tileContainerRect ? { left: tileContainerRect.left, top: tileContainerRect.top, width: tileContainerRect.width, height: tileContainerRect.height } : null,
			firstTilePosition: firstVisibleTile ? getComputedStyle(firstVisibleTile).position : "",
			visibleLoadedTileCount: visibleTiles.filter((img) => img.naturalWidth > 0).length,
			visibleBrokenTileCount: visibleTiles.filter((img) => img.complete && img.naturalWidth === 0).length,
			totalTileCount: document.querySelectorAll(".leaflet-tile").length,
		};
	})())`

	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &raw)); err != nil {
		return viewportSnapshot{}, err
	}

	var snapshot viewportSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return viewportSnapshot{}, err
	}

	return snapshot, nil
}

func snapshotReady(snapshot viewportSnapshot, viewport viewportSpec) bool {
	if !snapshot.LeafletSheetLoaded || snapshot.MapRect == nil || snapshot.TileContainerRect == nil {
		return false
	}
	if snapshot.FirstTilePosition != "absolute" {
		return false
	}
	if snapshot.MapRect.Width < 100 || snapshot.MapRect.Height < 100 {
		return false
	}
	if snapshot.MapRect.Height > snapshot.Viewport.Height*1.6 {
		return false
	}
	return snapshot.VisibleLoadedTileCount >= viewport.MinVisibleTiles
}

func resolveRepoRoot() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..")), nil
}

func chdir(directory string) (func(), error) {
	previous, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if err := os.Chdir(directory); err != nil {
		return nil, err
	}
	return func() {
		_ = os.Chdir(previous)
	}, nil
}

func findBrowserBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("PROPERTY_LINES_CHROME")); override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("PROPERTY_LINES_CHROME is not usable: %w", err)
		}
		return override, nil
	}

	for _, candidate := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	candidates, err := filepath.Glob(filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux64", "chrome"))
	if err != nil {
		return "", err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(candidates)))
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no Chrome or Chromium binary found; set PROPERTY_LINES_CHROME or run scripts/check-map-render.sh")
}

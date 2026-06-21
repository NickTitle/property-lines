#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [[ -z "${PROPERTY_LINES_CHROME:-}" ]]; then
  mapfile -t PLAYWRIGHT_CHROMES < <(find "$HOME/.cache/ms-playwright" -maxdepth 4 -type f -path '*/chrome-linux64/chrome' 2>/dev/null | sort -r)
  if [[ ${#PLAYWRIGHT_CHROMES[@]} -eq 0 ]]; then
    npx playwright install chromium >/dev/null
    mapfile -t PLAYWRIGHT_CHROMES < <(find "$HOME/.cache/ms-playwright" -maxdepth 4 -type f -path '*/chrome-linux64/chrome' 2>/dev/null | sort -r)
  fi
  if [[ ${#PLAYWRIGHT_CHROMES[@]} -gt 0 ]]; then
    export PROPERTY_LINES_CHROME="${PLAYWRIGHT_CHROMES[0]}"
  fi
fi

go test -run 'Test(MapRenderSmoke|SatelliteBasemapSmoke|OfflineShellSmoke|BrowserParcelQuerySmoke)$' -v ./cmd/property-lines "$@"

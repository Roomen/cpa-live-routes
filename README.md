# Live Routes

[English](README.md) | [简体中文](README_CN.md)

Live Routes is a dynamic-library plugin for [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) (CPA) that adds a real-time routing dashboard. It shows every request flowing from a client (Claude Code, Codex, SDKs, ...) through the client API key and the routing rule to the upstream credential and model, including in-flight state, retries and 429 back-off, token usage, and session / sub-agent relationships. The page is served by CPA itself, needs no extra process, and keeps everything in memory.

![Flow view](docs/images/flow-light.png)

![Timeline view, dark theme](docs/images/timeline-dark.png)

## Requirements

- CLIProxyAPI v8 or later with dynamic-library plugin support (plugin schema 6). Tested with v8.0.29.
- One of: macOS arm64/amd64, Linux amd64/arm64, Windows amd64.
- Plugins are trusted code that CPA loads into its own process, and CPA disables them by default. Set `plugins.enabled: true` in the CPA config before you install.
- The dashboard is not available in CPA Home mode, because Home mode disables plugin resource pages.

## Install

### 1. Enable plugins and the plugin

Add this to the CPA `config.yaml`:

```yaml
plugins:
  enabled: true
  dir: "plugins"          # relative to CPA's working directory; "~/..." is expanded
  configs:
    live-routes:
      enabled: true
      priority: -100      # run after other request interceptors
```

### 2. Install the library

**Plugin store.** If Live Routes is listed in a registry your CPA uses, install it from the plugin store in the management panel. `store/registry-entry.json` is the entry to submit to a registry.

**Release download (manual).** Download `live-routes_<version>_<goos>_<goarch>.zip` and `checksums.txt` from the [releases page](https://github.com/Roomen/cpa-live-routes/releases). Then put the library into `<plugins.dir>/<goos>/<goarch>/`. CPA also accepts it directly in `<plugins.dir>/`.

macOS / Linux:

```sh
VERSION=0.1.0; OS=darwin; ARCH=arm64          # linux/amd64, linux/arm64, darwin/amd64 ...
ZIP=live-routes_${VERSION}_${OS}_${ARCH}.zip
grep "$ZIP" checksums.txt | shasum -a 256 -c -  # or: sha256sum -c --ignore-missing checksums.txt
mkdir -p plugins/$OS/$ARCH
unzip -o "$ZIP" -d plugins/$OS/$ARCH           # live-routes.dylib (macOS) or live-routes.so (Linux)
xattr -d com.apple.quarantine plugins/$OS/$ARCH/live-routes.dylib 2>/dev/null || true   # macOS only
```

Windows (PowerShell):

```powershell
$zip = "live-routes_0.1.0_windows_amd64.zip"
(Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()   # compare with checksums.txt
Expand-Archive $zip -DestinationPath plugins\windows\amd64 -Force   # live-routes.dll
```

**From source.** Run `make install-local PLUGINS_DIR=/path/to/plugins`. See [Build from source](#build-from-source).

### 3. Restart CPA and open the dashboard

After you restart CPA (or reload plugins from the management panel), the CPA log should show that the `live-routes` plugin was loaded. Open the dashboard in one of these ways:

- In a browser on the same machine: `http://127.0.0.1:<port>/v0/resource/plugins/live-routes/live` (8317 is the default port).
- From the CPA management panel or the EasyCLIProxyAPI plugin page, through the **Live Routes** menu entry.
- With `?demo=1` appended to the URL, for an offline demo with simulated traffic and no CPA data.

Send any request through CPA. It appears within a few seconds (the page polls every 1 s while requests are active, and backs off to 5 s when idle).

### Update and uninstall

To update, replace the library file and restart CPA. To uninstall, set `plugins.configs.live-routes.enabled: false`, or delete the library and restart. The plugin keeps no data, so there is nothing else to clean up.

## Using the dashboard

- **Flow view** shows what is happening now, in columns entry (OAuth / API credential) → CPA rule (alias → model) → harness / client → session. Requests travel right to left, from the session or client through CPA to the entry credential. Link width follows the request rate of the last 60 seconds. Dots wait at CPA or at the entry while a request is in flight, and fly back when it completes. A node without traffic inside `active_window_minutes` fades out. Click a client to expand its sessions; they stack to its right inside the client's own row band, so the links of other clients do not cross them. Hover over a session for its full ID and its parent session. Completions with no model and no credential (such as the `GET /v1/models` health checks that management GUIs send every 30 seconds) are not recorded.
- **Timeline view** is for looking back. It has one lane per entry credential, zoom levels of 30 s / 2 min / 10 min, and you can scroll or drag back to the oldest data still in memory. 429 responses are marked, and the tooltip shows `Retry-After`.
- **Top stats** cover the selected range: requests, in-flight, success rate (done / (done + failed)), tokens/s (total tokens / range seconds) and average latency.
- **Controls:** pause, a temporary animation speed (0.5x / 1x / 2x), and the layout (tabs, or split with the flow strip above the entry lanes). These preferences are stored only in this browser. The page follows the system light/dark theme and honors `prefers-reduced-motion`.

## Troubleshooting

| Symptom | Cause / fix |
| --- | --- |
| `403 forbidden_host` | The page was not opened through a loopback address. Use `127.0.0.1`/`localhost`, or set `require_key: true`. |
| The page asks for the management key | `require_key: true` is set. Enter the CPA management key; it is exchanged for a short-lived token kept in page memory only. |
| No "queued / routing" state, client shows as unknown | `inflight_tracking` is `false`, so only completed requests (usage records) are shown. |
| Client IP is missing | CPA does not pass the client address to plugins. It is shown only behind a reverse proxy that sets `X-Forwarded-For` / `X-Real-IP`. |
| Data disappeared | CPA or the plugin restarted, or the data aged out of `retention_minutes` / `max_records`. Data is memory-only by design. |
| macOS refuses to load the library | Remove the quarantine attribute (see the `xattr` command above). |

## Configuration

All options live under `plugins.configs.live-routes`. The **配置** button on the page shows the values in effect and the matching YAML; the page is read-only, edit `config.yaml` and CPA pushes the change without a restart. Besides the options below, `enabled` and `priority` are handled by CPA itself. Use `priority: -100` so the observer runs after other interceptors.

| Key | Default | Range / values | Description |
| --- | --- | --- | --- |
| `retention_minutes` | `30` | 1 - 120 | How long finished requests are kept in memory. |
| `max_records` | `5000` | 1000 - 100000 | Record cap; the oldest records are evicted first. |
| `active_window_minutes` | `10` | 1 - 60 | Flow view: nodes with no request inside this window fade out. |
| `animation_speed` | `"1x"` | `0.5x`, `1x`, `2x` | Default animation speed of the page (viewers can change it). |
| `reduced_animation` | `false` | boolean | No flying dots, only link widths and counters. |
| `inflight_tracking` | `true` | boolean | Track queued and routed requests live. When `false` the plugin only replays usage records after completion. |
| `layout` | `"tabs"` | `tabs`, `split` | Default page layout: switchable views, or flow strip above entry lanes. |
| `require_key` | `false` | boolean | `false`: data routes answer requests whose `Origin`/`Referer` points at a loopback host; safe only when CPA listens on `127.0.0.1`. `true`: the page asks for the management key and uses a short-lived in-memory token. |
| `api_key_labels` | `{}` | map | Friendly names for client API keys: sha256 hex prefix (at least 8 characters) of the key to a label. |

Compute a key prefix without exposing the key in a file:

```sh
printf %s "$KEY" | shasum -a 256 | cut -c1-8
```

```yaml
plugins:
  configs:
    live-routes:
      enabled: true
      priority: -100
      api_key_labels:
        "3f9a12bc": "alice laptop"
        "a1b2c3d4": "ci runner"
```

## Access model

The plugin gates its own data routes because CPA does not authenticate plugin resource pages.

- Default (`require_key: false`): `GET /v0/resource/plugins/live-routes/api/{snapshot,events,records}` answer only when the request's `Origin` (or, without it, `Referer`) points at `127.0.0.1`, `localhost` or `::1`; anything else gets `403 forbidden_host`. The page itself is static and carries no data.
- CPA does not pass the client address or the `Host` header to plugins, so this check relies on headers that any non-browser client can forge. It only stops browsers on other machines. If CPA listens on anything other than `127.0.0.1` (CPA's default `host: ""` listens on all interfaces), set `require_key: true`.
- `require_key: true`: the page asks for the management key once and exchanges it (`POST /v0/management/live-routes/session`) for a token kept in memory only and sent as `X-Live-Routes-Token`. The key is never stored by the page or the plugin. Without a valid token the data routes answer `401 unauthorized`. Use this when you reach CPA through a domain name or a reverse proxy.
- Scripts can read the same snapshot with the management key: `GET /v0/management/live-routes/snapshot` with the management key in a bearer `Authorization` header.

Client API keys are redacted inside the plugin: only a mask such as `sk-...3f9a` and a short fingerprint are exposed, never the raw key. E-mail addresses in credential labels are masked.

## Performance

- With `inflight_tracking: true` the plugin declares a request interceptor. CPA calls it synchronously twice per request (before authentication, and after authentication for each attempt) and the host serializes the full request body for each call, roughly 1 - 2 ms per MB of body. The plugin itself returns a constant pass-through in microseconds.
- Set `inflight_tracking: false` for zero request-path overhead. You then get usage replay only: no queued / routed states and no user-agent based client detection.
- Usage handling and page polling cost are negligible. When nobody is viewing the page, the plugin does no background work.
- Use `priority: -100` so the observer runs after other interceptors.

## Storage guarantee

Memory only. The plugin writes nothing to disk, and all data is cleared when CPA or the plugin restarts. The page uses browser `localStorage` only for UI preferences (`lr-view`, `lr-layout`, `lr-speed`, `lr-range`).

`scripts/e2e/run.sh` verifies this against a real CPA process by comparing its open regular files with and without the plugin.

## Limitations

- The client IP is shown only when CPA sits behind a reverse proxy that sets `X-Forwarded-For` or `X-Real-IP`.
- The client user agent (and so the client kind) is known only with `inflight_tracking: true`.
- CPA Home mode disables plugin resource pages, so the dashboard is not available there.
- Data is bounded by `retention_minutes` and `max_records`; older history is gone by design.

## Build from source

Requires Go (see `go.mod`) and a C compiler, because plugins are built with `-buildmode=c-shared`.

```sh
make test                      # go vet + go test -race
make build                     # host platform -> bin/live-routes.<ext>
make darwin                    # macOS arm64 + amd64 (needs clang on macOS)
make dist VERSION=0.1.0        # dist/live-routes_0.1.0_<goos>_<goarch>.zip + checksums.txt
make install-local PLUGINS_DIR=/path/to/plugins
make e2e                       # end-to-end test against a local CPA and a mock upstream
```

Linux and Windows builds need a native toolchain; release builds run on native GitHub Actions runners (`.github/workflows/release.yml`) for darwin arm64/amd64, linux amd64/arm64 and windows amd64. Pushing a tag `vX.Y.Z` builds all targets and publishes a release with the zips and `checksums.txt`, which is the layout the CPA plugin store installs from. `store/registry-entry.json` is the entry to submit to a plugin registry.

The e2e script needs `node`, `python3`, `curl` and `lsof`, plus CPA sources next to this repository (override with `CPA_SRC`). It uses a mock upstream only and never touches `~/.cli-proxy-api`.

## License

MIT, see [LICENSE](LICENSE).

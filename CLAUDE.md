# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

ShadowVPN Desktop: a Windows x64 VPN client. There are three layers, each a separate process or runtime:

1. **WebView UI** (`src/renderer/`): plain HTML/CSS/JS with no bundler or framework. Tauri serves the directory as-is (`frontendDist: ../src/renderer`). Scripts load in a fixed order from `index.html`: `tauri-bridge.js` → helpers/stores → `renderer.js`.
2. **Rust/Tauri shell** (`src-tauri/`): the window, tray, single-instance lock, updater, autostart and the lifecycle of the Go process.
3. **Go core** (`backend/`, built to `bin/shadowvpn-core.exe`): embeds Xray-core. It parses subscriptions, creates the Wintun TUN, handles routes, DNS, ping, the Auto/leastPing balancer, the WFP Kill Switch, traffic counters and HWID.

The README and UI strings are in Russian. Error messages shown to the user are written in Russian too.

## Commands

The full build only works on Windows (MSVC Rust, Go 1.27+, Node LTS, VS C++ Build Tools):

```powershell
.\build-windows.ps1        # runs all tests, builds the Go core, runs `npm run build` (NSIS installer), writes updates/latest.json
.\start-tun.cmd            # elevates to admin, then runs the release exe if its version matches package.json, else `npm start` (tauri dev)
.\stop-shadowvpn.cmd       # kills leftover shadowvpn / shadowvpn-core processes that lock files
```

Tests:

```powershell
cd backend; go test ./...                          # Go: runs on any OS (Windows-only code has *_other.go stubs)
go test -run TestName ./...                        # single Go test
cd src-tauri; cargo test --no-default-features --lib   # Rust: tests backend.rs/settings.rs without Tauri (lib.rs exposes only those)
node scripts/test-bridge.cjs                       # run from repo root
node scripts/test-renderer-helpers.cjs
node scripts/test-subscription-store.cjs
```

On Linux, also run `GOOS=windows go vet ./...` to type-check the Windows-only files. `cargo check` of the Tauri binary on Linux needs the GTK/WebKit dev packages and a placeholder `bin/shadowvpn-core.exe` (it is a bundle resource). The JS tests load renderer files into `node:vm` with stubbed `window`, `localStorage` and `__TAURI__`. A renderer module is only testable if it attaches its API to `window.*` and avoids top-level DOM access.

## Architecture

**IPC chain:** `renderer.js` → `window.vpnApi` (`tauri-bridge.js`) → `invoke('vpn_*')` Tauri commands (`src-tauri/src/main.rs`) → `Backend` (`src-tauri/src/backend.rs`) → JSON lines over the child's **stdin/stdout** → `backend/main.go` request loop (`switch r.Method`: `import`, `importMany`, `connect`, `switch`, `ping`, `disconnect`, `publicIp`, `deviceInfo`, `shutdown`).
- Requests carry an `id`. Responses are `{id, ok, result|error}`. Messages without an `id` are events (`ready`, state, profile, traffic, log). Rust re-emits them to the WebView as `vpn:state`, `vpn:profile`, `vpn:traffic`, `vpn:log`, `vpn:error`, `vpn:update-progress` and `vpn:tray-action`.
- In Go, stdout is reserved for IPC (`os.Stdout = os.Stderr` after the encoder is created). Never print to stdout from the core. Diagnostics go to stderr, and Rust collects them into an in-memory log of up to 1500 lines.
- There is no TCP control port and no shell. The Tauri capability (`src-tauri/capabilities/default.json`) only grants event listen/unlisten. The CSP is strict (`script-src 'self'`, no inline scripts or styles).

**Adding a backend operation** means touching every layer: a Go `case` in `main.go`, a `#[tauri::command]` registered in `main.rs` (with input validation, since Rust and Go each validate independently, e.g. DNS and GeoData URLs), and a wrapper in `tauri-bridge.js` (plus its assertion in `scripts/test-bridge.cjs`).

**Credential isolation:** server URIs, UUIDs and passwords stay inside the Go process. The WebView sees only name, protocol, transport and a random, stable profile ID. Connect calls pass profile IDs (and `autoProfileIds` for Auto or groups). Log lines are redacted in both Go and Rust (`sanitize_log_line`) before they reach the UI. Keep it this way.

**Subscription cache:** Go keeps the last good body of each subscription (`backend/subscription_cache.go`, DPAPI-sealed under `%LOCALAPPDATA%\ShadowVPN\subscriptions`). The sync `reason` controls it. `cache` is cache-first and downloads only sources that have no saved copy. `startup`, `manual` and `automatic` are network-first and fall back to the saved copy (`sources[].cached`). At startup the renderer uses `cache` unless auto-update is on and its interval has elapsed. A load served from the cache does not reset the last-sync time.

**Core lifecycle and recovery:** a hung or orphaned core keeps the `ShadowVPN` Wintun adapter and the Kill Switch WFP session alive, which used to block all traffic until a reboot. Several layers guard against that:
- `Service` in `main.rs` kills stale `shadowvpn-core.exe` processes before spawning.
- After a crash, a request timeout or the core's own exit, `Service` respawns the core on the next call and emits `vpn:core-exited`. The renderer then reloads servers with `reason: "cache"`.
- `Backend::shutdown` kills the child if it doesn't exit within 25 s.
- In Go, `closeInstance` exits the process if `instance.Close()` hangs for 15 s, `shutdown` has a 20 s watchdog, and `cleanupStaleNetworkState` flushes routes and DNS on a leftover adapter at startup.

**UI state** (subscriptions, groups, settings) lives in WebView `localStorage` through `subscription-store.js`, `group-store.js` and `settings-store.js`. Those stores include migration logic from earlier storage formats.

**Platform split in Go:** `*_windows.go` and `*_other.go` (HWID, platform/routes, WFP). The Kill Switch uses a dynamic WFP session, so Windows removes its filters when the core process dies. It is deliberately incompatible with split-routing modes.

**Network operations are serialized:** Rust has a `busy` guard and the Go worker is single-threaded per request, except for detached goroutines such as `publicIp`. Switching servers (`replaceCurrent: true` → `switch`) is one atomic operation that keeps the Kill Switch session alive.

## Releases and versioning

- The version appears in `package.json`, `src-tauri/Cargo.toml`, `src-tauri/tauri.conf.json`, `backend/identity.go` (`appVersion`, sent as the subscription `User-Agent` and asserted in `identity_test.go`) and the README title. Bump all of them together.
- The updater reads `updates/latest.json` from `main` on GitHub (raw). `scripts/write-update-manifest.ps1` generates it from the signed NSIS build. The release `.exe` and `.exe.sig` go to the GitHub Release tagged `v<version>`.
- Signing keys live in `src-tauri/.updater/` (gitignored), or come from the `TAURI_SIGNING_PRIVATE_KEY` / `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` env vars.
- `bin/shadowvpn-core.exe` is a build output (gitignored). `bin/wintun.dll` is committed. Xray-core is pinned to a specific commit in `backend/go.mod`.

## Leftovers

`src/main/` (Electron `main.js`/`preload.js`) and `src/shared/servers.js` come from the earlier Electron version. Tauri doesn't load them, and no current code references them.

## License

AGPL-3.0-only. Third-party licenses are in `licenses/` and `src/renderer/assets/*.LICENSE.txt`.

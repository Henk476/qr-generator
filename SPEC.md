# Canute's QR Generator — Specification

| | |
|---|---|
| **Status** | Draft v0.2 |
| **Date** | 2026-10-11 |
| **Language** | Go (1.26+) |
| **Feature reference** | [qr.io](https://qr.io/) (static-code feature set) |

---

## 1. Purpose

Canute's QR Generator is a self-hosted, single-binary generator for **static** QR codes. It covers qr.io's static-code and design feature set and runs entirely on the user's own machine. It has:

- **No ads.** No ad slots, no ad SDKs, no tracking pixels, and no upsell banners.
- **No accounts or sign-up walls.** Every feature is unlocked, including downloads, which qr.io puts behind sign-up.
- **No outbound network calls**: no telemetry, no CDN assets, no remote fonts, and no third-party APIs.
- **A local-first design.** All data stays in a local data directory that the user controls.

## 2. Goals and non-goals

### Goals
1. Generate **static** QR codes, plus common 2D barcodes, for all content types in §4.
2. Provide rich **visual customization**: frames, module shapes, eye shapes, colors and gradients, and logos (§5).
3. Export to **PNG, SVG, PDF, EPS, and JPEG** at any resolution (§6).
4. Serve a server-rendered **web UI on localhost**, used in the user's normal browser, and offer an equivalent **CLI** and **local REST API**.
5. Ship as a **single static binary** for Windows, macOS, and Linux, with no CGO and no runtime dependencies.

### Non-goals
- **Dynamic (editable or redirecting) QR codes.** Every code encodes its content directly.
- **Scan tracking or analytics** of any kind.
- **Hosted content** such as landing pages, PDF hosting, image galleries, or link hubs. Users who need these can host the content elsewhere and use the URL type.
- **A native desktop window.** The browser on localhost is the UI.
- User accounts, multi-tenant hosting, billing, and cloud sync.
- Any form of advertising or monetization.

## 3. Architecture

```
┌──────────────────────── canute-qr (single Go binary) ────────────────────────┐
│                                                                              │
│  cmd/canute-qr        CLI entry (serve | generate | batch | export | ...)    │
│                                                                              │
│  internal/encode      payload builders per content type  (§4)                │
│  internal/matrix      QR / 2D barcode matrix generation (interface)          │
│  internal/render      vector scene → SVG / PNG / JPEG / PDF / EPS  (§5, §6)  │
│  internal/verify      decode-after-render scannability check                 │
│  internal/generate    pipeline shared by CLI, UI and API                     │
│  internal/store       SQLite persistence (saved codes, presets, assets)      │
│  internal/api         JSON REST API  (/api/v1)                               │
│  internal/web         server-rendered UI: html/template + small JS (embed)   │
│  internal/i18n        message catalogs                                       │
│                                                                              │
└───────────────────────────────┬──────────────────────────────────────────────┘
                                │
                      data dir (default: OS user-config dir/canute-qr)
                      ├── canute.db        SQLite
                      ├── assets/          uploaded logos
                      └── config.toml
```

### 3.1 Technology choices

| Concern | Choice | Reason |
|---|---|---|
| HTTP | `net/http` (Go 1.22+ routing patterns) | stdlib, no framework needed |
| UI | Server-rendered `html/template` and one small vanilla JS file, all embedded with `go:embed` | No JS toolchain or third-party scripts, works offline |
| QR matrix | `github.com/skip2/go-qrcode` (module access via `Bitmap()`) behind the `matrix` interface | Fast start, swappable later |
| 2D barcodes | `github.com/boombuler/barcode` | Data Matrix, PDF417, Aztec, Code128, and EAN/UPC |
| Database | `modernc.org/sqlite` | Pure Go and CGO-free, so it cross-compiles to Windows |
| Raster | `image`, `golang.org/x/image/vector` | Anti-aliased path rasterization |
| PDF | `github.com/go-pdf/fpdf` or a hand-written vector PDF writer | Vector output |
| EPS | Hand-written PostScript emitter from the shared vector scene | Simple and dependency-free |
| Decoding/verify | `github.com/makiuchi-d/gozxing` | Decodes the render to confirm it scans |
| Config | TOML (`github.com/BurntSushi/toml`) | Easy for humans to edit |

All dependencies must be MIT, BSD, or Apache-2.0 licensed, or similarly permissive. **No dependency may make network calls at runtime.**

### 3.2 Rendering pipeline

```
content form ─► encode.Payload (string/bytes)
             ─► matrix.Build(payload, ECC, version?) → [][]bool + finder/alignment map
             ─► render.Scene(matrix, Style)          → vector primitives (paths, gradients, image)
             ─► render.Export(scene, format, size)   → bytes
             ─► verify.Decode(PNG) == payload ?      → warning if unscannable
```

All output formats are produced from one **vector scene**. This keeps SVG, PDF, EPS, and raster output visually identical.

## 4. Content types

All codes are static: the content is encoded directly in the symbol and works without the app running. Each type has a form with field validation, a live preview, and a defined payload format.

| # | Type | Payload | Notes |
|---|---|---|---|
| 1 | **Link / URL** | `https://…` | Auto-prefixes `https://`. Also covers app store links, videos, PDFs, and social profiles hosted elsewhere. |
| 2 | **Text** | Raw UTF-8 | Shows a byte or character count and a capacity warning |
| 3 | **E-mail** | `mailto:to?subject=&body=` | Percent-encoding handled |
| 4 | **Call** | `tel:+…` | E.164 validation |
| 5 | **SMS** | `SMSTO:number:message` | |
| 6 | **vCard** | vCard 3.0, with a MeCard option for smaller codes | Name, organization, title, phones, email, URL, address, and note. Warns when the payload is large. |
| 7 | **WhatsApp** | `https://wa.me/{num}?text=` | |
| 8 | **Wi-Fi** | `WIFI:T:{WPA/WEP/nopass};S:;P:;H:true;;` | Escapes `\ ; , : "` |
| 9 | **Event** | iCalendar `BEGIN:VEVENT…END:VEVENT` | Title, start and end, time zone, location, and description |
| 10 | **2D Barcode** | Data Matrix, PDF417, Aztec, Code128, EAN-13, or UPC-A | Product and logistics data. The symbology picker validates check digits. Design options are limited to colors and quiet zone. |

**Stretch types:** Location (`geo:lat,lng`), Crypto payment (`bitcoin:` and `ethereum:` URIs), and PayPal.me link.

## 5. Design and customization

All options are live-previewed. Every change re-renders within **200 ms** for a typical code.

### 5.1 Error correction and sizing
- ECC level L, M, Q, or H. **H is enforced automatically when a logo is present.**
- Auto or fixed QR version (1–40), and a quiet-zone width (0–10 modules, default 4).
- Encoding mode is chosen automatically: numeric, alphanumeric, byte, or Kanji.

### 5.2 Module ("shape") style
- Square, rounded, dots, classy, classy-rounded, extra-rounded, diamond, and vertical or horizontal bars. Connected styles merge neighboring modules.
- Foreground color can be **solid**, **linear gradient** (2 stops plus an angle), or **radial gradient**.
- Background can be a solid color or **transparent**. Transparency applies to PNG, SVG, PDF, and EPS. JPEG falls back to white.

### 5.3 Finder pattern ("eyes")
- **Border (outer eye):** square, rounded, circle, leaf, or cut-corner, each with its own color.
- **Center (inner eye):** square, rounded, circle, diamond, leaf, or star, each with its own color.

### 5.4 Logo
- Upload PNG, JPEG, or SVG, or pick from a **built-in icon library**. The library is embedded in the binary and includes link, location, email, WhatsApp, Wi-Fi, vCard, PayPal, Bitcoin, "scan me", menu, and fork/knife, among others.
- Logo size is set as 10–30% of the code width. The limit is enforced by ECC capacity.
- **"Remove background behind logo"** clears the modules under the logo area, with adjustable padding.
- Logo shape can be square or circle-clipped.

### 5.5 Frames
- About 10 frame templates: plain border, bottom banner, top banner, speech bubble, rounded badge, and phone outline, among others.
- The frame phrase defaults to "SCAN ME" and holds up to 30 characters. Font can be Sans-Serif, Serif, or Monospace. The fonts are embedded Go fonts or bundled OFL fonts, never remote.
- Frame color and text color are set separately.

### 5.6 Presets
- The user can save the current style as a named **preset** and apply it to any code.
- About 8 built-in presets ship with the app.

### 5.7 Scannability guard
After every render, `internal/verify` decodes the PNG. If decoding fails, or if foreground and background contrast is below 4:1, the UI shows a warning with a suggested fix such as "increase contrast", "shrink logo", or "raise ECC". This is a warning only and never blocks the user.

## 6. Export

| Format | Vector | Transparency | Options |
|---|---|---|---|
| PNG | ✘ | ✔ | Size in px (128–4096) |
| JPEG | ✘ | ✘ | Size and quality |
| SVG | ✔ | ✔ | Logos are embedded as data URIs and fonts as outlined paths |
| PDF | ✔ | ✔ | Page size (fit, A4, Letter) and print size in mm or inches |
| EPS | ✔ | ✔ | For print shops |

- Downloads are free, require no account, and carry **no watermark**.
- Batch export produces a ZIP (§7).
- "Copy to clipboard" (PNG) is available in the UI.

## 7. Code library
- **Saving is optional.** A code can be generated and downloaded without saving it.
- Library view lists saved codes with search, a type filter, tags, and sorting.
- Codes can be re-opened for editing, duplicated, re-exported, or deleted. Editing a saved code produces a new image; it does not change codes that are already printed.
- **Bulk generation:** import a CSV with one row per code and columns mapped to type fields. One shared style or preset is applied, and the output is a ZIP of image files. Optionally, the codes are also saved to the library.
- Full backup and restore: a single `.zip` containing the DB and assets.

## 8. Security and privacy
- Binds to `127.0.0.1` only by default. Binding to another interface requires an explicit `--addr` and prints a warning. No authentication is built in, because the app is single-user and local.
- CSRF tokens on all state-changing UI forms, and a strict Content-Security-Policy with `default-src 'self'`.
- Uploads are logos only, with a default limit of 5 MB. Content types are sniffed, and SVG uploads are sanitized by stripping scripts and external references.
- No telemetry, no update checks, and no outbound requests. An automated test asserts that no outbound HTTP client is ever constructed outside tests.

## 9. Interfaces

### 9.1 Web UI (served at `http://127.0.0.1:8080`)
The UI is server-rendered. Every page is a plain HTML form that works without JavaScript; a small script adds live preview, auto-applied presets, delete confirmations and copy-to-clipboard. It uses a 3-step flow that mirrors qr.io:
1. **Content:** pick a type and fill the form.
2. **Design:** frame, shape, eyes, logo, and colors, with a live preview pinned on the right.
3. **Download:** choose the format and size, then download and optionally save to the library.

Other pages: **Library**, **Presets**, and **Settings**. Settings covers language, default export options, and backup.

Light and dark themes are supported. The UI is responsive and keyboard accessible, targeting WCAG 2.1 AA. `canute-qr serve --open` opens the UI in the default browser.

### 9.2 CLI
```
canute-qr serve    [--addr 127.0.0.1:8080] [--data-dir PATH] [--open]
canute-qr generate --type url --data https://example.com --style preset:classic \
                   --format svg --size 1024 -o code.svg
canute-qr generate --type wifi --ssid Home --password ... --enc WPA -o wifi.png
canute-qr batch    --csv codes.csv --map mapping.toml --format png --out codes.zip
canute-qr export   --id <code-id> --format pdf -o out.pdf
canute-qr backup   -o backup.zip      |   canute-qr restore backup.zip
canute-qr version
```
`generate` and `batch` work **without** a running server or a database.

### 9.3 REST API (`/api/v1`, JSON)
| Method | Path | Purpose |
|---|---|---|
| POST | `/render` | Stateless render: payload, style, and format in, image bytes out |
| GET/POST | `/codes` | List or save codes |
| GET/PATCH/DELETE | `/codes/{id}` | Read, update, or delete a saved code |
| GET | `/codes/{id}/image?format=&size=` | Download a saved code's image |
| GET/POST/DELETE | `/presets[/{id}]` | Style presets |
| POST | `/assets` | Upload a logo |
| POST | `/batch` | Bulk generate |

An OpenAPI 3 document is served at `/api/v1/openapi.json`.

## 10. Data model (SQLite)

```sql
codes    (id TEXT PK, name, type, content JSON, style JSON, tags JSON,
          created_at, updated_at)
assets   (id TEXT PK, kind /*logo*/, mime, path, size, sha256, created_at)
presets  (id TEXT PK, name, style JSON, builtin BOOL)
settings (key TEXT PK, value TEXT)
```
Migrations are embedded and versioned, and run automatically on startup.

## 11. Internationalization
English ships first. Catalogs for Spanish, Portuguese, Italian, French, and German follow, matching qr.io's language set. Strings live in embedded JSON catalogs, and the language comes from Settings or `Accept-Language`.

## 12. Quality requirements

| Area | Requirement |
|---|---|
| Correctness | Every content type has golden-payload unit tests. Each render is decoded by gozxing and must match the payload (round-trip tests across styles). |
| Performance | Single render to 1024 px PNG in **< 50 ms** on a mid-range laptop. A batch of 1,000 PNGs in < 30 s. |
| Footprint | Binary < 40 MB. Idle RAM < 50 MB. |
| Portability | `GOOS` windows, darwin, and linux; `GOARCH` amd64 and arm64; `CGO_ENABLED=0`. |
| Code quality | `go vet`, `staticcheck`, and `golangci-lint` are clean. `go test ./... -race` passes. Core packages (`encode`, `render`) have ≥ 80% coverage. |
| Offline | The full test suite and the app run with networking disabled. |

## 13. Packaging and distribution
- `goreleaser` builds archives for all targets, plus a Windows `.zip` with `canute-qr.exe`.

## 14. Milestones

| Milestone | Scope |
|---|---|
| **M1: Core engine** | All QR content types in §4 (types 1–9); basic style (colors, ECC, quiet zone); PNG and SVG export; CLI `generate`; verify step |
| **M2: Web UI** | Server-rendered 3-step UI, live preview, SQLite library, presets |
| **M3: Full design** | Module and eye shapes, gradients, logos with background knockout, icon library, frames; PDF, EPS, and JPEG export |
| **M4: Bulk, API, polish** | CSV batch, REST API and OpenAPI, 2D barcodes, backup and restore, i18n, release packaging |
| **Stretch** | Location, crypto, and PayPal types; per-eye styling; custom fonts |

## 15. Decisions

| # | Decision |
|---|---|
| D1 | The UI is server-rendered (`html/template`) with progressive enhancement from one small vanilla JS file. There is no SPA, no htmx, and no JS build toolchain. |
| D2 | Start with `skip2/go-qrcode` behind the `matrix` interface, so an in-house encoder can replace it later. |
| D3 | There is no routing logic (A/B or time-based). This follows from D5. |
| D4 | The UI is the user's normal browser on localhost. There is no native desktop wrapper. |
| D5 | Dynamic codes and scan analytics are out of scope. Hosted content types (PDF, Images, Video, Social Media, and App landing pages) are dropped with them, because they require a server that phones can reach. |

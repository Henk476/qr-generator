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
│  cmd/canute-qr        CLI (serve, generate, batch, backup, restore, ...)     │
│                                                                              │
│  internal/encode      payload builders per content type  (§4)                │
│  internal/matrix      QR matrix generation (interface)                       │
│  internal/barcode     Data Matrix, PDF417, Aztec, Code 128, EAN-13, UPC-A    │
│  internal/render      vector scene → SVG / PNG / JPEG / PDF / EPS  (§5, §6)  │
│  internal/verify      decode-after-render scannability check                 │
│  internal/generate    pipeline shared by CLI, UI and API                     │
│  internal/logo        logo decoding: PNG/JPEG/SVG → normalized PNG           │
│  internal/batch       CSV → ZIP bulk generation with report.csv              │
│  internal/store       SQLite persistence (saved codes, presets, assets)      │
│  internal/api         JSON REST API  (/api/v1)                               │
│  internal/web         server-rendered UI: html/template + small JS (embed)   │
│  internal/i18n        UI translations (de, es, fr, it, pt)                   │
│                                                                              │
└───────────────────────────────┬──────────────────────────────────────────────┘
                                │
                      data dir (default: OS user-config dir/canute-qr)
                      └── canute.db        SQLite: codes, presets, settings and
                                           uploaded logos (settings are edited
                                           in the UI; there is no config file)
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
| PDF | Hand-written PDF 1.4 writer from the shared vector scene | Vector output with shadings and image soft masks, no dependency |
| EPS | Hand-written PostScript emitter from the shared vector scene | Simple and dependency-free |
| Decoding/verify | `github.com/makiuchi-d/gozxing` | Decodes the render to confirm it scans |
| SVG logos | `github.com/srwiley/oksvg` + `rasterx` | Rasterizes uploaded SVG logos to PNG |
| Fonts | Go fonts (`golang.org/x/image/font/gofont`) and Libre Baskerville (OFL) | Embedded, outlined into paths |

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
| 10 | **Location** | `geo:lat,lng`, with `?q=lat,lng(label)` when a place name is given | Decimal degrees with range checks; a comma decimal separator is accepted. |
| 11 | **PayPal** | `https://paypal.me/{name}/{amount}{CUR}` | PayPal.me name, optional amount (2 decimals) and 3-letter currency. |
| 12 | **Crypto payment** | BIP 21 `bitcoin:` and `litecoin:` URIs; EIP-681 `ethereum:` with the amount in wei | Addresses are checksum-verified to catch typos: Base58Check and bech32/bech32m for Bitcoin and Litecoin, EIP-55 for mixed-case Ethereum addresses. Amounts allow 8 decimals (18 for Ether). |
| 13 | **Barcode** | Data Matrix, PDF417, Aztec, Code 128, EAN-13, or UPC-A | Product and logistics data. EAN-13 and UPC-A add or verify the GS1 check digit; text symbologies take ASCII. Colors, gradient, and quiet zone apply (linear codes get at least a 10-module side quiet zone and human-readable text). Every symbology is decode-checked except PDF417, which has no offline reader, so the UI reports it as not checked. |

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
- **Per-corner styling:** each of the three corners (top-left, top-right, bottom-left) can override the border shape, center shape, and both colors. Unset values inherit the settings above.

### 5.4 Logo
- Upload PNG, JPEG, or SVG, or pick from a **built-in icon library** of 15 vector icons drawn for this project: link, location, e-mail, phone, chat (for WhatsApp and SMS), Wi-Fi, contact card, calendar, payment card, Bitcoin, "scan me", menu, fork and knife, home, and star. Brand logos such as WhatsApp or PayPal are not included, because they are trademarks.
- Uploaded SVGs are rasterized to PNG on upload, so the uploaded markup is never stored, served, or embedded. The trade-off is that an SVG logo is not vector in SVG, PDF, or EPS output.
- Logo size is set as 10–30% of the code width. The limit is enforced by ECC capacity.
- **"Remove background behind logo"** clears the modules under the logo area, with adjustable padding.
- Logo shape can be square or circle-clipped.

### 5.5 Frames
- About 10 frame templates: plain border, bottom banner, top banner, speech bubble, rounded badge, and phone outline, among others.
- The frame phrase defaults to "SCAN ME" and holds up to 30 characters. Font can be Sans-Serif (Go Bold), Serif (Libre Baskerville Bold, bundled under the SIL OFL in `internal/render/fonts/`), or Monospace (Go Mono Bold). You can also upload your own TTF or OTF font (up to 10 MB; it must contain Latin letters). Fonts are embedded, and text is converted to outlines, so no output depends on installed fonts. Making sure an uploaded font's license allows embedding is up to the user.
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
| EPS | ✔ | ✘ | For print shops. PostScript has no transparency, so translucent colors and logo pixels are flattened onto the background. Logos are capped at 512 px. |

- Downloads are free, require no account, and carry **no watermark**.
- Batch export produces a ZIP (§7).
- "Copy to clipboard" (PNG) is available in the UI.

## 7. Code library
- **Saving is optional.** A code can be generated and downloaded without saving it.
- Library view lists saved codes with search, a type filter, tags, and sorting.
- Codes can be re-opened for editing, duplicated, re-exported, or deleted. Editing a saved code produces a new image; it does not change codes that are already printed.
- **Bulk generation** (Bulk page, `canute-qr batch`, `POST /api/v1/batch`): a CSV with a header row whose columns are named after content fields, plus optional `type` and `name` columns. Other headers can be mapped with `header=field` pairs. One preset or design applies to all rows, up to 5,000 rows. The output is a ZIP of images plus `report.csv`, which gives each row's status. A failing row is reported, not fatal. Optionally the codes are also saved to the library with tags.
- **Backup and restore** (Settings page, `canute-qr backup` and `restore`): a single `.zip` with a consistent SQLite snapshot and a manifest. Restore migrates older backups and replaces codes, user presets, logos, and settings in one transaction. Built-in presets and the machine-local CSRF and API tokens are kept, and nothing changes if the archive is invalid.

## 8. Security and privacy
- Binds to `127.0.0.1` only by default, and then accepts only its own host names in the `Host` header, which blocks DNS rebinding. Binding to another interface requires an explicit `--addr` and prints a warning; `--allowed-hosts` restores the host check there (the Docker image uses it). The web UI has no login, because the app is single-user and local.
- CSRF tokens on all state-changing UI forms, a `Sec-Fetch-Site` check, request size limits, and a strict Content-Security-Policy with `default-src 'self'` and no inline scripts or styles.
- The REST API needs `Authorization: Bearer <token>`. The token is generated once, shown in Settings, and can be replaced there. Cross-site pages cannot read it, and browsers will not send it cross-origin without CORS approval, which the server never gives.
- Uploads are logos (5 MB, 4096 × 4096 px), CSV files (5 MB), and backups (512 MB, on the restore route only). Image sizes are checked before the pixels are decoded. Every logo is re-encoded as PNG. This strips metadata and turns SVG into pixels, so no uploaded markup ever reaches a page or an exported file.
- No telemetry, no update checks, and no outbound requests. A test scans the source and fails if any program code creates an HTTP client, makes a request, or dials out.

## 9. Interfaces

### 9.1 Web UI (served at `http://127.0.0.1:8080`)
The UI is server-rendered. Every page is a plain HTML form that works without JavaScript; a small script adds live preview, auto-applied presets, delete confirmations and copy-to-clipboard. It uses a 3-step flow that mirrors qr.io:
1. **Content:** pick a type and fill the form.
2. **Design:** frame, shape, eyes, logo, and colors, with a live preview pinned on the right.
3. **Download:** choose the format and size, then download and optionally save to the library.

Other pages: **Library**, **Bulk**, **Presets**, and **Settings**. Settings covers language, theme, default export options, the API token, and backup and restore.

Light and dark themes are supported. The UI is responsive and keyboard accessible, targeting WCAG 2.1 AA. `canute-qr serve --open` opens the UI in the default browser.

### 9.2 CLI
```
canute-qr serve    [--addr 127.0.0.1:8080] [--data-dir PATH] [--open] [--allowed-hosts LIST]
canute-qr generate --data https://example.com --shape dots --logo icon:link -o code.svg
canute-qr generate --type wifi --ssid Home --password ... -o wifi.png
canute-qr generate --type barcode --symbology ean13 --data 590123412345 -o product.pdf
canute-qr batch    --csv codes.csv [--type url] [--map "Website=url"] --format png -o codes.zip
canute-qr backup   -o backup.zip      |   canute-qr restore backup.zip
canute-qr types    |   canute-qr styles   |   canute-qr version
```
`generate` and `batch` take every design and export option as flags and work **without** a running server or a database.

### 9.3 REST API (`/api/v1`, JSON)
| Method | Path | Purpose |
|---|---|---|
| POST | `/render` | Stateless render: type, fields, design and export options in, image bytes out |
| POST | `/check` | Validate and run the scan check; JSON report |
| GET | `/types`, `/styles` | Content types with their fields; allowed style values |
| GET/POST | `/codes` | List (search, type, tag, sort) or save codes |
| GET/PATCH/DELETE | `/codes/{id}` | Read, partly update, or delete a saved code |
| GET | `/codes/{id}/image?format=&size=` | Render a saved code |
| GET/POST/DELETE | `/presets[/{id}]` | Style presets (built-ins cannot be deleted) |
| POST | `/assets` | Upload a logo; returns `asset:<id>` for use in a design |
| POST | `/fonts` | Upload a TTF/OTF font; returns `asset:<id>` for `design.frameFont` |
| POST | `/batch` | CSV to ZIP |

Partial `design` objects merge onto the defaults, or onto the current design for PATCH. Errors are `{"error": "..."}`. An OpenAPI 3.0 document is served at `/api/v1/openapi.json` without a token.

## 10. Data model (SQLite)

```sql
codes    (id TEXT PK, name, type, content JSON, style JSON, tags JSON,
          created_at, updated_at)
assets   (id TEXT PK, kind /*logo|font*/, mime, sha256 UNIQUE, data BLOB, created_at)  -- deduplicated by hash
presets  (id TEXT PK, name, style JSON, builtin BOOL)
settings (key TEXT PK, value TEXT)
```
Migrations are embedded and versioned, and run automatically on startup.

## 11. Internationalization
The web UI is available in English, German, Spanish, French, Italian, and Portuguese, matching qr.io's language set. English strings are the catalog keys, catalogs are embedded JSON, and anything missing falls back to English. The language comes from Settings, else from `Accept-Language`. Scan-check warnings are translated; content validation errors, the CLI, and the API stay in English. A test extracts every translatable string and fails if any catalog misses one or garbles a format verb.

## 12. Quality requirements

| Area | Requirement |
|---|---|
| Correctness | Every content type has golden-payload unit tests. Each render is decoded by gozxing and must match the payload (round-trip tests across styles). |
| Performance | Single render to 1024 px PNG in **< 50 ms** on a mid-range laptop. A batch of 1,000 PNGs in < 30 s. |
| Footprint | Binary < 40 MB. Idle RAM < 50 MB. |
| Portability | `GOOS` windows, darwin, and linux; `GOARCH` amd64 and arm64; `CGO_ENABLED=0`. |
| Code quality | `go vet` and `gofmt` are clean, and `go test ./...` passes on Linux, Windows, and macOS in CI, with `-race` on Linux. Core packages (`encode`, `render`) have ≥ 80% coverage. (`staticcheck` and `golangci-lint` are not yet run.) |
| Offline | The full test suite and the app run with networking disabled. |

## 13. Packaging and distribution
- `.goreleaser.yaml` builds archives for Windows, macOS, and Linux on amd64 and arm64 (a `.zip` for Windows), with checksums and the bundled font license. `.github/workflows/release.yml` runs it when a `v*` tag is pushed; `ci.yml` runs vet, format, and tests on every push to main and every pull request.
- An optional `Dockerfile` builds a distroless, non-root image that serves on port 8080 with its data in `/data`, keeping the host check on with `--allowed-hosts`.
- `go install github.com/Henk476/qr-generator/cmd/canute-qr@latest` works too; the version then comes from the module.

## 14. Milestones

| Milestone | Scope | Status |
|---|---|---|
| **M1: Core engine** | QR content types 1–9 in §4; basic style (colors, ECC, quiet zone); PNG and SVG export; CLI `generate`; verify step | Done |
| **M2: Web UI** | Server-rendered 3-step UI, live preview, SQLite library, presets | Done |
| **M3: Full design** | Module and eye shapes, gradients, logos with background knockout, icon library, frames; PDF, EPS, and JPEG export | Done |
| **M4: Bulk, API, polish** | CSV batch, REST API and OpenAPI, 2D barcodes, backup and restore, i18n, release packaging | Done |
| **Stretch** | Location, crypto, and PayPal types; per-corner styling; custom fonts; ZXing-checked mask selection | Done |

## 15. Decisions

| # | Decision |
|---|---|
| D1 | The UI is server-rendered (`html/template`) with progressive enhancement from one small vanilla JS file. There is no SPA, no htmx, and no JS build toolchain. |
| D2 | Start with `skip2/go-qrcode` behind the `matrix` interface, so an in-house encoder can replace it later. |
| D3 | There is no routing logic (A/B or time-based). This follows from D5. |
| D4 | The UI is the user's normal browser on localhost. There is no native desktop wrapper. |
| D5 | Dynamic codes and scan analytics are out of scope. Hosted content types (PDF, Images, Video, Social Media, and App landing pages) are dropped with them, because they require a server that phones can reach. |
| D6 | Uploaded logos are stored as normalized PNG blobs in SQLite rather than as files in an `assets/` folder. A backup is then one file, and no user-supplied path is ever touched. |
| D7 | PDF and EPS are written by hand from the shared vector scene, with no PDF library. They use real gradients (PDF shadings and PostScript `shfill`) and vector text outlines. |
| D8 | Bulk CSV column mapping uses `header=field` pairs (`--map`, a form field, or an API field) instead of a separate `mapping.toml` file, so no TOML dependency is needed. |
| D9 | The REST API uses a bearer token from Settings rather than the web UI's form token, so scripts can call it while web pages on other sites cannot. |
| D10 | After encoding, the data mask is re-chosen. Masks are tried from the lowest ISO/IEC 18004 penalty up, and the first one a ZXing-equivalent reader decodes from a plain render wins. The encoder library's own choice failed ZXing detection for about 1% of codes at level H, because finder-like patterns in the data caused false finder detection. A test re-masks every version (1–40) with every mask and requires an exact decode. |

## 16. Open questions

| # | Question |
|---|---|
| Q1 | ~~Should a serif frame font be bundled?~~ Resolved: Libre Baskerville Bold (154 KB, SIL OFL 1.1) is bundled. |

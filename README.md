# Canute's QR Generator

Static QR codes and barcodes, made locally. No ads, no accounts, no tracking, no network calls.
Written in Go and shipped as a single binary for Windows, macOS and Linux. See [SPEC.md](SPEC.md)
for the full specification.

**What it does**

- **13 content types:** link, text, e-mail, call, SMS, WhatsApp, Wi-Fi, vCard, calendar event,
  map location, PayPal.me, crypto payment (Bitcoin, Ethereum, Litecoin, with address checksum
  verification), and barcodes (Data Matrix, PDF417, Aztec, Code 128, EAN-13, UPC-A).
- **Design:** 9 module shapes, 5 corner-border and 6 corner-center shapes (optionally styled per
  corner), linear and radial gradients, 15 built-in logo icons or your own PNG/JPEG/SVG logo,
  and 9 frames with a call-to-action phrase in sans-serif, serif, monospace or your own font.
- **Export:** PNG, JPEG, SVG, PDF (A4, Letter or fit-to-code) and EPS, with no watermark.
- **Scan check:** every code is decoded after rendering, and you are warned about low
  contrast, light-on-dark colors, a thin quiet zone or a too-small export.
- **Library, presets, bulk creation from CSV, backup and restore**, a local REST API, and a
  web UI in English, German, Spanish, French, Italian and Portuguese.

## Install

Download a release archive, or build it yourself with Go 1.26 or newer:

```bash
go build -o canute-qr ./cmd/canute-qr
```

## Web UI

```bash
canute-qr serve --open
```

This opens http://127.0.0.1:8080. Pick a content type, style the code with a live preview, then
download it or save it to your library. Library, presets, logos and settings live in one SQLite
file in your user config folder (`--data-dir` to change). The UI only listens on your own
machine.

## Command line

```bash
canute-qr generate --data https://example.com -o link.png
canute-qr generate --type wifi --ssid Home --password secret -o wifi.svg
canute-qr generate --type barcode --symbology ean13 --data 590123412345 -o product.pdf
canute-qr generate --data example.com --shape dots --eye-border circle --logo icon:link --frame banner-bottom --frame-text "Scan me" -o styled.pdf
canute-qr batch --csv codes.csv --type url --format svg -o codes.zip
canute-qr backup -o backup.zip
canute-qr restore backup.zip
canute-qr types     # content types and their fields
canute-qr styles    # shapes, frames, fonts and logo icons
```

Run `canute-qr COMMAND -h` for all flags. Add `--strict` to `generate` or `batch` to fail
instead of warning when a code may not scan.

### Bulk CSV

Name the columns after content fields; `type` and `name` columns are optional:

```csv
type,name,url,ssid,password
url,Shop,example.com/shop,,
wifi,Guest Wi-Fi,,Guest,secret
```

The ZIP contains one image per row and a `report.csv` with every row's result. Map other
headers with `--map "Website=url,Network=ssid"`.

## REST API

The server also offers a JSON API at `/api/v1`. Copy the token from **Settings** in the web UI:

```bash
curl -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"type":"url","fields":{"url":"example.com"},"design":{"module":"dots"},"export":{"format":"svg"}}' \
  http://127.0.0.1:8080/api/v1/render -o code.svg
```

The full description is at http://127.0.0.1:8080/api/v1/openapi.json.

## Docker (optional)

For an always-on server on your own network:

```bash
docker build -t canute-qr .
docker run -p 127.0.0.1:8080:8080 -v canute-qr-data:/data canute-qr
```

The web UI has no password. Publish the port on `127.0.0.1` unless other devices should
reach it, and pass the host names you will browse to with `--allowed-hosts`.

## Development

```bash
go test ./...
go vet ./...
```

Releases are built by GoReleaser (`.goreleaser.yaml`) when a `v*` tag is pushed.

## License

Apache 2.0 (see [LICENSE](LICENSE)). The bundled Libre Baskerville font is under the SIL Open
Font License; see `internal/render/fonts/`.

# Canute's QR Generator

Static QR codes, made locally. No ads, no accounts, no tracking, no network calls.
Written in Go; ships as a single binary. See [SPEC.md](SPEC.md) for the full plan.

> **Status:** milestones M1 (core engine + CLI), M2 (web UI, library, presets) and M3 (full design: shapes, gradients, logos, frames; PNG, JPEG, SVG, PDF and EPS export) are done. Next: M4, bulk generation, REST API and 2D barcodes.

## Build

Requires Go 1.26 or newer.

```bash
go build -o canute-qr ./cmd/canute-qr
```

## Use

### Web UI

```bash
canute-qr serve --open
```

Opens http://127.0.0.1:8080: pick a content type, style it with a live preview, then
download it or save it to your library. Your library, presets and settings are stored in
SQLite in your user config folder (`--data-dir` to change). The UI only listens on your
own machine.

### Command line

```bash
canute-qr generate --data https://example.com -o link.png
canute-qr generate --type wifi --ssid Home --password secret -o wifi.svg
canute-qr generate --type vcard --first Canute --last Knutsson --mobile +4512345678 -o me.png
canute-qr generate --type event --title "Tide test" --start "2026-10-11 10:00" --tz Europe/London -o event.png
canute-qr generate --data example.com --shape dots --eye-border circle --logo icon:link \n  --frame banner-bottom --frame-text "Scan me" -o styled.pdf
canute-qr types          # all content types and their fields
canute-qr styles         # shapes, frames, fonts and logo icons
canute-qr generate -h    # colors, size, error correction, quiet zone, ...
```

Content types: `url`, `text`, `email`, `call`, `sms`, `whatsapp`, `wifi`, `vcard`, `event`.
Output formats: PNG, JPEG, SVG, PDF (A4, Letter or fit-to-code) and EPS.

Every code is decoded after rendering. If it might not scan (low contrast,
light-on-dark, missing quiet zone, too small), you get a warning; add
`--strict` to fail instead.

## Test

```bash
go test ./...
```

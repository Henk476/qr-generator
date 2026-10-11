# Canute's QR Generator

Static QR codes, made locally. No ads, no accounts, no tracking, no network calls.
Written in Go; ships as a single binary. See [SPEC.md](SPEC.md) for the full plan.

> **Status:** milestone M1 (core engine + CLI). The web UI arrives in M2.

## Build

Requires Go 1.26 or newer.

```bash
go build -o canute-qr ./cmd/canute-qr
```

## Use

```bash
canute-qr generate --data https://example.com -o link.png
canute-qr generate --type wifi --ssid Home --password secret -o wifi.svg
canute-qr generate --type vcard --first Canute --last Knutsson --mobile +4512345678 -o me.png
canute-qr generate --type event --title "Tide test" --start "2026-10-11 10:00" --tz Europe/London -o event.png
canute-qr types          # all content types and their fields
canute-qr generate -h    # colors, size, error correction, quiet zone, ...
```

Content types: `url`, `text`, `email`, `call`, `sms`, `whatsapp`, `wifi`, `vcard`, `event`.
Output formats: PNG and SVG.

Every code is decoded after rendering. If it might not scan (low contrast,
light-on-dark, missing quiet zone, too small), you get a warning; add
`--strict` to fail instead.

## Test

```bash
go test ./...
```

package encode

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // IANA zones for the event "tz" field, even on Windows

	"github.com/Henk476/qr-generator/internal/barcode"
)

func init() {
	register(Type{
		Name: "url", Title: "Link", Primary: "url",
		Fields: []Field{{Name: "url", Label: "Website address", Kind: KindURL, Placeholder: "https://example.com",
			Usage: "web address; https:// is added when no scheme is given", Required: true}},
		build: buildURL,
	})
	register(Type{
		Name: "text", Title: "Text", Primary: "text",
		Fields: []Field{{Name: "text", Label: "Text", Kind: KindTextarea, Usage: "free text", Required: true}},
		build:  func(f fields) (string, error) { return f["text"], nil },
	})
	register(Type{
		Name: "email", Title: "E-mail", Primary: "to",
		Fields: []Field{
			{Name: "to", Label: "To", Kind: KindEmail, Placeholder: "name@example.com", Usage: "recipient address", Required: true},
			{Name: "subject", Label: "Subject", Usage: "subject line"},
			{Name: "body", Label: "Message", Kind: KindTextarea, Usage: "message body"},
		},
		build: buildEmail,
	})
	register(Type{
		Name: "call", Title: "Call", Primary: "phone",
		Fields: []Field{{Name: "phone", Label: "Phone number", Kind: KindTel, Placeholder: "+15551234567",
			Usage: "phone number, international format preferred (+15551234567)", Required: true}},
		build: func(f fields) (string, error) {
			p, err := normalizePhone(f["phone"])
			if err != nil {
				return "", err
			}
			return "tel:" + p, nil
		},
	})
	register(Type{
		Name: "sms", Title: "SMS", Primary: "phone",
		Fields: []Field{
			{Name: "phone", Label: "Phone number", Kind: KindTel, Placeholder: "+15551234567", Usage: "recipient phone number", Required: true},
			{Name: "message", Label: "Message", Kind: KindTextarea, Usage: "prefilled message"},
		},
		build: func(f fields) (string, error) {
			p, err := normalizePhone(f["phone"])
			if err != nil {
				return "", err
			}
			return "SMSTO:" + p + ":" + f["message"], nil
		},
	})
	register(Type{
		Name: "whatsapp", Title: "WhatsApp", Primary: "phone",
		Fields: []Field{
			{Name: "phone", Label: "WhatsApp number", Kind: KindTel, Placeholder: "+15551234567",
				Usage: "WhatsApp number in international format (+15551234567)", Required: true},
			{Name: "message", Label: "Message", Kind: KindTextarea, Usage: "prefilled message"},
		},
		build: buildWhatsApp,
	})
	register(Type{
		Name: "wifi", Title: "Wi-Fi", Primary: "ssid",
		Fields: []Field{
			{Name: "ssid", Label: "Network name (SSID)", Usage: "network name", Required: true},
			{Name: "password", Label: "Password", Usage: "network password"},
			{Name: "enc", Label: "Security", Kind: KindSelect, Options: []string{"WPA", "WEP", "nopass"},
				Usage: "WPA (also WPA2/WPA3), WEP or nopass; default WPA with a password, nopass without"},
			{Name: "hidden", Label: "Hidden network", Kind: KindCheckbox, Usage: "true if the network does not broadcast its name"},
		},
		build: buildWiFi,
	})
	register(Type{
		Name: "vcard", Title: "vCard",
		Fields: []Field{
			{Name: "first", Label: "First name", Usage: "first name"},
			{Name: "last", Label: "Last name", Usage: "last name"},
			{Name: "org", Label: "Organization", Usage: "organization"},
			{Name: "title", Label: "Job title", Usage: "job title"},
			{Name: "phone", Label: "Work phone", Kind: KindTel, Usage: "work phone"},
			{Name: "mobile", Label: "Mobile phone", Kind: KindTel, Usage: "mobile phone"},
			{Name: "email", Label: "E-mail", Kind: KindEmail, Usage: "e-mail address"},
			{Name: "url", Label: "Website", Kind: KindURL, Usage: "website"},
			{Name: "street", Label: "Street", Usage: "street address"},
			{Name: "city", Label: "City", Usage: "city"},
			{Name: "region", Label: "State / region", Usage: "state or region"},
			{Name: "zip", Label: "Postal code", Usage: "postal code"},
			{Name: "country", Label: "Country", Usage: "country"},
			{Name: "note", Label: "Note", Kind: KindTextarea, Usage: "note"},
			{Name: "variant", Label: "Format", Kind: KindSelect, Options: []string{"vcard", "mecard"},
				Usage: "vcard (default) or mecard (smaller code, fewer fields)"},
		},
		build: buildVCard,
	})
	register(Type{
		Name: "event", Title: "Event", Primary: "title",
		Fields: []Field{
			{Name: "title", Label: "Event name", Usage: "event name", Required: true},
			{Name: "start", Label: "Start", Placeholder: "2026-10-11 14:30", Usage: "start: 2026-10-11, 2026-10-11 14:30 or RFC 3339", Required: true},
			{Name: "end", Label: "End", Placeholder: "2026-10-11 16:00", Usage: "end, same formats as start; default 1 hour (or 1 day for all-day events)"},
			{Name: "tz", Label: "Time zone", Placeholder: "Europe/Amsterdam", Usage: "IANA time zone for start/end, e.g. Europe/Amsterdam; times are converted to UTC"},
			{Name: "location", Label: "Location", Usage: "location"},
			{Name: "description", Label: "Description", Kind: KindTextarea, Usage: "description"},
		},
		build: buildEvent,
	})
	register(Type{
		Name: "location", Title: "Location",
		Fields: []Field{
			{Name: "latitude", Label: "Latitude", Placeholder: "52.3676", Required: true,
				Usage: "decimal degrees, -90 to 90 (north is positive)"},
			{Name: "longitude", Label: "Longitude", Placeholder: "4.9041", Required: true,
				Usage: "decimal degrees, -180 to 180 (east is positive)"},
			{Name: "label", Label: "Place name", Usage: "shown on the map pin by apps that support it"},
		},
		build: buildLocation,
	})
	register(Type{
		Name: "paypal", Title: "PayPal", Primary: "username",
		Fields: []Field{
			{Name: "username", Label: "PayPal.me name", Placeholder: "canute", Required: true,
				Usage: "the name in your paypal.me link"},
			{Name: "amount", Label: "Amount", Placeholder: "12.50", Usage: "optional amount to request"},
			{Name: "currency", Label: "Currency", Placeholder: "EUR", Usage: "optional 3-letter currency code, e.g. EUR or USD"},
		},
		build: buildPayPal,
	})
	register(Type{
		Name: "crypto", Title: "Crypto payment", Primary: "address",
		Fields: []Field{
			{Name: "currency", Label: "Currency", Kind: KindSelect, Options: CryptoCurrencies, Required: true,
				Usage: "bitcoin, ethereum or litecoin"},
			{Name: "address", Label: "Wallet address", Required: true,
				Usage: "the receiving address; its checksum is verified to catch typos"},
			{Name: "amount", Label: "Amount", Usage: "optional amount in the coin, e.g. 0.001"},
			{Name: "label", Label: "Label", Usage: "optional recipient name (Bitcoin and Litecoin)"},
			{Name: "message", Label: "Message", Kind: KindTextarea, Usage: "optional payment note (Bitcoin and Litecoin)"},
		},
		build: buildCrypto,
	})
	syms := barcode.Symbologies()
	symNames := make([]string, len(syms))
	for i, s := range syms {
		symNames[i] = string(s.Symbology)
	}
	register(Type{
		Name: "barcode", Title: "Barcode", Primary: "value",
		Fields: []Field{
			{Name: "symbology", Label: "Barcode format", Kind: KindSelect, Options: symNames, Required: true,
				Usage: "datamatrix, pdf417, aztec, code128, ean13 or upca"},
			{Name: "value", Label: "Value", Required: true,
				Usage: "the value to encode; EAN-13 takes 12 digits and UPC-A 11, and the check digit is added"},
		},
		build: func(f fields) (string, error) {
			return barcode.Normalize(barcode.Symbology(f["symbology"]), f["value"])
		},
	})
}

var schemeRE = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)

func buildURL(f fields) (string, error) {
	raw := f["url"]
	if !schemeRE.MatchString(raw) {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if s := strings.ToLower(u.Scheme); s != "http" && s != "https" {
		return "", fmt.Errorf("url scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("url has no host")
	}
	return raw, nil
}

func buildEmail(f fields) (string, error) {
	to := f["to"]
	if !strings.Contains(to, "@") || strings.ContainsAny(to, " ?&") {
		return "", fmt.Errorf("invalid e-mail address %q", to)
	}
	var q []string
	if s := f["subject"]; s != "" {
		q = append(q, "subject="+mailtoEscape(s))
	}
	if b := f["body"]; b != "" {
		q = append(q, "body="+mailtoEscape(b))
	}
	out := "mailto:" + to
	if len(q) > 0 {
		out += "?" + strings.Join(q, "&")
	}
	return out, nil
}

// mailtoEscape percent-encodes a mailto header value. Spaces become %20
// because mail clients do not reliably decode "+".
func mailtoEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

var phoneRE = regexp.MustCompile(`^\+?[0-9]{3,15}$`)

// normalizePhone strips common separators and checks the result looks like a
// dialable number (optionally E.164 with a leading +).
func normalizePhone(s string) (string, error) {
	p := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", ".", "").Replace(s)
	if !phoneRE.MatchString(p) {
		return "", fmt.Errorf("invalid phone number %q", s)
	}
	return p, nil
}

func buildWhatsApp(f fields) (string, error) {
	p, err := normalizePhone(f["phone"])
	if err != nil {
		return "", err
	}
	out := "https://wa.me/" + strings.TrimPrefix(p, "+")
	if m := f["message"]; m != "" {
		out += "?text=" + url.QueryEscape(m)
	}
	return out, nil
}

// escapeMeCard escapes the characters that are special in WIFI: and MECARD:
// payloads.
var escapeMeCard = strings.NewReplacer(`\`, `\\`, `;`, `\;`, `,`, `\,`, `:`, `\:`, `"`, `\"`).Replace

func buildWiFi(f fields) (string, error) {
	enc := strings.ToUpper(f["enc"])
	switch enc {
	case "":
		enc = "WPA"
		if f["password"] == "" {
			enc = "nopass"
		}
	case "WPA", "WPA2", "WPA3":
		enc = "WPA"
	case "WEP":
	case "NOPASS", "NONE", "OPEN":
		enc = "nopass"
	default:
		return "", fmt.Errorf("enc must be WPA, WEP or nopass, got %q", f["enc"])
	}
	if enc != "nopass" && f["password"] == "" {
		return "", fmt.Errorf("a password is required for %s networks", enc)
	}
	hidden := false
	if h := f["hidden"]; h != "" {
		var err error
		if hidden, err = strconv.ParseBool(h); err != nil {
			return "", fmt.Errorf("hidden must be true or false, got %q", h)
		}
	}

	var b strings.Builder
	b.WriteString("WIFI:T:" + enc + ";S:" + escapeMeCard(f["ssid"]) + ";")
	if enc != "nopass" {
		b.WriteString("P:" + escapeMeCard(f["password"]) + ";")
	}
	if hidden {
		b.WriteString("H:true;")
	}
	b.WriteString(";")
	return b.String(), nil
}

// escapeText escapes a TEXT value for vCard 3.0 and iCalendar.
var escapeText = strings.NewReplacer(`\`, `\\`, `;`, `\;`, `,`, `\,`, "\r\n", `\n`, "\n", `\n`).Replace

func buildVCard(f fields) (string, error) {
	if f["first"] == "" && f["last"] == "" && f["org"] == "" {
		return "", errors.New("at least one of first, last or org is required")
	}
	switch strings.ToLower(f["variant"]) {
	case "", "vcard":
		return buildVCard3(f), nil
	case "mecard":
		return buildMeCard(f), nil
	default:
		return "", fmt.Errorf("variant must be vcard or mecard, got %q", f["variant"])
	}
}

func buildVCard3(f fields) string {
	var lines []string
	add := func(s string) { lines = append(lines, s) }
	addIf := func(prefix, v string) {
		if v != "" {
			add(prefix + escapeText(v))
		}
	}

	add("BEGIN:VCARD")
	add("VERSION:3.0")
	add("N:" + escapeText(f["last"]) + ";" + escapeText(f["first"]) + ";;;")
	fn := strings.TrimSpace(f["first"] + " " + f["last"])
	if fn == "" {
		fn = f["org"]
	}
	add("FN:" + escapeText(fn))
	addIf("ORG:", f["org"])
	addIf("TITLE:", f["title"])
	addIf("TEL;TYPE=CELL:", f["mobile"])
	addIf("TEL;TYPE=WORK,VOICE:", f["phone"])
	addIf("EMAIL:", f["email"])
	addIf("URL:", f["url"])
	if f["street"]+f["city"]+f["region"]+f["zip"]+f["country"] != "" {
		add("ADR;TYPE=WORK:;;" + strings.Join([]string{
			escapeText(f["street"]), escapeText(f["city"]), escapeText(f["region"]),
			escapeText(f["zip"]), escapeText(f["country"]),
		}, ";"))
	}
	addIf("NOTE:", f["note"])
	add("END:VCARD")
	return strings.Join(lines, "\r\n")
}

func buildMeCard(f fields) string {
	var b strings.Builder
	b.WriteString("MECARD:N:")
	b.WriteString(escapeMeCard(f["last"]))
	if f["first"] != "" {
		if f["last"] != "" {
			b.WriteString(",")
		}
		b.WriteString(escapeMeCard(f["first"]))
	}
	b.WriteString(";")
	addIf := func(key, v string) {
		if v != "" {
			b.WriteString(key + ":" + escapeMeCard(v) + ";")
		}
	}
	addIf("ORG", f["org"])
	addIf("TEL", f["mobile"])
	addIf("TEL", f["phone"])
	addIf("EMAIL", f["email"])
	addIf("URL", f["url"])
	var adr []string
	for _, k := range []string{"street", "city", "region", "zip", "country"} {
		if f[k] != "" {
			adr = append(adr, f[k])
		}
	}
	addIf("ADR", strings.Join(adr, " "))
	addIf("NOTE", f["note"])
	b.WriteString(";")
	return b.String()
}

// eventTime is a parsed start or end time.
type eventTime struct {
	t        time.Time
	allDay   bool // date only
	floating bool // no zone known: local time wherever the scanner is
}

func (e eventTime) format() string {
	switch {
	case e.allDay:
		return ";VALUE=DATE:" + e.t.Format("20060102")
	case e.floating:
		return ":" + e.t.Format("20060102T150405")
	default:
		return ":" + e.t.UTC().Format("20060102T150405Z")
	}
}

func parseEventTime(s string, loc *time.Location) (eventTime, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return eventTime{t: t}, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.UTC); err == nil {
		return eventTime{t: t, allDay: true}, nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if loc == nil {
			if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
				return eventTime{t: t, floating: true}, nil
			}
		} else if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return eventTime{t: t}, nil
		}
	}
	return eventTime{}, fmt.Errorf("cannot parse time %q (use 2026-10-11, 2026-10-11 14:30 or RFC 3339)", s)
}

func buildEvent(f fields) (string, error) {
	var loc *time.Location
	if tz := f["tz"]; tz != "" {
		var err error
		if loc, err = time.LoadLocation(tz); err != nil {
			return "", fmt.Errorf("unknown time zone %q", tz)
		}
	}
	start, err := parseEventTime(f["start"], loc)
	if err != nil {
		return "", err
	}
	var end eventTime
	if f["end"] == "" {
		end = start
		if start.allDay {
			end.t = start.t.AddDate(0, 0, 1)
		} else {
			end.t = start.t.Add(time.Hour)
		}
	} else {
		if end, err = parseEventTime(f["end"], loc); err != nil {
			return "", err
		}
		if end.allDay != start.allDay {
			return "", errors.New("start and end must both be dates or both be date-times")
		}
		if end.floating != start.floating {
			return "", errors.New("start and end must both have a time zone or both have none")
		}
		if start.allDay {
			// iCalendar all-day end dates are exclusive; users give inclusive ones.
			end.t = end.t.AddDate(0, 0, 1)
		}
		if !end.t.After(start.t) {
			return "", errors.New("end must be after start")
		}
	}

	lines := []string{
		"BEGIN:VEVENT",
		"SUMMARY:" + escapeText(f["title"]),
		"DTSTART" + start.format(),
		"DTEND" + end.format(),
	}
	if v := f["location"]; v != "" {
		lines = append(lines, "LOCATION:"+escapeText(v))
	}
	if v := f["description"]; v != "" {
		lines = append(lines, "DESCRIPTION:"+escapeText(v))
	}
	lines = append(lines, "END:VEVENT")
	return strings.Join(lines, "\r\n"), nil
}

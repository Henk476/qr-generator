package encode

import (
	"strings"
	"testing"
)

func TestBuildGolden(t *testing.T) {
	tests := []struct {
		name string
		typ  string
		in   map[string]string
		want string
	}{
		{"url keeps scheme", "url", map[string]string{"url": "http://example.com/a?b=c"}, "http://example.com/a?b=c"},
		{"url adds https", "url", map[string]string{"url": " example.com/path "}, "https://example.com/path"},
		{"url with port", "url", map[string]string{"url": "example.com:8080"}, "https://example.com:8080"},
		{"text", "text", map[string]string{"text": "héllo wörld"}, "héllo wörld"},
		{"email plain", "email", map[string]string{"to": "a@b.co"}, "mailto:a@b.co"},
		{"email full", "email", map[string]string{"to": "a@b.co", "subject": "Hi there", "body": "a&b=c+d"},
			"mailto:a@b.co?subject=Hi%20there&body=a%26b%3Dc%2Bd"},
		{"call", "call", map[string]string{"phone": "+1 (555) 123-4567"}, "tel:+15551234567"},
		{"sms", "sms", map[string]string{"phone": "0612345678", "message": "On my way"}, "SMSTO:0612345678:On my way"},
		{"whatsapp", "whatsapp", map[string]string{"phone": "+31 6 1234 5678", "message": "Hi & bye"},
			"https://wa.me/31612345678?text=Hi+%26+bye"},
		{"wifi wpa", "wifi", map[string]string{"ssid": "Home", "password": "secret"}, "WIFI:T:WPA;S:Home;P:secret;;"},
		{"wifi escaped hidden", "wifi", map[string]string{"ssid": `My;Net`, "password": `p:a,s\s"`, "enc": "wpa2", "hidden": "true"},
			`WIFI:T:WPA;S:My\;Net;P:p\:a\,s\\s\";H:true;;`},
		{"wifi open", "wifi", map[string]string{"ssid": "Cafe"}, "WIFI:T:nopass;S:Cafe;;"},
		{"wifi wep", "wifi", map[string]string{"ssid": "Old", "password": "12345", "enc": "WEP"}, "WIFI:T:WEP;S:Old;P:12345;;"},
		{"vcard", "vcard", map[string]string{
			"first": "Canute", "last": "Knutsson", "org": "North Sea, Ltd", "mobile": "+4412345",
			"email": "c@k.dk", "city": "Winchester", "country": "England", "note": "line1\nline2",
		}, strings.Join([]string{
			"BEGIN:VCARD", "VERSION:3.0", "N:Knutsson;Canute;;;", "FN:Canute Knutsson",
			`ORG:North Sea\, Ltd`, "TEL;TYPE=CELL:+4412345", "EMAIL:c@k.dk",
			"ADR;TYPE=WORK:;;;Winchester;;;England", `NOTE:line1\nline2`, "END:VCARD",
		}, "\r\n")},
		{"vcard org only", "vcard", map[string]string{"org": "Acme"},
			"BEGIN:VCARD\r\nVERSION:3.0\r\nN:;;;;\r\nFN:Acme\r\nORG:Acme\r\nEND:VCARD"},
		{"mecard", "vcard", map[string]string{"first": "Canute", "last": "Knutsson", "phone": "+4412345", "email": "c@k.dk", "variant": "mecard"},
			"MECARD:N:Knutsson,Canute;TEL:+4412345;EMAIL:c@k.dk;;"},
		{"event utc", "event", map[string]string{"title": "Tide, test", "start": "2026-10-11T14:00:00+02:00"},
			"BEGIN:VEVENT\r\nSUMMARY:Tide\\, test\r\nDTSTART:20261011T120000Z\r\nDTEND:20261011T130000Z\r\nEND:VEVENT"},
		{"event tz", "event", map[string]string{"title": "T", "start": "2026-07-01 09:30", "end": "2026-07-01 11:00", "tz": "Europe/London", "location": "Bosham"},
			"BEGIN:VEVENT\r\nSUMMARY:T\r\nDTSTART:20260701T083000Z\r\nDTEND:20260701T100000Z\r\nLOCATION:Bosham\r\nEND:VEVENT"},
		{"event floating", "event", map[string]string{"title": "T", "start": "2026-07-01 09:30"},
			"BEGIN:VEVENT\r\nSUMMARY:T\r\nDTSTART:20260701T093000\r\nDTEND:20260701T103000\r\nEND:VEVENT"},
		{"event all day", "event", map[string]string{"title": "T", "start": "2026-07-01", "end": "2026-07-03"},
			"BEGIN:VEVENT\r\nSUMMARY:T\r\nDTSTART;VALUE=DATE:20260701\r\nDTEND;VALUE=DATE:20260704\r\nEND:VEVENT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Build(tt.typ, tt.in)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if got != tt.want {
				t.Errorf("got\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestBuildErrors(t *testing.T) {
	tests := []struct {
		name    string
		typ     string
		in      map[string]string
		wantErr string
	}{
		{"unknown type", "fax", nil, "unknown content type"},
		{"unknown field", "url", map[string]string{"url": "a.com", "nope": "x"}, "unknown field"},
		{"missing required", "url", map[string]string{}, "required"},
		{"blank required", "text", map[string]string{"text": "   "}, "required"},
		{"bad scheme", "url", map[string]string{"url": "javascript://alert(1)"}, "scheme"},
		{"no host", "url", map[string]string{"url": "https://"}, "no host"},
		{"bad email", "email", map[string]string{"to": "nobody"}, "invalid e-mail"},
		{"bad phone", "call", map[string]string{"phone": "call me"}, "invalid phone"},
		{"wifi no password", "wifi", map[string]string{"ssid": "x", "enc": "WPA"}, "password is required"},
		{"wifi bad enc", "wifi", map[string]string{"ssid": "x", "enc": "WPA9"}, "enc must be"},
		{"wifi bad hidden", "wifi", map[string]string{"ssid": "x", "hidden": "maybe"}, "hidden"},
		{"vcard empty", "vcard", map[string]string{"email": "a@b.c"}, "at least one"},
		{"vcard variant", "vcard", map[string]string{"org": "a", "variant": "xml"}, "variant"},
		{"event bad time", "event", map[string]string{"title": "t", "start": "tomorrow"}, "cannot parse"},
		{"event bad tz", "event", map[string]string{"title": "t", "start": "2026-01-01 10:00", "tz": "Mars/Base"}, "time zone"},
		{"event end before start", "event", map[string]string{"title": "t", "start": "2026-01-02 10:00", "end": "2026-01-01 10:00"}, "after start"},
		{"event mixed", "event", map[string]string{"title": "t", "start": "2026-01-02", "end": "2026-01-03 10:00"}, "both be dates"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Build(tt.typ, tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestTypesRegistered(t *testing.T) {
	want := []string{"call", "email", "event", "sms", "text", "url", "vcard", "whatsapp", "wifi"}
	ts := Types()
	if len(ts) != len(want) {
		t.Fatalf("got %d types, want %d", len(ts), len(want))
	}
	for i, ty := range ts {
		if ty.Name != want[i] {
			t.Errorf("type %d = %q, want %q", i, ty.Name, want[i])
		}
		if ty.Primary != "" && !ty.hasField(ty.Primary) {
			t.Errorf("%s: primary field %q is not a field", ty.Name, ty.Primary)
		}
	}
}

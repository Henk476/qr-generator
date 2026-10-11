package encode

import (
	"crypto/sha256"
	"math/big"
	"strings"
	"testing"
)

// base58Check builds a valid Base58Check string for tests.
func base58Check(version byte, body []byte) string {
	payload := append([]byte{version}, body...)
	h1 := sha256.Sum256(payload)
	h2 := sha256.Sum256(h1[:])
	full := append(payload, h2[:4]...)
	n := new(big.Int).SetBytes(full)
	var out []byte
	for n.Sign() > 0 {
		m := new(big.Int)
		n.DivMod(n, big.NewInt(58), m)
		out = append([]byte{base58Alphabet[m.Int64()]}, out...)
	}
	for _, b := range full {
		if b != 0 {
			break
		}
		out = append([]byte{'1'}, out...)
	}
	return string(out)
}

func TestPaymentGolden(t *testing.T) {
	ltc := base58Check(0x30, make([]byte, 20))
	tests := []struct {
		name, typ string
		in        map[string]string
		want      string
	}{
		{"location", "location", map[string]string{"latitude": "52.3676", "longitude": "4.90410"}, "geo:52.3676,4.9041"},
		{"location comma decimals and label", "location", map[string]string{"latitude": "-33,9249", "longitude": "18,4241", "label": "Table (Mountain)"},
			"geo:-33.9249,18.4241?q=-33.9249,18.4241(Table%20Mountain)"},
		{"paypal", "paypal", map[string]string{"username": "canute"}, "https://paypal.me/canute"},
		{"paypal amount currency", "paypal", map[string]string{"username": "paypal.me/canute", "amount": "12,50", "currency": "eur"}, "https://paypal.me/canute/12.5EUR"},
		{"bitcoin p2pkh", "crypto", map[string]string{"currency": "bitcoin", "address": "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2", "amount": "0.0010", "label": "Canute", "message": "Tide fund"},
			"bitcoin:1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2?amount=0.001&label=Canute&message=Tide%20fund"},
		{"bitcoin p2sh", "crypto", map[string]string{"currency": "bitcoin", "address": "3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy"}, "bitcoin:3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy"},
		{"bitcoin bech32 upper case", "crypto", map[string]string{"currency": "bitcoin", "address": "BC1QAR0SRRR7XFKVY5L643LYDNW9RE59GTZZWF5MDQ"}, "bitcoin:BC1QAR0SRRR7XFKVY5L643LYDNW9RE59GTZZWF5MDQ"},
		{"bitcoin bech32", "crypto", map[string]string{"currency": "bitcoin", "address": "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq"}, "bitcoin:bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq"},
		{"bitcoin taproot bech32m", "crypto", map[string]string{"currency": "bitcoin", "address": "bc1p0xlxvlhemja6c4dqv22uapctqupfhlxm9h8z3k2e72q4k9hcz7vqzk5jj0"},
			"bitcoin:bc1p0xlxvlhemja6c4dqv22uapctqupfhlxm9h8z3k2e72q4k9hcz7vqzk5jj0"},
		{"litecoin", "crypto", map[string]string{"currency": "litecoin", "address": ltc, "amount": "1"}, "litecoin:" + ltc + "?amount=1"},
		{"ethereum checksummed", "crypto", map[string]string{"currency": "ethereum", "address": "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed", "amount": "1.5"},
			"ethereum:0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed?value=1500000000000000000"},
		{"ethereum lower case", "crypto", map[string]string{"currency": "ethereum", "address": "0xfb6916095ca1df60bb79ce92ce3ea74c37c5d359", "amount": "0.000000000000000001"},
			"ethereum:0xfb6916095ca1df60bb79ce92ce3ea74c37c5d359?value=1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Build(tt.typ, tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestPaymentErrors(t *testing.T) {
	tests := []struct {
		name, typ string
		in        map[string]string
		want      string
	}{
		{"latitude range", "location", map[string]string{"latitude": "91", "longitude": "0"}, "between -90 and 90"},
		{"longitude text", "location", map[string]string{"latitude": "1", "longitude": "east"}, "decimal number"},
		{"paypal name", "paypal", map[string]string{"username": "can ute"}, "1-20 letters"},
		{"paypal currency without amount", "paypal", map[string]string{"username": "c", "currency": "EUR"}, "needs an amount"},
		{"paypal currency", "paypal", map[string]string{"username": "c", "amount": "1", "currency": "EURO"}, "3-letter"},
		{"paypal decimals", "paypal", map[string]string{"username": "c", "amount": "1.005"}, "more than 2 decimals"},
		{"zero amount", "paypal", map[string]string{"username": "c", "amount": "0.00"}, "more than zero"},
		{"unknown coin", "crypto", map[string]string{"currency": "dogecoin", "address": "x"}, "bitcoin, ethereum or litecoin"},
		{"bitcoin typo", "crypto", map[string]string{"currency": "bitcoin", "address": "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN3"}, "checksum is wrong"},
		{"bitcoin bad char", "crypto", map[string]string{"currency": "bitcoin", "address": "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN0"}, "not a valid character"},
		{"bech32 typo", "crypto", map[string]string{"currency": "bitcoin", "address": "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdp"}, "checksum is wrong"},
		{"bech32 mixed case", "crypto", map[string]string{"currency": "bitcoin", "address": "bc1qAR0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq"}, "mixes upper and lower"},
		{"litecoin address for bitcoin", "crypto", map[string]string{"currency": "bitcoin", "address": base58Check(0x30, make([]byte, 20))}, "not a bitcoin address"},
		{"ethereum short", "crypto", map[string]string{"currency": "ethereum", "address": "0x123"}, "40 hexadecimal"},
		{"ethereum checksum", "crypto", map[string]string{"currency": "ethereum", "address": "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAeD"}, "checksum is wrong"},
		{"ethereum label", "crypto", map[string]string{"currency": "ethereum", "address": "0xfb6916095ca1df60bb79ce92ce3ea74c37c5d359", "label": "x"}, "no label"},
		{"bitcoin decimals", "crypto", map[string]string{"currency": "bitcoin", "address": "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2", "amount": "0.123456789"}, "more than 8 decimals"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Build(tt.typ, tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestEIP55Vectors(t *testing.T) {
	for _, a := range []string{
		"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		"0xfB6916095ca1df60bB79Ce92cE3Ea74c37c5d359",
		"0xdbF03B407c01E7cD3CBea99509d93f8DDDC8C6FB",
		"0xD1220A0cf47c7B9Be7A2E6BA89F429762e7b9aDb",
	} {
		if err := validateEthereumAddress(a); err != nil {
			t.Errorf("%s: %v", a, err)
		}
	}
}

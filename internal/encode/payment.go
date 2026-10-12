package encode

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/sha3"
)

// ---- Location ----

func buildLocation(f fields) (string, error) {
	lat, err := coordinate(f["latitude"], "latitude", 90)
	if err != nil {
		return "", err
	}
	lng, err := coordinate(f["longitude"], "longitude", 180)
	if err != nil {
		return "", err
	}
	out := "geo:" + lat + "," + lng
	if label := strings.TrimSpace(strings.NewReplacer("(", "", ")", "").Replace(f["label"])); label != "" {
		// The common "q=lat,lng(label)" form shows a named pin on Android
		// and in most map apps; others ignore the query.
		out += "?q=" + lat + "," + lng + "(" + strings.ReplaceAll(url.QueryEscape(label), "+", "%20") + ")"
	}
	return out, nil
}

// coordinate validates a decimal degree and returns it trimmed of noise.
func coordinate(s, name string, limit float64) (string, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return "", fmt.Errorf("%s must be a decimal number such as 52.3676, got %q", name, s)
	}
	if v < -limit || v > limit {
		return "", fmt.Errorf("%s must be between -%g and %g, got %g", name, limit, limit, v)
	}
	return strconv.FormatFloat(v, 'f', -1, 64), nil
}

// ---- PayPal ----

var (
	paypalUserRE = regexp.MustCompile(`^[A-Za-z0-9]{1,20}$`)
	currencyRE   = regexp.MustCompile(`^[A-Z]{3}$`)
)

func buildPayPal(f fields) (string, error) {
	user := strings.TrimPrefix(strings.TrimPrefix(f["username"], "https://"), "paypal.me/")
	if !paypalUserRE.MatchString(user) {
		return "", fmt.Errorf("PayPal.me name must be 1-20 letters or digits, got %q", f["username"])
	}
	out := "https://paypal.me/" + user
	if f["amount"] == "" {
		if f["currency"] != "" {
			return "", errors.New("a currency needs an amount")
		}
		return out, nil
	}
	amount, err := decimalAmount(f["amount"], 2)
	if err != nil {
		return "", err
	}
	out += "/" + amount
	if c := strings.ToUpper(f["currency"]); c != "" {
		if !currencyRE.MatchString(c) {
			return "", fmt.Errorf("currency must be a 3-letter code such as EUR or USD, got %q", f["currency"])
		}
		out += c
	}
	return out, nil
}

// decimalAmount validates a positive amount with at most maxDecimals
// fraction digits and returns it in canonical form ("1.50" -> "1.5").
func decimalAmount(s string, maxDecimals int) (string, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	if !isDigits(whole) || (frac != "" && !isDigits(frac)) {
		return "", fmt.Errorf("amount must be a number such as 12.50, got %q", s)
	}
	if len(frac) > maxDecimals {
		return "", fmt.Errorf("amount has more than %d decimals: %q", maxDecimals, s)
	}
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	frac = strings.TrimRight(frac, "0")
	if whole == "0" && frac == "" {
		return "", errors.New("amount must be more than zero")
	}
	if frac == "" {
		return whole, nil
	}
	return whole + "." + frac, nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// ---- Crypto ----

// cryptoCoins describes the supported payment URI schemes.
var cryptoCoins = map[string]struct {
	decimals int
	validate func(string) error
}{
	"bitcoin":  {8, func(a string) error { return validateUTXOAddress(a, "bitcoin", "bc", []byte{0x00, 0x05}) }},
	"litecoin": {8, func(a string) error { return validateUTXOAddress(a, "litecoin", "ltc", []byte{0x30, 0x32, 0x05}) }},
	"ethereum": {18, validateEthereumAddress},
}

// CryptoCurrencies lists the supported coins.
var CryptoCurrencies = []string{"bitcoin", "ethereum", "litecoin"}

func buildCrypto(f fields) (string, error) {
	coin := strings.ToLower(f["currency"])
	c, ok := cryptoCoins[coin]
	if !ok {
		return "", fmt.Errorf("currency must be bitcoin, ethereum or litecoin, got %q", f["currency"])
	}
	addr := strings.TrimSpace(f["address"])
	if err := c.validate(addr); err != nil {
		return "", err
	}

	if coin == "ethereum" {
		// EIP-681: the amount is an integer number of wei.
		if f["label"] != "" || f["message"] != "" {
			return "", errors.New("Ethereum payment links have no label or message")
		}
		out := "ethereum:" + addr
		if f["amount"] != "" {
			amount, err := decimalAmount(f["amount"], c.decimals)
			if err != nil {
				return "", err
			}
			out += "?value=" + toBaseUnits(amount, c.decimals)
		}
		return out, nil
	}

	// BIP 21 (Bitcoin) and its Litecoin equivalent.
	var q []string
	if f["amount"] != "" {
		amount, err := decimalAmount(f["amount"], c.decimals)
		if err != nil {
			return "", err
		}
		q = append(q, "amount="+amount)
	}
	if v := f["label"]; v != "" {
		q = append(q, "label="+mailtoEscape(v))
	}
	if v := f["message"]; v != "" {
		q = append(q, "message="+mailtoEscape(v))
	}
	out := coin + ":" + addr
	if len(q) > 0 {
		out += "?" + strings.Join(q, "&")
	}
	return out, nil
}

// toBaseUnits converts a canonical decimal amount to integer base units.
func toBaseUnits(amount string, decimals int) string {
	whole, frac, _ := strings.Cut(amount, ".")
	frac += strings.Repeat("0", decimals-len(frac))
	n, _ := new(big.Int).SetString(strings.TrimLeft(whole+frac, "0"), 10)
	if n == nil {
		return "0"
	}
	return n.String()
}

// validateUTXOAddress accepts a Base58Check address with one of the given
// version bytes, or a SegWit address (bech32 or bech32m) with hrp.
func validateUTXOAddress(addr, coin, hrp string, versions []byte) error {
	lower := strings.ToLower(addr)
	if strings.HasPrefix(lower, hrp+"1") {
		if addr != lower && addr != strings.ToUpper(addr) {
			return fmt.Errorf("%s address mixes upper and lower case", coin)
		}
		if !bech32Valid(lower, hrp) {
			return fmt.Errorf("%s address checksum is wrong: check for a typo", coin)
		}
		return nil
	}
	payload, err := base58CheckDecode(addr)
	if err != nil {
		return fmt.Errorf("%s address %w", coin, err)
	}
	if len(payload) != 21 || !strings.ContainsRune(string(versions), rune(payload[0])) {
		return fmt.Errorf("not a %s address", coin)
	}
	return nil
}

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58CheckDecode decodes and verifies a Base58Check string, returning
// the payload without the 4-byte checksum.
func base58CheckDecode(s string) ([]byte, error) {
	if len(s) < 26 || len(s) > 35 {
		return nil, errors.New("has the wrong length")
	}
	n := new(big.Int)
	for _, r := range s {
		i := strings.IndexRune(base58Alphabet, r)
		if i < 0 {
			return nil, fmt.Errorf("contains %q, which is not a valid character", r)
		}
		n.Mul(n, big.NewInt(58))
		n.Add(n, big.NewInt(int64(i)))
	}
	b := n.Bytes()
	for _, r := range s { // leading '1's are leading zero bytes
		if r != '1' {
			break
		}
		b = append([]byte{0}, b...)
	}
	if len(b) < 5 {
		return nil, errors.New("is too short")
	}
	payload, sum := b[:len(b)-4], b[len(b)-4:]
	h1 := sha256.Sum256(payload)
	h2 := sha256.Sum256(h1[:])
	if string(h2[:4]) != string(sum) {
		return nil, errors.New("checksum is wrong: check for a typo")
	}
	return payload, nil
}

// bech32Valid checks a lower-case bech32 or bech32m string (BIP 173 / 350)
// with the given human-readable part.
func bech32Valid(s, hrp string) bool {
	const charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"
	if len(s) < 14 || len(s) > 90 || !strings.HasPrefix(s, hrp+"1") {
		return false
	}
	data := s[len(hrp)+1:]
	if len(data) < 6 {
		return false
	}
	values := make([]int, 0, len(hrp)*2+1+len(data))
	for _, c := range hrp {
		values = append(values, int(c)>>5)
	}
	values = append(values, 0)
	for _, c := range hrp {
		values = append(values, int(c)&31)
	}
	for _, c := range data {
		i := strings.IndexRune(charset, c)
		if i < 0 {
			return false
		}
		values = append(values, i)
	}
	gen := []int{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	chk := 1
	for _, v := range values {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ v
		for i := 0; i < 5; i++ {
			if (top>>i)&1 == 1 {
				chk ^= gen[i]
			}
		}
	}
	return chk == 1 || chk == 0x2bc830a3 // bech32 or bech32m
}

var ethAddrRE = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

// validateEthereumAddress checks the format and, for mixed-case
// addresses, the EIP-55 checksum.
func validateEthereumAddress(addr string) error {
	if !ethAddrRE.MatchString(addr) {
		return errors.New("an Ethereum address is 0x followed by 40 hexadecimal characters")
	}
	hexPart := addr[2:]
	if hexPart == strings.ToLower(hexPart) || hexPart == strings.ToUpper(hexPart) {
		return nil // no checksum to verify
	}
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(strings.ToLower(hexPart)))
	hash := hex.EncodeToString(h.Sum(nil))
	for i, c := range hexPart {
		if c >= 'a' && c <= 'f' && hash[i] >= '8' || c >= 'A' && c <= 'F' && hash[i] < '8' {
			return errors.New("Ethereum address checksum is wrong: check for a typo")
		}
	}
	return nil
}

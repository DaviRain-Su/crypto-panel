package intents

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/davirain-su/lintel/internal/base58"
)

const productionManagerPublicKey = "ed25519:reYaWhvwu8Jzo3WUM3zhn6VrhuMEF4eADL17qtRVifc"
const quoteFreshness = 5 * time.Minute

var signedRequestFields = []string{
	"dry",
	"swapType",
	"slippageTolerance",
	"originAsset",
	"depositType",
	"destinationAsset",
	"amount",
	"refundTo",
	"refundType",
	"recipient",
	"recipientType",
	"deadline",
}

var signedDryQuoteFields = []string{
	"amountIn",
	"amountInFormatted",
	"amountInUsd",
	"minAmountIn",
	"amountOut",
	"amountOutFormatted",
	"amountOutUsd",
	"minAmountOut",
}

var signedWetQuoteFields = []string{
	"depositAddress",
	"depositMemo",
	"deadline",
	"timeWhenInactive",
	"timeEstimate",
	"refundFee",
	"withdrawFee",
}

var optionalSignedRequestFields = []string{
	"quoteWaitingTimeMs",
	"referral",
	"virtualChainRecipient",
	"virtualChainRefundRecipient",
	"customRecipientMsg",
}

// verifyQuoteResponse checks the original response, not a re-marshaled Go
// struct. Asset amounts remain raw JSON strings, while protocol numbers are
// canonicalized with JavaScript number semantics before stable serialization.
// The returned hash is the message Ed25519 signs.
func verifyQuoteResponse(data []byte, publicKey string, now time.Time) (string, error) {
	return verifySignedQuoteResponse(data, publicKey, now, true)
}

// verifySignedQuoteResponse optionally applies current-time freshness. Status
// responses contain an historical quote: its original signature and timestamp
// remain mandatory, but it does not become invalid as execution progresses.
func verifySignedQuoteResponse(data []byte, publicKey string, now time.Time, requireFreshness bool) (string, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil || top == nil {
		return "", errors.New("1Click: the signed quote could not be read")
	}
	timestamp, ok := jsonString(top["timestamp"])
	if !ok {
		return "", errors.New("1Click: the quote has no valid timestamp")
	}
	if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
		return "", errors.New("1Click: the quote has an invalid timestamp")
	}
	if requireFreshness {
		if err := checkQuoteTimestamp(timestamp, now); err != nil {
			return "", err
		}
	}
	payload, err := signedQuotePayload(top)
	if err != nil {
		return "", err
	}
	signature, ok := jsonString(top["signature"])
	if !ok || signature == "" {
		return "", errors.New("1Click: the quote has no signature")
	}
	sig, err := decodeEd25519(signature, ed25519.SignatureSize)
	if err != nil {
		return "", fmt.Errorf("1Click: invalid quote signature: %w", err)
	}
	key, err := decodeEd25519(publicKey, ed25519.PublicKeySize)
	if err != nil {
		return "", fmt.Errorf("1Click: invalid quote verifier key: %w", err)
	}
	digest := sha256.Sum256(payload)
	hash := base58.Encode(digest[:])
	if !ed25519.Verify(ed25519.PublicKey(key), []byte(hash), sig) {
		return "", errors.New("1Click: the quote signature is invalid")
	}
	return hash, nil
}

func checkQuoteTimestamp(timestamp string, now time.Time) error {
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return errors.New("1Click: the quote has an invalid timestamp")
	}
	delta := now.Sub(parsed)
	if delta < -quoteFreshness || delta > quoteFreshness {
		return errors.New("1Click: the quote timestamp is outside the five-minute freshness window")
	}
	return nil
}

func decodeEd25519(value string, size int) ([]byte, error) {
	value = strings.TrimPrefix(value, "ed25519:")
	decoded, err := base58.Decode(value)
	if err != nil {
		return nil, err
	}
	if len(decoded) != size {
		return nil, fmt.Errorf("decoded length is %d, want %d", len(decoded), size)
	}
	return decoded, nil
}

// signedQuotePayload is compatible with json-stable-stringify for the fields
// selected by one-click-sdk-typescript's quote-signature.ts. Required values
// remain raw JSON; object keys are sorted and optional values use JavaScript
// truthiness exactly as the SDK does.
func signedQuotePayload(top map[string]json.RawMessage) ([]byte, error) {
	request, err := rawObject(top["quoteRequest"])
	if err != nil {
		return nil, errors.New("1Click: the signed quote request is missing")
	}
	quote, err := rawObject(top["quote"])
	if err != nil {
		return nil, errors.New("1Click: the signed quote is missing")
	}
	timestamp, ok := jsonString(top["timestamp"])
	if !ok {
		return nil, errors.New("1Click: the quote has no valid timestamp")
	}

	fields := make(map[string]json.RawMessage, 25)
	for _, name := range signedRequestFields {
		raw := request[name]
		if !validRequiredJSON(raw) {
			return nil, fmt.Errorf("1Click: signed request field %q is missing", name)
		}
		if name != "dry" && name != "slippageTolerance" {
			if _, ok := jsonString(raw); !ok {
				return nil, fmt.Errorf("1Click: signed request field %q is not a string", name)
			}
		}
		fields[name] = raw
	}
	var dry bool
	if err := json.Unmarshal(request["dry"], &dry); err != nil {
		return nil, errors.New("1Click: the signed dry flag is invalid")
	}
	if !validJSONNumber(request["slippageTolerance"]) {
		return nil, errors.New("1Click: the signed slippage tolerance is invalid")
	}
	for _, name := range signedDryQuoteFields {
		raw, ok := quote[name]
		if !ok {
			return nil, fmt.Errorf("1Click: signed quote field %q is missing", name)
		}
		if _, ok := jsonString(raw); !ok {
			return nil, fmt.Errorf("1Click: signed quote field %q is not a string", name)
		}
		fields[name] = raw
	}
	if !dry {
		for _, name := range signedWetQuoteFields {
			if raw, ok := quote[name]; ok && javascriptTruthy(raw) {
				fields[name] = raw
			}
		}
	}
	for _, name := range optionalSignedRequestFields {
		if raw, ok := request[name]; ok && javascriptTruthy(raw) {
			fields[name] = raw
		}
	}
	fields["timestamp"], _ = json.Marshal(timestamp)
	return stableJSONObject(fields)
}

func rawObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return nil, errors.New("not an object")
	}
	return out, nil
}

func validRequiredJSON(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) != 0 && string(raw) != "null" && json.Valid(raw)
}

func validJSONNumber(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}
	for i, c := range raw {
		if (c < '0' || c > '9') && c != '-' && c != '+' && c != '.' && c != 'e' && c != 'E' {
			return false
		}
		if i == 0 && c == '+' {
			return false
		}
	}
	var number json.Number
	return json.Unmarshal(raw, &number) == nil
}

func javascriptTruthy(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "false" {
		return false
	}
	if string(raw) == `""` {
		return false
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		text := number.String()
		if text == "0" || text == "-0" || text == "0.0" || text == "0e0" || text == "-0e0" {
			return false
		}
		// JSON numbers with a zero coefficient are false at every exponent.
		coefficient := text
		if i := strings.IndexAny(coefficient, "eE"); i >= 0 {
			coefficient = coefficient[:i]
		}
		coefficient = strings.NewReplacer("-", "", ".", "").Replace(coefficient)
		return strings.Trim(coefficient, "0") != ""
	}
	return true
}

func stableJSONObject(fields map[string]json.RawMessage) ([]byte, error) {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []byte{'{'}
	for i, key := range keys {
		if i != 0 {
			out = append(out, ',')
		}
		encodedKey, _ := json.Marshal(key)
		out = append(out, javascriptString(encodedKey)...)
		out = append(out, ':')
		raw := bytes.TrimSpace(fields[key])
		if !json.Valid(raw) {
			return nil, fmt.Errorf("invalid JSON for %q", key)
		}
		if len(raw) != 0 && raw[0] == '"' {
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, fmt.Errorf("invalid string for %q: %w", key, err)
			}
			encoded, _ := json.Marshal(value)
			raw = javascriptString(encoded)
		} else if validJSONNumber(raw) {
			// json-stable-stringify has already parsed JSON into JavaScript
			// numbers. Match JSON.stringify rather than retaining spelling such
			// as 1e2. Amounts never take this branch: they remain exact strings.
			number, err := strconv.ParseFloat(string(raw), 64)
			if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
				return nil, fmt.Errorf("invalid JavaScript number for %q", key)
			}
			raw = []byte(javascriptNumber(number))
		}
		out = append(out, raw...)
	}
	return append(out, '}'), nil
}

// javascriptNumber follows JSON.stringify's finite-number notation. Go's
// shortest representation has different exponent thresholds and includes a
// plus sign and padded exponent, neither of which JavaScript emits.
func javascriptNumber(number float64) string {
	absolute := math.Abs(number)
	if number == 0 {
		return "0"
	}
	if absolute >= 1e-6 && absolute < 1e21 {
		return strconv.FormatFloat(number, 'f', -1, 64)
	}
	formatted := strconv.FormatFloat(number, 'e', -1, 64)
	parts := strings.SplitN(formatted, "e", 2)
	exponent, _ := strconv.Atoi(parts[1])
	sign := "+"
	if exponent < 0 {
		sign = "-"
		exponent = -exponent
	}
	return parts[0] + "e" + sign + strconv.Itoa(exponent)
}

// encoding/json uses HTML-safe string escapes. JSON.stringify, and therefore
// json-stable-stringify, leaves these five runes unescaped.
func javascriptString(encoded []byte) []byte {
	text := string(encoded)
	for _, replacement := range []struct{ old, new string }{
		{`\u003c`, `<`}, {`\u003e`, `>`}, {`\u0026`, `&`},
		{`\u2028`, "\u2028"}, {`\u2029`, "\u2029"},
	} {
		text = strings.ReplaceAll(text, replacement.old, replacement.new)
	}
	return []byte(text)
}

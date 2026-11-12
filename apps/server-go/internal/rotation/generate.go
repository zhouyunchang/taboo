package rotation

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strings"
)

// Generation is the per-policy generation profile. The server owns the
// catalog; clients pick a profile and may override the random length.
type Generation struct {
	Profile        string `json:"profile"`
	Length         int    `json:"length"`
	Prefix         string `json:"prefix"`
	MinEntropyBits int    `json:"min_entropy_bits"`
}

type generationProfile struct {
	charset       string
	defaultLength int
	minLength     int
	maxLength     int
	bytes         int // non-zero: draw this many random bytes (base64/hex)
	encoding      string
	requiresClass bool
}

const minGenerationEntropy = 128

var generationProfiles = map[string]generationProfile{
	// 58 chars: A-Z a-z 0-9 minus I l O 0.
	"alnum32":     {charset: "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz123456789", defaultLength: 32, minLength: 16, maxLength: 128},
	"alnum64":     {charset: "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz123456789", defaultLength: 64, minLength: 16, maxLength: 128},
	"base64url32": {bytes: 32, encoding: "base64url", defaultLength: 43, minLength: 43, maxLength: 43},
	"hex32":       {bytes: 32, encoding: "hex", defaultLength: 64, minLength: 64, maxLength: 64},
	"password24": {
		charset:       "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%^&*()-_=+[]{}:,.?",
		defaultLength: 24, minLength: 24, maxLength: 32, requiresClass: true,
	},
	"custom": {charset: "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz123456789", defaultLength: 32, minLength: 16, maxLength: 128},
}

// NormalizeGeneration validates a requested profile and fills defaults.
func NormalizeGeneration(in Generation) (Generation, error) {
	profile := strings.TrimSpace(strings.ToLower(in.Profile))
	if profile == "" {
		profile = "alnum32"
	}
	p, ok := generationProfiles[profile]
	if !ok {
		return Generation{}, fmt.Errorf("unknown generation profile")
	}
	length := in.Length
	if length == 0 {
		length = p.defaultLength
	}
	if length < p.minLength || length > p.maxLength {
		return Generation{}, fmt.Errorf("generation length out of range")
	}
	prefix := strings.TrimSpace(in.Prefix)
	if len(prefix) > 32 {
		return Generation{}, fmt.Errorf("generation prefix too long")
	}
	if strings.ContainsAny(prefix, " \t\r\n\x00") {
		return Generation{}, fmt.Errorf("generation prefix contains whitespace")
	}
	entropy := generationEntropy(profile, length)
	if entropy < minGenerationEntropy {
		return Generation{}, fmt.Errorf("generation profile below 128 bit entropy")
	}
	return Generation{
		Profile:        profile,
		Length:         length,
		Prefix:         prefix,
		MinEntropyBits: entropy,
	}, nil
}

func generationEntropy(profile string, length int) int {
	p := generationProfiles[profile]
	if p.bytes > 0 {
		return p.bytes * 8
	}
	if p.charset == "" {
		return 0
	}
	return int(math.Floor(float64(length) * math.Log2(float64(len(p.charset)))))
}

// GenerateValue returns a new random value for the normalized profile.
func GenerateValue(g Generation) (string, error) {
	p, ok := generationProfiles[g.Profile]
	if !ok {
		return "", fmt.Errorf("unknown generation profile")
	}
	if g.Length < p.minLength || g.Length > p.maxLength {
		return "", fmt.Errorf("generation length out of range")
	}
	if generationEntropy(g.Profile, g.Length) < minGenerationEntropy {
		return "", fmt.Errorf("generation profile below 128 bit entropy")
	}

	var body string
	switch p.encoding {
	case "base64url":
		buf := make([]byte, p.bytes)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		body = base64.RawURLEncoding.EncodeToString(buf)
	case "hex":
		buf := make([]byte, p.bytes)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		body = hex.EncodeToString(buf)
	default:
		if p.charset == "" {
			return "", fmt.Errorf("generation profile has no charset")
		}
		chars := []byte(p.charset)
		if p.requiresClass {
			body = generateWithClasses(chars, g.Length)
		} else {
			var b strings.Builder
			b.Grow(g.Length)
			for i := 0; i < g.Length; i++ {
				n, err := randomInt(len(chars))
				if err != nil {
					return "", err
				}
				b.WriteByte(chars[n])
			}
			body = b.String()
		}
	}
	return g.Prefix + body, nil
}

func generateWithClasses(charset []byte, length int) string {
	// Guarantee one upper, lower, digit and symbol, then fill and shuffle.
	var upper, lower, digit, symbol []byte
	for _, c := range charset {
		switch {
		case c >= 'A' && c <= 'Z':
			upper = append(upper, c)
		case c >= 'a' && c <= 'z':
			lower = append(lower, c)
		case c >= '0' && c <= '9':
			digit = append(digit, c)
		default:
			symbol = append(symbol, c)
		}
	}
	pool := make([]byte, 0, length)
	for _, class := range [][]byte{upper, lower, digit, symbol} {
		if len(class) == 0 {
			continue
		}
		n, _ := randomInt(len(class))
		pool = append(pool, class[n])
	}
	for len(pool) < length {
		n, _ := randomInt(len(charset))
		pool = append(pool, charset[n])
	}
	// Fisher-Yates with crypto randomness.
	for i := len(pool) - 1; i > 0; i-- {
		j, err := randomInt(i + 1)
		if err != nil {
			break
		}
		pool[i], pool[j] = pool[j], pool[i]
	}
	return string(pool)
}

func randomInt(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("invalid random range")
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(v.Int64()), nil
}

package sources

import (
	"errors"
	"math"
	"math/bits"
	"strconv"
	"strings"
)

// Prices are fixed point: fiat with 2 decimals (cents), BTC with 8 (satoshis).
const (
	FiatScale = 2
	BTCScale  = 8
)

var errDecimal = errors.New("invalid decimal")

// Scale returns how many decimals prices quoted in quote carry.
func Scale(quote string) int {
	if quote == "BTC" {
		return BTCScale
	}
	return FiatScale
}

func pow10(n int) uint64 {
	p := uint64(1)
	for range n {
		p *= 10
	}
	return p
}

// ParseDecimal reads decimal text such as "312.4567" or "3.05e-3" into an
// integer with scale decimals, rounding half up. No floats are involved.
func ParseDecimal(s string, scale int) (uint64, error) {
	s = strings.TrimSpace(s)
	mantissa, exp := s, 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		e, err := strconv.Atoi(s[i+1:])
		if err != nil || e < -64 || e > 64 {
			return 0, errDecimal
		}
		mantissa, exp = s[:i], e
	}
	intPart, fracPart, _ := strings.Cut(mantissa, ".")
	digits := intPart + fracPart
	if digits == "" || strings.Trim(digits, "0123456789") != "" {
		return 0, errDecimal
	}

	keep := len(intPart) + exp + scale
	var v uint64
	for i := range max(keep, 0) {
		d := uint64(0)
		if i < len(digits) {
			d = uint64(digits[i] - '0')
		}
		if v > (math.MaxUint64-d)/10 {
			return 0, errDecimal
		}
		v = v*10 + d
	}
	if keep >= 0 && keep < len(digits) && digits[keep] >= '5' {
		if v == math.MaxUint64 {
			return 0, errDecimal
		}
		v++
	}
	return v, nil
}

// FormatDecimal is the inverse of ParseDecimal.
func FormatDecimal(v uint64, scale int) string {
	s := strconv.FormatUint(v, 10)
	if scale == 0 {
		return s
	}
	if len(s) <= scale {
		s = strings.Repeat("0", scale-len(s)+1) + s
	}
	return s[:len(s)-scale] + "." + s[len(s)-scale:]
}

// mulDiv returns round(a*b/c) using 128-bit intermediates.
func mulDiv(a, b, c uint64) (uint64, bool) {
	hi, lo := bits.Mul64(a, b)
	if c == 0 || hi >= c {
		return 0, false
	}
	q, rem := bits.Div64(hi, lo, c)
	if rem >= c-rem {
		if q == math.MaxUint64 {
			return 0, false
		}
		q++
	}
	return q, true
}

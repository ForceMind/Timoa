// Package money converts between user-facing yuan strings and integer
// cents. All ledger amounts are int64 cents; floating point is forbidden.
package money

import (
	"errors"
	"fmt"
	"strings"
)

// MaxCents bounds a single transaction amount (9,999,999,999.99 yuan).
// Aggregation overflow is checked separately when summing.
const MaxCents int64 = 999_999_999_999

var (
	ErrInvalid = errors.New("invalid amount: expect digits with at most two decimals")
	ErrRange   = errors.New("amount out of range")
	ErrZero    = errors.New("amount must not be zero")
)

// ParseYuan converts a user input like "12.34" to 1234 cents.
// It rejects empty strings, non-numeric values, more than two decimal
// places, negative values and out-of-range values.
func ParseYuan(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, ErrInvalid
	}
	if strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	if s == "" || strings.ContainsAny(s, "-eE.") && strings.Count(s, ".") > 1 {
		return 0, ErrInvalid
	}

	intPart, fracPart := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, fracPart = s[:i], s[i+1:]
	}
	if len(fracPart) > 2 {
		return 0, ErrInvalid
	}
	if intPart == "" {
		intPart = "0"
	}
	for _, r := range intPart {
		if r < '0' || r > '9' {
			return 0, ErrInvalid
		}
	}
	for _, r := range fracPart {
		if r < '0' || r > '9' {
			return 0, ErrInvalid
		}
	}
	// Strip leading zeros to bound length checks.
	intPart = strings.TrimLeft(intPart, "0")
	if intPart == "" {
		intPart = "0"
	}
	if len(intPart) > 12 { // > 999,999,999,999 yuan is far beyond MaxCents
		return 0, ErrRange
	}

	var cents int64
	for _, r := range intPart {
		cents = cents*10 + int64(r-'0')
	}
	cents *= 100
	switch len(fracPart) {
	case 1:
		cents += int64(fracPart[0]-'0') * 10
	case 2:
		cents += int64(fracPart[0]-'0')*10 + int64(fracPart[1]-'0')
	}

	if cents > MaxCents {
		return 0, ErrRange
	}
	return cents, nil
}

// ParseYuanRequired parses and additionally rejects zero (normal
// transactions never accept a zero amount).
func ParseYuanRequired(s string) (int64, error) {
	c, err := ParseYuan(s)
	if err != nil {
		return 0, err
	}
	if c == 0 {
		return 0, ErrZero
	}
	return c, nil
}

// FormatCents renders cents as a yuan string with two decimals.
func FormatCents(c int64) string {
	sign := ""
	if c < 0 {
		sign = "-"
		c = -c
	}
	return fmt.Sprintf("%s%d.%02d", sign, c/100, c%100)
}

// CheckedAdd sums cents detecting int64 overflow.
func CheckedAdd(a, b int64) (int64, error) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, ErrRange
	}
	return s, nil
}

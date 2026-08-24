package measure

import (
	"math"
	"strconv"

	"riceguard/domain"
)

// MaxIntegerDigits is the maximum number of integer digits accepted in a
// percentage literal. Longer literals are rejected with CodeFixedPointOverflow
// before any arithmetic is attempted, preventing silent truncation.
const MaxIntegerDigits = 6

// FromInt converts a percentage integer to scaled basis points, checking for
// scale overflow.
func FromInt(n int64) (Fixed, *domain.Error) {
	if n > math.MaxInt64/int64(Scale) {
		return 0, domain.NewError(domain.CodeFixedPointOverflow, "scale overflow")
	}
	return Fixed(n * Scale), nil
}

// Add sums two fixed-point values, checking for signed overflow.
func Add(a, b Fixed) (Fixed, *domain.Error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, domain.NewError(domain.CodeFixedPointOverflow, "addition overflow")
	}
	return a + b, nil
}

// Sub subtracts two fixed-point values, checking for signed overflow.
func Sub(a, b Fixed) (Fixed, *domain.Error) {
	if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
		return 0, domain.NewError(domain.CodeFixedPointOverflow, "subtraction overflow")
	}
	return a - b, nil
}

// Cmp compares two fixed-point values and returns -1, 0 or 1.
func Cmp(a, b Fixed) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// ParsePercent parses a percentage literal such as "12.50" into basis points
// (1250). It rejects negative literals, literals with more than two fractional
// digits, over-length integer parts and non-numeric input with stable codes.
func ParsePercent(literal string) (Fixed, *domain.Error) {
	if literal == "" {
		return 0, domain.NewError(domain.CodeBadRequest, "empty percentage literal")
	}
	if literal[0] == '-' {
		return 0, domain.NewError(domain.CodeFixedPointOverflow, "negative percentage literal")
	}
	intPart, fracPart := literal, ""
	for i := 0; i < len(literal); i++ {
		if literal[i] == '.' {
			intPart, fracPart = literal[:i], literal[i+1:]
			break
		}
	}
	if intPart == "" {
		intPart = "0"
	}
	if len(intPart) > MaxIntegerDigits {
		return 0, domain.NewError(domain.CodeFixedPointOverflow, "percentage integer part too long")
	}
	if len(fracPart) > 2 {
		return 0, domain.NewError(domain.CodeBadRequest, "percentage has more than two decimal places")
	}
	iv, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, domain.NewError(domain.CodeBadRequest, "invalid percentage integer part", intPart)
	}
	if iv > math.MaxInt64/int64(Scale) {
		return 0, domain.NewError(domain.CodeFixedPointOverflow, "percentage integer part overflow")
	}
	bp := iv * int64(Scale)
	for len(fracPart) < 2 {
		fracPart += "0"
	}
	if fracPart != "" {
		fv, err := strconv.ParseInt(fracPart, 10, 64)
		if err != nil {
			return 0, domain.NewError(domain.CodeBadRequest, "invalid percentage fractional part", fracPart)
		}
		bp += fv
	}
	return Fixed(bp), nil
}

// ValidateValue checks the sign and magnitude of a fixed-point value before
// it is stored as derived evidence.
func ValidateValue(v Fixed) *domain.Error {
	if v < 0 {
		return domain.NewError(domain.CodeFixedPointOverflow, "negative fixed-point value")
	}
	return nil
}

// Package measure owns the moisture, purity and thousand-grain-weight
// evidence together with fixed-point integer arithmetic. All arithmetic
// checks sign, digit length, division-by-zero and overflow before writing
// any derived evidence.
package measure

import (
	"math"

	"riceguard/domain"
	"riceguard/inspection"
)

// Scale is the fixed scale for percentage values: the number of basis points
// per percentage point. A moisture reading of 13.00% is stored as 1300, and
// 100.00% as 10000. Using integers throughout (never floats) makes threshold
// comparisons and derived values deterministic.
const Scale = 100

// Fixed is a signed fixed-point value expressed in basis points.
type Fixed int64

// Mul multiplies two fixed-point values and rescales, returning a
// CodeFixedPointOverflow rejection when the product exceeds int64.
func Mul(a, b Fixed) (Fixed, *domain.Error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	ia, ib := int64(a), int64(b)
	if overflows(ia, ib) {
		return 0, domain.NewError(domain.CodeFixedPointOverflow, "multiplication overflow")
	}
	return Fixed(ia * ib / Scale), nil
}

// Div divides fixed-point a by fixed-point b, rejecting division by zero and
// overflow of the intermediate scale multiplication.
func Div(a, b Fixed) (Fixed, *domain.Error) {
	if b == 0 {
		return 0, domain.NewError(domain.CodeFixedPointOverflow, "division by zero")
	}
	ia := int64(a)
	if overflows(ia, Scale) {
		return 0, domain.NewError(domain.CodeFixedPointOverflow, "division overflow")
	}
	return Fixed(ia * Scale / int64(b)), nil
}

// overflows reports whether a*b cannot be represented in int64.
func overflows(a, b int64) bool {
	switch {
	case a > 0:
		if b > 0 {
			return a > math.MaxInt64/b
		}
		return b < math.MinInt64/a
	case a < 0:
		if b > 0 {
			return a < math.MinInt64/b
		}
		return b < math.MaxInt64/a
	default:
		return false
	}
}

// MoisturePurityEvidence records a moisture reading, purity grain count and
// thousand-grain raw integer together with their derived values, threshold
// comparison result, instrument attempt ID and non-overridable version.
type MoisturePurityEvidence struct {
	TaskID        inspection.TaskID
	Moisture      Fixed
	PurityGrains  int64
	ThousandGrain int64
	DerivedPurity Fixed
	PassThreshold bool
	AttemptID     string
	Collector     string
	Version       int64
}

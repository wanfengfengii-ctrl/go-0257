package measure

import "riceguard/domain"

// basisPointsPerFraction is the factor converting a pure/total fraction into
// basis points: fraction * 100 (percent) * 100 (basis points) = fraction *
// 10000. A fraction of 0.98 becomes 9800 basis points (98.00%).
const basisPointsPerFraction = 10000

// PurityDerive computes the derived purity percentage (basis points) from a
// pure grain count and a total grain count using deterministic integer
// arithmetic. It rejects a zero or negative total and a negative pure count.
func PurityDerive(pure, total int64) (Fixed, *domain.Error) {
	if total <= 0 {
		return 0, domain.NewError(domain.CodeBadRequest, "non-positive purity total")
	}
	if pure < 0 {
		return 0, domain.NewError(domain.CodeFixedPointOverflow, "negative pure grain count")
	}
	if pure > total {
		return 0, domain.NewError(domain.CodeBadRequest, "pure grains exceed total")
	}
	return Fixed(pure * basisPointsPerFraction / total), nil
}

// PurityPass reports whether a derived purity meets the locked minimum
// purity floor (both in basis points).
func PurityPass(derived Fixed, min int32) bool {
	return int64(derived) >= int64(min)
}

// ThousandGrainValidate checks a raw thousand-grain weight integer for sign
// and magnitude before it is stored. Negative or zero weights are rejected.
func ThousandGrainValidate(raw int64) *domain.Error {
	if raw <= 0 {
		return domain.NewError(domain.CodeFixedPointOverflow, "non-positive thousand-grain weight")
	}
	return nil
}

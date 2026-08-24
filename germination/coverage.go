package germination

import (
	"sort"
	"strconv"

	"riceguard/blindcode"
	"riceguard/domain"
)

// CellKey identifies a single observation cell by blind code and day age.
type CellKey struct {
	Code   blindcode.BlindCode
	DayAge DayAge
}

// Duplicate reports whether the given cell already has a valid observation
// for the same blind code and day age. Repeated day-age readings must be
// rejected before writing.
func Duplicate(cells []GerminationCell, code blindcode.BlindCode, day DayAge) bool {
	for _, c := range cells {
		if c.Valid && c.BlindCode == code && c.DayAge == day {
			return true
		}
	}
	return false
}

// MissingCells returns the sorted list of (blind code, day age) observation
// cells not yet validly covered, given the locked blind codes and day-age
// schedule. An empty result means the full grid is covered.
func MissingCells(cells []GerminationCell, codes []string, dayAges []int32) []CellKey {
	covered := make(map[CellKey]bool)
	for _, c := range cells {
		if c.Valid {
			covered[CellKey{Code: c.BlindCode, DayAge: c.DayAge}] = true
		}
	}
	var missing []CellKey
	for _, code := range codes {
		for _, d := range dayAges {
			k := CellKey{Code: blindcode.BlindCode(code), DayAge: DayAge(d)}
			if !covered[k] {
				missing = append(missing, k)
			}
		}
	}
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].Code != missing[j].Code {
			return missing[i].Code < missing[j].Code
		}
		return missing[i].DayAge < missing[j].DayAge
	})
	return missing
}

// Covered reports whether the full observation grid (every blind code at
// every day age) is validly covered.
func Covered(cells []GerminationCell, codes []string, dayAges []int32) bool {
	return len(MissingCells(cells, codes, dayAges)) == 0
}

// ValidateCell enforces the count-conservation invariant for a single
// observation cell: all counts must be non-negative and normal + abnormal +
// dead must equal the locked grain count. It returns a
// CodeGerminationDrift rejection on violation.
func ValidateCell(locked int, c GerminationCell) *domain.Error {
	if c.Normal < 0 || c.Abnormal < 0 || c.Dead < 0 {
		return domain.NewError(domain.CodeGerminationDrift,
			"negative count", string(c.BlindCode))
	}
	if !Conserved(locked, c) {
		return domain.NewError(domain.CodeGerminationDrift,
			"count drift", string(c.BlindCode), "expected", strconv.Itoa(locked),
			"got", strconv.Itoa(c.Normal+c.Abnormal+c.Dead))
	}
	return nil
}

package blindcode

import (
	"riceguard/domain"
	"riceguard/inspection"
)

// SplitMatrix is the triple-split consistency matrix for a task: one
// TripleSplit cell per blind code per aliquot class. Building the matrix
// validates blind-code uniqueness, quantity conservation and the current
// generation, and fails atomically (no partial matrix is returned).
type SplitMatrix struct {
	Cells []TripleSplit
}

// BuildMatrix constructs the triple-split matrix from a set of frozen blind
// allocations. It enforces:
//   - every blind code is non-empty and unique;
//   - every aliquot quantity is strictly positive;
//   - the matrix covers exactly the three aliquot classes per code.
//
// It returns a CodeBlindDuplicate rejection for a repeated code and a
// CodeBadRequest rejection for a non-positive quantity.
func BuildMatrix(task inspection.TaskID, gen inspection.Generation, allocs []inspection.BlindAllocation) (SplitMatrix, *domain.Error) {
	if len(allocs) == 0 {
		return SplitMatrix{}, domain.NewError(domain.CodeBadRequest, "no blind allocations")
	}
	seen := make(map[string]bool)
	var cells []TripleSplit
	for _, a := range allocs {
		if a.Code == "" {
			return SplitMatrix{}, domain.NewError(domain.CodeBadRequest, "empty blind code")
		}
		if seen[a.Code] {
			return SplitMatrix{}, domain.NewError(domain.CodeBlindDuplicate,
				"duplicate blind code", a.Code)
		}
		seen[a.Code] = true
		if a.Germination <= 0 || a.Pathogen <= 0 || a.Moisture <= 0 {
			return SplitMatrix{}, domain.NewError(domain.CodeBadRequest,
				"non-positive aliquot quantity", a.Code)
		}
		cells = append(cells,
			TripleSplit{Code: BlindCode(a.Code), Split: SplitGermination, Quantity: a.Germination, TaskID: task},
			TripleSplit{Code: BlindCode(a.Code), Split: SplitPathogen, Quantity: a.Pathogen, TaskID: task},
			TripleSplit{Code: BlindCode(a.Code), Split: SplitMoisture, Quantity: a.Moisture, TaskID: task},
		)
	}
	return SplitMatrix{Cells: cells}, nil
}

// Total returns the total allocated grains of a given split class across the
// matrix. It is used to assert quantity conservation between the locked
// allocations and the materialized matrix.
func (m SplitMatrix) Total(s SplitType) int {
	sum := 0
	for _, c := range m.Cells {
		if c.Split == s {
			sum += c.Quantity
		}
	}
	return sum
}

// Codes returns the sorted list of distinct blind codes in the matrix.
func (m SplitMatrix) Codes() []string {
	seen := make(map[string]bool)
	var out []string
	for _, c := range m.Cells {
		if !seen[string(c.Code)] {
			seen[string(c.Code)] = true
			out = append(out, string(c.Code))
		}
	}
	return out
}

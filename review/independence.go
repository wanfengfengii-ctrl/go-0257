package review

import (
	"riceguard/catalog"
	"riceguard/domain"
)

// IndependenceCheck validates that a set of review submissions satisfies the
// independent-review invariant: at least two distinct qualified reviewers,
// neither of whom overlaps with the key collectors.
type IndependenceCheck struct {
	Dir        catalog.RoleDirectory
	Collectors map[string]bool
}

// Validate checks a single review submission for qualification, overlap and
// (when another reviewer is supplied) distinctness.
func (c IndependenceCheck) Validate(reviewer ReviewerID, other ReviewerID) *domain.Error {
	if err := Qualified(c.Dir, string(reviewer)); err != nil {
		return err
	}
	if err := NoCollectorOverlap(string(reviewer), c.Collectors); err != nil {
		return err
	}
	if other != "" {
		if err := Distinct(reviewer, other); err != nil {
			return err
		}
	}
	return nil
}

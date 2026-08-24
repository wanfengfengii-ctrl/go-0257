package review

import (
	"riceguard/catalog"
	"riceguard/domain"
)

// Qualified reports whether a reviewer holds the reviewer role. A reviewer
// without the role is rejected with CodeBadRequest.
func Qualified(dir catalog.RoleDirectory, reviewer string) *domain.Error {
	p, ok := dir.Personnel(reviewer)
	if !ok {
		return domain.NewError(domain.CodeBadRequest, "unknown reviewer", reviewer)
	}
	if !p.Holds(catalog.RoleReviewer) {
		return domain.NewError(domain.CodeBadRequest, "reviewer not qualified", reviewer)
	}
	return nil
}

// Distinct reports whether two reviewers are different people. The two
// independent reviewers must never be the same person.
func Distinct(a, b ReviewerID) *domain.Error {
	if a == b {
		return domain.NewError(domain.CodeBadRequest, "reviewers must be distinct", string(a))
	}
	return nil
}

// NoCollectorOverlap reports whether a reviewer is disjoint from the key
// collectors of a task. A reviewer who also collected germination,
// pathogen or measurement evidence introduces a role overlap and is rejected.
func NoCollectorOverlap(reviewer string, collectors map[string]bool) *domain.Error {
	if collectors[reviewer] {
		return domain.NewError(domain.CodeBadRequest, "reviewer overlaps collector", reviewer)
	}
	return nil
}

package catalog

import "sort"

// Role qualifications are part of the fictitious variety and inspection rule
// catalog: a reviewer must hold a matching qualification and two independent
// reviewers must never overlap with the key collectors of a task.
//
// The role names are intentionally small and domain-specific:
//   - sampler:      qualified to perform dual sampling confirmation
//   - germinator:   qualified to record germination observations
//   - pathologist:  qualified to verify amplification readouts
//   - metrologist:  qualified to record moisture/purity measurements
//   - reviewer:     qualified to perform independent review
const (
	RoleSampler     RoleID = "sampler"
	RoleGerminator  RoleID = "germinator"
	RolePathologist RoleID = "pathologist"
	RoleMetrologist RoleID = "metrologist"
	RoleReviewer    RoleID = "reviewer"
)

// Personnel is a qualified staff member and their held roles.
type Personnel struct {
	ID    string
	Roles []RoleID
}

// Holds reports whether the personnel holds the given role.
func (p Personnel) Holds(r RoleID) bool {
	for _, held := range p.Roles {
		if held == r {
			return true
		}
	}
	return false
}

// RoleDirectory is the read model of qualified personnel used to validate
// sampling confirmations and independent review.
type RoleDirectory interface {
	// Personnel returns the personnel record for an ID, if known.
	Personnel(id string) (Personnel, bool)

	// ListPeople returns every personnel record, ordered by ID.
	ListPeople() []Personnel
}

// MemoryRoles is an in-memory RoleDirectory implementation.
type MemoryRoles struct {
	people map[string]Personnel
}

// NewMemoryRoles builds an empty in-memory role directory.
func NewMemoryRoles() *MemoryRoles {
	return &MemoryRoles{people: make(map[string]Personnel)}
}

// Register adds a personnel record.
func (m *MemoryRoles) Register(p Personnel) {
	m.people[p.ID] = p
}

// Personnel implements RoleDirectory.
func (m *MemoryRoles) Personnel(id string) (Personnel, bool) {
	p, ok := m.people[id]
	return p, ok
}

// ListPeople returns every registered personnel record, ordered by ID. It
// powers the console's reviewer picker.
func (m *MemoryRoles) ListPeople() []Personnel {
	ids := make([]string, 0, len(m.people))
	for id := range m.people {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Personnel, 0, len(ids))
	for _, id := range ids {
		out = append(out, m.people[id])
	}
	return out
}

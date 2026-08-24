package catalog

import (
	"sort"

	"riceguard/domain"
)

// Memory is an in-memory, test-seeded Catalog implementation. It is a stable
// foundation catalog; production persistence can replace it behind the same
// interface.
type Memory struct {
	varieties map[VarietyID]CatalogVariety
	certs     map[ParentID]ParentCert
}

// NewMemory builds an empty in-memory catalog.
func NewMemory() *Memory {
	return &Memory{
		varieties: make(map[VarietyID]CatalogVariety),
		certs:     make(map[ParentID]ParentCert),
	}
}

// Register adds a variety rule template.
func (m *Memory) Register(v CatalogVariety) {
	m.varieties[v.ID] = v
}

// RegisterCert records an effective parental purity certificate.
func (m *Memory) RegisterCert(c ParentCert) {
	m.certs[c.Parent] = c
}

// Variety implements Catalog.
func (m *Memory) Variety(id VarietyID) (CatalogVariety, bool) {
	v, ok := m.varieties[id]
	return v, ok
}

// ListVarieties returns every registered variety rule template, ordered by ID.
// It powers the console's variety picker.
func (m *Memory) ListVarieties() []CatalogVariety {
	ids := make([]VarietyID, 0, len(m.varieties))
	for id := range m.varieties {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]CatalogVariety, 0, len(ids))
	for _, id := range ids {
		out = append(out, m.varieties[id])
	}
	return out
}

// ValidateField implements Catalog.
func (m *Memory) ValidateField(v VarietyID, f FieldID) *domain.Error {
	variety, ok := m.varieties[v]
	if !ok {
		return domain.NewError(domain.CodeVarietyMismatch, "unknown variety", string(v))
	}
	for _, allowed := range variety.AllowedFields {
		if allowed == f {
			return nil
		}
	}
	reasons := append([]string{string(f)}, string(v))
	sort.Strings(reasons)
	return domain.NewError(domain.CodeVarietyMismatch, reasons...)
}

// ValidateParentCert implements Catalog.
func (m *Memory) ValidateParentCert(p ParentID, revision int32) *domain.Error {
	cert, ok := m.certs[p]
	if !ok {
		return domain.NewError(domain.CodeStaleParentCert, "no certificate", string(p))
	}
	if revision < cert.Revision {
		return domain.NewError(domain.CodeStaleParentCert, string(p), "stale revision")
	}
	return nil
}

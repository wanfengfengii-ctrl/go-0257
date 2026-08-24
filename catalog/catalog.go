// Package catalog owns the fictitious rice variety combinations, parental
// purity certificate revisions, field compatibility, role qualifications and
// the threshold/day-age rule templates locked at task creation.
package catalog

import "riceguard/domain"

// VarietyID identifies a fictitious hybrid rice variety combination.
type VarietyID string

// ParentID identifies a parental purity line (female or male).
type ParentID string

// FieldID identifies a seed production field.
type FieldID string

// RoleID identifies a qualified laboratory role.
type RoleID string

// CatalogVariety is the locked variety combination together with its rule
// template: allowed fields, purity floor, certificate revision, day-age
// observation schedule, grain count and instrument thresholds.
type CatalogVariety struct {
	ID                 VarietyID
	FemaleParent       ParentID
	MaleParent         ParentID
	AllowedFields      []FieldID
	MinPurity          int32 // fixed-point basis points
	CertRevision       int32
	DayAges            []int32
	GrainCount         int // locked grains per aliquot for count conservation
	MoistureMax        int32
	PathogenMax        int32
	GerminationRateMin int32 // minimum germination rate (basis points) to release
}

// ParentCert is an effective parental purity certificate revision.
type ParentCert struct {
	Parent   ParentID
	Revision int32
	Purity   int32 // fixed-point basis points
}

// Catalog is the read model used to validate task creation locks. A
// production implementation is a static, test-seeded in-memory table; a
// future revision may source it from a persisted directory.
type Catalog interface {
	// Variety returns the locked rule template for a variety combination.
	Variety(VarietyID) (CatalogVariety, bool)

	// ValidateField reports whether the field is compatible with the
	// variety. An incompatible field yields CodeVarietyMismatch.
	ValidateField(VarietyID, FieldID) *domain.Error

	// ValidateParentCert reports whether the supplied certificate revision is
	// current and above the purity floor. A stale revision yields
	// CodeStaleParentCert.
	ValidateParentCert(ParentID, int32) *domain.Error

	// ListVarieties returns every variety rule template, ordered by ID.
	ListVarieties() []CatalogVariety
}

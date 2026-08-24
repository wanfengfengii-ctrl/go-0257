// Package blindcode owns the batch blind-code mapping, the one-way unblinding
// gate and the triple-split sample matrix (germination, pathogen and
// moisture/purity aliquots).
package blindcode

import (
	"riceguard/domain"
	"riceguard/inspection"
)

// BlindCode is an opaque sample blind code bound to a single task.
type BlindCode string

// SplitType enumerates the three aliquot classes of the triple-split matrix.
type SplitType string

// The three aliquot classes of a blind sample.
const (
	SplitGermination SplitType = "germination"
	SplitPathogen    SplitType = "pathogen"
	SplitMoisture    SplitType = "moisture_purity"
)

// Unblinded reports whether the code has been opened through the gate.
type Unblinded bool

// BlindSample binds a blind code to a task with its triple-split quantities,
// unblinding state and a consistency hash covering the matrix.
type BlindSample struct {
	Code            BlindCode
	TaskID          inspection.TaskID
	Generation      inspection.Generation
	Unblinded       Unblinded
	GerminationQty  int
	PathogenQty     int
	MoistureQty     int
	ConsistencyHash string
}

// TripleSplit is one cell of the triple-split consistency matrix.
type TripleSplit struct {
	Code     BlindCode
	Split    SplitType
	Quantity int
	TaskID   inspection.TaskID
}

// UnblindGate is the one-way gate. Once a blind code is opened it can never
// be re-sealed or remapped; early, duplicate, cross-generation or post-open
// mapping mutations are all rejected.
type UnblindGate interface {
	// Open performs the one-way unblinding for a code at the given
	// generation. It returns the revealed sample and a definitive rejection
	// on any early, duplicate or cross-generation attempt.
	Open(TaskID inspection.TaskID, Generation inspection.Generation, Code BlindCode) (BlindSample, *domain.Error)
}

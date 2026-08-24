// Package inspection owns the seed batch inspection task aggregate: the
// state machine, task generation, idempotency operation numbers, terminal
// fence, audit sequence and the unique release credential.
package inspection

import (
	"riceguard/catalog"
	"riceguard/domain"
)

// TaskID identifies a seed batch inspection task.
type TaskID string

// Generation is the task generation number. Every state advancement that
// opens a new evidence window increments it; stale writers are rejected by
// comparing against the current generation.
type Generation int64

// TaskStatus is the single ordered inspection state. States may only advance
// forward, except within an explicit re-chamber transaction.
type TaskStatus string

// Ordered task statuses from the approved project document.
const (
	StatusPendingCreate   TaskStatus = "pending_create"
	StatusPendingSampling TaskStatus = "pending_sampling"
	StatusBlindSplit      TaskStatus = "blind_split"
	StatusOccupying       TaskStatus = "occupying"
	StatusGerminating     TaskStatus = "germinating"
	StatusPathogen        TaskStatus = "pathogen_checking"
	StatusMoisture        TaskStatus = "moisture_checking"
	StatusPendingReview   TaskStatus = "pending_review"
	StatusReleasable      TaskStatus = "releasable"
	StatusReleased        TaskStatus = "released"
	StatusQuarantined     TaskStatus = "quarantined"
	StatusCancelled       TaskStatus = "cancelled"
)

// StatusOrder assigns each status a monotonic rank used to reject backward
// transitions. Terminal statuses rank highest and cannot be left.
var StatusOrder = map[TaskStatus]int{
	StatusPendingCreate:   0,
	StatusPendingSampling: 1,
	StatusBlindSplit:      2,
	StatusOccupying:       3,
	StatusGerminating:     4,
	StatusPathogen:        5,
	StatusMoisture:        6,
	StatusPendingReview:   7,
	StatusReleasable:      8,
	StatusReleased:        9,
	StatusQuarantined:     9,
	StatusCancelled:       9,
}

// IsTerminal reports whether the status is a valid terminal outcome.
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case StatusReleased, StatusQuarantined, StatusCancelled:
		return true
	default:
		return false
	}
}

// Before reports whether s ranks strictly before target.
func (s TaskStatus) Before(target TaskStatus) bool {
	return StatusOrder[s] < StatusOrder[target]
}

// BlindAllocation is the frozen triple-split allocation for one blind code,
// locked at task creation. It records how many grains of each aliquot class
// (germination, pathogen and moisture/purity) were allocated.
type BlindAllocation struct {
	Code        string
	Germination int
	Pathogen    int
	Moisture    int
}

// InspectionTask is the task aggregate root. It freezes the seed lot number,
// field, variety combination, certificate summary, blind-code allocations,
// chamber/plate resources, thresholds, observation schedule, grain count and
// reviewer roster at creation time.
type InspectionTask struct {
	ID              TaskID
	SeedLot         string
	Field           catalog.FieldID
	Variety         catalog.VarietyID
	FemaleParent    catalog.ParentID
	MaleParent      catalog.ParentID
	FemaleCert      int32
	MaleCert        int32
	CertSummary     string
	Status          TaskStatus
	Generation      Generation
	MoistureMax     int32 // fixed-point basis points
	PathogenMax     int32 // raw amplification threshold
	MinPurity       int32 // fixed-point basis points
	GrainCount      int   // locked grains per aliquot
	Chamber         string
	ChamberStart    uint64 // logical time window start
	ChamberEnd      uint64 // logical time window end
	Plate           string
	Wells           []string
	DayAges         []int32
	BlindAllocs     []BlindAllocation
	ReviewerRoster  []string
	TerminalVersion int64
	TerminalOutcome string // "released" | "quarantined" | "cancelled" when terminal
	CreatedAt       domain.LogicalTime
}

// NextTerminalVersion increments the terminal-competition version. The first
// finalization attempt compares-and-sets version 0 -> 1; later attempts on a
// terminal task see a non-zero version and are rejected by the terminal
// fence.
func (t *InspectionTask) NextTerminalVersion() int64 {
	t.TerminalVersion++
	return t.TerminalVersion
}

// IsTerminal reports whether the task has reached a terminal outcome.
func (t *InspectionTask) IsTerminal() bool {
	return t.Status.IsTerminal()
}

// IdempotencyRecord captures an idempotent operation keyed by operation
// number. Identical retries return the recorded result; conflicting content
// yields CodeIdempotencyConflict.
type IdempotencyRecord struct {
	OperationID   string
	TaskID        TaskID
	Generation    Generation
	RequestDigest string
	ResponseCode  domain.ErrorCode
	Reasons       []string
	ResultDigest  string
}

// SamplingConfirmation is one qualified reviewer's confirmation that the
// field, seed lot, blind-code seal and sample count match the locked values.
type SamplingConfirmation struct {
	TaskID      TaskID
	Reviewer    string
	Field       string
	SeedLot     string
	BlindSeal   string
	SampleCount int
	Generation  Generation
	OperationID string
}

// AuditEvent is an append-only, ordered audit entry.
type AuditEvent struct {
	Seq         uint64
	TaskID      TaskID
	LogicalTime domain.LogicalTime
	Actor       string
	TaskStatus  TaskStatus
	Action      string
	Code        domain.ErrorCode
	Reasons     []string
	BlindCodes  []string
	DayAges     []int32
	PlateWells  []string
}

// ReleaseCredential is the unique release credential minted exactly once at a
// successful release finalization. Its uniqueness is enforced by a terminal
// version constraint.
type ReleaseCredential struct {
	TaskID     TaskID
	Credential string
	Version    int64
}

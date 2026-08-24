// Package store defines the persistence boundary for the task aggregate and
// all of its evidence, occupancy, idempotency and audit records. Two
// implementations satisfy the boundary: an in-memory store used by
// deterministic tests, and a SQLite WAL store used in production that
// supports deterministic restart recovery.
package store

import (
	"riceguard/blindcode"
	"riceguard/domain"
	"riceguard/germination"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/occupancy"
	"riceguard/pathogen"
	"riceguard/review"
)

// Reader exposes the read side of the store. Both the Store and an open
// transaction satisfy it, so validation can read the current snapshot inside
// a transaction.
type Reader interface {
	GetTask(inspection.TaskID) (*inspection.InspectionTask, error)
	ListTasks() ([]*inspection.InspectionTask, error)

	ListConfirmations(inspection.TaskID) ([]inspection.SamplingConfirmation, error)
	ListBlindSamples(inspection.TaskID) ([]blindcode.BlindSample, error)
	ListSplits(inspection.TaskID) ([]blindcode.TripleSplit, error)

	ListOccupancies(inspection.TaskID) ([]occupancy.OccupancySlot, error)
	ListOpenOccupancies() ([]occupancy.OccupancySlot, error)

	ListGerminations(inspection.TaskID) ([]germination.GerminationCell, error)
	ListMoisture(inspection.TaskID) ([]measure.MoisturePurityEvidence, error)
	ListPathogen(inspection.TaskID) ([]pathogen.PathogenEvidence, error)
	ListAttempts(inspection.TaskID) ([]pathogen.Attempt, error)
	ListReviews(inspection.TaskID) ([]review.ReviewAndFinal, error)

	GetCredential(inspection.TaskID) (*inspection.ReleaseCredential, error)
	ListAudit(inspection.TaskID) ([]inspection.AuditEvent, error)
	ListAllAudit() ([]inspection.AuditEvent, error)
	FindOperation(string) (*inspection.IdempotencyRecord, bool)
}

// Tx is a single atomic transaction. Every write performed through a Tx is
// committed or rolled back together, so a failed triple-split or occupancy
// write never leaves partial state.
type Tx interface {
	Reader

	SaveTask(*inspection.InspectionTask) error
	SaveConfirmation(inspection.SamplingConfirmation) error
	SaveBlindSample(blindcode.BlindSample) error
	MarkBlindUnblinded(inspection.TaskID, blindcode.BlindCode) error
	SaveSplit(blindcode.TripleSplit) error
	SaveOccupancy(occupancy.OccupancySlot) error
	SaveGermination(germination.GerminationCell) error
	SaveMoisture(measure.MoisturePurityEvidence) error
	SavePathogen(pathogen.PathogenEvidence) error
	SaveAttempt(pathogen.Attempt) error
	SaveReview(review.ReviewAndFinal) error
	SaveCredential(inspection.ReleaseCredential) error
	RecordOperation(inspection.IdempotencyRecord) error
	AppendAudit(inspection.AuditEvent) error

	// NextTime issues the next logical time within the transaction, so audit
	// events and occupancy windows written atomically also carry atomic,
	// persisted logical timestamps.
	NextTime() domain.LogicalTime

	Commit() error
	Rollback() error
}

// Store is the persistence boundary. Mutate runs the supplied function inside
// a single transaction, committing on success and rolling back on any error.
type Store interface {
	Reader

	// Mutate runs fn inside a transaction. If fn returns an error the
	// transaction is rolled back and the error is returned unchanged.
	Mutate(fn func(Tx) error) error

	// NextTime issues the next persisted logical time, strictly greater than
	// all previously issued values, continuing across restarts.
	NextTime() domain.LogicalTime

	// Close releases the underlying storage.
	Close() error
}

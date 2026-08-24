// Package review owns the independent review and finalization records: the
// reviewer roster, qualification snapshots, review conclusions and the
// terminal competition that mints exactly one of released, quarantined or
// cancelled.
package review

import (
	"riceguard/inspection"
)

// ReviewerID identifies a qualified reviewer.
type ReviewerID string

// Conclusion is a reviewer's independent conclusion.
type Conclusion string

const (
	ConclusionApprove Conclusion = "approve"
	ConclusionReject  Conclusion = "reject"
)

// FinalOutcome is the single terminal outcome of a task.
type FinalOutcome string

const (
	OutcomeReleased    FinalOutcome = "released"
	OutcomeQuarantined FinalOutcome = "quarantined"
	OutcomeCancelled   FinalOutcome = "cancelled"
)

// ReviewAndFinal records a reviewer, the reviewed scope, a qualification
// snapshot, the conclusion and — for the winning finalization — the terminal
// outcome, isolation or cancellation reason and the competing terminal
// version.
type ReviewAndFinal struct {
	TaskID          inspection.TaskID
	Reviewer        ReviewerID
	Scope           string
	Qualified       bool
	Conclusion      Conclusion
	Outcome         FinalOutcome
	IsolationReason string
	CancelReason    string
	TerminalVersion int64
}

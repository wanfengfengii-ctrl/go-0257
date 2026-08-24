package review

import (
	"riceguard/domain"
	"riceguard/inspection"
)

// FinalizeDecision is the outcome of the terminal-competition arbitration. It
// mints exactly one of released, quarantined or cancelled, guarded by the
// task terminal version.
type FinalizeDecision struct {
	Outcome    FinalOutcome
	Reason     string
	Version    int64
	Credential string
}

// Quarantine reasons used when contamination forces isolation.
const (
	QuarantineReasonContaminated = "pathogen_contamination"
	QuarantineReasonPositive     = "pathogen_positive"
)

// ArbitrateFinalize resolves the terminal competition given the reviewer
// conclusions and any contamination flags. It returns a CodeFinalized
// rejection when the task is already terminal, and a CodeBadRequest rejection
// when the reviewers have not both approved.
func ArbitrateFinalize(task *inspection.InspectionTask, approved int, contaminated bool) (FinalizeDecision, *domain.Error) {
	if task.IsTerminal() {
		return FinalizeDecision{}, domain.NewError(domain.CodeFinalized, string(task.Status))
	}
	if contaminated {
		return FinalizeDecision{
			Outcome: OutcomeQuarantined,
			Reason:  QuarantineReasonContaminated,
			Version: task.NextTerminalVersion(),
		}, nil
	}
	if approved < 2 {
		return FinalizeDecision{}, domain.NewError(domain.CodeBadRequest, "insufficient approvals")
	}
	return FinalizeDecision{
		Outcome: OutcomeReleased,
		Version: task.NextTerminalVersion(),
	}, nil
}

// CancelOutcome produces a cancellation terminal decision for a task that
// must be abandoned before release.
func CancelOutcome(task *inspection.InspectionTask, reason string) (FinalizeDecision, *domain.Error) {
	if task.IsTerminal() {
		return FinalizeDecision{}, domain.NewError(domain.CodeFinalized, string(task.Status))
	}
	return FinalizeDecision{
		Outcome: OutcomeCancelled,
		Reason:  reason,
		Version: task.NextTerminalVersion(),
	}, nil
}

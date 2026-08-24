package inspection

import (
	"strconv"

	"riceguard/domain"
)

// advanceTarget is the ordered transition table. A task may only move from
// its current status to the listed next status. Transitions are validated by
// the state machine before any evidence is written.
var advanceTarget = map[TaskStatus]TaskStatus{
	StatusPendingCreate:   StatusPendingSampling,
	StatusPendingSampling: StatusBlindSplit,
	StatusBlindSplit:      StatusOccupying,
	StatusOccupying:       StatusGerminating,
	StatusGerminating:     StatusPathogen,
	StatusPathogen:        StatusMoisture,
	StatusMoisture:        StatusPendingReview,
	StatusPendingReview:   StatusReleasable,
}

// Advance validates and applies a forward state transition. It returns a
// CodeFinalized rejection for any attempt to mutate a terminal task, a
// CodeGenerationStale rejection for any transition from an unexpected
// generation, and a CodeBadRequest rejection for an illegal forward jump.
func (t *InspectionTask) Advance(target TaskStatus, gen Generation) *domain.Error {
	if t.IsTerminal() {
		return domain.NewError(domain.CodeFinalized, string(t.Status))
	}
	if gen != t.Generation {
		return domain.NewError(domain.CodeGenerationStale,
			"expected generation", genText(t.Generation), "got", genText(gen))
	}
	if advanceTarget[t.Status] != target {
		return domain.NewError(domain.CodeBadRequest,
			"illegal transition", string(t.Status), "->", string(target))
	}
	t.Status = target
	t.Generation++
	return nil
}

// CanAdvanceTo reports whether target is the legal next status for the task
// without mutating it.
func (t *InspectionTask) CanAdvanceTo(target TaskStatus) bool {
	if t.IsTerminal() {
		return false
	}
	return advanceTarget[t.Status] == target
}

func genText(g Generation) string {
	return strconv.FormatInt(int64(g), 10)
}

package api

import (
	"encoding/json"

	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/occupancy"
	"riceguard/review"
	"riceguard/store"
)

// CancelRequest cancels an open task, releasing every resource it holds.
type CancelRequest struct {
	OperationID string `json:"operation_id"`
	Reason      string `json:"reason"`
}

// CancelResponse reports the terminal cancelled outcome.
type CancelResponse struct {
	TaskID  inspection.TaskID     `json:"task_id"`
	Status  inspection.TaskStatus `json:"status"`
	Reason  string                `json:"reason"`
	Version int64                 `json:"terminal_version"`
}

// Cancel abandons an open task from any non-terminal state. It releases every
// open occupancy slot with the cancellation reason and applies the cancelled
// terminal outcome. Cancellation is guarded by the terminal version so only
// one terminal outcome can win.
func (s *Service) Cancel(id string, req CancelRequest) (CancelResponse, *domain.Error) {
	taskID := inspection.TaskID(id)
	digest := inspection.Digest(req.Reason)

	if rec, ok := s.store.FindOperation(taskID, req.OperationID); ok {
		if rec.RequestDigest != digest {
			return CancelResponse{}, domain.NewError(domain.CodeIdempotencyConflict, "operation content conflict", req.OperationID)
		}
		var resp CancelResponse
		_ = json.Unmarshal([]byte(rec.ResultDigest), &resp)
		return resp, nil
	}

	var resp CancelResponse
	err := s.store.Mutate(func(tx store.Tx) error {
		t, err := tx.GetTask(taskID)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return domain.NewError(domain.CodeFinalized, string(t.Status))
		}

		decision, derr := review.CancelOutcome(t, req.Reason)
		if derr != nil {
			return derr
		}
		applyTerminal(t, decision)

		if err := s.releaseOccupancies(tx, taskID, "cancelled"); err != nil {
			return err
		}
		if err := tx.SaveTask(t); err != nil {
			return err
		}

		resp = CancelResponse{TaskID: taskID, Status: t.Status, Reason: req.Reason, Version: decision.Version}
		tx.RecordOperation(inspection.NewRecord(req.OperationID, taskID, t.Generation, digest, encode(resp)))
		return tx.AppendAudit(audit(tx.NextTime(), "system", t.Status, "cancel", domain.CodeNone, nil))
	})
	if err != nil {
		return CancelResponse{}, asDomain(err)
	}
	return resp, nil
}

// releaseOccupancies marks every open occupancy slot for a task as released
// with the supplied reason. It is called at every legal terminal outcome.
func (s *Service) releaseOccupancies(tx store.Tx, taskID inspection.TaskID, reason string) error {
	slots, err := tx.ListOccupancies(taskID)
	if err != nil {
		return err
	}
	for _, sl := range slots {
		if occupancy.Active(sl) {
			if err := tx.SaveOccupancy(occupancy.Release(sl, reason)); err != nil {
				return err
			}
		}
	}
	return nil
}

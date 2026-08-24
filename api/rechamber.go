package api

import (
	"encoding/json"

	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/occupancy"
	"riceguard/store"
)

// RechamberRequest atomically moves a task to a new germination chamber time
// window, releasing its current chamber slot and occupying the replacement.
type RechamberRequest struct {
	OperationID  string `json:"operation_id"`
	Chamber      string `json:"chamber"`
	ChamberStart uint64 `json:"chamber_start"`
	ChamberEnd   uint64 `json:"chamber_end"`
}

// RechamberResponse reports the new occupancy window and state.
type RechamberResponse struct {
	TaskID     inspection.TaskID     `json:"task_id"`
	Status     inspection.TaskStatus `json:"status"`
	Generation inspection.Generation `json:"generation"`
	Chamber    string                `json:"chamber"`
}

// Rechamber performs an explicit re-chamber transaction: the current chamber
// slot is released and a new window is occupied atomically, arbitrated
// against every other open slot. A failed arbitration leaves the original
// occupancy untouched.
func (s *Service) Rechamber(id string, req RechamberRequest) (RechamberResponse, *domain.Error) {
	taskID := inspection.TaskID(id)
	digest := inspection.Digest(req.Chamber, uintText(req.ChamberStart), uintText(req.ChamberEnd))

	if rec, ok := s.store.FindOperation(req.OperationID); ok {
		if rec.RequestDigest != digest {
			return RechamberResponse{}, domain.NewError(domain.CodeIdempotencyConflict, "operation content conflict", req.OperationID)
		}
		var resp RechamberResponse
		_ = json.Unmarshal([]byte(rec.ResultDigest), &resp)
		return resp, nil
	}
	if req.ChamberEnd <= req.ChamberStart {
		return RechamberResponse{}, domain.NewError(domain.CodeBadRequest, "chamber window end before start")
	}

	var resp RechamberResponse
	err := s.store.Mutate(func(tx store.Tx) error {
		t, err := tx.GetTask(taskID)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return domain.NewError(domain.CodeFinalized, string(t.Status))
		}
		if t.Status == inspection.StatusPendingSampling || t.Status == inspection.StatusPendingCreate ||
			t.Status == inspection.StatusBlindSplit {
			return domain.NewError(domain.CodeBadRequest, "task has not yet occupied a chamber", string(t.Status))
		}

		openSlots, err := tx.ListOpenOccupancies()
		if err != nil {
			return err
		}

		// Release this task's current chamber slot, then arbitrate the new
		// window against the remaining open slots. Only the moving task's own
		// current chamber slot is dropped; every other task's open slot must
		// stay in the arbitration set so a move onto an already-occupied window
		// is rejected as a conflict.
		var remaining []occupancy.OccupancySlot
		for _, sl := range openSlots {
			if sl.TaskID == taskID && sl.Chamber != "" {
				old := occupancy.Rechamber(sl, "rechambered")
				if err := tx.SaveOccupancy(old); err != nil {
					return err
				}
				continue
			}
			remaining = append(remaining, sl)
		}
		arb2 := occupancy.NewArbiter(remaining)
		if derr := arb2.ReserveChamber(occupancy.ChamberID(req.Chamber),
			domain.LogicalTime(req.ChamberStart), domain.LogicalTime(req.ChamberEnd)); derr != nil {
			return derr
		}
		newSlot := occupancy.BuildSlot(occupancy.ChamberID(req.Chamber),
			domain.LogicalTime(req.ChamberStart), domain.LogicalTime(req.ChamberEnd), taskID, "", t.Generation)
		if err := tx.SaveOccupancy(newSlot); err != nil {
			return err
		}

		t.Chamber = req.Chamber
		t.ChamberStart = req.ChamberStart
		t.ChamberEnd = req.ChamberEnd
		if err := tx.SaveTask(t); err != nil {
			return err
		}

		resp = RechamberResponse{TaskID: taskID, Status: t.Status, Generation: t.Generation, Chamber: req.Chamber}
		tx.RecordOperation(inspection.NewRecord(req.OperationID, taskID, t.Generation, digest, encode(resp)))
		return tx.AppendAudit(audit(tx.NextTime(), "system", t.Status, "rechamber", domain.CodeNone, nil))
	})
	if err != nil {
		return RechamberResponse{}, asDomain(err)
	}
	return resp, nil
}

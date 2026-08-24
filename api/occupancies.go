package api

import (
	"encoding/json"

	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/occupancy"
	"riceguard/store"
)

// Occupy atomically occupies the locked chamber time window and every plate
// well for the task. The chamber window and each well are arbitrated against
// all currently open slots inside a single transaction, so concurrent
// creation, re-chambering or start-up arbitrates exactly one winner.
func (s *Service) Occupy(id string, req OccupyRequest) (OccupyResponse, *domain.Error) {
	taskID := inspection.TaskID(id)
	digest := inspection.Digest("occupy")

	if rec, ok := s.store.FindOperation(taskID, req.OperationID); ok {
		if rec.RequestDigest != digest {
			return OccupyResponse{}, domain.NewError(domain.CodeIdempotencyConflict, "operation content conflict", req.OperationID)
		}
		var resp OccupyResponse
		_ = json.Unmarshal([]byte(rec.ResultDigest), &resp)
		return resp, nil
	}

	var resp OccupyResponse
	err := s.store.Mutate(func(tx store.Tx) error {
		t, err := tx.GetTask(taskID)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return domain.NewError(domain.CodeFinalized, string(t.Status))
		}
		if t.Status != inspection.StatusOccupying {
			return domain.NewError(domain.CodeBadRequest, "not in occupying state", string(t.Status))
		}

		openSlots, err := tx.ListOpenOccupancies()
		if err != nil {
			return err
		}
		arb := occupancy.NewArbiter(openSlots)

		if derr := arb.ReserveChamber(occupancy.ChamberID(t.Chamber),
			domain.LogicalTime(t.ChamberStart), domain.LogicalTime(t.ChamberEnd)); derr != nil {
			return derr
		}
		for _, w := range t.Wells {
			if derr := arb.ReserveWell(occupancy.PlateID(t.Plate), occupancy.WellID(w)); derr != nil {
				return derr
			}
		}

		chamberSlot := occupancy.BuildSlot(occupancy.ChamberID(t.Chamber),
			domain.LogicalTime(t.ChamberStart), domain.LogicalTime(t.ChamberEnd), taskID, "", t.Generation)
		if err := tx.SaveOccupancy(chamberSlot); err != nil {
			return err
		}
		for _, w := range t.Wells {
			wellSlot := occupancy.BuildWell(occupancy.PlateID(t.Plate), occupancy.WellID(w), taskID, "", t.Generation)
			if err := tx.SaveOccupancy(wellSlot); err != nil {
				return err
			}
		}

		if err := t.Advance(inspection.StatusGerminating, t.Generation); err != nil {
			return err
		}
		if err := tx.SaveTask(t); err != nil {
			return err
		}

		resp = OccupyResponse{TaskID: taskID, Status: t.Status, Generation: t.Generation}
		tx.RecordOperation(inspection.NewRecord(req.OperationID, taskID, t.Generation, digest, encode(resp)))
		return tx.AppendAudit(audit(tx.NextTime(), "system", t.Status, "occupy_resources", domain.CodeNone, nil))
	})
	if err != nil {
		return OccupyResponse{}, asDomain(err)
	}
	return resp, nil
}

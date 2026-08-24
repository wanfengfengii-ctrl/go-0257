package api

import (
	"encoding/json"

	"riceguard/blindcode"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/store"
)

// SplitBlindSamples materializes the triple-split matrix and blind samples
// from the frozen allocations, then advances the task to occupying. The
// whole operation is a single transaction so a failed matrix never leaves
// partial splits or samples.
func (s *Service) SplitBlindSamples(id string, req SplitRequest) (SplitResponse, *domain.Error) {
	taskID := inspection.TaskID(id)
	digest := inspection.Digest("split")

	if rec, ok := s.store.FindOperation(req.OperationID); ok {
		if rec.RequestDigest != digest {
			return SplitResponse{}, domain.NewError(domain.CodeIdempotencyConflict, "operation content conflict", req.OperationID)
		}
		var resp SplitResponse
		_ = json.Unmarshal([]byte(rec.ResultDigest), &resp)
		return resp, nil
	}

	var resp SplitResponse
	err := s.store.Mutate(func(tx store.Tx) error {
		t, err := tx.GetTask(taskID)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return domain.NewError(domain.CodeFinalized, string(t.Status))
		}
		if t.Status != inspection.StatusBlindSplit {
			return domain.NewError(domain.CodeBadRequest, "not in blind split state", string(t.Status))
		}

		matrix, derr := blindcode.BuildMatrix(taskID, t.Generation, t.BlindAllocs)
		if derr != nil {
			return derr
		}
		for _, sp := range matrix.Cells {
			if err := tx.SaveSplit(sp); err != nil {
				return err
			}
		}
		if err := t.Advance(inspection.StatusOccupying, t.Generation); err != nil {
			return err
		}
		if err := tx.SaveTask(t); err != nil {
			return err
		}

		resp = SplitResponse{TaskID: taskID, Status: t.Status, Generation: t.Generation, BlindCodes: matrix.Codes()}
		tx.RecordOperation(inspection.NewRecord(req.OperationID, taskID, t.Generation, digest, encode(resp)))
		return tx.AppendAudit(audit(tx.NextTime(), "system", t.Status, "split_blind_samples", domain.CodeNone, nil))
	})
	if err != nil {
		return SplitResponse{}, asDomain(err)
	}
	return resp, nil
}

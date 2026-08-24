package api

import (
	"encoding/json"

	"riceguard/catalog"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/store"
)

// ConfirmSampling submits one reviewer's dual-sampling confirmation. Any
// field (field, seed lot, blind seal or sample count) that disagrees with the
// locked task is rejected. Two distinct, qualified confirmations advance the
// task to blind_split.
func (s *Service) ConfirmSampling(id string, req SamplingRequest) (SamplingResponse, *domain.Error) {
	taskID := inspection.TaskID(id)
	digest := inspection.Digest(req.Reviewer, req.Field, req.SeedLot, req.BlindSeal, intText(int32(req.SampleCount)))

	if rec, ok := s.store.FindOperation(taskID, req.OperationID); ok {
		if rec.RequestDigest != digest {
			return SamplingResponse{}, domain.NewError(domain.CodeIdempotencyConflict, "operation content conflict", req.OperationID)
		}
		var resp SamplingResponse
		_ = json.Unmarshal([]byte(rec.ResultDigest), &resp)
		return resp, nil
	}

	var resp SamplingResponse
	err := s.store.Mutate(func(tx store.Tx) error {
		t, err := tx.GetTask(taskID)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return domain.NewError(domain.CodeFinalized, string(t.Status))
		}
		if t.Status != inspection.StatusPendingSampling {
			return domain.NewError(domain.CodeBadRequest, "not in sampling state", string(t.Status))
		}
		if req.Field != string(t.Field) {
			return domain.NewError(domain.CodeVarietyMismatch, "field mismatch", req.Field, string(t.Field))
		}
		if req.SeedLot != t.SeedLot {
			return domain.NewError(domain.CodeVarietyMismatch, "seed lot mismatch", req.SeedLot, t.SeedLot)
		}
		if req.SampleCount != totalAllocatedGrains(t.BlindAllocs) {
			return domain.NewError(domain.CodeVarietyMismatch, "sample count mismatch")
		}
		if err := s.requireSampler(tx, req.Reviewer); err != nil {
			return err
		}
		if err := s.requireDistinctConfirmer(tx, taskID, req.Reviewer); err != nil {
			return err
		}

		now := tx.NextTime()
		conf := inspection.SamplingConfirmation{
			TaskID: taskID, Reviewer: req.Reviewer, Field: req.Field, SeedLot: req.SeedLot,
			BlindSeal: req.BlindSeal, SampleCount: req.SampleCount,
			Generation: t.Generation, OperationID: req.OperationID,
		}
		if err := tx.SaveConfirmation(conf); err != nil {
			return err
		}

		confs, _ := tx.ListConfirmations(taskID)
		resp = SamplingResponse{TaskID: taskID, Status: t.Status, Generation: t.Generation}
		for _, c := range confs {
			resp.Confirmations = append(resp.Confirmations, c.Reviewer)
		}

		if len(confs) >= 2 {
			if err := t.Advance(inspection.StatusBlindSplit, t.Generation); err != nil {
				return err
			}
			if err := tx.SaveTask(t); err != nil {
				return err
			}
			resp.Status = t.Status
			resp.Generation = t.Generation
			resp.Advanced = true
		}
		tx.RecordOperation(inspection.NewRecord(req.OperationID, taskID, t.Generation, digest, encode(resp)))
		return tx.AppendAudit(audit(now, req.Reviewer, t.Status, "sampling_confirmation", domain.CodeNone, nil))
	})
	if err != nil {
		return SamplingResponse{}, asDomain(err)
	}
	return resp, nil
}

func (s *Service) requireSampler(tx store.Tx, reviewer string) *domain.Error {
	p, ok := s.roles.Personnel(reviewer)
	if !ok {
		return domain.NewError(domain.CodeBadRequest, "unknown sampler", reviewer)
	}
	if !p.Holds(catalog.RoleSampler) {
		return domain.NewError(domain.CodeBadRequest, "reviewer not a qualified sampler", reviewer)
	}
	return nil
}

func (s *Service) requireDistinctConfirmer(tx store.Tx, taskID inspection.TaskID, reviewer string) *domain.Error {
	confs, _ := tx.ListConfirmations(taskID)
	for _, c := range confs {
		if c.Reviewer == reviewer {
			return domain.NewError(domain.CodeBadRequest, "reviewer already confirmed", reviewer)
		}
	}
	return nil
}

// totalAllocatedGrains returns the locked total sample grains across all
// blind-code triple-split allocations.
func totalAllocatedGrains(allocs []inspection.BlindAllocation) int {
	total := 0
	for _, a := range allocs {
		total += a.Germination + a.Pathogen + a.Moisture
	}
	return total
}

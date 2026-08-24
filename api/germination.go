package api

import (
	"encoding/json"

	"riceguard/blindcode"
	"riceguard/domain"
	"riceguard/germination"
	"riceguard/inspection"
	"riceguard/store"
)

// RecordGermination submits one day-age observation cell. It enforces the
// count-conservation invariant, rejects duplicate day-age readings and illegal
// retests, and — once the full blind-code × day-age grid is covered — computes
// the integer germination energy and rate and advances the task.
func (s *Service) RecordGermination(id string, req GerminationRequest) (GerminationResponse, *domain.Error) {
	taskID := inspection.TaskID(id)
	digest := inspection.Digest(req.BlindCode, intText(req.DayAge), intText(int32(req.Normal)),
		intText(int32(req.Abnormal)), intText(int32(req.Dead)), boolText(req.Retest), req.Collector)

	if rec, ok := s.store.FindOperation(req.OperationID); ok {
		if rec.RequestDigest != digest {
			return GerminationResponse{}, domain.NewError(domain.CodeIdempotencyConflict, "operation content conflict", req.OperationID)
		}
		var resp GerminationResponse
		_ = json.Unmarshal([]byte(rec.ResultDigest), &resp)
		return resp, nil
	}

	var resp GerminationResponse
	err := s.store.Mutate(func(tx store.Tx) error {
		t, err := tx.GetTask(taskID)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return domain.NewError(domain.CodeFinalized, string(t.Status))
		}
		if t.Status != inspection.StatusGerminating {
			return domain.NewError(domain.CodeBadRequest, "not in germinating state", string(t.Status))
		}
		if !containsCode(t.BlindAllocs, req.BlindCode) {
			return domain.NewError(domain.CodeBlindDuplicate, "unknown blind code", req.BlindCode)
		}
		if !containsDay(t.DayAges, req.DayAge) {
			return domain.NewError(domain.CodeBadRequest, "day age not in schedule", intText(req.DayAge))
		}

		cells, err := tx.ListGerminations(taskID)
		if err != nil {
			return err
		}
		if germination.Duplicate(cells, blindcode.BlindCode(req.BlindCode), germination.DayAge(req.DayAge)) {
			return domain.NewError(domain.CodeGerminationDrift, "duplicate day-age reading", req.BlindCode, intText(req.DayAge))
		}

		cell := germination.GerminationCell{
			TaskID: taskID, BlindCode: blindcode.BlindCode(req.BlindCode),
			Split: blindcode.SplitGermination, DayAge: germination.DayAge(req.DayAge),
			Normal: req.Normal, Abnormal: req.Abnormal, Dead: req.Dead,
			Retest: req.Retest, Collector: req.Collector, OperationID: req.OperationID, Valid: true,
		}
		if derr := germination.ValidateCell(t.GrainCount, cell); derr != nil {
			return derr
		}
		if err := tx.SaveGermination(cell); err != nil {
			return err
		}
		cells = append(cells, cell)

		resp = GerminationResponse{TaskID: taskID, Status: t.Status, Generation: t.Generation}

		codes := allocCodes(t.BlindAllocs)
		if germination.Covered(cells, codes, t.DayAges) {
			energy, rate := minVigor(cells, codes, t.DayAges, t.GrainCount)
			resp.EnergyBp = energy
			resp.RateBp = rate
			if err := t.Advance(inspection.StatusPathogen, t.Generation); err != nil {
				return err
			}
			if err := tx.SaveTask(t); err != nil {
				return err
			}
			resp.Status = t.Status
			resp.Generation = t.Generation
			resp.Advanced = true
		} else {
			for _, k := range germination.MissingCells(cells, codes, t.DayAges) {
				resp.MissingCells = append(resp.MissingCells, string(k.Code)+"@"+intText(int32(k.DayAge)))
			}
		}

		tx.RecordOperation(inspection.NewRecord(req.OperationID, taskID, t.Generation, digest, encode(resp)))
		ev := audit(tx.NextTime(), req.Collector, t.Status, "germination_observation", domain.CodeNone, nil)
		ev.BlindCodes = []string{req.BlindCode}
		ev.DayAges = []int32{req.DayAge}
		return tx.AppendAudit(ev)
	})
	if err != nil {
		return GerminationResponse{}, asDomain(err)
	}
	return resp, nil
}

func minVigor(cells []germination.GerminationCell, codes []string, dayAges []int32, grains int) (int32, int32) {
	var minE, minR int32
	for i, code := range codes {
		e, err := germination.Energy(cells, blindcode.BlindCode(code), dayAges, grains)
		if err != nil {
			e = 0
		}
		r, err := germination.Rate(cells, blindcode.BlindCode(code), dayAges, grains)
		if err != nil {
			r = 0
		}
		if i == 0 || e < minE {
			minE = e
		}
		if i == 0 || r < minR {
			minR = r
		}
	}
	return minE, minR
}

func containsCode(allocs []inspection.BlindAllocation, code string) bool {
	for _, a := range allocs {
		if a.Code == code {
			return true
		}
	}
	return false
}

func containsDay(days []int32, d int32) bool {
	for _, dd := range days {
		if dd == d {
			return true
		}
	}
	return false
}

func allocCodes(allocs []inspection.BlindAllocation) []string {
	out := make([]string, 0, len(allocs))
	for _, a := range allocs {
		out = append(out, a.Code)
	}
	return out
}

func boolText(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

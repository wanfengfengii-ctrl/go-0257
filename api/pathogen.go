package api

import (
	"encoding/json"

	"riceguard/blindcode"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/occupancy"
	"riceguard/pathogen"
	"riceguard/store"
)

// RecordPathogen submits an amplification reading for a plate well. The
// reading is obtained from the amplifier adapter under a bounded retry budget
// (or supplied explicitly for deterministic tests), adjudicated against the
// locked threshold, and — when positive or contaminated — marked for a
// generation-scoped re-judgment. Refused, disconnected, timed-out or
// malformed invocations are persisted as retryable attempts and never produce
// a valid reading. A late reading for an older generation is isolated and
// never overwrites the current conclusion.
func (s *Service) RecordPathogen(id string, req PathogenRequest) (PathogenResponse, *domain.Error) {
	taskID := inspection.TaskID(id)
	digest := inspection.Digest(req.BlindCode, req.Plate, req.Well, req.Verifier,
		boolText(req.Contaminated), int64Text(req.Generation))

	if rec, ok := s.store.FindOperation(req.OperationID); ok {
		if rec.RequestDigest != digest {
			return PathogenResponse{}, domain.NewError(domain.CodeIdempotencyConflict, "operation content conflict", req.OperationID)
		}
		var resp PathogenResponse
		_ = json.Unmarshal([]byte(rec.ResultDigest), &resp)
		return resp, nil
	}

	// Obtain the reading (and attempt history) from the instrument before
	// entering the transaction, so a retryable fault can be committed as a
	// pending-retry record even though no valid reading is produced.
	reading, attempts, derr := s.runAmplifier(req)
	if derr != nil {
		if persistErr := s.persistAttempts(taskID, req, attempts, "pathogen_retryable_attempt", req.Verifier); persistErr != nil {
			return PathogenResponse{}, persistErr
		}
		s.recordRetryableOperation(req.OperationID, taskID, digest)
		return PathogenResponse{}, derr
	}

	var resp PathogenResponse
	err := s.store.Mutate(func(tx store.Tx) error {
		t, err := tx.GetTask(taskID)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return domain.NewError(domain.CodeFinalized, string(t.Status))
		}
		if !containsWell(t.Wells, req.Well) {
			return domain.NewError(domain.CodeBadRequest, "well not in plate", req.Well)
		}
		if !containsCode(t.BlindAllocs, req.BlindCode) {
			return domain.NewError(domain.CodeBlindDuplicate, "unknown blind code", req.BlindCode)
		}

		existing, _ := tx.ListPathogen(taskID)

		// Late reading for an older generation: isolate, never overwrite.
		if req.Generation > 0 && req.Generation < int64(t.Generation) {
			return isolateLate(tx, t, taskID, req)
		}
		if wellCovered(existing, req.Plate, req.Well) {
			return domain.NewError(domain.CodeBadRequest, "well already read", req.Plate, req.Well)
		}
		if t.Status != inspection.StatusPathogen {
			return domain.NewError(domain.CodeBadRequest, "not in pathogen state", string(t.Status))
		}

		verdict := pathogen.Adjudicate(reading, t.PathogenMax)
		rejudgeGen := currentRejudgeGen(existing)
		if pathogen.NeedsRejudge(verdict, req.Contaminated) && rejudgeGen == 0 {
			rejudgeGen = t.Generation
		}

		evidence := pathogen.PathogenEvidence{
			TaskID: taskID, BlindCode: blindcode.BlindCode(req.BlindCode),
			Plate: occupancy.PlateID(req.Plate), Well: occupancy.WellID(req.Well),
			Reading: reading, Verdict: verdict, DeviceStatus: pathogen.DeviceOk,
			Verifier: req.Verifier, RejudgeGen: rejudgeGen, Contaminated: req.Contaminated,
		}
		if err := tx.SavePathogen(evidence); err != nil {
			return err
		}
		for _, a := range attempts {
			a.TaskID = taskID
			a.LogicalSeq = uint64(tx.NextTime())
			if err := tx.SaveAttempt(a); err != nil {
				return err
			}
		}
		existing = append(existing, evidence)

		resp = PathogenResponse{TaskID: taskID, Status: t.Status, Generation: t.Generation,
			Verdict: verdict, RejudgeGen: rejudgeGen, Contaminated: req.Contaminated}

		if allWellsCovered(existing, t.Wells, t.Plate) {
			if err := t.Advance(inspection.StatusMoisture, t.Generation); err != nil {
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
		ev := audit(tx.NextTime(), req.Verifier, t.Status, "pathogen_reading", domain.CodeNone, nil)
		ev.BlindCodes = []string{req.BlindCode}
		ev.PlateWells = []string{req.Plate + "/" + req.Well}
		return tx.AppendAudit(ev)
	})
	if err != nil {
		return PathogenResponse{}, asDomain(err)
	}
	return resp, nil
}

func (s *Service) runAmplifier(req PathogenRequest) (int32, []pathogen.Attempt, *domain.Error) {
	var reading int32
	var attempts []pathogen.Attempt
	if req.Reading != nil {
		reading = *req.Reading
		attempts = []pathogen.Attempt{{Attempt: 1, Status: pathogen.DeviceOk, Reading: reading}}
	} else {
		var derr *domain.Error
		reading, attempts, derr = s.retry.Run(s.amp, occupancy.PlateID(req.Plate), occupancy.WellID(req.Well), func() uint64 { return 0 })
		if derr != nil {
			return 0, attempts, derr
		}
	}
	if derr := pathogen.ValidateReading(reading); derr != nil {
		return 0, attempts, derr
	}
	return reading, attempts, nil
}

// persistAttempts commits the instrument attempt history for a retryable
// fault in its own transaction, so pending retries survive even though no
// valid reading was produced.
func (s *Service) persistAttempts(taskID inspection.TaskID, req PathogenRequest, attempts []pathogen.Attempt, action, actor string) *domain.Error {
	err := s.store.Mutate(func(tx store.Tx) error {
		for _, a := range attempts {
			a.TaskID = taskID
			a.LogicalSeq = uint64(tx.NextTime())
			if err := tx.SaveAttempt(a); err != nil {
				return err
			}
		}
		t, err := tx.GetTask(taskID)
		if err != nil {
			return err
		}
		return tx.AppendAudit(audit(tx.NextTime(), actor, t.Status, action, domain.CodeDeviceRetryable,
			[]string{req.Plate, req.Well}))
	})
	if err != nil {
		return asDomain(err)
	}
	return nil
}

func (s *Service) recordRetryableOperation(op string, taskID inspection.TaskID, digest string) {
	_ = s.store.Mutate(func(tx store.Tx) error {
		t, err := tx.GetTask(taskID)
		if err != nil {
			return err
		}
		tx.RecordOperation(inspection.IdempotencyRecord{
			OperationID: op, TaskID: taskID, Generation: t.Generation,
			RequestDigest: digest, ResponseCode: domain.CodeDeviceRetryable,
		})
		return nil
	})
}

func isolateLate(tx store.Tx, t *inspection.InspectionTask, taskID inspection.TaskID, req PathogenRequest) error {
	evidence := pathogen.IsolateLate(pathogen.PathogenEvidence{
		TaskID: taskID, BlindCode: blindcode.BlindCode(req.BlindCode),
		Plate: occupancy.PlateID(req.Plate), Well: occupancy.WellID(req.Well),
		Verifier: req.Verifier,
	})
	if err := tx.SavePathogen(evidence); err != nil {
		return err
	}
	return tx.AppendAudit(audit(tx.NextTime(), req.Verifier, t.Status, "pathogen_late_reading_isolated", domain.CodeNone,
		[]string{req.BlindCode, req.Well}))
}

func wellCovered(ev []pathogen.PathogenEvidence, plate, well string) bool {
	for _, e := range ev {
		if !e.LateIsolated && string(e.Plate) == plate && string(e.Well) == well {
			return true
		}
	}
	return false
}

func allWellsCovered(ev []pathogen.PathogenEvidence, wells []string, plate string) bool {
	for _, w := range wells {
		if !wellCovered(ev, plate, w) {
			return false
		}
	}
	return true
}

func currentRejudgeGen(ev []pathogen.PathogenEvidence) inspection.Generation {
	var max inspection.Generation
	for _, e := range ev {
		if e.RejudgeGen > max {
			max = e.RejudgeGen
		}
	}
	return max
}

func containsWell(wells []string, w string) bool {
	for _, x := range wells {
		if x == w {
			return true
		}
	}
	return false
}

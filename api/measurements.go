package api

import (
	"encoding/json"
	"strconv"

	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/store"
)

// RecordMoisture submits moisture, purity and thousand-grain measurements.
// The moisture reading is either parsed from the supplied fixed-point literal
// (validating sign and length) or obtained from the moisture-meter adapter
// under a bounded retry budget. Purity is derived with fixed-point integer
// arithmetic and the moisture threshold comparison is deterministic. Any
// arithmetic failure is rejected before derived evidence is written.
func (s *Service) RecordMoisture(id string, req MoistureRequest) (MoistureResponse, *domain.Error) {
	taskID := inspection.TaskID(id)
	digest := inspection.Digest(req.Moisture, int64Text(req.PurityGrains), int64Text(req.TotalGrains), int64Text(req.ThousandGrain), req.Collector)

	if rec, ok := s.store.FindOperation(req.OperationID); ok {
		if rec.RequestDigest != digest {
			return MoistureResponse{}, domain.NewError(domain.CodeIdempotencyConflict, "operation content conflict", req.OperationID)
		}
		var resp MoistureResponse
		_ = json.Unmarshal([]byte(rec.ResultDigest), &resp)
		return resp, nil
	}

	var resp MoistureResponse
	err := s.store.Mutate(func(tx store.Tx) error {
		var moisture measure.Fixed
		var derr *domain.Error
		if req.Moisture != "" {
			moisture, derr = measure.ParsePercent(req.Moisture)
		} else {
			moisture, _, derr = measure.MeterAttempt(s.meter, req.OperationID, measure.DefaultMeterAttempts)
		}
		if derr != nil {
			return derr
		}

		t, err := tx.GetTask(taskID)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return domain.NewError(domain.CodeFinalized, string(t.Status))
		}
		if t.Status != inspection.StatusMoisture {
			return domain.NewError(domain.CodeBadRequest, "not in moisture state", string(t.Status))
		}

		if derr := measure.ThousandGrainValidate(req.ThousandGrain); derr != nil {
			return derr
		}
		derivedPurity, derr := measure.PurityDerive(req.PurityGrains, req.TotalGrains)
		if derr != nil {
			return derr
		}
		decision, derr := measure.DecideMoisture(moisture, t.MoistureMax)
		if derr != nil {
			return derr
		}

		evidence := measure.MoisturePurityEvidence{
			TaskID: taskID, Moisture: moisture, PurityGrains: req.PurityGrains,
			ThousandGrain: req.ThousandGrain, DerivedPurity: derivedPurity,
			PassThreshold: decision.Pass && measure.PurityPass(derivedPurity, t.MinPurity),
			AttemptID:     req.OperationID, Collector: req.Collector, Version: int64(t.Generation),
		}
		if err := tx.SaveMoisture(evidence); err != nil {
			return err
		}

		if err := t.Advance(inspection.StatusPendingReview, t.Generation); err != nil {
			return err
		}
		if err := tx.SaveTask(t); err != nil {
			return err
		}

		resp = MoistureResponse{
			TaskID: taskID, Status: t.Status, Generation: t.Generation,
			MoistureBp: int64(moisture), DerivedPurity: int64(derivedPurity),
			Pass: evidence.PassThreshold, Advanced: true,
		}
		tx.RecordOperation(inspection.NewRecord(req.OperationID, taskID, t.Generation, digest, encode(resp)))
		return tx.AppendAudit(audit(tx.NextTime(), req.Collector, t.Status, "moisture_purity_measurement", domain.CodeNone, nil))
	})
	if err != nil {
		return MoistureResponse{}, asDomain(err)
	}
	return resp, nil
}

func int64Text(n int64) string {
	return strconv.FormatInt(n, 10)
}

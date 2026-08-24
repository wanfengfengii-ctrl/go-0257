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

// RejudgeRequest resolves a generation-scoped contamination re-judgment. The
// verifier supplies a conclusion (negative to clear, positive to confirm) that
// must cover the affected blind codes and plate wells.
type RejudgeRequest struct {
	OperationID string   `json:"operation_id"`
	Verifier    string   `json:"verifier"`
	Conclusion  string   `json:"conclusion"`
	BlindCodes  []string `json:"blind_codes"`
	Wells       []string `json:"wells"`
	Generation  int64    `json:"generation,omitempty"`
}

// RejudgeResponse reports the resolution and the re-judgment generation.
type RejudgeResponse struct {
	TaskID     inspection.TaskID     `json:"task_id"`
	Status     inspection.TaskStatus `json:"status"`
	Generation inspection.Generation `json:"generation"`
	RejudgeGen inspection.Generation `json:"rejudge_gen"`
	Conclusion pathogen.Verdict      `json:"conclusion"`
}

// ResolveRejudge records the resolution of a contamination re-judgment. It
// requires the re-judgment scope to cover the affected blind codes and wells
// and appends a re-judgment audit event. A positive or still-contaminated
// resolution keeps the evidence contaminated; a negative resolution clears the
// contamination markers for the covered wells.
func (s *Service) ResolveRejudge(id string, req RejudgeRequest) (RejudgeResponse, *domain.Error) {
	taskID := inspection.TaskID(id)
	digest := inspection.Digest(req.Verifier, req.Conclusion, joinStrings(req.BlindCodes), joinStrings(req.Wells))

	if rec, ok := s.store.FindOperation(taskID, req.OperationID); ok {
		if rec.RequestDigest != digest {
			return RejudgeResponse{}, domain.NewError(domain.CodeIdempotencyConflict, "operation content conflict", req.OperationID)
		}
		var resp RejudgeResponse
		_ = json.Unmarshal([]byte(rec.ResultDigest), &resp)
		return resp, nil
	}
	if len(req.BlindCodes) == 0 || len(req.Wells) == 0 {
		return RejudgeResponse{}, domain.NewError(domain.CodeBadRequest, "re-judgment scope must cover blind codes and wells")
	}

	conclusion := pathogen.VerdictNegative
	if verdict := pathogen.ParseVerdict(req.Conclusion); verdict == pathogen.VerdictPositive || verdict == pathogen.VerdictContaminated {
		conclusion = pathogen.VerdictPositive
	}

	var resp RejudgeResponse
	err := s.store.Mutate(func(tx store.Tx) error {
		t, err := tx.GetTask(taskID)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return domain.NewError(domain.CodeFinalized, string(t.Status))
		}

		existing, _ := tx.ListPathogen(taskID)
		rejudgeGen := currentRejudgeGen(existing)
		if rejudgeGen == 0 {
			return domain.NewError(domain.CodeBadRequest, "no re-judgment pending")
		}
		if req.Conclusion == string(pathogen.VerdictNegative) && req.Generation > 0 && req.Generation < int64(rejudgeGen) {
			// A late resolution for an old re-judgment generation is isolated.
			return tx.AppendAudit(audit(tx.NextTime(), req.Verifier, t.Status, "rejudge_late_resolution_isolated", domain.CodeNone,
				append(req.BlindCodes, req.Wells...)))
		}

		// Record a re-judgment evidence for each covered well, clearing or
		// confirming contamination deterministically.
		for _, w := range req.Wells {
			code := req.BlindCodes[0]
			evidence := pathogen.PathogenEvidence{
				TaskID: taskID, BlindCode: blindcode.BlindCode(code),
				Plate: occupancy.PlateID(t.Plate), Well: occupancy.WellID(w),
				Verdict: conclusion, RejudgeGen: rejudgeGen,
				Contaminated: conclusion == pathogen.VerdictPositive,
				Verifier:     req.Verifier,
			}
			if err := tx.SavePathogen(evidence); err != nil {
				return err
			}
		}

		resp = RejudgeResponse{TaskID: taskID, Status: t.Status, Generation: t.Generation,
			RejudgeGen: rejudgeGen, Conclusion: conclusion}
		tx.RecordOperation(inspection.NewRecord(req.OperationID, taskID, t.Generation, digest, encode(resp)))
		ev := audit(tx.NextTime(), req.Verifier, t.Status, "rejudge_resolution", domain.CodeNone, nil)
		ev.BlindCodes = append([]string(nil), req.BlindCodes...)
		ev.PlateWells = append([]string(nil), req.Wells...)
		return tx.AppendAudit(ev)
	})
	if err != nil {
		return RejudgeResponse{}, asDomain(err)
	}
	return resp, nil
}

func joinStrings(s []string) string {
	out := ""
	for _, v := range s {
		out += v + ","
	}
	return out
}

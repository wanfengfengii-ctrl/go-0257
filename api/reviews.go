package api

import (
	"encoding/json"

	"riceguard/blindcode"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/review"
	"riceguard/store"
)

// Review submits one independent review. Two distinct, qualified reviewers
// who do not overlap with the key collectors must both approve before the
// task advances to releasable.
func (s *Service) Review(id string, req ReviewRequest) (ReviewResponse, *domain.Error) {
	taskID := inspection.TaskID(id)
	digest := inspection.Digest(req.Reviewer, req.Conclusion)

	if rec, ok := s.store.FindOperation(req.OperationID); ok {
		if rec.RequestDigest != digest {
			return ReviewResponse{}, domain.NewError(domain.CodeIdempotencyConflict, "operation content conflict", req.OperationID)
		}
		var resp ReviewResponse
		_ = json.Unmarshal([]byte(rec.ResultDigest), &resp)
		return resp, nil
	}

	var resp ReviewResponse
	err := s.store.Mutate(func(tx store.Tx) error {
		t, err := tx.GetTask(taskID)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return domain.NewError(domain.CodeFinalized, string(t.Status))
		}
		if t.Status != inspection.StatusPendingReview {
			return domain.NewError(domain.CodeBadRequest, "not in pending review state", string(t.Status))
		}

		collectors := s.collectorSet(tx, taskID)
		check := review.IndependenceCheck{Dir: s.roles, Collectors: collectors}

		existing, _ := tx.ListReviews(taskID)
		var other review.ReviewerID
		for _, r := range existing {
			if r.Reviewer != review.ReviewerID(req.Reviewer) {
				other = r.Reviewer
			}
		}
		if err := check.Validate(review.ReviewerID(req.Reviewer), other); err != nil {
			return err
		}
		// Reject a duplicate reviewer.
		for _, r := range existing {
			if r.Reviewer == review.ReviewerID(req.Reviewer) {
				return domain.NewError(domain.CodeBadRequest, "reviewer already submitted", req.Reviewer)
			}
		}

		conclusion := review.ConclusionApprove
		if req.Conclusion == string(review.ConclusionReject) {
			conclusion = review.ConclusionReject
		}

		rec := review.ReviewAndFinal{
			TaskID: taskID, Reviewer: review.ReviewerID(req.Reviewer),
			Scope: "full_inspection", Qualified: true, Conclusion: conclusion,
		}
		if err := tx.SaveReview(rec); err != nil {
			return err
		}
		existing = append(existing, rec)

		resp = ReviewResponse{TaskID: taskID, Status: t.Status, Generation: t.Generation}
		approvals := 0
		for _, r := range existing {
			resp.Reviewers = append(resp.Reviewers, string(r.Reviewer))
			if r.Conclusion == review.ConclusionApprove {
				approvals++
			}
		}

		if approvals >= 2 {
			if err := t.Advance(inspection.StatusReleasable, t.Generation); err != nil {
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
		return tx.AppendAudit(audit(tx.NextTime(), req.Reviewer, t.Status, "independent_review", domain.CodeNone, nil))
	})
	if err != nil {
		return ReviewResponse{}, asDomain(err)
	}
	return resp, nil
}

// Finalize runs the terminal competition. Contamination forces quarantine;
// otherwise two approvals mint a unique release credential; an explicit cancel
// request cancels. Only one terminal outcome can win.
func (s *Service) Finalize(id string, req FinalizeRequest) (FinalizeResponse, *domain.Error) {
	taskID := inspection.TaskID(id)
	digest := inspection.Digest(req.Outcome, req.Reason)

	if rec, ok := s.store.FindOperation(req.OperationID); ok {
		if rec.RequestDigest != digest {
			return FinalizeResponse{}, domain.NewError(domain.CodeIdempotencyConflict, "operation content conflict", req.OperationID)
		}
		var resp FinalizeResponse
		_ = json.Unmarshal([]byte(rec.ResultDigest), &resp)
		return resp, nil
	}

	var resp FinalizeResponse
	err := s.store.Mutate(func(tx store.Tx) error {
		t, err := tx.GetTask(taskID)
		if err != nil {
			return err
		}
		if t.IsTerminal() {
			return domain.NewError(domain.CodeFinalized, string(t.Status))
		}
		if t.Status != inspection.StatusReleasable {
			return domain.NewError(domain.CodeBadRequest, "not releasable", string(t.Status))
		}

		contaminated := s.anyContaminated(tx, taskID)
		approvals := s.approvalCount(tx, taskID)

		var decision review.FinalizeDecision
		var derr *domain.Error
		switch req.Outcome {
		case "cancelled":
			decision, derr = review.CancelOutcome(t, req.Reason)
		case "quarantined":
			decision, derr = review.ArbitrateFinalize(t, approvals, true)
		default:
			if contaminated {
				decision, derr = review.ArbitrateFinalize(t, approvals, true)
			} else if derr = s.checkReleaseReadiness(tx, t, approvals); derr != nil {
				return derr
			} else {
				decision, derr = review.ArbitrateFinalize(t, approvals, false)
			}
		}
		if derr != nil {
			return derr
		}

		applyTerminal(t, decision)

		if err := s.releaseOccupancies(tx, taskID, string(decision.Outcome)); err != nil {
			return err
		}
		if err := tx.SaveTask(t); err != nil {
			return err
		}
		if decision.Outcome == review.OutcomeReleased {
			// Reveal every blind code through the one-way unblinding gate and
			// persist the reveal so it survives restart. The gate is global and
			// backed by the store: a code already opened by any task — even a
			// terminal batch whose blind code is reused by a later batch — can
			// never pass the terminal unblinding again.
			for _, a := range t.BlindAllocs {
				code := blindcode.BlindCode(a.Code)
				already, err := tx.BlindCodeUnblinded(code)
				if err != nil {
					return err
				}
				if already {
					return domain.NewError(domain.CodeBlindDuplicate, "blind code already unblinded", a.Code)
				}
				if err := tx.MarkBlindUnblinded(taskID, code); err != nil {
					return err
				}
			}
			cred := inspection.NewCredential(taskID, decision.Version)
			decision.Credential = cred.Credential
			if err := tx.SaveCredential(cred); err != nil {
				return err
			}
		}
		tx.SaveReview(review.ReviewAndFinal{
			TaskID: taskID, Outcome: decision.Outcome, TerminalVersion: decision.Version,
		})

		resp = FinalizeResponse{
			TaskID: taskID, Status: t.Status, Outcome: review.OutcomeText(decision.Outcome),
			Credential: decision.Credential, Version: decision.Version,
		}
		tx.RecordOperation(inspection.NewRecord(req.OperationID, taskID, t.Generation, digest, encode(resp)))
		return tx.AppendAudit(audit(tx.NextTime(), "system", t.Status, "finalize", domain.CodeNone, nil))
	})
	if err != nil {
		return FinalizeResponse{}, asDomain(err)
	}
	return resp, nil
}

func applyTerminal(t *inspection.InspectionTask, d review.FinalizeDecision) {
	switch d.Outcome {
	case review.OutcomeReleased:
		t.Status = inspection.StatusReleased
		t.TerminalOutcome = "released"
	case review.OutcomeQuarantined:
		t.Status = inspection.StatusQuarantined
		t.TerminalOutcome = "quarantined"
	case review.OutcomeCancelled:
		t.Status = inspection.StatusCancelled
		t.TerminalOutcome = "cancelled"
	}
	t.TerminalVersion = d.Version
}

func (s *Service) collectorSet(tx store.Tx, taskID inspection.TaskID) map[string]bool {
	collectors := make(map[string]bool)
	germs, _ := tx.ListGerminations(taskID)
	for _, g := range germs {
		collectors[g.Collector] = true
	}
	paths, _ := tx.ListPathogen(taskID)
	for _, p := range paths {
		collectors[p.Verifier] = true
	}
	moists, _ := tx.ListMoisture(taskID)
	for _, m := range moists {
		collectors[m.Collector] = true
	}
	return collectors
}

func (s *Service) anyContaminated(tx store.Tx, taskID inspection.TaskID) bool {
	paths, _ := tx.ListPathogen(taskID)
	return pathogenContaminated(paths)
}

// checkReleaseReadiness verifies the viability, moisture and purity gates
// before a release is allowed. It returns a rejection when any gate fails so
// that a task cannot be released below its locked thresholds.
func (s *Service) checkReleaseReadiness(tx store.Tx, task *inspection.InspectionTask, approvals int) *domain.Error {
	if approvals < 2 {
		return domain.NewError(domain.CodeBadRequest, "insufficient independent approvals")
	}
	rate, covered := s.germinationRate(tx, task)
	if !covered {
		return domain.NewError(domain.CodeBadRequest, "germination grid not covered")
	}
	if v, ok := s.catalog.Variety(task.Variety); ok {
		if rate < v.GerminationRateMin {
			return domain.NewError(domain.CodeBadRequest, "germination rate below threshold")
		}
	}
	moists, _ := tx.ListMoisture(task.ID)
	if len(moists) == 0 || !moists[len(moists)-1].PassThreshold {
		return domain.NewError(domain.CodeBadRequest, "moisture or purity below threshold")
	}
	return nil
}

func (s *Service) approvalCount(tx store.Tx, taskID inspection.TaskID) int {
	reviews, _ := tx.ListReviews(taskID)
	n := 0
	for _, r := range reviews {
		if r.Conclusion == review.ConclusionApprove {
			n++
		}
	}
	return n
}

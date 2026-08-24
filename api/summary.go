package api

import (
	"riceguard/domain"
	"riceguard/germination"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/pathogen"
	"riceguard/review"
	"riceguard/store"
)

// TaskSummary is a computed, read-only inspection summary used by the browser
// console to render the live state of a task without interpreting raw
// evidence client-side.
type TaskSummary struct {
	Status             inspection.TaskStatus `json:"status"`
	StatusText         string                `json:"status_text"`
	Generation         inspection.Generation `json:"generation"`
	GerminationCovered bool                  `json:"germination_covered"`
	GerminationRateBp  int32                 `json:"germination_rate_bp"`
	GerminationRateMin int32                 `json:"germination_rate_min_bp"`
	PathogenCovered    bool                  `json:"pathogen_covered"`
	PathogenClean      bool                  `json:"pathogen_clean"`
	MoisturePassed     bool                  `json:"moisture_passed"`
	PurityPassed       bool                  `json:"purity_passed"`
	Approvals          int                   `json:"approvals"`
	Releasable         bool                  `json:"releasable"`
}

// ComputeSummary builds the inspection summary for a task by reading its
// evidence and applying the integer viability rules.
func (s *Service) ComputeSummary(id string) (TaskSummary, *domain.Error) {
	t, err := s.store.GetTask(inspection.TaskID(id))
	if err != nil {
		return TaskSummary{}, asDomain(err)
	}
	sum := TaskSummary{Status: t.Status, StatusText: t.Status.Description(), Generation: t.Generation}
	if v, ok := s.catalog.Variety(t.Variety); ok {
		sum.GerminationRateMin = v.GerminationRateMin
	}

	germs, _ := s.store.ListGerminations(t.ID)
	codes := allocCodes(t.BlindAllocs)
	sum.GerminationCovered = germination.Covered(germs, codes, t.DayAges)
	if sum.GerminationCovered {
		sum.GerminationRateBp, _ = minVigor(germs, codes, t.DayAges, t.GrainCount)
	}

	paths, _ := s.store.ListPathogen(t.ID)
	sum.PathogenCovered = pathogenCovered(paths, t.BlindAllocs, t.Wells, t.Plate)
	sum.PathogenClean = !pathogenContaminated(paths)

	moists, _ := s.store.ListMoisture(t.ID)
	if len(moists) > 0 {
		sum.MoisturePassed = moists[len(moists)-1].PassThreshold
		sum.PurityPassed = measure.PurityPass(moists[len(moists)-1].DerivedPurity, t.MinPurity)
	}

	reviews, _ := s.store.ListReviews(t.ID)
	for _, r := range reviews {
		if r.Conclusion == review.ConclusionApprove {
			sum.Approvals++
		}
	}

	sum.Releasable = sum.GerminationCovered &&
		sum.GerminationRateBp >= sum.GerminationRateMin &&
		sum.PathogenCovered && sum.PathogenClean &&
		sum.MoisturePassed && sum.PurityPassed &&
		sum.Approvals >= 2

	return sum, nil
}

// germinationRate returns the minimum germination rate (basis points) across
// all blind codes, or false if the grid is not fully covered.
func (s *Service) germinationRate(tx store.Tx, task *inspection.InspectionTask) (int32, bool) {
	germs, _ := tx.ListGerminations(task.ID)
	codes := allocCodes(task.BlindAllocs)
	if !germination.Covered(germs, codes, task.DayAges) {
		return 0, false
	}
	rate, _ := minVigor(germs, codes, task.DayAges, task.GrainCount)
	return rate, true
}

// pathogenContaminated reports whether any pathogen evidence is positive or
// contaminated.
func pathogenContaminated(paths []pathogen.PathogenEvidence) bool {
	for _, p := range paths {
		if p.Contaminated || p.Verdict == pathogen.VerdictPositive {
			return true
		}
	}
	return false
}

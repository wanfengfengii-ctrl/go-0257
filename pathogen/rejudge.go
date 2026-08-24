package pathogen

import (
	"sort"

	"riceguard/blindcode"
	"riceguard/inspection"
	"riceguard/occupancy"
)

// Rejudge is the generation-scoped contamination re-judgment arbitration
// record. A single re-judgment per generation covers the affected blind codes
// and plate wells; late readings for an older generation are isolated and
// never overwrite the current conclusion.
type Rejudge struct {
	TaskID       inspection.TaskID
	Generation   inspection.Generation
	RejudgeGen   inspection.Generation
	BlindCodes   []blindcode.BlindCode
	PlateWells   []string
	Conclusion   Verdict
	IsolatedOnly bool
}

// ScopeCovers reports whether a blind code belongs to the re-judgment scope.
func (r Rejudge) ScopeCovers(code blindcode.BlindCode) bool {
	for _, c := range r.BlindCodes {
		if c == code {
			return true
		}
	}
	return false
}

// WellCovers reports whether a plate well belongs to the re-judgment scope.
func (r Rejudge) WellCovers(plate occupancy.PlateID, well occupancy.WellID) bool {
	key := string(plate) + "/" + string(well)
	for _, w := range r.PlateWells {
		if w == key {
			return true
		}
	}
	return false
}

// BuildRejudge assembles a re-judgment record from the affected evidence. It
// deduplicates and sorts the covered blind codes and wells so the record is
// deterministic.
func BuildRejudge(task inspection.TaskID, gen inspection.Generation, rejudgeGen inspection.Generation, evidence []PathogenEvidence) Rejudge {
	codeSet := make(map[blindcode.BlindCode]bool)
	wellSet := make(map[string]bool)
	for _, e := range evidence {
		codeSet[e.BlindCode] = true
		wellSet[string(e.Plate)+"/"+string(e.Well)] = true
	}
	var codes []blindcode.BlindCode
	for c := range codeSet {
		codes = append(codes, c)
	}
	var wells []string
	for w := range wellSet {
		wells = append(wells, w)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	sort.Strings(wells)
	return Rejudge{
		TaskID:     task,
		Generation: gen,
		RejudgeGen: rejudgeGen,
		BlindCodes: codes,
		PlateWells: wells,
	}
}

// IsolateLate marks evidence as a late reading for an old generation. It is
// stored in a quarantined audit event and never overwrites current evidence.
func IsolateLate(e PathogenEvidence) PathogenEvidence {
	e.LateIsolated = true
	return e
}

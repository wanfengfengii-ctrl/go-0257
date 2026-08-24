package measure

import (
	"riceguard/domain"
	"riceguard/inspection"
)

// MoistureDecision captures the threshold comparison result for a moisture
// reading against the locked maximum.
type MoistureDecision struct {
	Moisture    Fixed
	Max         int32
	Pass        bool
	ExceedsByBp int32
}

// DecideMoisture compares a moisture reading (basis points) against the
// locked maximum (basis points). A reading strictly above the maximum fails;
// a reading at or below the maximum passes. The exceedance is reported in
// basis points.
func DecideMoisture(moisture Fixed, max int32) (MoistureDecision, *domain.Error) {
	if err := ValidateValue(moisture); err != nil {
		return MoistureDecision{}, err
	}
	d := MoistureDecision{Moisture: moisture, Max: max}
	if moisture > Fixed(max) {
		d.Pass = false
		d.ExceedsByBp = int32(moisture - Fixed(max))
	} else {
		d.Pass = true
	}
	return d, nil
}

// BuildMoisture assembles a moisture/purity evidence record for a task,
// attaching the instrument attempt ID and a non-overridable version.
func BuildMoisture(task inspection.TaskID, moisture Fixed, purityGrains, thousandGrain int64, attempt, collector string, version int64) MoisturePurityEvidence {
	return MoisturePurityEvidence{
		TaskID:        task,
		Moisture:      moisture,
		PurityGrains:  purityGrains,
		ThousandGrain: thousandGrain,
		AttemptID:     attempt,
		Collector:     collector,
		Version:       version,
	}
}

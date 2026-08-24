package api

import (
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/pathogen"
)

// InspectionReport is a formal, read-only record of a task's inspection
// outcome. It is produced once a task reaches a terminal state and used by the
// console and downstream seed-release workflows.
type InspectionReport struct {
	TaskID          inspection.TaskID     `json:"task_id"`
	SeedLot         string                `json:"seed_lot"`
	Field           string                `json:"field"`
	Variety         string                `json:"variety"`
	Status          inspection.TaskStatus `json:"status"`
	Outcome         string                `json:"outcome"`
	Generation      inspection.Generation `json:"generation"`
	GerminationRate int32                 `json:"germination_rate_bp"`
	MoistureBp      int64                 `json:"moisture_bp"`
	PurityBp        int64                 `json:"purity_bp"`
	PathogenClean   bool                  `json:"pathogen_clean"`
	PathogenVerdict string                `json:"pathogen_verdict"`
	Reviewers       []string              `json:"reviewers"`
	BlindCodes      []string              `json:"blind_codes"`
	Credential      string                `json:"credential,omitempty"`
	Reason          string                `json:"reason,omitempty"`
	GerminationText string                `json:"germination_rate"`
	MoistureText    string                `json:"moisture"`
	PurityText      string                `json:"purity"`
	ThousandGrain   string                `json:"thousand_grain"`
	DeviceStatus    string                `json:"device_status"`
}

// GenerateReport builds the formal inspection report for a task. A report can
// only be produced for a terminal task; open tasks are rejected.
func (s *Service) GenerateReport(id string) (InspectionReport, *domain.Error) {
	view, derr := s.GetTask(id)
	if derr != nil {
		return InspectionReport{}, derr
	}
	t := view.Task
	if !t.IsTerminal() {
		return InspectionReport{}, domain.NewError(domain.CodeBadRequest, "task is not terminal", string(t.Status))
	}

	report := InspectionReport{
		TaskID:     t.ID,
		SeedLot:    t.SeedLot,
		Field:      string(t.Field),
		Variety:    string(t.Variety),
		Status:     t.Status,
		Outcome:    t.TerminalOutcome,
		Generation: t.Generation,
		BlindCodes: allocCodes(t.BlindAllocs),
	}

	report.GerminationRate = view.Summary.GerminationRateBp
	report.PathogenClean = view.Summary.PathogenClean
	report.PathogenVerdict = pathogen.VerdictText(pathogen.VerdictNegative)
	if report.PathogenClean {
		report.PathogenVerdict = pathogen.VerdictText(pathogen.VerdictNegative)
	} else {
		report.PathogenVerdict = pathogen.VerdictText(pathogen.VerdictPositive)
	}
	report.GerminationText = measure.FormatBp(int64(view.Summary.GerminationRateBp))

	if len(view.Moisture) > 0 {
		report.MoistureBp = int64(view.Moisture[len(view.Moisture)-1].Moisture)
		report.PurityBp = int64(view.Moisture[len(view.Moisture)-1].DerivedPurity)
		report.MoistureText = measure.FormatBp(int64(view.Moisture[len(view.Moisture)-1].Moisture))
		report.PurityText = measure.FormatBp(int64(view.Moisture[len(view.Moisture)-1].DerivedPurity))
		report.ThousandGrain = measure.FormatWeight(view.Moisture[len(view.Moisture)-1].ThousandGrain)
	}
	if len(view.Pathogen) > 0 {
		report.DeviceStatus = pathogen.DeviceText(view.Pathogen[len(view.Pathogen)-1].DeviceStatus)
	}
	for _, r := range view.Reviews {
		report.Reviewers = append(report.Reviewers, string(r.Reviewer))
	}
	if view.Credential != nil {
		report.Credential = view.Credential.Credential
	}
	switch t.TerminalOutcome {
	case "quarantined":
		report.Reason = "pathogen_contamination"
	case "cancelled":
		report.Reason = "cancelled"
	}
	return report, nil
}

package api

import (
	"riceguard/blindcode"
	"riceguard/catalog"
	"riceguard/germination"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/occupancy"
	"riceguard/pathogen"
	"riceguard/review"
)

// BlindAllocInput is a blind-code triple-split allocation supplied at task
// creation.
type BlindAllocInput struct {
	Code        string `json:"code"`
	Germination int    `json:"germination"`
	Pathogen    int    `json:"pathogen"`
	Moisture    int    `json:"moisture"`
}

// CreateTaskRequest locks a new inspection task.
type CreateTaskRequest struct {
	OperationID    string            `json:"operation_id"`
	SeedLot        string            `json:"seed_lot"`
	Field          string            `json:"field"`
	Variety        string            `json:"variety"`
	FemaleCert     int32             `json:"female_cert_revision"`
	MaleCert       int32             `json:"male_cert_revision"`
	BlindAllocs    []BlindAllocInput `json:"blind_allocations"`
	Chamber        string            `json:"chamber"`
	ChamberStart   uint64            `json:"chamber_start"`
	ChamberEnd     uint64            `json:"chamber_end"`
	Plate          string            `json:"plate"`
	Wells          []string          `json:"wells"`
	ReviewerRoster []string          `json:"reviewer_roster"`
}

// CreateTaskResponse is the result of task creation.
type CreateTaskResponse struct {
	TaskID     string                `json:"task_id"`
	Status     inspection.TaskStatus `json:"status"`
	Generation inspection.Generation `json:"generation"`
}

// SamplingRequest submits one reviewer's sampling confirmation.
type SamplingRequest struct {
	OperationID string `json:"operation_id"`
	Reviewer    string `json:"reviewer"`
	Field       string `json:"field"`
	SeedLot     string `json:"seed_lot"`
	BlindSeal   string `json:"blind_seal"`
	SampleCount int    `json:"sample_count"`
}

// SamplingResponse reports the confirmations so far and the current state.
type SamplingResponse struct {
	TaskID        inspection.TaskID     `json:"task_id"`
	Status        inspection.TaskStatus `json:"status"`
	Generation    inspection.Generation `json:"generation"`
	Confirmations []string              `json:"reviewers"`
	Advanced      bool                  `json:"advanced"`
}

// SplitRequest triggers the triple-split materialization.
type SplitRequest struct {
	OperationID string `json:"operation_id"`
}

// SplitResponse reports the materialized blind codes and new state.
type SplitResponse struct {
	TaskID     inspection.TaskID     `json:"task_id"`
	Status     inspection.TaskStatus `json:"status"`
	Generation inspection.Generation `json:"generation"`
	BlindCodes []string              `json:"blind_codes"`
}

// OccupyRequest requests atomic chamber and plate-well occupancy.
type OccupyRequest struct {
	OperationID string `json:"operation_id"`
}

// OccupyResponse reports the occupied resources.
type OccupyResponse struct {
	TaskID     inspection.TaskID     `json:"task_id"`
	Status     inspection.TaskStatus `json:"status"`
	Generation inspection.Generation `json:"generation"`
}

// GerminationRequest submits one day-age observation cell.
type GerminationRequest struct {
	OperationID string `json:"operation_id"`
	BlindCode   string `json:"blind_code"`
	DayAge      int32  `json:"day_age"`
	Normal      int    `json:"normal"`
	Abnormal    int    `json:"abnormal"`
	Dead        int    `json:"dead"`
	Retest      bool   `json:"retest"`
	Collector   string `json:"collector"`
}

// GerminationResponse reports the energy/rate verdict and missing cells.
type GerminationResponse struct {
	TaskID       inspection.TaskID     `json:"task_id"`
	Status       inspection.TaskStatus `json:"status"`
	Generation   inspection.Generation `json:"generation"`
	EnergyBp     int32                 `json:"energy_bp"`
	RateBp       int32                 `json:"rate_bp"`
	MissingCells []string              `json:"missing_cells"`
	Advanced     bool                  `json:"advanced"`
}

// MoistureRequest submits moisture/purity/thousand-grain measurements.
type MoistureRequest struct {
	OperationID   string `json:"operation_id"`
	Moisture      string `json:"moisture"`
	PurityGrains  int64  `json:"purity_grains"`
	TotalGrains   int64  `json:"total_grains"`
	ThousandGrain int64  `json:"thousand_grain"`
	Collector     string `json:"collector"`
}

// MoistureResponse reports the fixed-point derived evidence.
type MoistureResponse struct {
	TaskID        inspection.TaskID     `json:"task_id"`
	Status        inspection.TaskStatus `json:"status"`
	Generation    inspection.Generation `json:"generation"`
	MoistureBp    int64                 `json:"moisture_bp"`
	DerivedPurity int64                 `json:"derived_purity_bp"`
	Pass          bool                  `json:"pass_threshold"`
	Advanced      bool                  `json:"advanced"`
}

// PathogenRequest submits an amplification reading, verification, contamination
// flag or re-judgment result for one plate well. Generation is optional: when
// supplied, a value below the current task generation marks a late reading
// that is isolated instead of overwriting the current conclusion.
type PathogenRequest struct {
	OperationID  string `json:"operation_id"`
	BlindCode    string `json:"blind_code"`
	Plate        string `json:"plate"`
	Well         string `json:"well"`
	Verifier     string `json:"verifier"`
	Contaminated bool   `json:"contaminated"`
	Generation   int64  `json:"generation,omitempty"`
	Reading      *int32 `json:"reading,omitempty"`
}

// PathogenResponse reports the verdict and re-judgment generation.
type PathogenResponse struct {
	TaskID       inspection.TaskID     `json:"task_id"`
	Status       inspection.TaskStatus `json:"status"`
	Generation   inspection.Generation `json:"generation"`
	Verdict      pathogen.Verdict      `json:"verdict"`
	RejudgeGen   inspection.Generation `json:"rejudge_gen"`
	Contaminated bool                  `json:"contaminated"`
	Advanced     bool                  `json:"advanced"`
}

// ReviewRequest submits one independent review.
type ReviewRequest struct {
	OperationID string `json:"operation_id"`
	Reviewer    string `json:"reviewer"`
	Conclusion  string `json:"conclusion"`
}

// ReviewResponse reports review progress.
type ReviewResponse struct {
	TaskID     inspection.TaskID     `json:"task_id"`
	Status     inspection.TaskStatus `json:"status"`
	Generation inspection.Generation `json:"generation"`
	Reviewers  []string              `json:"reviewers"`
	Advanced   bool                  `json:"advanced"`
}

// FinalizeRequest triggers the terminal competition.
type FinalizeRequest struct {
	OperationID string `json:"operation_id"`
	Outcome     string `json:"outcome"` // "" (auto), "released", "quarantined", "cancelled"
	Reason      string `json:"reason"`
}

// FinalizeResponse reports the winning terminal outcome and credential.
type FinalizeResponse struct {
	TaskID     inspection.TaskID     `json:"task_id"`
	Status     inspection.TaskStatus `json:"status"`
	Outcome    string                `json:"outcome"`
	Credential string                `json:"credential,omitempty"`
	Version    int64                 `json:"terminal_version"`
}

// TaskView is the full aggregate read returned by GET /api/tasks/{id}.
type TaskView struct {
	Task          inspection.InspectionTask         `json:"task"`
	Confirmations []inspection.SamplingConfirmation `json:"confirmations"`
	BlindSamples  []blindcode.BlindSample           `json:"blind_samples"`
	Splits        []blindcode.TripleSplit           `json:"splits"`
	Occupancies   []occupancy.OccupancySlot         `json:"occupancies"`
	Germinations  []germination.GerminationCell     `json:"germinations"`
	Moisture      []measure.MoisturePurityEvidence  `json:"moisture"`
	Pathogen      []pathogen.PathogenEvidence       `json:"pathogen"`
	Reviews       []review.ReviewAndFinal           `json:"reviews"`
	Credential    *inspection.ReleaseCredential     `json:"credential,omitempty"`
	Audit         []inspection.AuditEvent           `json:"audit"`
	Variety       catalog.CatalogVariety            `json:"variety"`
	Summary       TaskSummary                       `json:"summary"`
}

// Package api exposes the deterministic JSON HTTP interface and the browser
// inspection console for the RiceGuard backend. The Service coordinates the
// catalog read model, the domain rule packages and the transactional store.
package api

import (
	"encoding/json"
	"fmt"

	"riceguard/blindcode"
	"riceguard/catalog"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/pathogen"
	"riceguard/store"
)

// Service is the application service that coordinates catalog validation,
// the task aggregate, the domain rule packages and persistence.
type Service struct {
	catalog catalog.Catalog
	roles   catalog.RoleDirectory
	store   store.Store
	amp     pathogen.Amplifier
	meter   measure.MoistureMeter
	retry   pathogen.RetryPolicy
	gate    *blindcode.MemoryGate
}

// NewService wires the catalog, role directory, store and instrument adapters
// into the application service.
func NewService(c catalog.Catalog, r catalog.RoleDirectory, s store.Store, amp pathogen.Amplifier, meter measure.MoistureMeter) *Service {
	return &Service{
		catalog: c,
		roles:   r,
		store:   s,
		amp:     amp,
		meter:   meter,
		retry:   pathogen.DefaultRetryPolicy,
		gate:    blindcode.NewMemoryGate(),
	}
}

// CreateTask creates and locks a new inspection task after validating the
// variety, field, parental certificates, blind-code allocations, resources
// and reviewer roster. Identical idempotent retries return the recorded
// result.
func (s *Service) CreateTask(req CreateTaskRequest) (CreateTaskResponse, *domain.Error) {
	digest := inspection.Digest(fmt.Sprintf("%v", req.BlindAllocs), req.SeedLot, req.Field,
		req.Variety, intText(req.FemaleCert), intText(req.MaleCert), req.Chamber,
		uintText(req.ChamberStart), uintText(req.ChamberEnd), req.Plate, fmt.Sprintf("%v", req.Wells),
		fmt.Sprintf("%v", req.ReviewerRoster))

	if rec, ok := s.store.FindOperation(req.OperationID); ok {
		return s.resolveCreate(rec, digest)
	}

	variety, ok := s.catalog.Variety(catalog.VarietyID(req.Variety))
	if !ok {
		return CreateTaskResponse{}, domain.NewError(domain.CodeVarietyMismatch, "unknown variety", req.Variety)
	}
	if err := s.catalog.ValidateField(variety.ID, catalog.FieldID(req.Field)); err != nil {
		return CreateTaskResponse{}, err
	}
	if err := s.catalog.ValidateParentCert(variety.FemaleParent, req.FemaleCert); err != nil {
		return CreateTaskResponse{}, err
	}
	if err := s.catalog.ValidateParentCert(variety.MaleParent, req.MaleCert); err != nil {
		return CreateTaskResponse{}, err
	}
	if req.ChamberEnd <= req.ChamberStart {
		return CreateTaskResponse{}, domain.NewError(domain.CodeBadRequest, "chamber window end before start")
	}
	if len(req.Wells) == 0 {
		return CreateTaskResponse{}, domain.NewError(domain.CodeBadRequest, "no plate wells")
	}
	if len(req.ReviewerRoster) < 2 {
		return CreateTaskResponse{}, domain.NewError(domain.CodeBadRequest, "reviewer roster needs at least two reviewers")
	}

	allocs := toBlindAllocs(req.BlindAllocs)
	if _, err := validateAllocations(allocs); err != nil {
		return CreateTaskResponse{}, err
	}

	var resp CreateTaskResponse
	err := s.store.Mutate(func(tx store.Tx) error {
		// Blind codes must not be bound to another open task.
		if err := s.assertBlindCodesFree(tx, allocs); err != nil {
			return err
		}
		now := tx.NextTime()
		t := &inspection.InspectionTask{
			ID:              inspection.TaskID(fmt.Sprintf("task-%d", now)),
			SeedLot:         req.SeedLot,
			Field:           catalog.FieldID(req.Field),
			Variety:         variety.ID,
			FemaleParent:    variety.FemaleParent,
			MaleParent:      variety.MaleParent,
			FemaleCert:      req.FemaleCert,
			MaleCert:        req.MaleCert,
			CertSummary:     fmt.Sprintf("female:%s@%d male:%s@%d", variety.FemaleParent, req.FemaleCert, variety.MaleParent, req.MaleCert),
			Status:          inspection.StatusPendingSampling,
			Generation:      1,
			MoistureMax:     variety.MoistureMax,
			PathogenMax:     variety.PathogenMax,
			MinPurity:       variety.MinPurity,
			GrainCount:      variety.GrainCount,
			Chamber:         req.Chamber,
			ChamberStart:    req.ChamberStart,
			ChamberEnd:      req.ChamberEnd,
			Plate:           req.Plate,
			Wells:           append([]string(nil), req.Wells...),
			DayAges:         append([]int32(nil), variety.DayAges...),
			BlindAllocs:     allocs,
			ReviewerRoster:  append([]string(nil), req.ReviewerRoster...),
			TerminalVersion: 0,
			CreatedAt:       now,
		}
		if err := tx.SaveTask(t); err != nil {
			return err
		}
		resp = CreateTaskResponse{TaskID: string(t.ID), Status: t.Status, Generation: t.Generation}
		tx.RecordOperation(inspection.NewRecord(req.OperationID, t.ID, t.Generation, digest, encode(resp)))
		return tx.AppendAudit(audit(now, "system", t.Status, "create_task", domain.CodeNone, nil))
	})
	if err != nil {
		return CreateTaskResponse{}, asDomain(err)
	}
	return resp, nil
}

func (s *Service) resolveCreate(rec *inspection.IdempotencyRecord, digest string) (CreateTaskResponse, *domain.Error) {
	if rec.RequestDigest != digest {
		return CreateTaskResponse{}, domain.NewError(domain.CodeIdempotencyConflict, "operation content conflict", rec.OperationID)
	}
	var resp CreateTaskResponse
	_ = json.Unmarshal([]byte(rec.ResultDigest), &resp)
	return resp, nil
}

// GetTask returns the full task aggregate view.
func (s *Service) GetTask(id string) (TaskView, *domain.Error) {
	t, err := s.store.GetTask(inspection.TaskID(id))
	if err != nil {
		return TaskView{}, asDomain(err)
	}
	return s.assembleView(t), nil
}

// ListTasks returns all tasks ordered by creation.
func (s *Service) ListTasks() ([]inspection.InspectionTask, *domain.Error) {
	ts, err := s.store.ListTasks()
	if err != nil {
		return nil, asDomain(err)
	}
	out := make([]inspection.InspectionTask, 0, len(ts))
	for _, t := range ts {
		out = append(out, *t)
	}
	return out, nil
}

// assembleView loads every sub-entity for a task into a TaskView.
func (s *Service) assembleView(t *inspection.InspectionTask) TaskView {
	v := TaskView{Task: *t}
	if variety, ok := s.catalog.Variety(t.Variety); ok {
		v.Variety = variety
	}
	v.Confirmations, _ = s.store.ListConfirmations(t.ID)
	v.BlindSamples, _ = s.store.ListBlindSamples(t.ID)
	v.Splits, _ = s.store.ListSplits(t.ID)
	v.Occupancies, _ = s.store.ListOccupancies(t.ID)
	v.Germinations, _ = s.store.ListGerminations(t.ID)
	v.Moisture, _ = s.store.ListMoisture(t.ID)
	v.Pathogen, _ = s.store.ListPathogen(t.ID)
	v.Reviews, _ = s.store.ListReviews(t.ID)
	v.Credential, _ = s.store.GetCredential(t.ID)
	v.Audit, _ = s.store.ListAudit(t.ID)
	v.Summary, _ = s.ComputeSummary(string(t.ID))
	return v
}

// assertBlindCodesFree rejects creation when any blind code is already bound
// to another open task.
func (s *Service) assertBlindCodesFree(tx store.Tx, allocs []inspection.BlindAllocation) *domain.Error {
	tasks, err := tx.ListTasks()
	if err != nil {
		return asDomain(err)
	}
	bound := make(map[string]string)
	for _, t := range tasks {
		if t.IsTerminal() {
			continue
		}
		for _, a := range t.BlindAllocs {
			bound[a.Code] = string(t.ID)
		}
	}
	var reasons []string
	for _, a := range allocs {
		if other, ok := bound[a.Code]; ok {
			reasons = append(reasons, a.Code, other)
		}
	}
	if len(reasons) > 0 {
		return domain.NewError(domain.CodeBlindDuplicate, domain.SortReasons(reasons...)...)
	}
	return nil
}

func toBlindAllocs(in []BlindAllocInput) []inspection.BlindAllocation {
	out := make([]inspection.BlindAllocation, 0, len(in))
	for _, b := range in {
		out = append(out, inspection.BlindAllocation{
			Code: b.Code, Germination: b.Germination, Pathogen: b.Pathogen, Moisture: b.Moisture,
		})
	}
	return out
}

func validateAllocations(allocs []inspection.BlindAllocation) ([]string, *domain.Error) {
	if len(allocs) == 0 {
		return nil, domain.NewError(domain.CodeBadRequest, "no blind allocations")
	}
	seen := make(map[string]bool)
	var codes []string
	for _, a := range allocs {
		if a.Code == "" {
			return nil, domain.NewError(domain.CodeBadRequest, "empty blind code")
		}
		if seen[a.Code] {
			return nil, domain.NewError(domain.CodeBlindDuplicate, "duplicate blind code", a.Code)
		}
		seen[a.Code] = true
		codes = append(codes, a.Code)
		if a.Germination <= 0 || a.Pathogen <= 0 || a.Moisture <= 0 {
			return nil, domain.NewError(domain.CodeBadRequest, "non-positive aliquot quantity", a.Code)
		}
	}
	return codes, nil
}

// encode serializes a response for idempotent replay.
func encode(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// audit builds an audit event with the current logical time.
func audit(now domain.LogicalTime, actor string, status inspection.TaskStatus, action string, code domain.ErrorCode, reasons []string) inspection.AuditEvent {
	return inspection.AuditEvent{
		LogicalTime: now,
		Actor:       actor,
		TaskStatus:  status,
		Action:      action,
		Code:        code,
		Reasons:     domain.SortReasons(reasons...),
	}
}

// asDomain coerces a store error into a stable domain error.
func asDomain(err error) *domain.Error {
	if de, ok := err.(*domain.Error); ok {
		return de
	}
	return domain.NewError(domain.CodeNotFound, err.Error())
}

func intText(n int32) string   { return fmt.Sprintf("%d", n) }
func uintText(n uint64) string { return fmt.Sprintf("%d", n) }

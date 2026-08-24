package api_test

import (
	"testing"

	"riceguard/api"
	"riceguard/catalog"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/pathogen"
	"riceguard/store"
)

func TestModel_MoistureBoundaryChecksPrecedeInstrumentAndEvidence(t *testing.T) {
	cases := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "premature sampling meter request preserves timeout for real moisture stage",
			run: func(t *testing.T) {
				meter := newMoistureBoundaryMeter()
				svc, st := newMoistureBoundaryService(meter)
				id := moistureBoundaryCreate(t, svc, "premature")
				moistureBoundaryConfirmOne(t, svc, id, "premature")

				before := moistureBoundarySnapshotTask(t, st, id)
				_, derr := svc.RecordMoisture(string(id), moistureBoundaryMeterRequest("premature-illegal"))
				moistureBoundaryRequireCode(t, derr, domain.CodeBadRequest)
				moistureBoundaryRequireMeter(t, meter, 0, measure.DefaultMeterAttempts)
				moistureBoundaryAssertTaskSnapshot(t, st, id, before)

				moistureBoundaryDriveSamplingTaskToMoisture(t, svc, id, "premature")
				legalBefore := moistureBoundarySnapshotTask(t, st, id)
				_, derr = svc.RecordMoisture(string(id), moistureBoundaryMeterRequest("premature-legal"))
				moistureBoundaryRequireCode(t, derr, domain.CodeDeviceRetryable)
				moistureBoundaryRequireMeter(t, meter, measure.DefaultMeterAttempts, 0)
				moistureBoundaryAssertTaskSnapshot(t, st, id, legalBefore)
			},
		},
		{
			name: "missing task meter request rejects before consuming timeout",
			run: func(t *testing.T) {
				meter := newMoistureBoundaryMeter()
				svc, _ := newMoistureBoundaryService(meter)

				_, derr := svc.RecordMoisture("task-does-not-exist", moistureBoundaryMeterRequest("missing-illegal"))
				moistureBoundaryRequireCode(t, derr, domain.CodeNotFound)
				moistureBoundaryRequireMeter(t, meter, 0, measure.DefaultMeterAttempts)
			},
		},
		{
			name: "terminal task meter request rejects before consuming timeout",
			run: func(t *testing.T) {
				meter := newMoistureBoundaryMeter()
				svc, st := newMoistureBoundaryService(meter)
				id := moistureBoundaryCreate(t, svc, "terminal")
				moistureBoundaryConfirmOne(t, svc, id, "terminal")
				moistureBoundaryDriveSamplingTaskToMoisture(t, svc, id, "terminal")
				moistureBoundaryRecordExplicit(t, svc, id, "terminal-m")
				moistureBoundaryApproveAndFinalize(t, svc, id, "terminal")

				before := moistureBoundarySnapshotTask(t, st, id)
				_, derr := svc.RecordMoisture(string(id), moistureBoundaryMeterRequest("terminal-illegal"))
				moistureBoundaryRequireCode(t, derr, domain.CodeFinalized)
				moistureBoundaryRequireMeter(t, meter, 0, measure.DefaultMeterAttempts)
				moistureBoundaryAssertTaskSnapshot(t, st, id, before)
			},
		},
		{
			name: "premature explicit moisture string does not derive or save evidence",
			run: func(t *testing.T) {
				meter := newMoistureBoundaryMeter()
				svc, st := newMoistureBoundaryService(meter)
				id := moistureBoundaryCreate(t, svc, "explicit-illegal")
				moistureBoundaryConfirmOne(t, svc, id, "explicit-illegal")

				before := moistureBoundarySnapshotTask(t, st, id)
				_, derr := svc.RecordMoisture(string(id), moistureBoundaryExplicitRequest("explicit-illegal-m"))
				moistureBoundaryRequireCode(t, derr, domain.CodeBadRequest)
				moistureBoundaryRequireMeter(t, meter, 0, measure.DefaultMeterAttempts)
				moistureBoundaryAssertTaskSnapshot(t, st, id, before)
			},
		},
		{
			name: "legal explicit moisture string saves without touching meter script",
			run: func(t *testing.T) {
				meter := newMoistureBoundaryMeter()
				svc, st := newMoistureBoundaryService(meter)
				id := moistureBoundaryCreate(t, svc, "explicit-legal")
				moistureBoundaryConfirmOne(t, svc, id, "explicit-legal")
				moistureBoundaryDriveSamplingTaskToMoisture(t, svc, id, "explicit-legal")

				resp, derr := svc.RecordMoisture(string(id), moistureBoundaryExplicitRequest("explicit-legal-m"))
				moistureBoundaryRequireNoError(t, derr)
				if resp.Status != inspection.StatusPendingReview || !resp.Advanced {
					t.Fatalf("expected explicit moisture to advance to pending_review, got status %s advanced %v", resp.Status, resp.Advanced)
				}
				moistureBoundaryRequireMeter(t, meter, 0, measure.DefaultMeterAttempts)
				view := moistureBoundarySnapshotTask(t, st, id)
				if view.Status != inspection.StatusPendingReview || view.MoistureCount != 1 {
					t.Fatalf("expected one saved moisture evidence in pending_review, got status %s evidence %d", view.Status, view.MoistureCount)
				}
			},
		},
		{
			name: "legal meter timeout consumes retry budget only in moisture stage",
			run: func(t *testing.T) {
				meter := newMoistureBoundaryMeter()
				svc, st := newMoistureBoundaryService(meter)
				id := moistureBoundaryCreate(t, svc, "legal-timeout")
				moistureBoundaryConfirmOne(t, svc, id, "legal-timeout")
				moistureBoundaryDriveSamplingTaskToMoisture(t, svc, id, "legal-timeout")

				before := moistureBoundarySnapshotTask(t, st, id)
				_, derr := svc.RecordMoisture(string(id), moistureBoundaryMeterRequest("legal-timeout-m"))
				moistureBoundaryRequireCode(t, derr, domain.CodeDeviceRetryable)
				moistureBoundaryRequireMeter(t, meter, measure.DefaultMeterAttempts, 0)
				moistureBoundaryAssertTaskSnapshot(t, st, id, before)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

type moistureBoundaryMeter struct {
	reads  int
	faults []measure.MoistureDeviceStatus
}

func newMoistureBoundaryMeter() *moistureBoundaryMeter {
	m := &moistureBoundaryMeter{}
	for i := 0; i < measure.DefaultMeterAttempts; i++ {
		m.faults = append(m.faults, measure.MoistureTimeout)
	}
	return m
}

func (m *moistureBoundaryMeter) Read(attempt string) (measure.Fixed, *domain.Error) {
	m.reads++
	if len(m.faults) > 0 {
		fault := m.faults[0]
		m.faults = m.faults[1:]
		return 0, domain.NewError(domain.CodeDeviceRetryable, string(fault), attempt)
	}
	return measure.Fixed(1200 + m.reads), nil
}

func newMoistureBoundaryService(meter measure.MoistureMeter) (*api.Service, *store.Memory) {
	c, roles := catalog.Seed()
	st := store.NewMemory()
	return api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), meter), st
}

func moistureBoundaryCreate(t *testing.T, svc *api.Service, op string) inspection.TaskID {
	t.Helper()
	resp, derr := svc.CreateTask(api.CreateTaskRequest{
		OperationID: op + "-create",
		SeedLot:     "lot-1001",
		Field:       "field-01",
		Variety:     "xiangliangyou-900",
		FemaleCert:  3,
		MaleCert:    3,
		BlindAllocs: []api.BlindAllocInput{
			{Code: "b1", Germination: 100, Pathogen: 50, Moisture: 30},
		},
		Chamber:        "ch-1",
		ChamberStart:   100,
		ChamberEnd:     200,
		Plate:          "p-1",
		Wells:          []string{"w1"},
		ReviewerRoster: []string{"reviewer-f", "reviewer-g"},
	})
	moistureBoundaryRequireNoError(t, derr)
	return inspection.TaskID(resp.TaskID)
}

func moistureBoundaryConfirmOne(t *testing.T, svc *api.Service, id inspection.TaskID, op string) {
	t.Helper()
	_, derr := svc.ConfirmSampling(string(id), api.SamplingRequest{
		OperationID: op + "-s1", Reviewer: "sampler-a", Field: "field-01",
		SeedLot: "lot-1001", BlindSeal: "seal-1", SampleCount: 180,
	})
	moistureBoundaryRequireNoError(t, derr)
}

func moistureBoundaryDriveSamplingTaskToMoisture(t *testing.T, svc *api.Service, id inspection.TaskID, op string) {
	t.Helper()
	_, derr := svc.ConfirmSampling(string(id), api.SamplingRequest{
		OperationID: op + "-s2", Reviewer: "sampler-b", Field: "field-01",
		SeedLot: "lot-1001", BlindSeal: "seal-1", SampleCount: 180,
	})
	moistureBoundaryRequireNoError(t, derr)
	_, derr = svc.SplitBlindSamples(string(id), api.SplitRequest{OperationID: op + "-split"})
	moistureBoundaryRequireNoError(t, derr)
	_, derr = svc.Occupy(string(id), api.OccupyRequest{OperationID: op + "-occupy"})
	moistureBoundaryRequireNoError(t, derr)
	for _, day := range []int32{2, 5, 8} {
		_, derr = svc.RecordGermination(string(id), api.GerminationRequest{
			OperationID: op + "-g" + string(rune('0'+day)), BlindCode: "b1",
			DayAge: day, Normal: 95, Abnormal: 3, Dead: 2, Collector: "germinator-c",
		})
		moistureBoundaryRequireNoError(t, derr)
	}
	_, derr = svc.RecordPathogen(string(id), api.PathogenRequest{
		OperationID: op + "-p", BlindCode: "b1", Plate: "p-1", Well: "w1",
		Verifier: "pathologist-d", Reading: moistureBoundaryInt32(10),
	})
	moistureBoundaryRequireNoError(t, derr)
}

func moistureBoundaryRecordExplicit(t *testing.T, svc *api.Service, id inspection.TaskID, op string) {
	t.Helper()
	_, derr := svc.RecordMoisture(string(id), moistureBoundaryExplicitRequest(op))
	moistureBoundaryRequireNoError(t, derr)
}

func moistureBoundaryApproveAndFinalize(t *testing.T, svc *api.Service, id inspection.TaskID, op string) {
	t.Helper()
	_, derr := svc.Review(string(id), api.ReviewRequest{OperationID: op + "-r1", Reviewer: "reviewer-f", Conclusion: "approve"})
	moistureBoundaryRequireNoError(t, derr)
	_, derr = svc.Review(string(id), api.ReviewRequest{OperationID: op + "-r2", Reviewer: "reviewer-g", Conclusion: "approve"})
	moistureBoundaryRequireNoError(t, derr)
	_, derr = svc.Finalize(string(id), api.FinalizeRequest{OperationID: op + "-final"})
	moistureBoundaryRequireNoError(t, derr)
}

func moistureBoundaryMeterRequest(op string) api.MoistureRequest {
	return api.MoistureRequest{
		OperationID: op, PurityGrains: 98, TotalGrains: 100,
		ThousandGrain: 25000, Collector: "metrologist-e",
	}
}

func moistureBoundaryExplicitRequest(op string) api.MoistureRequest {
	req := moistureBoundaryMeterRequest(op)
	req.Moisture = "12.50"
	return req
}

type moistureBoundaryTaskSnapshot struct {
	Status        inspection.TaskStatus
	Generation    inspection.Generation
	MoistureCount int
	AuditCount    int
}

func moistureBoundarySnapshotTask(t *testing.T, st store.Reader, id inspection.TaskID) moistureBoundaryTaskSnapshot {
	t.Helper()
	task, err := st.GetTask(id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	moisture, err := st.ListMoisture(id)
	if err != nil {
		t.Fatalf("list moisture: %v", err)
	}
	audit, err := st.ListAudit(id)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	return moistureBoundaryTaskSnapshot{
		Status: task.Status, Generation: task.Generation,
		MoistureCount: len(moisture), AuditCount: len(audit),
	}
}

func moistureBoundaryAssertTaskSnapshot(t *testing.T, st store.Reader, id inspection.TaskID, want moistureBoundaryTaskSnapshot) {
	t.Helper()
	got := moistureBoundarySnapshotTask(t, st, id)
	if got != want {
		t.Fatalf("task snapshot changed: got %+v want %+v", got, want)
	}
}

func moistureBoundaryRequireCode(t *testing.T, derr *domain.Error, want domain.ErrorCode) {
	t.Helper()
	if derr == nil {
		t.Fatalf("expected %s, got nil", want)
	}
	if derr.Code != want {
		t.Fatalf("expected %s, got %s", want, derr.Code)
	}
}

func moistureBoundaryRequireNoError(t *testing.T, derr *domain.Error) {
	t.Helper()
	if derr != nil {
		t.Fatalf("unexpected domain error: %s %v", derr.Code, derr.Reasons)
	}
}

func moistureBoundaryRequireMeter(t *testing.T, meter *moistureBoundaryMeter, reads, faults int) {
	t.Helper()
	if meter.reads != reads || len(meter.faults) != faults {
		t.Fatalf("meter state got reads=%d faults=%d, want reads=%d faults=%d", meter.reads, len(meter.faults), reads, faults)
	}
}

func moistureBoundaryInt32(v int32) *int32 {
	return &v
}

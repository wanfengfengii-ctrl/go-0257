package api_test

import (
	"riceguard/api"
	"riceguard/catalog"
	"riceguard/measure"
	"riceguard/pathogen"
	"riceguard/store"
)

// seedService builds a full in-memory service with a seeded catalog, role
// directory and deterministic instrument adapters.
func seedService() *api.Service {
	c, roles := catalog.Seed()
	return api.NewService(c, roles, store.NewMemory(), pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())
}

// validCreate returns a valid task-creation request for the seeded variety.
func validCreate(op string) api.CreateTaskRequest {
	return api.CreateTaskRequest{
		OperationID: op,
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
	}
}

// driveToReleasable walks a task from creation to the releasable state.
func driveToReleasable(t requireT, svc *api.Service, opPrefix string) string {
	t.Helper()
	create := validCreate(opPrefix + "-create")
	resp, derr := svc.CreateTask(create)
	if derr != nil {
		t.Fatalf("create: %v", derr)
	}
	id := resp.TaskID

	if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
		OperationID: opPrefix + "-s1", Reviewer: "sampler-a", Field: "field-01",
		SeedLot: "lot-1001", BlindSeal: "seal-1", SampleCount: 180,
	}); derr != nil {
		t.Fatalf("sampling 1: %v", derr)
	}
	if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
		OperationID: opPrefix + "-s2", Reviewer: "sampler-b", Field: "field-01",
		SeedLot: "lot-1001", BlindSeal: "seal-1", SampleCount: 180,
	}); derr != nil {
		t.Fatalf("sampling 2: %v", derr)
	}
	if _, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: opPrefix + "-split"}); derr != nil {
		t.Fatalf("split: %v", derr)
	}
	if _, derr := svc.Occupy(id, api.OccupyRequest{OperationID: opPrefix + "-occupy"}); derr != nil {
		t.Fatalf("occupy: %v", derr)
	}
	for _, d := range []int32{2, 5, 8} {
		if _, derr := svc.RecordGermination(id, api.GerminationRequest{
			OperationID: opPrefix + "-g" + string(rune('0'+d)), BlindCode: "b1",
			DayAge: d, Normal: 95, Abnormal: 3, Dead: 2, Collector: "germinator-c",
		}); derr != nil {
			t.Fatalf("germination day %d: %v", d, derr)
		}
	}
	if _, derr := svc.RecordPathogen(id, api.PathogenRequest{
		OperationID: opPrefix + "-p", BlindCode: "b1", Plate: "p-1", Well: "w1",
		Verifier: "pathologist-d", Reading: int32Ptr(10),
	}); derr != nil {
		t.Fatalf("pathogen: %v", derr)
	}
	if _, derr := svc.RecordMoisture(id, api.MoistureRequest{
		OperationID: opPrefix + "-m", Moisture: "12.50", PurityGrains: 98,
		TotalGrains: 100, ThousandGrain: 25000, Collector: "metrologist-e",
	}); derr != nil {
		t.Fatalf("moisture: %v", derr)
	}
	if _, derr := svc.Review(id, api.ReviewRequest{OperationID: opPrefix + "-r1", Reviewer: "reviewer-f", Conclusion: "approve"}); derr != nil {
		t.Fatalf("review 1: %v", derr)
	}
	if _, derr := svc.Review(id, api.ReviewRequest{OperationID: opPrefix + "-r2", Reviewer: "reviewer-g", Conclusion: "approve"}); derr != nil {
		t.Fatalf("review 2: %v", derr)
	}
	return id
}

type requireT interface {
	Helper()
	Fatalf(string, ...any)
}

func int32Ptr(v int32) *int32 { return &v }

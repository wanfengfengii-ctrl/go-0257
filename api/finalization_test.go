package api_test

import (
	"testing"

	"riceguard/api"
	"riceguard/domain"
	"riceguard/inspection"
)

func TestFinalizeCancel(t *testing.T) {
	svc := seedService()
	id := driveToReleasable(t, svc, "cancel")
	final, derr := svc.Finalize(id, api.FinalizeRequest{OperationID: "cancel-final", Outcome: "cancelled", Reason: "operator"})
	if derr != nil {
		t.Fatalf("finalize: %v", derr)
	}
	if final.Status != inspection.StatusCancelled {
		t.Fatalf("expected cancelled, got %s", final.Status)
	}
	if final.Credential != "" {
		t.Fatal("cancelled task must not mint a credential")
	}
}

func TestFinalizeOnlyOneTerminalOutcome(t *testing.T) {
	svc := seedService()
	id := driveToReleasable(t, svc, "once")
	if _, derr := svc.Finalize(id, api.FinalizeRequest{OperationID: "once-final"}); derr != nil {
		t.Fatalf("finalize: %v", derr)
	}
	_, derr := svc.Finalize(id, api.FinalizeRequest{OperationID: "once-final-2", Outcome: "cancelled"})
	if derr == nil {
		t.Fatal("expected second finalize to be rejected, got nil")
	}
	if derr.Code != domain.CodeFinalized {
		t.Fatalf("expected CodeFinalized, got %s", derr.Code)
	}
}

func TestReviewerOverlapRejected(t *testing.T) {
	svc := seedService()
	id := driveToPathogen(t, svc, "overlap")
	svc.RecordPathogen(id, api.PathogenRequest{OperationID: "overlap-p", BlindCode: "b1", Plate: "p-1", Well: "w1", Verifier: "pathologist-d", Reading: int32Ptr(10)})
	svc.RecordMoisture(id, api.MoistureRequest{OperationID: "overlap-m", Moisture: "12.50", PurityGrains: 98, TotalGrains: 100, ThousandGrain: 25000, Collector: "metrologist-e"})
	// A collector trying to review must be rejected.
	_, derr := svc.Review(id, api.ReviewRequest{OperationID: "overlap-r", Reviewer: "germinator-c", Conclusion: "approve"})
	if derr == nil {
		t.Fatal("expected collector-as-reviewer rejection, got nil")
	}
	if derr.Code != domain.CodeBadRequest {
		t.Fatalf("expected CodeBadRequest, got %s", derr.Code)
	}
}

func TestQuarantineFlow(t *testing.T) {
	svc := seedService()
	id := driveToPathogen(t, svc, "qf")
	// Submit a contaminated reading.
	_, derr := svc.RecordPathogen(id, api.PathogenRequest{
		OperationID: "qf-path", BlindCode: "b1", Plate: "p-1", Well: "w1",
		Verifier: "pathologist-d", Reading: int32Ptr(10), Contaminated: true,
	})
	if derr != nil {
		t.Fatalf("pathogen: %v", derr)
	}
	// Complete moisture and reviews, then finalize -> quarantine.
	finishToReleasable(t, svc, id, "qf")
	final, derr := svc.Finalize(id, api.FinalizeRequest{OperationID: "qf-final"})
	if derr != nil {
		t.Fatalf("finalize: %v", derr)
	}
	if final.Status != inspection.StatusQuarantined {
		t.Fatalf("expected quarantined, got %s", final.Status)
	}
}

// driveToPathogen walks a task to the pathogen_checking state.
func driveToPathogen(t *testing.T, svc *api.Service, op string) string {
	t.Helper()
	resp, derr := svc.CreateTask(validCreate(op + "-create"))
	if derr != nil {
		t.Fatalf("create: %v", derr)
	}
	id := resp.TaskID
	for i, r := range []string{"sampler-a", "sampler-b"} {
		svc.ConfirmSampling(id, api.SamplingRequest{OperationID: op + "-s" + string(rune('0'+i)), Reviewer: r, Field: "field-01", SeedLot: "lot-1001", BlindSeal: "seal-1", SampleCount: 180})
	}
	svc.SplitBlindSamples(id, api.SplitRequest{OperationID: op + "-split"})
	svc.Occupy(id, api.OccupyRequest{OperationID: op + "-occupy"})
	for _, d := range []int32{2, 5, 8} {
		svc.RecordGermination(id, api.GerminationRequest{OperationID: op + "-g" + string(rune('0'+d)), BlindCode: "b1", DayAge: d, Normal: 95, Abnormal: 3, Dead: 2, Collector: "germinator-c"})
	}
	return id
}

// finishToReleasable completes moisture and review for a task already in the
// pathogen state.
func finishToReleasable(t *testing.T, svc *api.Service, id string, op string) {
	t.Helper()
	if _, derr := svc.RecordMoisture(id, api.MoistureRequest{
		OperationID: op + "-m", Moisture: "12.50", PurityGrains: 98, TotalGrains: 100,
		ThousandGrain: 25000, Collector: "metrologist-e",
	}); derr != nil {
		t.Fatalf("moisture: %v", derr)
	}
	if _, derr := svc.Review(id, api.ReviewRequest{OperationID: op + "-r1", Reviewer: "reviewer-f", Conclusion: "approve"}); derr != nil {
		t.Fatalf("review 1: %v", derr)
	}
	if _, derr := svc.Review(id, api.ReviewRequest{OperationID: op + "-r2", Reviewer: "reviewer-g", Conclusion: "approve"}); derr != nil {
		t.Fatalf("review 2: %v", derr)
	}
}

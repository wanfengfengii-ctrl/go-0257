package api_test

import (
	"testing"

	"riceguard/api"
	"riceguard/domain"
	"riceguard/inspection"
)

// TestBlindCodeReusableAfterRelease reproduces the reported instability: once a
// batch is released, the lab re-enters the SAME blind code on a different
// field with a different chamber and plate well. Before the fix the second
// batch sailed through create, sampling, split, occupy, germination, pathogen,
// moisture and review, only to be rejected with RICE_BLIND_DUPLICATE at
// finalize — and only in-process, because the unblinding gate was in-memory and
// keyed by code alone, so a restart made the rejection vanish. The verdict was
// therefore unstable across restarts. After the fix the finalize-time gate is
// scoped per task, matching the create-time assertion that already skips
// terminal tasks, so reuse after a terminal outcome is allowed consistently.
func TestBlindCodeReusableAfterRelease(t *testing.T) {
	svc := seedService()

	// Batch A: drive to release with blind code b1 on field-01 / lot-1001.
	idA := driveToReleasable(t, svc, "reuse-a")
	finalA, derr := svc.Finalize(idA, api.FinalizeRequest{OperationID: "reuse-a-final"})
	if derr != nil {
		t.Fatalf("finalize A: %v", derr)
	}
	if finalA.Status != inspection.StatusReleased {
		t.Fatalf("expected A released, got %s", finalA.Status)
	}

	// Batch B: SAME blind code b1, but a different field, seed lot, chamber and
	// plate/well so it occupies no resource held by A.
	req := validCreate("reuse-b-create")
	req.SeedLot = "lot-2002"
	req.Field = "field-02"
	req.Chamber = "ch-2"
	req.ChamberStart = 300
	req.ChamberEnd = 400
	req.Plate = "p-2"
	req.Wells = []string{"w2"}
	resp, derr := svc.CreateTask(req)
	if derr != nil {
		t.Fatalf("create B reusing blind code after release: %v", derr)
	}
	idB := resp.TaskID

	// Every downstream step that previously advanced must still advance; none
	// may surface RICE_BLIND_DUPLICATE.
	for i, r := range []string{"sampler-a", "sampler-b"} {
		if _, derr := svc.ConfirmSampling(idB, api.SamplingRequest{
			OperationID: "reuse-b-s" + string(rune('0'+i)), Reviewer: r,
			Field: "field-02", SeedLot: "lot-2002", BlindSeal: "seal-2", SampleCount: 180,
		}); derr != nil {
			t.Fatalf("sampling B %d: %v", i, derr)
		}
	}
	if _, derr := svc.SplitBlindSamples(idB, api.SplitRequest{OperationID: "reuse-b-split"}); derr != nil {
		t.Fatalf("split B: %v", derr)
	}
	if _, derr := svc.Occupy(idB, api.OccupyRequest{OperationID: "reuse-b-occupy"}); derr != nil {
		t.Fatalf("occupy B: %v", derr)
	}
	for _, d := range []int32{2, 5, 8} {
		if _, derr := svc.RecordGermination(idB, api.GerminationRequest{
			OperationID: "reuse-b-g" + string(rune('0'+d)), BlindCode: "b1",
			DayAge: d, Normal: 95, Abnormal: 3, Dead: 2, Collector: "germinator-c",
		}); derr != nil {
			t.Fatalf("germination B day %d: %v", d, derr)
		}
	}
	if _, derr := svc.RecordPathogen(idB, api.PathogenRequest{
		OperationID: "reuse-b-p", BlindCode: "b1", Plate: "p-2", Well: "w2",
		Verifier: "pathologist-d", Reading: int32Ptr(10),
	}); derr != nil {
		t.Fatalf("pathogen B: %v", derr)
	}
	if _, derr := svc.RecordMoisture(idB, api.MoistureRequest{
		OperationID: "reuse-b-m", Moisture: "12.50", PurityGrains: 98,
		TotalGrains: 100, ThousandGrain: 25000, Collector: "metrologist-e",
	}); derr != nil {
		t.Fatalf("moisture B: %v", derr)
	}
	if _, derr := svc.Review(idB, api.ReviewRequest{OperationID: "reuse-b-r1", Reviewer: "reviewer-f", Conclusion: "approve"}); derr != nil {
		t.Fatalf("review B 1: %v", derr)
	}
	if _, derr := svc.Review(idB, api.ReviewRequest{OperationID: "reuse-b-r2", Reviewer: "reviewer-g", Conclusion: "approve"}); derr != nil {
		t.Fatalf("review B 2: %v", derr)
	}

	// The decisive assertion: finalize must succeed and mint a second, distinct
	// release credential. Before the fix this returned RICE_BLIND_DUPLICATE.
	finalB, derr := svc.Finalize(idB, api.FinalizeRequest{OperationID: "reuse-b-final"})
	if derr != nil {
		t.Fatalf("finalize B reusing released blind code: %v", derr)
	}
	if finalB.Status != inspection.StatusReleased {
		t.Fatalf("expected B released, got %s", finalB.Status)
	}
	if finalB.Credential == "" {
		t.Fatal("expected B release credential")
	}
	if finalB.Credential == finalA.Credential {
		t.Fatal("distinct released batches must mint distinct credentials")
	}
}

// TestBlindCodeStillUniqueWithinOpenTasks asserts the other half of the
// contract: while the owning task is still open, a code may not be reused by a
// second open task. Reuse is permitted only after a terminal outcome.
func TestBlindCodeStillUniqueWithinOpenTasks(t *testing.T) {
	svc := seedService()

	// Task A holds b1 while still open (pending sampling).
	if _, derr := svc.CreateTask(validCreate("open-a-create")); derr != nil {
		t.Fatalf("create A: %v", derr)
	}

	// A second open task may not bind the same code.
	req := validCreate("open-b-create")
	req.SeedLot = "lot-2002"
	req.Field = "field-02"
	req.Chamber = "ch-2"
	req.ChamberStart = 300
	req.ChamberEnd = 400
	req.Plate = "p-2"
	req.Wells = []string{"w2"}
	_, derr := svc.CreateTask(req)
	if derr == nil {
		t.Fatal("expected duplicate blind code rejection while owner is open, got nil")
	}
	if derr.Code != domain.CodeBlindDuplicate {
		t.Fatalf("expected CodeBlindDuplicate, got %s", derr.Code)
	}
}

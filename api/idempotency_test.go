package api_test

import (
	"testing"

	"riceguard/api"
	"riceguard/domain"
)

func TestCreateTaskIdempotentRetry(t *testing.T) {
	svc := seedService()
	req := validCreate("op-idem")
	first, derr := svc.CreateTask(req)
	if derr != nil {
		t.Fatalf("create: %v", derr)
	}
	second, derr := svc.CreateTask(req)
	if derr != nil {
		t.Fatalf("retry: %v", derr)
	}
	if first.TaskID != second.TaskID {
		t.Fatalf("idempotent retry returned different task: %s vs %s", first.TaskID, second.TaskID)
	}
}

func TestCreateTaskIdempotencyConflict(t *testing.T) {
	svc := seedService()
	if _, derr := svc.CreateTask(validCreate("op-conflict")); derr != nil {
		t.Fatalf("create: %v", derr)
	}
	req := validCreate("op-conflict")
	req.SeedLot = "lot-2002"
	_, derr := svc.CreateTask(req)
	if derr == nil {
		t.Fatal("expected conflict, got nil")
	}
	if derr.Code != domain.CodeIdempotencyConflict {
		t.Fatalf("expected CodeIdempotencyConflict, got %s", derr.Code)
	}
}

func TestSplitIdempotentRetrySameResult(t *testing.T) {
	svc := seedService()
	create := validCreate("op-split-idem")
	resp, _ := svc.CreateTask(create)
	id := resp.TaskID
	for _, r := range []string{"sampler-a", "sampler-b"} {
		svc.ConfirmSampling(id, api.SamplingRequest{OperationID: "op-" + r, Reviewer: r, Field: "field-01", SeedLot: "lot-1001", BlindSeal: "seal-1", SampleCount: 180})
	}
	first, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: "op-split"})
	if derr != nil {
		t.Fatalf("split: %v", derr)
	}
	second, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: "op-split"})
	if derr != nil {
		t.Fatalf("split retry: %v", derr)
	}
	if len(first.BlindCodes) != len(second.BlindCodes) {
		t.Fatal("idempotent split retry returned different result")
	}
}

func TestDuplicateBlindCodeRejected(t *testing.T) {
	svc := seedService()
	req := validCreate("op-dup-blind")
	req.BlindAllocs = []api.BlindAllocInput{
		{Code: "b1", Germination: 100, Pathogen: 50, Moisture: 30},
		{Code: "b1", Germination: 100, Pathogen: 50, Moisture: 30},
	}
	_, derr := svc.CreateTask(req)
	if derr == nil {
		t.Fatal("expected duplicate blind code rejection, got nil")
	}
	if derr.Code != domain.CodeBlindDuplicate {
		t.Fatalf("expected CodeBlindDuplicate, got %s", derr.Code)
	}
}

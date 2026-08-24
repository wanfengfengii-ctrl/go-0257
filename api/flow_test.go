package api_test

import (
	"testing"

	"riceguard/api"
	"riceguard/domain"
	"riceguard/inspection"
)

func TestCreateTaskHappyPath(t *testing.T) {
	svc := seedService()
	resp, derr := svc.CreateTask(validCreate("op-1"))
	if derr != nil {
		t.Fatalf("unexpected error: %v", derr)
	}
	if resp.Status != inspection.StatusPendingSampling {
		t.Fatalf("expected pending_sampling, got %s", resp.Status)
	}
	if resp.Generation != 1 {
		t.Fatalf("expected generation 1, got %d", resp.Generation)
	}
}

func TestCreateTaskVarietyMismatch(t *testing.T) {
	svc := seedService()
	req := validCreate("op-x")
	req.Variety = "unknown"
	_, derr := svc.CreateTask(req)
	if derr == nil {
		t.Fatal("expected mismatch, got nil")
	}
	if derr.Code != domain.CodeVarietyMismatch {
		t.Fatalf("expected CodeVarietyMismatch, got %s", derr.Code)
	}
}

func TestCreateTaskStaleCert(t *testing.T) {
	svc := seedService()
	req := validCreate("op-x")
	req.FemaleCert = 1
	_, derr := svc.CreateTask(req)
	if derr == nil {
		t.Fatal("expected stale cert, got nil")
	}
	if derr.Code != domain.CodeStaleParentCert {
		t.Fatalf("expected CodeStaleParentCert, got %s", derr.Code)
	}
}

func TestSamplingFieldMismatchRejected(t *testing.T) {
	svc := seedService()
	resp, _ := svc.CreateTask(validCreate("op-1"))
	_, derr := svc.ConfirmSampling(resp.TaskID, api.SamplingRequest{
		OperationID: "op-s", Reviewer: "sampler-a", Field: "field-99",
		SeedLot: "lot-1001", BlindSeal: "seal-1", SampleCount: 180,
	})
	if derr == nil {
		t.Fatal("expected field mismatch, got nil")
	}
	if derr.Code != domain.CodeVarietyMismatch {
		t.Fatalf("expected CodeVarietyMismatch, got %s", derr.Code)
	}
}

func TestFullPipelineReleases(t *testing.T) {
	svc := seedService()
	id := driveToReleasable(t, svc, "fp")
	final, derr := svc.Finalize(id, api.FinalizeRequest{OperationID: "fp-final", Outcome: ""})
	if derr != nil {
		t.Fatalf("finalize: %v", derr)
	}
	if final.Status != inspection.StatusReleased {
		t.Fatalf("expected released, got %s", final.Status)
	}
	if final.Credential == "" {
		t.Fatal("expected a release credential")
	}

	view, derr := svc.GetTask(id)
	if derr != nil {
		t.Fatalf("get task: %v", derr)
	}
	if view.Task.TerminalVersion == 0 {
		t.Fatal("expected terminal version set")
	}
}

func TestFinalizedRejectsFurtherReadings(t *testing.T) {
	svc := seedService()
	id := driveToReleasable(t, svc, "fin")
	if _, derr := svc.Finalize(id, api.FinalizeRequest{OperationID: "fin-final"}); derr != nil {
		t.Fatalf("finalize: %v", derr)
	}
	_, derr := svc.RecordGermination(id, api.GerminationRequest{
		OperationID: "fin-late", BlindCode: "b1", DayAge: 2, Normal: 95, Abnormal: 3, Dead: 2, Collector: "germinator-c",
	})
	if derr == nil {
		t.Fatal("expected finalized rejection, got nil")
	}
	if derr.Code != domain.CodeFinalized {
		t.Fatalf("expected CodeFinalized, got %s", derr.Code)
	}
}

func TestListTasksReturnsCreated(t *testing.T) {
	svc := seedService()
	if _, derr := svc.CreateTask(validCreate("op-list")); derr != nil {
		t.Fatalf("create: %v", derr)
	}
	tasks, derr := svc.ListTasks()
	if derr != nil {
		t.Fatalf("list: %v", derr)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
}

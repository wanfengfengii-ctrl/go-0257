package api_test

import (
	"testing"

	"riceguard/api"
	"riceguard/domain"
)

func TestModel_MemoryStorePreservesIdempotencyAcrossTransactions(t *testing.T) {
	makeCreate := func(op, lot, code string) api.CreateTaskRequest {
		req := validCreate(op)
		req.SeedLot = lot
		req.BlindAllocs = []api.BlindAllocInput{
			{Code: code, Germination: 100, Pathogen: 50, Moisture: 30},
		}
		req.Chamber = "ch-" + code
		req.Plate = "p-" + code
		req.Wells = []string{"w-" + code}
		return req
	}

	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "successful transaction keeps prior create operation",
			run: func(t *testing.T) {
				svc := seedService()
				original := makeCreate("op-create", "lot-1001", "b1")
				first, derr := svc.CreateTask(original)
				if derr != nil {
					t.Fatalf("create original: %v", derr)
				}
				if _, derr := svc.CreateTask(makeCreate("op-other", "lot-2002", "b2")); derr != nil {
					t.Fatalf("create unrelated task: %v", derr)
				}

				replay, derr := svc.CreateTask(original)
				if derr != nil {
					t.Fatalf("same-content replay after successful transaction: %v", derr)
				}
				if replay != first {
					t.Fatalf("same-content replay returned %+v, want %+v", replay, first)
				}

				_, derr = svc.CreateTask(makeCreate("op-create", "lot-3003", "b3"))
				if derr == nil {
					t.Fatal("expected idempotency conflict after successful transaction, got nil")
				}
				if derr.Code != domain.CodeIdempotencyConflict {
					t.Fatalf("expected %s, got %s", domain.CodeIdempotencyConflict, derr.Code)
				}
			},
		},
		{
			name: "failed transaction keeps prior create operation and does not claim failed operation",
			run: func(t *testing.T) {
				svc := seedService()
				original := makeCreate("op-create", "lot-1001", "b1")
				first, derr := svc.CreateTask(original)
				if derr != nil {
					t.Fatalf("create original: %v", derr)
				}

				failed := makeCreate("op-failed", "lot-1001", "b4")
				if _, derr := svc.CreateTask(failed); derr == nil {
					t.Fatal("expected duplicate seed-lot create to fail")
				}

				corrected := failed
				corrected.SeedLot = "lot-4004"
				correctedResp, derr := svc.CreateTask(corrected)
				if derr != nil {
					t.Fatalf("corrected retry of failed operation: %v", derr)
				}
				if correctedResp.TaskID == first.TaskID {
					t.Fatalf("corrected failed operation reused original task %s", correctedResp.TaskID)
				}

				replay, derr := svc.CreateTask(original)
				if derr != nil {
					t.Fatalf("same-content replay after failed transaction: %v", derr)
				}
				if replay != first {
					t.Fatalf("same-content replay returned %+v, want %+v", replay, first)
				}

				_, derr = svc.CreateTask(makeCreate("op-create", "lot-5005", "b5"))
				if derr == nil {
					t.Fatal("expected idempotency conflict after failed transaction, got nil")
				}
				if derr.Code != domain.CodeIdempotencyConflict {
					t.Fatalf("expected %s, got %s", domain.CodeIdempotencyConflict, derr.Code)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

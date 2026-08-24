package api_test

import (
	"reflect"
	"testing"

	"riceguard/api"
	"riceguard/domain"
	"riceguard/inspection"
)

func TestModel_SamplingConfirmationRequiresTwoDistinctSamplers(t *testing.T) {
	newTask := func(t *testing.T, op string) (*api.Service, string) {
		t.Helper()
		svc := seedService()
		created, derr := svc.CreateTask(validCreate(op))
		if derr != nil {
			t.Fatalf("create task: %v", derr)
		}
		return svc, created.TaskID
	}
	samplingReq := func(op, reviewer string) api.SamplingRequest {
		return api.SamplingRequest{
			OperationID: op,
			Reviewer:    reviewer,
			Field:       "field-01",
			SeedLot:     "lot-1001",
			BlindSeal:   "seal-1",
			SampleCount: 180,
		}
	}
	assertView := func(t *testing.T, svc *api.Service, id string, wantStatus inspection.TaskStatus, wantReviewers []string) {
		t.Helper()
		view, derr := svc.GetTask(id)
		if derr != nil {
			t.Fatalf("get task: %v", derr)
		}
		if view.Task.Status != wantStatus {
			t.Fatalf("expected status %s, got %s", wantStatus, view.Task.Status)
		}
		if len(view.Confirmations) != len(wantReviewers) {
			t.Fatalf("expected %d confirmations, got %d", len(wantReviewers), len(view.Confirmations))
		}
		for i, want := range wantReviewers {
			if view.Confirmations[i].Reviewer != want {
				t.Fatalf("confirmation %d reviewer: expected %s, got %s", i, want, view.Confirmations[i].Reviewer)
			}
		}
	}
	expectCode := func(t *testing.T, derr *domain.Error, want domain.ErrorCode) {
		t.Helper()
		if derr == nil {
			t.Fatalf("expected %s, got nil", want)
		}
		if derr.Code != want {
			t.Fatalf("expected %s, got %s", want, derr.Code)
		}
	}

	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "same operation id and content replays original confirmation",
			run: func(t *testing.T) {
				svc, id := newTask(t, "model-sampling-retry-create")
				req := samplingReq("model-sampling-retry-op", "sampler-a")

				first, derr := svc.ConfirmSampling(id, req)
				if derr != nil {
					t.Fatalf("first sampling confirmation: %v", derr)
				}
				second, derr := svc.ConfirmSampling(id, req)
				if derr != nil {
					t.Fatalf("idempotent retry: %v", derr)
				}
				if !reflect.DeepEqual(first, second) {
					t.Fatalf("idempotent retry returned different response: %#v vs %#v", first, second)
				}
				assertView(t, svc, id, inspection.StatusPendingSampling, []string{"sampler-a"})
			},
		},
		{
			name: "same operation id with different content conflicts and does not add a confirmer",
			run: func(t *testing.T) {
				svc, id := newTask(t, "model-sampling-conflict-create")
				req := samplingReq("model-sampling-conflict-op", "sampler-a")
				if _, derr := svc.ConfirmSampling(id, req); derr != nil {
					t.Fatalf("first sampling confirmation: %v", derr)
				}

				conflict := req
				conflict.BlindSeal = "seal-2"
				_, derr := svc.ConfirmSampling(id, conflict)
				expectCode(t, derr, domain.CodeIdempotencyConflict)
				assertView(t, svc, id, inspection.StatusPendingSampling, []string{"sampler-a"})

				advanced, derr := svc.ConfirmSampling(id, samplingReq("model-sampling-conflict-sampler-b", "sampler-b"))
				if derr != nil {
					t.Fatalf("qualified second sampler after conflict: %v", derr)
				}
				if !advanced.Advanced || advanced.Status != inspection.StatusBlindSplit {
					t.Fatalf("expected second qualified sampler to advance to blind_split, got status %s advanced=%v", advanced.Status, advanced.Advanced)
				}
				assertView(t, svc, id, inspection.StatusBlindSplit, []string{"sampler-a", "sampler-b"})
			},
		},
		{
			name: "same sampler using a new operation id cannot be counted twice",
			run: func(t *testing.T) {
				svc, id := newTask(t, "model-sampling-duplicate-create")
				if _, derr := svc.ConfirmSampling(id, samplingReq("model-sampling-duplicate-a1", "sampler-a")); derr != nil {
					t.Fatalf("first sampling confirmation: %v", derr)
				}

				_, derr := svc.ConfirmSampling(id, samplingReq("model-sampling-duplicate-a2", "sampler-a"))
				expectCode(t, derr, domain.CodeBadRequest)
				assertView(t, svc, id, inspection.StatusPendingSampling, []string{"sampler-a"})

				advanced, derr := svc.ConfirmSampling(id, samplingReq("model-sampling-duplicate-b", "sampler-b"))
				if derr != nil {
					t.Fatalf("qualified second sampler after duplicate rejection: %v", derr)
				}
				if !advanced.Advanced || advanced.Status != inspection.StatusBlindSplit {
					t.Fatalf("expected second qualified sampler to advance to blind_split, got status %s advanced=%v", advanced.Status, advanced.Advanced)
				}
				assertView(t, svc, id, inspection.StatusBlindSplit, []string{"sampler-a", "sampler-b"})
			},
		},
		{
			name: "different reviewer without sampler qualification cannot be counted",
			run: func(t *testing.T) {
				svc, id := newTask(t, "model-sampling-unqualified-create")
				if _, derr := svc.ConfirmSampling(id, samplingReq("model-sampling-unqualified-a", "sampler-a")); derr != nil {
					t.Fatalf("first sampling confirmation: %v", derr)
				}

				_, derr := svc.ConfirmSampling(id, samplingReq("model-sampling-unqualified-reviewer", "reviewer-f"))
				expectCode(t, derr, domain.CodeBadRequest)
				assertView(t, svc, id, inspection.StatusPendingSampling, []string{"sampler-a"})

				advanced, derr := svc.ConfirmSampling(id, samplingReq("model-sampling-unqualified-b", "sampler-b"))
				if derr != nil {
					t.Fatalf("qualified second sampler after unqualified rejection: %v", derr)
				}
				if !advanced.Advanced || advanced.Status != inspection.StatusBlindSplit {
					t.Fatalf("expected second qualified sampler to advance to blind_split, got status %s advanced=%v", advanced.Status, advanced.Advanced)
				}
				assertView(t, svc, id, inspection.StatusBlindSplit, []string{"sampler-a", "sampler-b"})
			},
		},
		{
			name: "two different qualified samplers advance to blind split",
			run: func(t *testing.T) {
				svc, id := newTask(t, "model-sampling-two-qualified-create")
				first, derr := svc.ConfirmSampling(id, samplingReq("model-sampling-two-qualified-a", "sampler-a"))
				if derr != nil {
					t.Fatalf("first sampling confirmation: %v", derr)
				}
				if first.Advanced || first.Status != inspection.StatusPendingSampling {
					t.Fatalf("expected first sampler to remain pending, got status %s advanced=%v", first.Status, first.Advanced)
				}

				second, derr := svc.ConfirmSampling(id, samplingReq("model-sampling-two-qualified-b", "sampler-b"))
				if derr != nil {
					t.Fatalf("second sampling confirmation: %v", derr)
				}
				if !second.Advanced || second.Status != inspection.StatusBlindSplit {
					t.Fatalf("expected second sampler to advance to blind_split, got status %s advanced=%v", second.Status, second.Advanced)
				}
				assertView(t, svc, id, inspection.StatusBlindSplit, []string{"sampler-a", "sampler-b"})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

package api_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"riceguard/api"
	"riceguard/catalog"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/pathogen"
	"riceguard/store"
)

func TestModel_SplitOperationIDScopedToTargetTask(t *testing.T) {
	driveToBlindSplit := func(t *testing.T, svc *api.Service, opPrefix, seedLot, blindCode string) string {
		t.Helper()
		req := validCreate(opPrefix + "-create")
		req.SeedLot = seedLot
		req.BlindAllocs = []api.BlindAllocInput{
			{Code: blindCode, Germination: 100, Pathogen: 50, Moisture: 30},
		}
		req.Chamber = opPrefix + "-chamber"
		req.Plate = opPrefix + "-plate"
		req.Wells = []string{opPrefix + "-well"}

		resp, derr := svc.CreateTask(req)
		if derr != nil {
			t.Fatalf("create %s: %v", opPrefix, derr)
		}

		sampleCount := 0
		for _, alloc := range req.BlindAllocs {
			sampleCount += alloc.Germination + alloc.Pathogen + alloc.Moisture
		}
		for _, reviewer := range []string{"sampler-a", "sampler-b"} {
			_, derr := svc.ConfirmSampling(resp.TaskID, api.SamplingRequest{
				OperationID: opPrefix + "-sampling-" + reviewer,
				Reviewer:    reviewer,
				Field:       req.Field,
				SeedLot:     req.SeedLot,
				BlindSeal:   opPrefix + "-seal",
				SampleCount: sampleCount,
			})
			if derr != nil {
				t.Fatalf("sampling %s/%s: %v", opPrefix, reviewer, derr)
			}
		}
		return resp.TaskID
	}

	backends := []struct {
		name string
		new  func(*testing.T) *api.Service
	}{
		{
			name: "memory",
			new: func(t *testing.T) *api.Service {
				t.Helper()
				return seedService()
			},
		},
		{
			name: "sqlite",
			new: func(t *testing.T) *api.Service {
				t.Helper()
				c, roles := catalog.Seed()
				st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "riceguard.db"))
				if err != nil {
					t.Fatalf("open sqlite: %v", err)
				}
				t.Cleanup(func() {
					if err := st.Close(); err != nil {
						t.Fatalf("close sqlite: %v", err)
					}
				})
				return api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())
			},
		},
	}

	cases := []struct {
		name string
		run  func(*testing.T, *api.Service)
	}{
		{
			name: "same task retry returns the original split without duplicate records",
			run: func(t *testing.T, svc *api.Service) {
				taskID := driveToBlindSplit(t, svc, "model-split-retry", "lot-model-retry", "model-retry-b1")

				first, derr := svc.SplitBlindSamples(taskID, api.SplitRequest{OperationID: "model-split-op"})
				if derr != nil {
					t.Fatalf("first split: %v", derr)
				}
				viewAfterFirst, derr := svc.GetTask(taskID)
				if derr != nil {
					t.Fatalf("view after first split: %v", derr)
				}

				second, derr := svc.SplitBlindSamples(taskID, api.SplitRequest{OperationID: "model-split-op"})
				if derr != nil {
					t.Fatalf("split retry: %v", derr)
				}
				viewAfterRetry, derr := svc.GetTask(taskID)
				if derr != nil {
					t.Fatalf("view after retry: %v", derr)
				}

				if first.TaskID != second.TaskID || first.Status != second.Status ||
					first.Generation != second.Generation || !reflect.DeepEqual(first.BlindCodes, second.BlindCodes) {
					t.Fatalf("retry response changed: first=%+v second=%+v", first, second)
				}
				if len(viewAfterRetry.Splits) != len(viewAfterFirst.Splits) {
					t.Fatalf("retry duplicated split records: before=%d after=%d", len(viewAfterFirst.Splits), len(viewAfterRetry.Splits))
				}
				if len(viewAfterRetry.BlindSamples) != len(viewAfterFirst.BlindSamples) {
					t.Fatalf("retry duplicated blind sample records: before=%d after=%d", len(viewAfterFirst.BlindSamples), len(viewAfterRetry.BlindSamples))
				}
			},
		},
		{
			name: "same operation id on another task does not replay the first task result",
			run: func(t *testing.T, svc *api.Service) {
				firstID := driveToBlindSplit(t, svc, "model-split-first", "lot-model-first", "model-first-b1")
				secondID := driveToBlindSplit(t, svc, "model-split-second", "lot-model-second", "model-second-b1")

				first, derr := svc.SplitBlindSamples(firstID, api.SplitRequest{OperationID: "model-shared-split-op"})
				if derr != nil {
					t.Fatalf("first split: %v", derr)
				}
				firstViewAfterSplit, derr := svc.GetTask(firstID)
				if derr != nil {
					t.Fatalf("view first task after split: %v", derr)
				}

				second, derr := svc.SplitBlindSamples(secondID, api.SplitRequest{OperationID: "model-shared-split-op"})
				secondView, viewErr := svc.GetTask(secondID)
				if viewErr != nil {
					t.Fatalf("view second task: %v", viewErr)
				}
				if derr != nil {
					if derr.Code != domain.CodeIdempotencyConflict {
						t.Fatalf("expected cross-task reuse to execute independently or conflict deterministically, got %s", derr.Code)
					}
					if secondView.Task.Status != inspection.StatusBlindSplit {
						t.Fatalf("conflicting cross-task reuse mutated second task status to %s", secondView.Task.Status)
					}
					if len(secondView.Splits) != 0 || len(secondView.BlindSamples) != 0 {
						t.Fatalf("conflicting cross-task reuse wrote second task split state: splits=%d samples=%d", len(secondView.Splits), len(secondView.BlindSamples))
					}
					firstRetry, retryErr := svc.SplitBlindSamples(firstID, api.SplitRequest{OperationID: "model-shared-split-op"})
					if retryErr != nil {
						t.Fatalf("first task retry after cross-task conflict: %v", retryErr)
					}
					if firstRetry.TaskID != first.TaskID || !reflect.DeepEqual(firstRetry.BlindCodes, first.BlindCodes) {
						t.Fatalf("first task retry changed after cross-task conflict: first=%+v retry=%+v", first, firstRetry)
					}
					return
				}

				if second.TaskID != inspection.TaskID(secondID) {
					t.Fatalf("cross-task operation replayed the wrong task: first=%s second=%s response=%s", first.TaskID, secondID, second.TaskID)
				}
				if second.TaskID == first.TaskID {
					t.Fatalf("cross-task operation returned first task id %s", first.TaskID)
				}
				if second.Status != inspection.StatusOccupying {
					t.Fatalf("second task did not advance after independent split: %s", second.Status)
				}
				if len(second.BlindCodes) != 1 || second.BlindCodes[0] != "model-second-b1" {
					t.Fatalf("second task got wrong blind codes: %v", second.BlindCodes)
				}
				if secondView.Task.Status != inspection.StatusOccupying {
					t.Fatalf("second task persisted status = %s, want %s", secondView.Task.Status, inspection.StatusOccupying)
				}
				if len(secondView.Splits) != 3 {
					t.Fatalf("second task split record count = %d, want 3", len(secondView.Splits))
				}
				if len(secondView.BlindSamples) != 1 {
					t.Fatalf("second task blind sample count = %d, want 1", len(secondView.BlindSamples))
				}
				for _, split := range secondView.Splits {
					if split.TaskID != second.TaskID {
						t.Fatalf("second task split was written under task %s", split.TaskID)
					}
					if string(split.Code) != "model-second-b1" {
						t.Fatalf("second task split used wrong blind code %s", split.Code)
					}
				}

				firstRetry, derr := svc.SplitBlindSamples(firstID, api.SplitRequest{OperationID: "model-shared-split-op"})
				if derr != nil {
					t.Fatalf("first task retry after second task split: %v", derr)
				}
				firstViewAfterRetry, derr := svc.GetTask(firstID)
				if derr != nil {
					t.Fatalf("view first task after retry: %v", derr)
				}
				if firstRetry.TaskID != first.TaskID || firstRetry.Status != first.Status ||
					firstRetry.Generation != first.Generation || !reflect.DeepEqual(firstRetry.BlindCodes, first.BlindCodes) {
					t.Fatalf("first task retry changed after cross-task split: first=%+v retry=%+v", first, firstRetry)
				}
				if len(firstViewAfterRetry.Splits) != len(firstViewAfterSplit.Splits) {
					t.Fatalf("first task retry duplicated split records after cross-task split: before=%d after=%d", len(firstViewAfterSplit.Splits), len(firstViewAfterRetry.Splits))
				}
			},
		},
	}

	for _, backend := range backends {
		for _, tc := range cases {
			t.Run(backend.name+"/"+tc.name, func(t *testing.T) {
				tc.run(t, backend.new(t))
			})
		}
	}
}

package api_test

import (
	"path/filepath"
	"testing"

	"riceguard/api"
	"riceguard/catalog"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/pathogen"
	"riceguard/store"
)

func TestModel_SQLiteSeedLotReuseAfterTerminalTask(t *testing.T) {
	newSQLiteService := func(t *testing.T) *api.Service {
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
	}

	createReq := func(op, lot, blindCode string) api.CreateTaskRequest {
		return api.CreateTaskRequest{
			OperationID: op,
			SeedLot:     lot,
			Field:       "field-01",
			Variety:     "xiangliangyou-900",
			FemaleCert:  3,
			MaleCert:    3,
			BlindAllocs: []api.BlindAllocInput{
				{Code: blindCode, Germination: 100, Pathogen: 50, Moisture: 30},
			},
			Chamber:        "ch-1",
			ChamberStart:   100,
			ChamberEnd:     200,
			Plate:          "p-1",
			Wells:          []string{"w1"},
			ReviewerRoster: []string{"reviewer-f", "reviewer-g"},
		}
	}

	finishToReleasable := func(t *testing.T, svc *api.Service, id, lot, blindCode, op string) {
		t.Helper()

		for i, reviewer := range []string{"sampler-a", "sampler-b"} {
			if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
				OperationID: op + "-sample-" + string(rune('0'+i)),
				Reviewer:    reviewer,
				Field:       "field-01",
				SeedLot:     lot,
				BlindSeal:   "seal-1",
				SampleCount: 180,
			}); derr != nil {
				t.Fatalf("sampling %s: %v", reviewer, derr)
			}
		}
		if _, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: op + "-split"}); derr != nil {
			t.Fatalf("split: %v", derr)
		}
		if _, derr := svc.Occupy(id, api.OccupyRequest{OperationID: op + "-occupy"}); derr != nil {
			t.Fatalf("occupy: %v", derr)
		}
		for _, day := range []int32{2, 5, 8} {
			if _, derr := svc.RecordGermination(id, api.GerminationRequest{
				OperationID: op + "-germination-" + string(rune('0'+day)),
				BlindCode:   blindCode,
				DayAge:      day,
				Normal:      95,
				Abnormal:    3,
				Dead:        2,
				Collector:   "germinator-c",
			}); derr != nil {
				t.Fatalf("germination day %d: %v", day, derr)
			}
		}
		reading := int32(10)
		if _, derr := svc.RecordPathogen(id, api.PathogenRequest{
			OperationID: op + "-pathogen",
			BlindCode:   blindCode,
			Plate:       "p-1",
			Well:        "w1",
			Verifier:    "pathologist-d",
			Reading:     &reading,
		}); derr != nil {
			t.Fatalf("pathogen: %v", derr)
		}
		if _, derr := svc.RecordMoisture(id, api.MoistureRequest{
			OperationID:   op + "-moisture",
			Moisture:      "12.50",
			PurityGrains:  98,
			TotalGrains:   100,
			ThousandGrain: 25000,
			Collector:     "metrologist-e",
		}); derr != nil {
			t.Fatalf("moisture: %v", derr)
		}
		for i, reviewer := range []string{"reviewer-f", "reviewer-g"} {
			if _, derr := svc.Review(id, api.ReviewRequest{
				OperationID: op + "-review-" + string(rune('0'+i)),
				Reviewer:    reviewer,
				Conclusion:  "approve",
			}); derr != nil {
				t.Fatalf("review %s: %v", reviewer, derr)
			}
		}
	}

	cases := []struct {
		name       string
		wantStatus inspection.TaskStatus
		terminate  func(*testing.T, *api.Service, string, string, string, string)
	}{
		{
			name:       "cancelled",
			wantStatus: inspection.StatusCancelled,
			terminate: func(t *testing.T, svc *api.Service, id, _, _, op string) {
				t.Helper()

				resp, derr := svc.Cancel(id, api.CancelRequest{OperationID: op + "-cancel", Reason: "operator withdrew task"})
				if derr != nil {
					t.Fatalf("cancel: %v", derr)
				}
				if resp.Status != inspection.StatusCancelled {
					t.Fatalf("expected cancelled, got %s", resp.Status)
				}
			},
		},
		{
			name:       "quarantined",
			wantStatus: inspection.StatusQuarantined,
			terminate: func(t *testing.T, svc *api.Service, id, lot, blindCode, op string) {
				t.Helper()

				finishToReleasable(t, svc, id, lot, blindCode, op)
				resp, derr := svc.Finalize(id, api.FinalizeRequest{OperationID: op + "-finalize", Outcome: "quarantined"})
				if derr != nil {
					t.Fatalf("finalize quarantine: %v", derr)
				}
				if resp.Status != inspection.StatusQuarantined {
					t.Fatalf("expected quarantined, got %s", resp.Status)
				}
			},
		},
		{
			name:       "released",
			wantStatus: inspection.StatusReleased,
			terminate: func(t *testing.T, svc *api.Service, id, lot, blindCode, op string) {
				t.Helper()

				finishToReleasable(t, svc, id, lot, blindCode, op)
				resp, derr := svc.Finalize(id, api.FinalizeRequest{OperationID: op + "-finalize"})
				if derr != nil {
					t.Fatalf("finalize release: %v", derr)
				}
				if resp.Status != inspection.StatusReleased {
					t.Fatalf("expected released, got %s", resp.Status)
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			svc := newSQLiteService(t)
			lot := "lot-sqlite-reuse-" + tc.name

			firstReq := createReq(tc.name+"-create", lot, tc.name+"-blind-1")
			first, derr := svc.CreateTask(firstReq)
			if derr != nil {
				t.Fatalf("create first task: %v", derr)
			}

			tc.terminate(t, svc, first.TaskID, lot, tc.name+"-blind-1", tc.name)

			replacementReq := createReq(tc.name+"-recreate", lot, tc.name+"-blind-2")
			replacement, derr := svc.CreateTask(replacementReq)
			if derr != nil {
				t.Fatalf("terminal %s task should not block seed lot reuse: %v", tc.wantStatus, derr)
			}
			if replacement.TaskID == first.TaskID {
				t.Fatalf("seed lot reuse must create a fresh task, got %s twice", replacement.TaskID)
			}

			retry, derr := svc.CreateTask(replacementReq)
			if derr != nil {
				t.Fatalf("idempotent recreate retry: %v", derr)
			}
			if retry.TaskID != replacement.TaskID {
				t.Fatalf("idempotent retry returned %s, want %s", retry.TaskID, replacement.TaskID)
			}

			conflictReq := createReq(tc.name+"-recreate", lot+"-other", tc.name+"-blind-3")
			if _, derr := svc.CreateTask(conflictReq); derr == nil {
				t.Fatal("expected duplicate operation_id with different content to conflict")
			} else if derr.Code != domain.CodeIdempotencyConflict {
				t.Fatalf("expected %s, got %s", domain.CodeIdempotencyConflict, derr.Code)
			}

			openReq := createReq(tc.name+"-second-open", lot, tc.name+"-blind-4")
			if _, derr := svc.CreateTask(openReq); derr == nil {
				t.Fatal("expected open replacement task to keep seed lot occupied")
			}

			tasks, derr := svc.ListTasks()
			if derr != nil {
				t.Fatalf("list tasks: %v", derr)
			}
			if len(tasks) != 2 {
				t.Fatalf("expected terminal history plus replacement task, got %d tasks", len(tasks))
			}
			if tasks[0].SeedLot != lot || tasks[0].Status != tc.wantStatus {
				t.Fatalf("expected first history task %q at %s, got %q at %s", lot, tc.wantStatus, tasks[0].SeedLot, tasks[0].Status)
			}
			if tasks[1].SeedLot != lot || tasks[1].Status != inspection.StatusPendingSampling {
				t.Fatalf("expected replacement task %q at %s, got %q at %s", lot, inspection.StatusPendingSampling, tasks[1].SeedLot, tasks[1].Status)
			}
		})
	}
}

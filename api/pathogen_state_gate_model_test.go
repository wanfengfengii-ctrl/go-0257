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

func TestModel_PathogenReadRejectedBeforePathogenStagePreservesRetries(t *testing.T) {
	confirmSampling := func(t *testing.T, svc *api.Service, id, op, reviewer string) {
		t.Helper()
		_, derr := svc.ConfirmSampling(id, api.SamplingRequest{
			OperationID: op, Reviewer: reviewer, Field: "field-01",
			SeedLot: "lot-1001", BlindSeal: "seal-1", SampleCount: 180,
		})
		if derr != nil {
			t.Fatalf("confirm sampling %s: %v", reviewer, derr)
		}
	}
	completeSplitAndOccupy := func(t *testing.T, svc *api.Service, id, op string) {
		t.Helper()
		if _, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: op + "-split"}); derr != nil {
			t.Fatalf("split: %v", derr)
		}
		if _, derr := svc.Occupy(id, api.OccupyRequest{OperationID: op + "-occupy"}); derr != nil {
			t.Fatalf("occupy: %v", derr)
		}
	}
	completeGermination := func(t *testing.T, svc *api.Service, id, op string) {
		t.Helper()
		for _, day := range []struct {
			age    int32
			suffix string
		}{{2, "2"}, {5, "5"}, {8, "8"}} {
			_, derr := svc.RecordGermination(id, api.GerminationRequest{
				OperationID: op + "-g" + day.suffix, BlindCode: "b1",
				DayAge: day.age, Normal: 95, Abnormal: 3, Dead: 2,
				Collector: "germinator-c",
			})
			if derr != nil {
				t.Fatalf("germination day %d: %v", day.age, derr)
			}
		}
	}

	cases := []struct {
		name            string
		wantStatus      inspection.TaskStatus
		arrange         func(*testing.T, *api.Service, string, string)
		advancePathogen func(*testing.T, *api.Service, string, string)
	}{
		{
			name:       "new_task_without_sampling_confirmation",
			wantStatus: inspection.StatusPendingSampling,
			arrange:    func(*testing.T, *api.Service, string, string) {},
			advancePathogen: func(t *testing.T, svc *api.Service, id, op string) {
				confirmSampling(t, svc, id, op+"-s1", "sampler-a")
				confirmSampling(t, svc, id, op+"-s2", "sampler-b")
				completeSplitAndOccupy(t, svc, id, op)
				completeGermination(t, svc, id, op)
			},
		},
		{
			name:       "one_sampling_confirmation_still_pending",
			wantStatus: inspection.StatusPendingSampling,
			arrange: func(t *testing.T, svc *api.Service, id, op string) {
				confirmSampling(t, svc, id, op+"-s1", "sampler-a")
			},
			advancePathogen: func(t *testing.T, svc *api.Service, id, op string) {
				confirmSampling(t, svc, id, op+"-s2", "sampler-b")
				completeSplitAndOccupy(t, svc, id, op)
				completeGermination(t, svc, id, op)
			},
		},
		{
			name:       "sampling_confirmed_but_not_split",
			wantStatus: inspection.StatusBlindSplit,
			arrange: func(t *testing.T, svc *api.Service, id, op string) {
				confirmSampling(t, svc, id, op+"-s1", "sampler-a")
				confirmSampling(t, svc, id, op+"-s2", "sampler-b")
			},
			advancePathogen: func(t *testing.T, svc *api.Service, id, op string) {
				completeSplitAndOccupy(t, svc, id, op)
				completeGermination(t, svc, id, op)
			},
		},
		{
			name:       "germination_not_complete",
			wantStatus: inspection.StatusGerminating,
			arrange: func(t *testing.T, svc *api.Service, id, op string) {
				confirmSampling(t, svc, id, op+"-s1", "sampler-a")
				confirmSampling(t, svc, id, op+"-s2", "sampler-b")
				completeSplitAndOccupy(t, svc, id, op)
			},
			advancePathogen: func(t *testing.T, svc *api.Service, id, op string) {
				completeGermination(t, svc, id, op)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, roles := catalog.Seed()
			st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "riceguard.db"))
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			defer st.Close()
			amp := pathogen.NewScriptedAmplifier()
			amp.AddFault("p-1", "w1", pathogen.DeviceRefused)
			amp.AddFault("p-1", "w1", pathogen.DeviceDisconnect)
			amp.AddFault("p-1", "w1", pathogen.DeviceTimeout)
			svc := api.NewService(c, roles, st, amp, measure.NewScriptedMeter())
			op := "model-" + tc.name

			created, derr := svc.CreateTask(validCreate(op + "-create"))
			if derr != nil {
				t.Fatalf("create: %v", derr)
			}
			id := created.TaskID
			tc.arrange(t, svc, id, op+"-arrange")

			before, derr := svc.GetTask(id)
			if derr != nil {
				t.Fatalf("get before: %v", derr)
			}
			if before.Task.Status != tc.wantStatus {
				t.Fatalf("expected setup status %s, got %s", tc.wantStatus, before.Task.Status)
			}

			_, derr = svc.RecordPathogen(id, api.PathogenRequest{
				OperationID: op + "-early-pathogen", BlindCode: "b1",
				Plate: "p-1", Well: "w1", Verifier: "pathologist-d",
				Contaminated: true,
			})
			if derr == nil || derr.Code != domain.CodeBadRequest {
				t.Fatalf("pathogen read before pathogen_checking must be state-rejected, got %v", derr)
			}

			after, derr := svc.GetTask(id)
			if derr != nil {
				t.Fatalf("get after: %v", derr)
			}
			if after.Task.Status != before.Task.Status {
				t.Fatalf("early rejection changed status from %s to %s", before.Task.Status, after.Task.Status)
			}
			if len(after.Pathogen) != 0 {
				t.Fatalf("early rejection persisted pathogen evidence: %d", len(after.Pathogen))
			}
			attempts, err := st.ListAttempts(inspection.TaskID(id))
			if err != nil {
				t.Fatalf("list attempts: %v", err)
			}
			if len(attempts) != 0 {
				t.Fatalf("early rejection persisted %d instrument attempts", len(attempts))
			}
			for _, event := range after.Audit {
				if event.Action == "pathogen_retryable_attempt" || event.Code == domain.CodeDeviceRetryable {
					t.Fatalf("early rejection appended retry audit event: action=%s code=%s", event.Action, event.Code)
				}
			}

			tc.advancePathogen(t, svc, id, op+"-advance")
			_, derr = svc.RecordPathogen(id, api.PathogenRequest{
				OperationID: op + "-legal-pathogen", BlindCode: "b1",
				Plate: "p-1", Well: "w1", Verifier: "pathologist-d",
				Contaminated: true,
			})
			if derr == nil || derr.Code != domain.CodeDeviceRetryable {
				t.Fatalf("expected legal pathogen call to consume preserved retry script, got %v", derr)
			}
			attempts, err = st.ListAttempts(inspection.TaskID(id))
			if err != nil {
				t.Fatalf("list attempts after legal pathogen call: %v", err)
			}
			if len(attempts) != 3 {
				t.Fatalf("expected retry script to remain untouched until pathogen_checking, got %d attempts", len(attempts))
			}
		})
	}
}

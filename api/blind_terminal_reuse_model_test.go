package api_test

import (
	"testing"

	"riceguard/api"
	"riceguard/catalog"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/pathogen"
	"riceguard/store"
)

func TestModel_BlindCodeReuseAfterTerminalTask(t *testing.T) {
	newService := func() *api.Service {
		c, roles := catalog.Seed()
		return api.NewService(c, roles, store.NewMemory(), pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())
	}
	createRequest := func(op, lot, code string) api.CreateTaskRequest {
		return api.CreateTaskRequest{
			OperationID: op,
			SeedLot:     lot,
			Field:       "field-01",
			Variety:     "xiangliangyou-900",
			FemaleCert:  3,
			MaleCert:    3,
			BlindAllocs: []api.BlindAllocInput{
				{Code: code, Germination: 100, Pathogen: 50, Moisture: 30},
			},
			Chamber:        "ch-1",
			ChamberStart:   100,
			ChamberEnd:     200,
			Plate:          "p-1",
			Wells:          []string{"w1"},
			ReviewerRoster: []string{"reviewer-f", "reviewer-g"},
		}
	}
	createTask := func(t *testing.T, svc *api.Service, op, lot, code string) string {
		t.Helper()
		resp, derr := svc.CreateTask(createRequest(op, lot, code))
		if derr != nil {
			t.Fatalf("create %s: %v", op, derr)
		}
		return resp.TaskID
	}
	samplingToSplit := func(t *testing.T, svc *api.Service, id, op, lot string) {
		t.Helper()
		for i, reviewer := range []string{"sampler-a", "sampler-b"} {
			if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
				OperationID: op + "-sample-" + string(rune('1'+i)),
				Reviewer:    reviewer,
				Field:       "field-01",
				SeedLot:     lot,
				BlindSeal:   "seal-1",
				SampleCount: 180,
			}); derr != nil {
				t.Fatalf("confirm sampling %s: %v", reviewer, derr)
			}
		}
		if _, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: op + "-split"}); derr != nil {
			t.Fatalf("split blind samples: %v", derr)
		}
	}
	driveToReleasable := func(t *testing.T, svc *api.Service, id, op, lot, code string, contaminated bool) {
		t.Helper()
		samplingToSplit(t, svc, id, op, lot)
		if _, derr := svc.Occupy(id, api.OccupyRequest{OperationID: op + "-occupy"}); derr != nil {
			t.Fatalf("occupy: %v", derr)
		}
		for _, dayAge := range []int32{2, 5, 8} {
			if _, derr := svc.RecordGermination(id, api.GerminationRequest{
				OperationID: op + "-germination-" + string(rune('0'+dayAge)),
				BlindCode:   code,
				DayAge:      dayAge,
				Normal:      95,
				Abnormal:    3,
				Dead:        2,
				Collector:   "germinator-c",
			}); derr != nil {
				t.Fatalf("record germination day %d: %v", dayAge, derr)
			}
		}
		reading := int32(10)
		if _, derr := svc.RecordPathogen(id, api.PathogenRequest{
			OperationID:  op + "-pathogen",
			BlindCode:    code,
			Plate:        "p-1",
			Well:         "w1",
			Verifier:     "pathologist-d",
			Reading:      &reading,
			Contaminated: contaminated,
		}); derr != nil {
			t.Fatalf("record pathogen: %v", derr)
		}
		if _, derr := svc.RecordMoisture(id, api.MoistureRequest{
			OperationID:   op + "-moisture",
			Moisture:      "12.50",
			PurityGrains:  98,
			TotalGrains:   100,
			ThousandGrain: 25000,
			Collector:     "metrologist-e",
		}); derr != nil {
			t.Fatalf("record moisture: %v", derr)
		}
		for i, reviewer := range []string{"reviewer-f", "reviewer-g"} {
			if _, derr := svc.Review(id, api.ReviewRequest{
				OperationID: op + "-review-" + string(rune('1'+i)),
				Reviewer:    reviewer,
				Conclusion:  "approve",
			}); derr != nil {
				t.Fatalf("review %s: %v", reviewer, derr)
			}
		}
	}
	requireQueryableTerminalHistory := func(t *testing.T, svc *api.Service, id, code string, status inspection.TaskStatus) {
		t.Helper()
		view, derr := svc.GetTask(id)
		if derr != nil {
			t.Fatalf("get terminal task: %v", derr)
		}
		if view.Task.Status != status {
			t.Fatalf("expected terminal status %s, got %s", status, view.Task.Status)
		}
		if len(view.Task.BlindAllocs) != 1 || view.Task.BlindAllocs[0].Code != code {
			t.Fatalf("terminal task detail lost blind allocation for %s: %#v", code, view.Task.BlindAllocs)
		}
		audit, derr := svc.ListAllAudit()
		if derr != nil {
			t.Fatalf("list audit: %v", derr)
		}
		foundTerminalAudit := false
		for _, event := range audit {
			if event.TaskStatus == status {
				foundTerminalAudit = true
				break
			}
		}
		if !foundTerminalAudit {
			t.Fatalf("expected terminal status %s to remain queryable in audit", status)
		}
	}

	cases := []struct {
		name          string
		arrange       func(t *testing.T, svc *api.Service) (string, string, inspection.TaskStatus)
		duplicateBody bool
		wantErr       domain.ErrorCode
	}{
		{
			name: "cancelled history frees blind code",
			arrange: func(t *testing.T, svc *api.Service) (string, string, inspection.TaskStatus) {
				code := "terminal-cancel-blind"
				id := createTask(t, svc, "cancel-old-create", "lot-cancel-old", code)
				cancelled, derr := svc.Cancel(id, api.CancelRequest{OperationID: "cancel-old-final", Reason: "operator cancelled"})
				if derr != nil {
					t.Fatalf("cancel task: %v", derr)
				}
				return code, string(cancelled.TaskID), inspection.StatusCancelled
			},
		},
		{
			name: "released history frees blind code",
			arrange: func(t *testing.T, svc *api.Service) (string, string, inspection.TaskStatus) {
				code := "terminal-release-blind"
				id := createTask(t, svc, "release-old-create", "lot-release-old", code)
				driveToReleasable(t, svc, id, "release-old", "lot-release-old", code, false)
				finalized, derr := svc.Finalize(id, api.FinalizeRequest{OperationID: "release-old-final"})
				if derr != nil {
					t.Fatalf("finalize release: %v", derr)
				}
				return code, string(finalized.TaskID), inspection.StatusReleased
			},
		},
		{
			name: "quarantined history frees blind code",
			arrange: func(t *testing.T, svc *api.Service) (string, string, inspection.TaskStatus) {
				code := "terminal-quarantine-blind"
				id := createTask(t, svc, "quarantine-old-create", "lot-quarantine-old", code)
				driveToReleasable(t, svc, id, "quarantine-old", "lot-quarantine-old", code, true)
				finalized, derr := svc.Finalize(id, api.FinalizeRequest{OperationID: "quarantine-old-final"})
				if derr != nil {
					t.Fatalf("finalize quarantine: %v", derr)
				}
				return code, string(finalized.TaskID), inspection.StatusQuarantined
			},
		},
		{
			name: "open task still blocks blind code",
			arrange: func(t *testing.T, svc *api.Service) (string, string, inspection.TaskStatus) {
				code := "open-blind"
				id := createTask(t, svc, "open-old-create", "lot-open-old", code)
				return code, id, ""
			},
			wantErr: domain.CodeBlindDuplicate,
		},
		{
			name: "request body duplicate blind code remains rejected",
			arrange: func(t *testing.T, svc *api.Service) (string, string, inspection.TaskStatus) {
				return "body-duplicate-blind", "", ""
			},
			duplicateBody: true,
			wantErr:       domain.CodeBlindDuplicate,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newService()
			code, historyID, terminalStatus := tc.arrange(t, svc)
			if terminalStatus.IsTerminal() {
				requireQueryableTerminalHistory(t, svc, historyID, code, terminalStatus)
			}

			req := createRequest("reuse-create", "lot-reuse", code)
			if tc.duplicateBody {
				req.BlindAllocs = append(req.BlindAllocs, api.BlindAllocInput{
					Code: code, Germination: 100, Pathogen: 50, Moisture: 30,
				})
			}
			resp, derr := svc.CreateTask(req)
			if tc.wantErr != "" {
				if derr == nil {
					t.Fatalf("expected %s, got success for task %s", tc.wantErr, resp.TaskID)
				}
				if derr.Code != tc.wantErr {
					t.Fatalf("expected %s, got %s", tc.wantErr, derr.Code)
				}
				return
			}
			if derr != nil {
				t.Fatalf("expected reused blind code %s to create a new task: %v", code, derr)
			}
			if resp.TaskID == "" {
				t.Fatal("expected created task id for reused blind code")
			}
		})
	}
}

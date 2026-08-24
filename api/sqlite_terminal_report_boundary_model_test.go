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
	"riceguard/review"
	"riceguard/store"
)

func TestModel_SQLiteTerminalReportBoundarySurvivesRestart(t *testing.T) {
	cases := []struct {
		name           string
		contaminated   bool
		finalize       api.FinalizeRequest
		wantStatus     inspection.TaskStatus
		wantOutcome    string
		wantReason     string
		wantCredential bool
		openOnly       bool
	}{
		{
			name:           "released report keeps credential",
			finalize:       api.FinalizeRequest{OperationID: "model-release-final"},
			wantStatus:     inspection.StatusReleased,
			wantOutcome:    "released",
			wantCredential: true,
		},
		{
			name:         "contaminated report stays quarantined",
			contaminated: true,
			finalize:     api.FinalizeRequest{OperationID: "model-quarantine-final"},
			wantStatus:   inspection.StatusQuarantined,
			wantOutcome:  "quarantined",
			wantReason:   "pathogen_contamination",
		},
		{
			name:        "cancelled report stays cancelled",
			finalize:    api.FinalizeRequest{OperationID: "model-cancel-final", Outcome: "cancelled", Reason: "operator_abort"},
			wantStatus:  inspection.StatusCancelled,
			wantOutcome: "cancelled",
			wantReason:  "cancelled",
		},
		{
			name:     "open task report remains unavailable",
			openOnly: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "riceguard.sqlite")
			svc, st := modelSQLiteService(t, dbPath)

			if tc.openOnly {
				resp, derr := svc.CreateTask(modelCreateTaskRequest("model-open-create"))
				if derr != nil {
					t.Fatalf("create open task: %v", derr)
				}
				modelRequireReportRejected(t, svc, resp.TaskID)
				modelCloseSQLite(t, st)

				svc, st = modelSQLiteService(t, dbPath)
				defer st.Close()
				modelRequireReportRejected(t, svc, resp.TaskID)
				return
			}

			id := modelDriveToTerminalReady(t, svc, "model-"+tc.wantOutcome, tc.contaminated)
			final, derr := svc.Finalize(id, tc.finalize)
			if derr != nil {
				t.Fatalf("finalize: %v", derr)
			}
			modelAssertTerminalBoundary(t, svc, id, tc.wantStatus, tc.wantOutcome, tc.wantReason, tc.wantCredential, final)
			modelCloseSQLite(t, st)

			svc, st = modelSQLiteService(t, dbPath)
			defer st.Close()
			modelAssertTerminalBoundary(t, svc, id, tc.wantStatus, tc.wantOutcome, tc.wantReason, tc.wantCredential, final)
		})
	}
}

func modelSQLiteService(t *testing.T, dbPath string) (*api.Service, *store.SQLite) {
	t.Helper()
	c, roles := catalog.Seed()
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter()), st
}

func modelCreateTaskRequest(operationID string) api.CreateTaskRequest {
	return api.CreateTaskRequest{
		OperationID: operationID,
		SeedLot:     "lot-1001",
		Field:       "field-01",
		Variety:     "xiangliangyou-900",
		FemaleCert:  3,
		MaleCert:    3,
		BlindAllocs: []api.BlindAllocInput{
			{Code: "b1", Germination: 100, Pathogen: 50, Moisture: 30},
		},
		Chamber:        "ch-1",
		ChamberStart:   100,
		ChamberEnd:     200,
		Plate:          "p-1",
		Wells:          []string{"w1"},
		ReviewerRoster: []string{"reviewer-f", "reviewer-g"},
	}
}

func modelDriveToTerminalReady(t *testing.T, svc *api.Service, op string, contaminated bool) string {
	t.Helper()
	resp, derr := svc.CreateTask(modelCreateTaskRequest(op + "-create"))
	if derr != nil {
		t.Fatalf("create task: %v", derr)
	}
	id := resp.TaskID

	for i, reviewer := range []string{"sampler-a", "sampler-b"} {
		_, derr := svc.ConfirmSampling(id, api.SamplingRequest{
			OperationID: op + "-sampling-" + string(rune('0'+i)),
			Reviewer:    reviewer,
			Field:       "field-01",
			SeedLot:     "lot-1001",
			BlindSeal:   "seal-1",
			SampleCount: 180,
		})
		if derr != nil {
			t.Fatalf("sampling %d: %v", i+1, derr)
		}
	}
	if _, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: op + "-split"}); derr != nil {
		t.Fatalf("split: %v", derr)
	}
	if _, derr := svc.Occupy(id, api.OccupyRequest{OperationID: op + "-occupy"}); derr != nil {
		t.Fatalf("occupy: %v", derr)
	}
	for _, dayAge := range []int32{2, 5, 8} {
		_, derr := svc.RecordGermination(id, api.GerminationRequest{
			OperationID: op + "-germination-" + string(rune('0'+dayAge)),
			BlindCode:   "b1",
			DayAge:      dayAge,
			Normal:      95,
			Abnormal:    3,
			Dead:        2,
			Collector:   "germinator-c",
		})
		if derr != nil {
			t.Fatalf("germination day %d: %v", dayAge, derr)
		}
	}
	reading := int32(10)
	if _, derr := svc.RecordPathogen(id, api.PathogenRequest{
		OperationID:  op + "-pathogen",
		BlindCode:    "b1",
		Plate:        "p-1",
		Well:         "w1",
		Verifier:     "pathologist-d",
		Reading:      &reading,
		Contaminated: contaminated,
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
		_, derr := svc.Review(id, api.ReviewRequest{
			OperationID: op + "-review-" + string(rune('0'+i)),
			Reviewer:    reviewer,
			Conclusion:  "approve",
		})
		if derr != nil {
			t.Fatalf("review %d: %v", i+1, derr)
		}
	}
	return id
}

func modelAssertTerminalBoundary(t *testing.T, svc *api.Service, id string, wantStatus inspection.TaskStatus, wantOutcome, wantReason string, wantCredential bool, final api.FinalizeResponse) {
	t.Helper()
	if final.Status != wantStatus {
		t.Fatalf("finalize status = %s, want %s", final.Status, wantStatus)
	}
	if final.Outcome != wantOutcome {
		t.Fatalf("finalize outcome = %q, want %q", final.Outcome, wantOutcome)
	}
	if final.Version == 0 {
		t.Fatal("finalize version was not assigned")
	}

	view, derr := svc.GetTask(id)
	if derr != nil {
		t.Fatalf("get task: %v", derr)
	}
	if view.Task.Status != wantStatus {
		t.Fatalf("task status = %s, want %s", view.Task.Status, wantStatus)
	}
	if view.Task.TerminalVersion != final.Version {
		t.Fatalf("task terminal version = %d, want %d", view.Task.TerminalVersion, final.Version)
	}
	if view.Task.TerminalOutcome != wantOutcome {
		t.Fatalf("task terminal outcome = %q, want %q", view.Task.TerminalOutcome, wantOutcome)
	}
	modelRequireTerminalReview(t, view, review.FinalOutcome(wantOutcome), final.Version)
	modelRequireFinalizeAudit(t, svc, wantStatus)

	report, derr := svc.GenerateReport(id)
	if derr != nil {
		t.Fatalf("generate report: %v", derr)
	}
	if report.Status != wantStatus {
		t.Fatalf("report status = %s, want %s", report.Status, wantStatus)
	}
	if report.Outcome != wantOutcome {
		t.Fatalf("report outcome = %q, want %q", report.Outcome, wantOutcome)
	}
	if report.Reason != wantReason {
		t.Fatalf("report reason = %q, want %q", report.Reason, wantReason)
	}
	if wantCredential {
		if final.Credential == "" {
			t.Fatal("finalize response missing release credential")
		}
		if view.Credential == nil {
			t.Fatal("task view missing release credential")
		}
		if view.Credential.Version != final.Version {
			t.Fatalf("credential version = %d, want %d", view.Credential.Version, final.Version)
		}
		if view.Credential.Credential != final.Credential {
			t.Fatalf("view credential = %q, want %q", view.Credential.Credential, final.Credential)
		}
		if report.Credential != final.Credential {
			t.Fatalf("report credential = %q, want %q", report.Credential, final.Credential)
		}
		return
	}
	if final.Credential != "" {
		t.Fatalf("unexpected finalize credential %q", final.Credential)
	}
	if view.Credential != nil {
		t.Fatalf("unexpected view credential %q", view.Credential.Credential)
	}
	if report.Credential != "" {
		t.Fatalf("unexpected report credential %q", report.Credential)
	}
}

func modelRequireTerminalReview(t *testing.T, view api.TaskView, outcome review.FinalOutcome, version int64) {
	t.Helper()
	for _, r := range view.Reviews {
		if r.Outcome == outcome && r.TerminalVersion == version {
			return
		}
	}
	t.Fatalf("missing terminal review outcome %q at version %d", outcome, version)
}

func modelRequireFinalizeAudit(t *testing.T, svc *api.Service, status inspection.TaskStatus) {
	t.Helper()
	events, derr := svc.ListAllAudit()
	if derr != nil {
		t.Fatalf("list audit: %v", derr)
	}
	for _, event := range events {
		if event.Action == "finalize" && event.TaskStatus == status {
			return
		}
	}
	t.Fatalf("missing finalize audit event for status %s", status)
}

func modelRequireReportRejected(t *testing.T, svc *api.Service, id string) {
	t.Helper()
	if _, derr := svc.GenerateReport(id); derr == nil || derr.Code != domain.CodeBadRequest {
		t.Fatalf("open task report error = %v, want %s", derr, domain.CodeBadRequest)
	}
}

func modelCloseSQLite(t *testing.T, st *store.SQLite) {
	t.Helper()
	if err := st.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
}

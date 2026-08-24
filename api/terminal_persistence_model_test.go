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

func TestModel_SQLiteTerminalOutcomesPersistIntoReports(t *testing.T) {
	newService := func(t *testing.T, dbPath string) (*api.Service, *store.SQLite) {
		t.Helper()
		c, roles := catalog.Seed()
		st, err := store.OpenSQLite(dbPath)
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		return api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter()), st
	}

	createRequest := func(op, seedLot string) api.CreateTaskRequest {
		return api.CreateTaskRequest{
			OperationID: op,
			SeedLot:     seedLot,
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

	driveToReleasable := func(t *testing.T, svc *api.Service, op, seedLot string, contaminated bool) string {
		t.Helper()
		resp, derr := svc.CreateTask(createRequest(op+"-create", seedLot))
		if derr != nil {
			t.Fatalf("create: %v", derr)
		}
		id := resp.TaskID

		for i, reviewer := range []string{"sampler-a", "sampler-b"} {
			_, derr := svc.ConfirmSampling(id, api.SamplingRequest{
				OperationID: op + "-sampling-" + string(rune('0'+i)),
				Reviewer:    reviewer,
				Field:       "field-01",
				SeedLot:     seedLot,
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
		for _, day := range []int32{2, 5, 8} {
			_, derr := svc.RecordGermination(id, api.GerminationRequest{
				OperationID: op + "-germ-" + string(rune('0'+day)),
				BlindCode:   "b1",
				DayAge:      day,
				Normal:      95,
				Abnormal:    3,
				Dead:        2,
				Collector:   "germinator-c",
			})
			if derr != nil {
				t.Fatalf("germination day %d: %v", day, derr)
			}
		}
		reading := int32(10)
		_, derr = svc.RecordPathogen(id, api.PathogenRequest{
			OperationID:  op + "-pathogen",
			BlindCode:    "b1",
			Plate:        "p-1",
			Well:         "w1",
			Verifier:     "pathologist-d",
			Reading:      &reading,
			Contaminated: contaminated,
		})
		if derr != nil {
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

	assertTerminalPersisted := func(t *testing.T, svc *api.Service, id string, status inspection.TaskStatus, outcome, reason string, wantCredential bool, final api.FinalizeResponse) {
		t.Helper()
		view, derr := svc.GetTask(id)
		if derr != nil {
			t.Fatalf("get task: %v", derr)
		}
		if view.Task.Status != status {
			t.Fatalf("task status = %s, want %s", view.Task.Status, status)
		}
		if view.Task.TerminalOutcome != outcome {
			t.Fatalf("task terminal outcome = %q, want %q", view.Task.TerminalOutcome, outcome)
		}
		if view.Task.TerminalVersion != final.Version {
			t.Fatalf("task terminal version = %d, want %d", view.Task.TerminalVersion, final.Version)
		}

		foundTerminalReview := false
		for _, r := range view.Reviews {
			if r.Outcome == review.FinalOutcome(outcome) && r.TerminalVersion == final.Version {
				foundTerminalReview = true
			}
		}
		if !foundTerminalReview {
			t.Fatalf("missing terminal review for outcome %q version %d", outcome, final.Version)
		}

		auditTrail, derr := svc.ListAllAudit()
		if derr != nil {
			t.Fatalf("list audit: %v", derr)
		}
		foundAudit := false
		for _, ev := range auditTrail {
			if ev.Action == "finalize" && ev.TaskStatus == status {
				foundAudit = true
			}
		}
		if !foundAudit {
			t.Fatalf("missing finalize audit event for status %s", status)
		}

		report, derr := svc.GenerateReport(id)
		if derr != nil {
			t.Fatalf("generate report: %v", derr)
		}
		if report.Status != status {
			t.Fatalf("report status = %s, want %s", report.Status, status)
		}
		if report.Outcome != outcome {
			t.Fatalf("report outcome = %q, want %q", report.Outcome, outcome)
		}
		if report.Reason != reason {
			t.Fatalf("report reason = %q, want %q", report.Reason, reason)
		}
		if wantCredential {
			if final.Credential == "" {
				t.Fatal("finalize response missing credential")
			}
			if view.Credential == nil {
				t.Fatal("task view missing credential")
			}
			if report.Credential != final.Credential {
				t.Fatalf("report credential = %q, want finalize credential %q", report.Credential, final.Credential)
			}
			if view.Credential.Credential != final.Credential {
				t.Fatalf("view credential = %q, want finalize credential %q", view.Credential.Credential, final.Credential)
			}
		} else {
			if final.Credential != "" {
				t.Fatalf("unexpected finalize credential %q", final.Credential)
			}
			if report.Credential != "" {
				t.Fatalf("unexpected report credential %q", report.Credential)
			}
			if view.Credential != nil {
				t.Fatalf("unexpected view credential %q", view.Credential.Credential)
			}
		}
	}

	cases := []struct {
		name           string
		seedLot        string
		contaminated   bool
		finalize       api.FinalizeRequest
		wantStatus     inspection.TaskStatus
		wantOutcome    string
		wantReason     string
		wantCredential bool
		openOnly       bool
	}{
		{
			name:           "released",
			seedLot:        "lot-1001",
			finalize:       api.FinalizeRequest{OperationID: "released-final"},
			wantStatus:     inspection.StatusReleased,
			wantOutcome:    "released",
			wantCredential: true,
		},
		{
			name:         "quarantined",
			seedLot:      "lot-1001",
			contaminated: true,
			finalize:     api.FinalizeRequest{OperationID: "quarantined-final"},
			wantStatus:   inspection.StatusQuarantined,
			wantOutcome:  "quarantined",
			wantReason:   "pathogen_contamination",
		},
		{
			name:        "cancelled",
			seedLot:     "lot-1001",
			finalize:    api.FinalizeRequest{OperationID: "cancelled-final", Outcome: "cancelled", Reason: "operator_abort"},
			wantStatus:  inspection.StatusCancelled,
			wantOutcome: "cancelled",
			wantReason:  "cancelled",
		},
		{
			name:     "open report rejected",
			seedLot:  "lot-1001",
			openOnly: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "riceguard.db")
			svc, st := newService(t, dbPath)

			if tc.openOnly {
				resp, derr := svc.CreateTask(createRequest(tc.name+"-create", tc.seedLot))
				if derr != nil {
					t.Fatalf("create open task: %v", derr)
				}
				if _, derr := svc.GenerateReport(resp.TaskID); derr == nil || derr.Code != domain.CodeBadRequest {
					t.Fatalf("open task report error = %v, want %s", derr, domain.CodeBadRequest)
				}
				if err := st.Close(); err != nil {
					t.Fatalf("close sqlite: %v", err)
				}
				svc2, st2 := newService(t, dbPath)
				defer st2.Close()
				if _, derr := svc2.GenerateReport(resp.TaskID); derr == nil || derr.Code != domain.CodeBadRequest {
					t.Fatalf("reopened open task report error = %v, want %s", derr, domain.CodeBadRequest)
				}
				return
			}

			id := driveToReleasable(t, svc, tc.name, tc.seedLot, tc.contaminated)
			final, derr := svc.Finalize(id, tc.finalize)
			if derr != nil {
				t.Fatalf("finalize: %v", derr)
			}
			if final.Status != tc.wantStatus {
				t.Fatalf("finalize status = %s, want %s", final.Status, tc.wantStatus)
			}
			if final.Outcome != tc.wantOutcome {
				t.Fatalf("finalize outcome = %q, want %q", final.Outcome, tc.wantOutcome)
			}

			assertTerminalPersisted(t, svc, id, tc.wantStatus, tc.wantOutcome, tc.wantReason, tc.wantCredential, final)
			if err := st.Close(); err != nil {
				t.Fatalf("close sqlite: %v", err)
			}

			svc2, st2 := newService(t, dbPath)
			defer st2.Close()
			assertTerminalPersisted(t, svc2, id, tc.wantStatus, tc.wantOutcome, tc.wantReason, tc.wantCredential, final)
		})
	}
}

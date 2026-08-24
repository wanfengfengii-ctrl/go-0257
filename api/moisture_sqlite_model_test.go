package api_test

import (
	"fmt"
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

func TestModel_SQLiteMoistureEvidenceThresholdIsTerminalGate(t *testing.T) {
	cases := []struct {
		name             string
		moisture         string
		purityGrains     int64
		totalGrains      int64
		wantMoisturePass bool
		wantPurityPass   bool
		wantEvidencePass bool
		wantRelease      bool
	}{
		{
			name:             "boundary_13_00_releases",
			moisture:         "13.00",
			purityGrains:     98,
			totalGrains:      100,
			wantMoisturePass: true,
			wantPurityPass:   true,
			wantEvidencePass: true,
			wantRelease:      true,
		},
		{
			name:             "moisture_13_01_blocks",
			moisture:         "13.01",
			purityGrains:     98,
			totalGrains:      100,
			wantMoisturePass: false,
			wantPurityPass:   true,
			wantEvidencePass: false,
			wantRelease:      false,
		},
		{
			name:             "purity_below_min_blocks",
			moisture:         "12.50",
			purityGrains:     97,
			totalGrains:      100,
			wantMoisturePass: true,
			wantPurityPass:   false,
			wantEvidencePass: false,
			wantRelease:      false,
		},
		{
			name:             "normal_qualified_releases",
			moisture:         "12.50",
			purityGrains:     99,
			totalGrains:      100,
			wantMoisturePass: true,
			wantPurityPass:   true,
			wantEvidencePass: true,
			wantRelease:      true,
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prefix := fmt.Sprintf("model-%02d", i)
			dbPath := filepath.Join(t.TempDir(), "riceguard.db")
			c, roles := catalog.Seed()
			st, err := store.OpenSQLite(dbPath)
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			svc := api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())

			taskID := driveModelTaskToMoisture(t, svc, prefix)
			task, derr := svc.GetTask(taskID)
			if derr != nil {
				t.Fatalf("get task before moisture: %v", derr)
			}

			moisture, derr := measure.ParsePercent(tc.moisture)
			if derr != nil {
				t.Fatalf("parse moisture: %v", derr)
			}
			moistureDecision, derr := measure.DecideMoisture(moisture, task.Task.MoistureMax)
			if derr != nil {
				t.Fatalf("domain moisture decision: %v", derr)
			}
			if moistureDecision.Pass != tc.wantMoisturePass {
				t.Fatalf("domain moisture pass for %s = %v, want %v", tc.moisture, moistureDecision.Pass, tc.wantMoisturePass)
			}
			derivedPurity, derr := measure.PurityDerive(tc.purityGrains, tc.totalGrains)
			if derr != nil {
				t.Fatalf("domain purity derive: %v", derr)
			}
			purityPass := measure.PurityPass(derivedPurity, task.Task.MinPurity)
			if purityPass != tc.wantPurityPass {
				t.Fatalf("domain purity pass = %v, want %v", purityPass, tc.wantPurityPass)
			}
			domainEvidencePass := moistureDecision.Pass && purityPass
			if domainEvidencePass != tc.wantEvidencePass {
				t.Fatalf("domain evidence pass = %v, want %v", domainEvidencePass, tc.wantEvidencePass)
			}

			moistureResp, derr := svc.RecordMoisture(taskID, api.MoistureRequest{
				OperationID:   prefix + "-moisture",
				Moisture:      tc.moisture,
				PurityGrains:  tc.purityGrains,
				TotalGrains:   tc.totalGrains,
				ThousandGrain: 25000,
				Collector:     "metrologist-e",
			})
			if derr != nil {
				t.Fatalf("record moisture: %v", derr)
			}
			if moistureResp.Pass != domainEvidencePass {
				t.Fatalf("record response pass = %v, want domain result %v", moistureResp.Pass, domainEvidencePass)
			}

			if err := st.Close(); err != nil {
				t.Fatalf("close sqlite: %v", err)
			}
			st, err = store.OpenSQLite(dbPath)
			if err != nil {
				t.Fatalf("reopen sqlite: %v", err)
			}
			defer st.Close()
			svc = api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())

			persisted, err := st.ListMoisture(inspection.TaskID(taskID))
			if err != nil {
				t.Fatalf("list persisted moisture: %v", err)
			}
			if len(persisted) != 1 {
				t.Fatalf("expected 1 persisted moisture evidence, got %d", len(persisted))
			}
			if persisted[0].PassThreshold != domainEvidencePass {
				t.Fatalf("sqlite pass_threshold = %v, want domain result %v", persisted[0].PassThreshold, domainEvidencePass)
			}
			if persisted[0].Moisture != moisture {
				t.Fatalf("sqlite moisture = %d, want %d", persisted[0].Moisture, moisture)
			}
			if persisted[0].DerivedPurity != derivedPurity {
				t.Fatalf("sqlite derived purity = %d, want %d", persisted[0].DerivedPurity, derivedPurity)
			}

			summary, derr := svc.ComputeSummary(taskID)
			if derr != nil {
				t.Fatalf("summary after persistence: %v", derr)
			}
			if summary.MoisturePassed != domainEvidencePass {
				t.Fatalf("summary moisture_passed = %v, want %v", summary.MoisturePassed, domainEvidencePass)
			}
			if summary.PurityPassed != purityPass {
				t.Fatalf("summary purity_passed = %v, want %v", summary.PurityPassed, purityPass)
			}

			if _, derr := svc.Review(taskID, api.ReviewRequest{OperationID: prefix + "-review-a", Reviewer: "reviewer-f", Conclusion: "approve"}); derr != nil {
				t.Fatalf("review 1: %v", derr)
			}
			if _, derr := svc.Review(taskID, api.ReviewRequest{OperationID: prefix + "-review-b", Reviewer: "reviewer-g", Conclusion: "approve"}); derr != nil {
				t.Fatalf("review 2: %v", derr)
			}
			viewAfterReview, derr := svc.GetTask(taskID)
			if derr != nil {
				t.Fatalf("get task after reviews: %v", derr)
			}
			if viewAfterReview.Summary.MoisturePassed != domainEvidencePass {
				t.Fatalf("post-review moisture_passed = %v, want %v", viewAfterReview.Summary.MoisturePassed, domainEvidencePass)
			}
			if viewAfterReview.Summary.Releasable != tc.wantRelease {
				t.Fatalf("post-review releasable = %v, want %v", viewAfterReview.Summary.Releasable, tc.wantRelease)
			}

			final, derr := svc.Finalize(taskID, api.FinalizeRequest{OperationID: prefix + "-final"})
			if tc.wantRelease {
				if derr != nil {
					t.Fatalf("finalize release: %v", derr)
				}
				if final.Status != inspection.StatusReleased {
					t.Fatalf("final status = %s, want %s", final.Status, inspection.StatusReleased)
				}
				if final.Credential == "" {
					t.Fatal("released evidence must mint a credential")
				}
				return
			}

			if derr == nil {
				t.Fatalf("finalize unexpectedly succeeded with status %s and credential %q", final.Status, final.Credential)
			}
			if derr.Code != domain.CodeBadRequest {
				t.Fatalf("finalize error code = %s, want %s", derr.Code, domain.CodeBadRequest)
			}
			if final.Credential != "" {
				t.Fatalf("failed finalization returned credential %q", final.Credential)
			}

			cancelled, derr := svc.Finalize(taskID, api.FinalizeRequest{OperationID: prefix + "-cancel", Outcome: "cancelled", Reason: "threshold"})
			if derr != nil {
				t.Fatalf("cancel threshold-failed task: %v", derr)
			}
			if cancelled.Status != inspection.StatusCancelled {
				t.Fatalf("cancelled status = %s, want %s", cancelled.Status, inspection.StatusCancelled)
			}
			terminalView, derr := svc.GetTask(taskID)
			if derr != nil {
				t.Fatalf("get terminal threshold-failed task: %v", derr)
			}
			if terminalView.Credential != nil {
				t.Fatalf("threshold-failed task must not have credential: %#v", terminalView.Credential)
			}
			if terminalView.Summary.MoisturePassed || terminalView.Summary.Releasable {
				t.Fatalf("terminal threshold-failed summary read as passing: moisture=%v releasable=%v", terminalView.Summary.MoisturePassed, terminalView.Summary.Releasable)
			}
		})
	}
}

func driveModelTaskToMoisture(t *testing.T, svc *api.Service, prefix string) string {
	t.Helper()
	create := api.CreateTaskRequest{
		OperationID: prefix + "-create",
		SeedLot:     prefix + "-lot",
		Field:       "field-01",
		Variety:     "xiangliangyou-900",
		FemaleCert:  3,
		MaleCert:    3,
		BlindAllocs: []api.BlindAllocInput{
			{Code: prefix + "-blind", Germination: 100, Pathogen: 50, Moisture: 30},
		},
		Chamber:        prefix + "-chamber",
		ChamberStart:   100,
		ChamberEnd:     200,
		Plate:          prefix + "-plate",
		Wells:          []string{prefix + "-well"},
		ReviewerRoster: []string{"reviewer-f", "reviewer-g"},
	}
	resp, derr := svc.CreateTask(create)
	if derr != nil {
		t.Fatalf("create task: %v", derr)
	}
	taskID := resp.TaskID

	if _, derr := svc.ConfirmSampling(taskID, api.SamplingRequest{
		OperationID: prefix + "-sampling-a",
		Reviewer:    "sampler-a",
		Field:       create.Field,
		SeedLot:     create.SeedLot,
		BlindSeal:   prefix + "-seal",
		SampleCount: 180,
	}); derr != nil {
		t.Fatalf("sampling 1: %v", derr)
	}
	if _, derr := svc.ConfirmSampling(taskID, api.SamplingRequest{
		OperationID: prefix + "-sampling-b",
		Reviewer:    "sampler-b",
		Field:       create.Field,
		SeedLot:     create.SeedLot,
		BlindSeal:   prefix + "-seal",
		SampleCount: 180,
	}); derr != nil {
		t.Fatalf("sampling 2: %v", derr)
	}
	if _, derr := svc.SplitBlindSamples(taskID, api.SplitRequest{OperationID: prefix + "-split"}); derr != nil {
		t.Fatalf("split: %v", derr)
	}
	if _, derr := svc.Occupy(taskID, api.OccupyRequest{OperationID: prefix + "-occupy"}); derr != nil {
		t.Fatalf("occupy: %v", derr)
	}
	for _, dayAge := range []int32{2, 5, 8} {
		if _, derr := svc.RecordGermination(taskID, api.GerminationRequest{
			OperationID: fmt.Sprintf("%s-germination-%d", prefix, dayAge),
			BlindCode:   create.BlindAllocs[0].Code,
			DayAge:      dayAge,
			Normal:      95,
			Abnormal:    3,
			Dead:        2,
			Collector:   "germinator-c",
		}); derr != nil {
			t.Fatalf("germination day %d: %v", dayAge, derr)
		}
	}
	if _, derr := svc.RecordPathogen(taskID, api.PathogenRequest{
		OperationID: prefix + "-pathogen",
		BlindCode:   create.BlindAllocs[0].Code,
		Plate:       create.Plate,
		Well:        create.Wells[0],
		Verifier:    "pathologist-d",
		Reading:     modelInt32Ptr(10),
	}); derr != nil {
		t.Fatalf("pathogen: %v", derr)
	}
	return taskID
}

func modelInt32Ptr(v int32) *int32 {
	return &v
}

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
	"riceguard/review"
	"riceguard/store"
)

func TestModel_SQLiteFinalizePersistsBlindSampleUnblinding(t *testing.T) {
	cases := []struct {
		name           string
		outcome        string
		reason         string
		contaminated   bool
		wantStatus     inspection.TaskStatus
		wantOutcome    review.FinalOutcome
		wantCredential bool
		wantUnblinded  bool
	}{
		{
			name:           "release marks every blind sample unblinded",
			wantStatus:     inspection.StatusReleased,
			wantOutcome:    review.OutcomeReleased,
			wantCredential: true,
			wantUnblinded:  true,
		},
		{
			name:          "cancel keeps blind samples blinded",
			outcome:       "cancelled",
			reason:        "operator_cancelled",
			wantStatus:    inspection.StatusCancelled,
			wantOutcome:   review.OutcomeCancelled,
			wantUnblinded: false,
		},
		{
			name:          "quarantine keeps blind samples blinded",
			contaminated:  true,
			wantStatus:    inspection.StatusQuarantined,
			wantOutcome:   review.OutcomeQuarantined,
			wantUnblinded: false,
		},
	}

	blindCodes := []string{"b1", "b2"}
	wells := []string{"w1", "w2"}

	openSQLiteService := func(t *testing.T, dbPath string) (*api.Service, store.Store, catalog.Catalog, catalog.RoleDirectory) {
		t.Helper()
		c, roles := catalog.Seed()
		st, err := store.OpenSQLite(dbPath)
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		return api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter()), st, c, roles
	}

	driveToReleasable := func(t *testing.T, svc *api.Service, opPrefix string, contaminated bool) string {
		t.Helper()
		create := validCreate(opPrefix + "-create")
		create.SeedLot = opPrefix + "-lot"
		create.BlindAllocs = []api.BlindAllocInput{
			{Code: blindCodes[0], Germination: 100, Pathogen: 50, Moisture: 30},
			{Code: blindCodes[1], Germination: 100, Pathogen: 50, Moisture: 30},
		}
		create.Wells = append([]string(nil), wells...)
		resp, derr := svc.CreateTask(create)
		if derr != nil {
			t.Fatalf("create: %v", derr)
		}
		id := resp.TaskID
		sampleCount := 360

		for i, reviewer := range []string{"sampler-a", "sampler-b"} {
			if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
				OperationID: fmt.Sprintf("%s-s%d", opPrefix, i+1),
				Reviewer:    reviewer,
				Field:       create.Field,
				SeedLot:     create.SeedLot,
				BlindSeal:   "seal-1",
				SampleCount: sampleCount,
			}); derr != nil {
				t.Fatalf("sampling %d: %v", i+1, derr)
			}
		}
		if _, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: opPrefix + "-split"}); derr != nil {
			t.Fatalf("split: %v", derr)
		}
		before, derr := svc.GetTask(id)
		if derr != nil {
			t.Fatalf("get task after split: %v", derr)
		}
		if len(before.BlindSamples) != len(blindCodes) {
			t.Fatalf("expected %d blind samples after split, got %d", len(blindCodes), len(before.BlindSamples))
		}
		for _, sample := range before.BlindSamples {
			if sample.Unblinded {
				t.Fatalf("blind sample %s was unblinded before finalization", sample.Code)
			}
		}
		if _, derr := svc.Occupy(id, api.OccupyRequest{OperationID: opPrefix + "-occupy"}); derr != nil {
			t.Fatalf("occupy: %v", derr)
		}
		for _, code := range blindCodes {
			for _, day := range []int32{2, 5, 8} {
				if _, derr := svc.RecordGermination(id, api.GerminationRequest{
					OperationID: fmt.Sprintf("%s-g-%s-%d", opPrefix, code, day),
					BlindCode:   code,
					DayAge:      day,
					Normal:      95,
					Abnormal:    3,
					Dead:        2,
					Collector:   "germinator-c",
				}); derr != nil {
					t.Fatalf("germination %s day %d: %v", code, day, derr)
				}
			}
		}
		for i, code := range blindCodes {
			reading := int32(10)
			if _, derr := svc.RecordPathogen(id, api.PathogenRequest{
				OperationID:  fmt.Sprintf("%s-p-%s", opPrefix, code),
				BlindCode:    code,
				Plate:        create.Plate,
				Well:         wells[i],
				Verifier:     "pathologist-d",
				Reading:      &reading,
				Contaminated: contaminated && i == 0,
			}); derr != nil {
				t.Fatalf("pathogen %s: %v", code, derr)
			}
		}
		if _, derr := svc.RecordMoisture(id, api.MoistureRequest{
			OperationID:   opPrefix + "-m",
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
				OperationID: fmt.Sprintf("%s-r%d", opPrefix, i+1),
				Reviewer:    reviewer,
				Conclusion:  "approve",
			}); derr != nil {
				t.Fatalf("review %d: %v", i+1, derr)
			}
		}
		return id
	}

	assertFinalizedView := func(t *testing.T, view api.TaskView, wantStatus inspection.TaskStatus, wantOutcome review.FinalOutcome, wantCredential, wantUnblinded bool, credential string, version int64) {
		t.Helper()
		if view.Task.Status != wantStatus {
			t.Fatalf("expected task status %s, got %s", wantStatus, view.Task.Status)
		}
		if view.Task.TerminalVersion != version {
			t.Fatalf("expected terminal version %d, got %d", version, view.Task.TerminalVersion)
		}
		if (view.Credential != nil) != wantCredential {
			t.Fatalf("credential presence mismatch: got %v want %v", view.Credential != nil, wantCredential)
		}
		if wantCredential && view.Credential.Credential != credential {
			t.Fatalf("credential mismatch after read: got %q want %q", view.Credential.Credential, credential)
		}
		if len(view.BlindSamples) != len(blindCodes) {
			t.Fatalf("expected %d blind samples, got %d", len(blindCodes), len(view.BlindSamples))
		}
		for _, sample := range view.BlindSamples {
			if bool(sample.Unblinded) != wantUnblinded {
				t.Fatalf("blind sample %s unblinded=%v, want %v", sample.Code, sample.Unblinded, wantUnblinded)
			}
		}
		terminalReviews := 0
		for _, r := range view.Reviews {
			if r.TerminalVersion == version && r.Outcome == wantOutcome {
				terminalReviews++
			}
		}
		if terminalReviews != 1 {
			t.Fatalf("expected one terminal review outcome %q at version %d, got %d", wantOutcome, version, terminalReviews)
		}
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "riceguard.db")
			svc, st, c, roles := openSQLiteService(t, dbPath)
			opPrefix := fmt.Sprintf("sqlite-final-%d", i)
			id := driveToReleasable(t, svc, opPrefix, tc.contaminated)

			final, derr := svc.Finalize(id, api.FinalizeRequest{
				OperationID: opPrefix + "-final",
				Outcome:     tc.outcome,
				Reason:      tc.reason,
			})
			if derr != nil {
				t.Fatalf("finalize: %v", derr)
			}
			if final.Status != tc.wantStatus {
				t.Fatalf("expected finalize status %s, got %s", tc.wantStatus, final.Status)
			}
			if (final.Credential != "") != tc.wantCredential {
				t.Fatalf("finalize credential presence mismatch: got %v want %v", final.Credential != "", tc.wantCredential)
			}

			_, derr = svc.Finalize(id, api.FinalizeRequest{OperationID: opPrefix + "-second-final", Outcome: "cancelled", Reason: "late"})
			if derr == nil || derr.Code != domain.CodeFinalized {
				t.Fatalf("expected repeated finalize to hit terminal fence, got %v", derr)
			}

			view, derr := svc.GetTask(id)
			if derr != nil {
				t.Fatalf("get task after finalize: %v", derr)
			}
			assertFinalizedView(t, view, tc.wantStatus, tc.wantOutcome, tc.wantCredential, tc.wantUnblinded, final.Credential, final.Version)

			if err := st.Close(); err != nil {
				t.Fatalf("close sqlite: %v", err)
			}

			st2, err := store.OpenSQLite(dbPath)
			if err != nil {
				t.Fatalf("reopen sqlite: %v", err)
			}
			defer st2.Close()
			recovered := api.NewService(c, roles, st2, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())
			recoveredView, derr := recovered.GetTask(id)
			if derr != nil {
				t.Fatalf("get recovered task: %v", derr)
			}
			assertFinalizedView(t, recoveredView, tc.wantStatus, tc.wantOutcome, tc.wantCredential, tc.wantUnblinded, final.Credential, final.Version)
		})
	}
}

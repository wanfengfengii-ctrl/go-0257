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

func TestModel_TerminalBlindCodeReuseVerdictIsStable(t *testing.T) {
	type verdict string

	const (
		verdictCreateRejected verdict = "create rejected"
		verdictFinalReleased  verdict = "final released"
	)

	cases := []struct {
		name       string
		opPrefix   string
		restartSvc bool
	}{
		{name: "same process after terminal release", opPrefix: "model-same", restartSvc: false},
		{name: "new process after terminal release", opPrefix: "model-restart", restartSvc: true},
	}

	outcomes := make(map[string]verdict)

	newService := func(t *testing.T, dbPath string) (*api.Service, store.Store) {
		t.Helper()
		c, roles := catalog.Seed()
		st, err := store.OpenSQLite(dbPath)
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		return api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter()), st
	}

	reuseCreate := func(op string) api.CreateTaskRequest {
		req := validCreate(op)
		req.SeedLot = "lot-2002"
		req.Field = "field-02"
		req.Chamber = "ch-2"
		req.ChamberStart = 300
		req.ChamberEnd = 400
		req.Plate = "p-2"
		req.Wells = []string{"w2"}
		return req
	}

	driveReuseToReleasable := func(t *testing.T, svc *api.Service, id string, op string) {
		t.Helper()
		for i, reviewer := range []string{"sampler-a", "sampler-b"} {
			if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
				OperationID: op + "-sampling-" + string(rune('0'+i)),
				Reviewer:    reviewer,
				Field:       "field-02",
				SeedLot:     "lot-2002",
				BlindSeal:   "seal-2",
				SampleCount: 180,
			}); derr != nil {
				t.Fatalf("sampling %d: %v", i, derr)
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
				BlindCode:   "b1",
				DayAge:      day,
				Normal:      95,
				Abnormal:    3,
				Dead:        2,
				Collector:   "germinator-c",
			}); derr != nil {
				t.Fatalf("germination day %d: %v", day, derr)
			}
		}
		if _, derr := svc.RecordPathogen(id, api.PathogenRequest{
			OperationID: op + "-pathogen",
			BlindCode:   "b1",
			Plate:       "p-2",
			Well:        "w2",
			Verifier:    "pathologist-d",
			Reading:     int32Ptr(10),
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
		if _, derr := svc.Review(id, api.ReviewRequest{OperationID: op + "-review-1", Reviewer: "reviewer-f", Conclusion: "approve"}); derr != nil {
			t.Fatalf("review 1: %v", derr)
		}
		if _, derr := svc.Review(id, api.ReviewRequest{OperationID: op + "-review-2", Reviewer: "reviewer-g", Conclusion: "approve"}); derr != nil {
			t.Fatalf("review 2: %v", derr)
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "riceguard.db")
			svc, st := newService(t, dbPath)

			firstID := driveToReleasable(t, svc, tc.opPrefix+"-first")
			firstFinal, derr := svc.Finalize(firstID, api.FinalizeRequest{OperationID: tc.opPrefix + "-first-final"})
			if derr != nil {
				t.Fatalf("first finalize: %v", derr)
			}
			if firstFinal.Status != inspection.StatusReleased {
				t.Fatalf("expected first task released, got %s", firstFinal.Status)
			}

			if tc.restartSvc {
				if err := st.Close(); err != nil {
					t.Fatalf("close before restart: %v", err)
				}
				svc, st = newService(t, dbPath)
			}
			defer st.Close()

			second, derr := svc.CreateTask(reuseCreate(tc.opPrefix + "-reuse-create"))
			if derr != nil {
				if derr.Code == domain.CodeBlindDuplicate {
					outcomes[tc.name] = verdictCreateRejected
					return
				}
				t.Fatalf("reuse create returned unexpected error: %v", derr)
			}

			driveReuseToReleasable(t, svc, second.TaskID, tc.opPrefix+"-reuse")
			secondFinal, derr := svc.Finalize(second.TaskID, api.FinalizeRequest{OperationID: tc.opPrefix + "-reuse-final"})
			if derr != nil {
				t.Fatalf("reuse was allowed at create, then rejected at finalize: %v", derr)
			}
			if secondFinal.Status != inspection.StatusReleased {
				t.Fatalf("expected reused task released, got %s", secondFinal.Status)
			}
			outcomes[tc.name] = verdictFinalReleased
		})
	}

	if len(outcomes) != len(cases) {
		return
	}
	if outcomes[cases[0].name] != outcomes[cases[1].name] {
		t.Fatalf("blind-code reuse verdict changed across restart: %s=%q %s=%q",
			cases[0].name, outcomes[cases[0].name], cases[1].name, outcomes[cases[1].name])
	}
}

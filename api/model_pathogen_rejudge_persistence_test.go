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

func TestModel_PathogenRejudgeStatePersistence(t *testing.T) {
	cases := []struct {
		name       string
		persistent bool
	}{
		{name: "memory_append_only_chain", persistent: false},
		{name: "sqlite_restart_recovers_chain", persistent: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, roles := catalog.Seed()
			var (
				st     store.Store
				svc    *api.Service
				dbPath string
				err    error
			)
			if tc.persistent {
				dbPath = filepath.Join(t.TempDir(), "riceguard.db")
				st, err = store.OpenSQLite(dbPath)
				if err != nil {
					t.Fatalf("open sqlite: %v", err)
				}
			} else {
				st = store.NewMemory()
			}
			defer func() {
				if st != nil {
					_ = st.Close()
				}
			}()
			svc = api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())

			prefix := "model-" + tc.name
			plate := "plate-" + tc.name
			create := api.CreateTaskRequest{
				OperationID: prefix + "-create",
				SeedLot:     "lot-" + tc.name,
				Field:       "field-01",
				Variety:     "xiangliangyou-900",
				FemaleCert:  3,
				MaleCert:    3,
				BlindAllocs: []api.BlindAllocInput{
					{Code: "b1", Germination: 100, Pathogen: 50, Moisture: 30},
				},
				Chamber:        "chamber-" + tc.name,
				ChamberStart:   100,
				ChamberEnd:     200,
				Plate:          plate,
				Wells:          []string{"w1"},
				ReviewerRoster: []string{"reviewer-f", "reviewer-g"},
			}
			created, derr := svc.CreateTask(create)
			if derr != nil {
				t.Fatalf("create: %v", derr)
			}
			id := created.TaskID

			for i, reviewer := range []string{"sampler-a", "sampler-b"} {
				if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
					OperationID: prefix + "-sampling-" + string(rune('0'+i)),
					Reviewer:    reviewer,
					Field:       "field-01",
					SeedLot:     create.SeedLot,
					BlindSeal:   "seal-1",
					SampleCount: 180,
				}); derr != nil {
					t.Fatalf("sampling %d: %v", i, derr)
				}
			}
			if _, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: prefix + "-split"}); derr != nil {
				t.Fatalf("split: %v", derr)
			}
			if _, derr := svc.Occupy(id, api.OccupyRequest{OperationID: prefix + "-occupy"}); derr != nil {
				t.Fatalf("occupy: %v", derr)
			}
			for _, day := range []int32{2, 5, 8} {
				if _, derr := svc.RecordGermination(id, api.GerminationRequest{
					OperationID: prefix + "-germination-" + string(rune('0'+day)),
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

			contaminatedReading := int32(10)
			first, derr := svc.RecordPathogen(id, api.PathogenRequest{
				OperationID:  prefix + "-pathogen-contaminated",
				BlindCode:    "b1",
				Plate:        plate,
				Well:         "w1",
				Verifier:     "pathologist-d",
				Contaminated: true,
				Reading:      &contaminatedReading,
			})
			if derr != nil {
				t.Fatalf("contaminated pathogen reading: %v", derr)
			}
			if first.RejudgeGen == 0 || first.Verdict != pathogen.VerdictNegative || !first.Contaminated {
				t.Fatalf("contaminated negative reading did not open rejudge generation: %+v", first)
			}

			rejudgeReq := api.RejudgeRequest{
				OperationID: prefix + "-rejudge-clear",
				Verifier:    "pathologist-d",
				Conclusion:  string(pathogen.VerdictNegative),
				BlindCodes:  []string{"b1"},
				Wells:       []string{"w1"},
			}
			rejudged, derr := svc.ResolveRejudge(id, rejudgeReq)
			if derr != nil {
				t.Fatalf("resolve rejudge: %v", derr)
			}
			if rejudged.TaskID != inspection.TaskID(id) || rejudged.Conclusion != pathogen.VerdictNegative || rejudged.RejudgeGen != first.RejudgeGen {
				t.Fatalf("unexpected rejudge response: %+v", rejudged)
			}
			if op, ok := st.FindOperation(rejudgeReq.OperationID); !ok || op.TaskID != inspection.TaskID(id) || op.ResultDigest == "" {
				t.Fatalf("rejudge operation was not recorded: ok=%v op=%+v", ok, op)
			}

			replayed, derr := svc.ResolveRejudge(id, rejudgeReq)
			if derr != nil {
				t.Fatalf("replay rejudge: %v", derr)
			}
			if replayed != rejudged {
				t.Fatalf("idempotent rejudge replay changed response: %+v vs %+v", replayed, rejudged)
			}
			viewAfterReplay, derr := svc.GetTask(id)
			if derr != nil {
				t.Fatalf("get task after rejudge replay: %v", derr)
			}
			if got := len(viewAfterReplay.Pathogen); got != 2 {
				t.Fatalf("idempotent rejudge replay appended evidence, got %d pathogen rows", got)
			}

			duplicateReading := int32(9)
			_, derr = svc.RecordPathogen(id, api.PathogenRequest{
				OperationID: prefix + "-duplicate-pathogen",
				BlindCode:   "b1",
				Plate:       plate,
				Well:        "w1",
				Verifier:    "pathologist-d",
				Reading:     &duplicateReading,
			})
			if derr == nil {
				t.Fatal("ordinary duplicate pathogen reading was accepted")
			}
			if derr.Code != domain.CodeBadRequest {
				t.Fatalf("expected duplicate pathogen reading to be bad request, got %s", derr.Code)
			}

			lateReading := int32(8)
			lateGeneration := int64(viewAfterReplay.Task.Generation - 1)
			if _, derr = svc.RecordPathogen(id, api.PathogenRequest{
				OperationID: prefix + "-late-pathogen",
				BlindCode:   "b1",
				Plate:       plate,
				Well:        "w1",
				Verifier:    "pathologist-d",
				Generation:  lateGeneration,
				Reading:     &lateReading,
			}); derr != nil {
				t.Fatalf("late pathogen reading should be isolated, got %v", derr)
			}

			if tc.persistent {
				if err := st.Close(); err != nil {
					t.Fatalf("close before restart: %v", err)
				}
				st = nil
				st, err = store.OpenSQLite(dbPath)
				if err != nil {
					t.Fatalf("reopen sqlite: %v", err)
				}
				svc = api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())

				replayed, derr = svc.ResolveRejudge(id, rejudgeReq)
				if derr != nil {
					t.Fatalf("replay rejudge after restart: %v", derr)
				}
				if replayed != rejudged {
					t.Fatalf("restart did not recover rejudge idempotency response: %+v vs %+v", replayed, rejudged)
				}
			}

			view, derr := svc.GetTask(id)
			if derr != nil {
				t.Fatalf("get final task: %v", derr)
			}
			if got := len(view.Pathogen); got != 3 {
				t.Fatalf("expected original, rejudge, and isolated late pathogen evidence, got %d rows: %+v", got, view.Pathogen)
			}
			original, resolution, late := view.Pathogen[0], view.Pathogen[1], view.Pathogen[2]
			if !original.Contaminated || original.RejudgeGen != first.RejudgeGen || original.LateIsolated {
				t.Fatalf("original contaminated evidence lost rejudge state: %+v", original)
			}
			if resolution.Contaminated || resolution.Verdict != pathogen.VerdictNegative || resolution.RejudgeGen != first.RejudgeGen || resolution.LateIsolated {
				t.Fatalf("rejudge resolution was not appended as current evidence: %+v", resolution)
			}
			if !late.LateIsolated || late.BlindCode != "b1" || string(late.Plate) != plate || late.Well != "w1" {
				t.Fatalf("late pathogen evidence was not isolated for the same well: %+v", late)
			}
			if !view.Summary.PathogenClean {
				t.Fatalf("negative rejudge should clear current pathogen contamination: %+v", view.Summary)
			}

			auditTrail, derr := svc.ListAllAudit()
			if derr != nil {
				t.Fatalf("list audit: %v", derr)
			}
			actions := map[string]int{}
			for _, ev := range auditTrail {
				actions[ev.Action]++
			}
			if actions["rejudge_resolution"] != 1 {
				t.Fatalf("expected one rejudge audit event, got actions %#v", actions)
			}
			if actions["pathogen_late_reading_isolated"] != 1 {
				t.Fatalf("expected one late-isolation audit event, got actions %#v", actions)
			}
			if op, ok := st.FindOperation(rejudgeReq.OperationID); !ok || op.TaskID != inspection.TaskID(id) || op.ResultDigest == "" {
				t.Fatalf("rejudge operation was not recoverable: ok=%v op=%+v", ok, op)
			}
		})
	}
}

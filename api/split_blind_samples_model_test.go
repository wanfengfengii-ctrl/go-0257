package api_test

import (
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

func TestModel_SplitBlindSamplesPersistsAtomicBoundary(t *testing.T) {
	cases := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "persists full matrix and blind sample mappings before idempotent occupying result",
			run: func(t *testing.T) {
				svc := seedService()
				req := validCreate("model-split-create")
				req.BlindAllocs = []api.BlindAllocInput{
					{Code: "mx-a", Germination: 90, Pathogen: 30, Moisture: 20},
					{Code: "mx-b", Germination: 60, Pathogen: 25, Moisture: 15},
				}
				req.Wells = []string{"w1", "w2"}

				created, derr := svc.CreateTask(req)
				if derr != nil {
					t.Fatalf("create: %v", derr)
				}
				id := created.TaskID
				for i, reviewer := range []string{"sampler-a", "sampler-b"} {
					if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
						OperationID: "model-split-confirm-" + string(rune('1'+i)),
						Reviewer:    reviewer,
						Field:       "field-01",
						SeedLot:     "lot-1001",
						BlindSeal:   "seal-model",
						SampleCount: 240,
					}); derr != nil {
						t.Fatalf("confirm %s: %v", reviewer, derr)
					}
				}

				first, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: "model-split"})
				if derr != nil {
					t.Fatalf("split: %v", derr)
				}
				if first.Status != inspection.StatusOccupying {
					t.Fatalf("expected split to advance to occupying, got %s", first.Status)
				}
				second, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: "model-split"})
				if derr != nil {
					t.Fatalf("split retry: %v", derr)
				}
				if !reflect.DeepEqual(first, second) {
					t.Fatalf("idempotent retry changed response: %#v vs %#v", first, second)
				}

				view, derr := svc.GetTask(id)
				if derr != nil {
					t.Fatalf("get task: %v", derr)
				}
				if view.Task.Status != inspection.StatusOccupying {
					t.Fatalf("expected task detail status occupying, got %s", view.Task.Status)
				}
				if len(view.Splits) != 6 {
					t.Fatalf("expected 6 triple-split cells, got %d", len(view.Splits))
				}
				if len(view.BlindSamples) != 2 {
					t.Fatalf("expected 2 blind sample mappings, got %d", len(view.BlindSamples))
				}

				expected := map[string]struct {
					germination int
					pathogen    int
					moisture    int
				}{
					"mx-a": {germination: 90, pathogen: 30, moisture: 20},
					"mx-b": {germination: 60, pathogen: 25, moisture: 15},
				}
				splitsByCode := make(map[string]map[string]int)
				for _, sp := range view.Splits {
					code := string(sp.Code)
					if _, ok := expected[code]; !ok {
						t.Fatalf("unexpected split code %q", code)
					}
					if sp.TaskID != inspection.TaskID(id) {
						t.Fatalf("split %s has task %s, want %s", code, sp.TaskID, id)
					}
					if splitsByCode[code] == nil {
						splitsByCode[code] = make(map[string]int)
					}
					split := string(sp.Split)
					if _, exists := splitsByCode[code][split]; exists {
						t.Fatalf("duplicate split cell for %s/%s", code, split)
					}
					splitsByCode[code][split] = sp.Quantity
				}
				seenSamples := make(map[string]bool)
				for _, sample := range view.BlindSamples {
					code := string(sample.Code)
					want, ok := expected[code]
					if !ok {
						t.Fatalf("unexpected blind sample code %q", code)
					}
					if seenSamples[code] {
						t.Fatalf("duplicate blind sample mapping for %s", code)
					}
					seenSamples[code] = true
					if sample.TaskID != inspection.TaskID(id) {
						t.Fatalf("sample %s has task %s, want %s", code, sample.TaskID, id)
					}
					if sample.Generation != first.Generation-1 {
						t.Fatalf("sample %s generation = %d, want split generation %d", code, sample.Generation, first.Generation-1)
					}
					if sample.Unblinded {
						t.Fatalf("sample %s should start blinded", code)
					}
					if sample.ConsistencyHash == "" {
						t.Fatalf("sample %s missing consistency hash", code)
					}
					if sample.GerminationQty != want.germination || sample.PathogenQty != want.pathogen || sample.MoistureQty != want.moisture {
						t.Fatalf("sample %s quantities = (%d,%d,%d), want (%d,%d,%d)",
							code, sample.GerminationQty, sample.PathogenQty, sample.MoistureQty,
							want.germination, want.pathogen, want.moisture)
					}
				}
				for code, want := range expected {
					got := splitsByCode[code]
					if got["germination"] != want.germination || got["pathogen"] != want.pathogen || got["moisture_purity"] != want.moisture {
						t.Fatalf("split matrix for %s = %v, want germination=%d pathogen=%d moisture_purity=%d",
							code, got, want.germination, want.pathogen, want.moisture)
					}
					if !seenSamples[code] {
						t.Fatalf("missing blind sample mapping for %s", code)
					}
				}
			},
		},
		{
			name: "illegal state leaves no split sample or idempotent result",
			run: func(t *testing.T) {
				svc := seedService()
				created, derr := svc.CreateTask(validCreate("model-illegal-create"))
				if derr != nil {
					t.Fatalf("create: %v", derr)
				}
				id := created.TaskID
				op := "model-illegal-then-valid-split"
				if _, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: op}); derr == nil || derr.Code != domain.CodeBadRequest {
					t.Fatalf("expected illegal-state bad request, got %v", derr)
				}
				view, derr := svc.GetTask(id)
				if derr != nil {
					t.Fatalf("get task after rejected split: %v", derr)
				}
				if view.Task.Status != inspection.StatusPendingSampling || len(view.Splits) != 0 || len(view.BlindSamples) != 0 {
					t.Fatalf("rejected split left status=%s splits=%d samples=%d", view.Task.Status, len(view.Splits), len(view.BlindSamples))
				}

				for i, reviewer := range []string{"sampler-a", "sampler-b"} {
					if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
						OperationID: "model-illegal-confirm-" + string(rune('1'+i)),
						Reviewer:    reviewer,
						Field:       "field-01",
						SeedLot:     "lot-1001",
						BlindSeal:   "seal-model",
						SampleCount: 180,
					}); derr != nil {
						t.Fatalf("confirm %s: %v", reviewer, derr)
					}
				}
				resp, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: op})
				if derr != nil {
					t.Fatalf("same operation id after rejected split should execute once valid: %v", derr)
				}
				if resp.Status != inspection.StatusOccupying {
					t.Fatalf("expected valid retry to advance to occupying, got %s", resp.Status)
				}
				view, derr = svc.GetTask(id)
				if derr != nil {
					t.Fatalf("get task after valid split: %v", derr)
				}
				if len(view.Splits) != 3 || len(view.BlindSamples) != 1 {
					t.Fatalf("valid split persisted splits=%d samples=%d, want 3 and 1", len(view.Splits), len(view.BlindSamples))
				}
			},
		},
		{
			name: "matrix validation failure leaves no split sample status or operation state",
			run: func(t *testing.T) {
				c, roles := catalog.Seed()
				st := store.NewMemory()
				svc := api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())
				id := inspection.TaskID("task-invalid-matrix")
				if err := st.Mutate(func(tx store.Tx) error {
					return tx.SaveTask(&inspection.InspectionTask{
						ID:         id,
						SeedLot:    "lot-invalid-matrix",
						Field:      "field-01",
						Variety:    "xiangliangyou-900",
						Status:     inspection.StatusBlindSplit,
						Generation: 1,
						BlindAllocs: []inspection.BlindAllocation{
							{Code: "bad-a", Germination: 90, Pathogen: 30, Moisture: 20},
							{Code: "bad-b", Germination: 60, Pathogen: 0, Moisture: 15},
						},
					})
				}); err != nil {
					t.Fatalf("seed malformed task: %v", err)
				}

				op := "model-invalid-matrix-split"
				if _, derr := svc.SplitBlindSamples(string(id), api.SplitRequest{OperationID: op}); derr == nil || derr.Code != domain.CodeBadRequest {
					t.Fatalf("expected matrix validation bad request, got %v", derr)
				}
				task, err := st.GetTask(id)
				if err != nil {
					t.Fatalf("get malformed task: %v", err)
				}
				splits, err := st.ListSplits(id)
				if err != nil {
					t.Fatalf("list splits: %v", err)
				}
				samples, err := st.ListBlindSamples(id)
				if err != nil {
					t.Fatalf("list samples: %v", err)
				}
				if task.Status != inspection.StatusBlindSplit || task.Generation != 1 || len(splits) != 0 || len(samples) != 0 {
					t.Fatalf("failed matrix left status=%s gen=%d splits=%d samples=%d", task.Status, task.Generation, len(splits), len(samples))
				}
				if _, ok := st.FindOperation(op); ok {
					t.Fatal("matrix validation failure recorded an idempotent split operation")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

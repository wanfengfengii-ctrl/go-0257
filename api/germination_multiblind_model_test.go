package api_test

import (
	"path/filepath"
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

func TestModel_GerminationPersistenceUsesBlindCodeDayAgeGrid(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "riceguard.db")
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()

	c, roles := catalog.Seed()
	svc := api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())

	create := validCreate("grid-create")
	create.SeedLot = "lot-grid"
	create.BlindAllocs = []api.BlindAllocInput{
		{Code: "b1", Germination: 100, Pathogen: 50, Moisture: 30},
		{Code: "b2", Germination: 100, Pathogen: 50, Moisture: 30},
	}
	create.Chamber = "ch-grid"
	create.Plate = "p-grid"
	create.Wells = []string{"w1", "w2"}

	created, derr := svc.CreateTask(create)
	if derr != nil {
		t.Fatalf("create: %v", derr)
	}
	id := created.TaskID

	for _, req := range []api.SamplingRequest{
		{OperationID: "grid-sampling-1", Reviewer: "sampler-a", Field: create.Field, SeedLot: create.SeedLot, BlindSeal: "seal-grid", SampleCount: 360},
		{OperationID: "grid-sampling-2", Reviewer: "sampler-b", Field: create.Field, SeedLot: create.SeedLot, BlindSeal: "seal-grid", SampleCount: 360},
	} {
		if _, derr := svc.ConfirmSampling(id, req); derr != nil {
			t.Fatalf("sampling %s: %v", req.OperationID, derr)
		}
	}
	if _, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: "grid-split"}); derr != nil {
		t.Fatalf("split: %v", derr)
	}
	if _, derr := svc.Occupy(id, api.OccupyRequest{OperationID: "grid-occupy"}); derr != nil {
		t.Fatalf("occupy: %v", derr)
	}

	cases := []struct {
		name        string
		code        string
		day         int32
		op          string
		wantErr     domain.ErrorCode
		wantSaved   int
		wantStatus  inspection.TaskStatus
		wantAdvance bool
		wantMissing []string
	}{
		{
			name:       "b1 day2 remains incomplete",
			code:       "b1",
			day:        2,
			op:         "grid-g-b1-d2",
			wantSaved:  1,
			wantStatus: inspection.StatusGerminating,
			wantMissing: []string{
				"b1@5", "b1@8", "b2@2", "b2@5", "b2@8",
			},
		},
		{
			name:       "b2 day2 persists beside b1",
			code:       "b2",
			day:        2,
			op:         "grid-g-b2-d2",
			wantSaved:  2,
			wantStatus: inspection.StatusGerminating,
			wantMissing: []string{
				"b1@5", "b1@8", "b2@5", "b2@8",
			},
		},
		{
			name:       "same blind code day2 duplicate drifts",
			code:       "b2",
			day:        2,
			op:         "grid-g-b2-d2-duplicate",
			wantErr:    domain.CodeGerminationDrift,
			wantSaved:  2,
			wantStatus: inspection.StatusGerminating,
		},
		{
			name:       "b1 day5 remains incomplete",
			code:       "b1",
			day:        5,
			op:         "grid-g-b1-d5",
			wantSaved:  3,
			wantStatus: inspection.StatusGerminating,
			wantMissing: []string{
				"b1@8", "b2@5", "b2@8",
			},
		},
		{
			name:       "b2 day5 remains incomplete",
			code:       "b2",
			day:        5,
			op:         "grid-g-b2-d5",
			wantSaved:  4,
			wantStatus: inspection.StatusGerminating,
			wantMissing: []string{
				"b1@8", "b2@8",
			},
		},
		{
			name:       "b1 day8 still waits for b2",
			code:       "b1",
			day:        8,
			op:         "grid-g-b1-d8",
			wantSaved:  5,
			wantStatus: inspection.StatusGerminating,
			wantMissing: []string{
				"b2@8",
			},
		},
		{
			name:        "b2 day8 completes full grid",
			code:        "b2",
			day:         8,
			op:          "grid-g-b2-d8",
			wantSaved:   6,
			wantStatus:  inspection.StatusPathogen,
			wantAdvance: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, derr := svc.RecordGermination(id, api.GerminationRequest{
				OperationID: tc.op,
				BlindCode:   tc.code,
				DayAge:      tc.day,
				Normal:      95,
				Abnormal:    3,
				Dead:        2,
				Collector:   "germinator-c",
			})
			if tc.wantErr != "" {
				if derr == nil {
					t.Fatalf("expected %s, got nil", tc.wantErr)
				}
				if derr.Code != tc.wantErr {
					t.Fatalf("expected %s, got %s", tc.wantErr, derr.Code)
				}
			} else if derr != nil {
				t.Fatalf("record germination: %v", derr)
			} else {
				if resp.Status != tc.wantStatus {
					t.Fatalf("expected response status %s, got %s", tc.wantStatus, resp.Status)
				}
				if resp.Advanced != tc.wantAdvance {
					t.Fatalf("expected advanced %v, got %v", tc.wantAdvance, resp.Advanced)
				}
				if !reflect.DeepEqual(resp.MissingCells, tc.wantMissing) {
					t.Fatalf("expected missing %v, got %v", tc.wantMissing, resp.MissingCells)
				}
			}

			view, derr := svc.GetTask(id)
			if derr != nil {
				t.Fatalf("get task: %v", derr)
			}
			if len(view.Germinations) != tc.wantSaved {
				t.Fatalf("expected %d persisted germinations, got %d", tc.wantSaved, len(view.Germinations))
			}
			if view.Task.Status != tc.wantStatus {
				t.Fatalf("expected task status %s, got %s", tc.wantStatus, view.Task.Status)
			}
		})
	}
}

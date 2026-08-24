package api_test

import (
	"slices"
	"strconv"
	"testing"

	"riceguard/api"
	"riceguard/catalog"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/pathogen"
	"riceguard/store"
)

func TestModel_GerminationCoverageUsesLockedBlindGrid(t *testing.T) {
	type step struct {
		code          string
		day           int32
		normal        int
		abnormal      int
		dead          int
		wantErr       domain.ErrorCode
		wantStatus    inspection.TaskStatus
		wantAdvanced  bool
		wantMissing   []string
		checkMissing  bool
		wantEnergyBp  int32
		wantRateBp    int32
		wantCellCount int
		checkCells    bool
	}

	cases := []struct {
		name   string
		prefix string
		allocs []api.BlindAllocInput
		steps  []step
	}{
		{
			name:   "single blind advances after its locked day ages",
			prefix: "single",
			allocs: []api.BlindAllocInput{
				{Code: "b1", Germination: 100, Pathogen: 50, Moisture: 30},
			},
			steps: []step{
				{code: "b1", day: 2, normal: 95, abnormal: 3, dead: 2, wantStatus: inspection.StatusGerminating},
				{code: "b1", day: 5, normal: 96, abnormal: 2, dead: 2, wantStatus: inspection.StatusGerminating},
				{
					code: "b1", day: 8, normal: 97, abnormal: 1, dead: 2,
					wantStatus: inspection.StatusPathogen, wantAdvanced: true,
					wantEnergyBp: 9500, wantRateBp: 9700, wantCellCount: 3, checkCells: true,
				},
			},
		},
		{
			name:   "two blind allocations require every code day cell",
			prefix: "multi",
			allocs: []api.BlindAllocInput{
				{Code: "b1", Germination: 100, Pathogen: 50, Moisture: 30},
				{Code: "b2", Germination: 100, Pathogen: 50, Moisture: 30},
			},
			steps: []step{
				{code: "b1", day: 2, normal: 92, abnormal: 5, dead: 3, wantStatus: inspection.StatusGerminating},
				{code: "b1", day: 5, normal: 94, abnormal: 4, dead: 2, wantStatus: inspection.StatusGerminating},
				{
					code: "b1", day: 8, normal: 96, abnormal: 2, dead: 2,
					wantStatus:  inspection.StatusGerminating,
					wantMissing: []string{"b2@2", "b2@5", "b2@8"}, checkMissing: true,
					wantCellCount: 3, checkCells: true,
				},
				{code: "b2", day: 2, normal: 81, abnormal: 10, dead: 9, wantStatus: inspection.StatusGerminating},
				{code: "b2", day: 5, normal: 85, abnormal: 9, dead: 6, wantStatus: inspection.StatusGerminating},
				{
					code: "b2", day: 8, normal: 88, abnormal: 7, dead: 5,
					wantStatus: inspection.StatusPathogen, wantAdvanced: true,
					wantEnergyBp: 8100, wantRateBp: 8800, wantCellCount: 6, checkCells: true,
				},
			},
		},
		{
			name:   "duplicate day age remains germination drift",
			prefix: "duplicate",
			allocs: []api.BlindAllocInput{
				{Code: "b1", Germination: 100, Pathogen: 50, Moisture: 30},
			},
			steps: []step{
				{code: "b1", day: 2, normal: 90, abnormal: 5, dead: 5, wantStatus: inspection.StatusGerminating, wantCellCount: 1, checkCells: true},
				{code: "b1", day: 2, normal: 91, abnormal: 4, dead: 5, wantErr: domain.CodeGerminationDrift, wantCellCount: 1, checkCells: true},
			},
		},
		{
			name:   "count drift does not occupy the observation cell",
			prefix: "drift",
			allocs: []api.BlindAllocInput{
				{Code: "b1", Germination: 100, Pathogen: 50, Moisture: 30},
			},
			steps: []step{
				{code: "b1", day: 2, normal: 90, abnormal: 5, dead: 4, wantErr: domain.CodeGerminationDrift, wantCellCount: 0, checkCells: true},
				{
					code: "b1", day: 2, normal: 90, abnormal: 5, dead: 5,
					wantStatus:  inspection.StatusGerminating,
					wantMissing: []string{"b1@5", "b1@8"}, checkMissing: true,
					wantCellCount: 1, checkCells: true,
				},
			},
		},
	}

	newService := func() *api.Service {
		c, roles := catalog.Seed()
		return api.NewService(c, roles, store.NewMemory(), pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())
	}

	setupGerminating := func(t *testing.T, svc *api.Service, prefix string, allocs []api.BlindAllocInput) string {
		t.Helper()
		req := api.CreateTaskRequest{
			OperationID:    prefix + "-create",
			SeedLot:        "lot-1001",
			Field:          "field-01",
			Variety:        "xiangliangyou-900",
			FemaleCert:     3,
			MaleCert:       3,
			BlindAllocs:    allocs,
			Chamber:        "ch-1",
			ChamberStart:   100,
			ChamberEnd:     200,
			Plate:          "p-1",
			Wells:          []string{"w1"},
			ReviewerRoster: []string{"reviewer-f", "reviewer-g"},
		}
		created, derr := svc.CreateTask(req)
		if derr != nil {
			t.Fatalf("create task: %v", derr)
		}

		sampleCount := 0
		for _, alloc := range allocs {
			sampleCount += alloc.Germination + alloc.Pathogen + alloc.Moisture
		}
		for _, reviewer := range []string{"sampler-a", "sampler-b"} {
			if _, derr := svc.ConfirmSampling(created.TaskID, api.SamplingRequest{
				OperationID: prefix + "-sampling-" + reviewer,
				Reviewer:    reviewer,
				Field:       req.Field,
				SeedLot:     req.SeedLot,
				BlindSeal:   prefix + "-seal",
				SampleCount: sampleCount,
			}); derr != nil {
				t.Fatalf("sampling %s: %v", reviewer, derr)
			}
		}
		if _, derr := svc.SplitBlindSamples(created.TaskID, api.SplitRequest{OperationID: prefix + "-split"}); derr != nil {
			t.Fatalf("split blind samples: %v", derr)
		}
		if _, derr := svc.Occupy(created.TaskID, api.OccupyRequest{OperationID: prefix + "-occupy"}); derr != nil {
			t.Fatalf("occupy: %v", derr)
		}
		return created.TaskID
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newService()
			taskID := setupGerminating(t, svc, tc.prefix, tc.allocs)

			for i, s := range tc.steps {
				resp, derr := svc.RecordGermination(taskID, api.GerminationRequest{
					OperationID: tc.prefix + "-germination-" + strconv.Itoa(i),
					BlindCode:   s.code,
					DayAge:      s.day,
					Normal:      s.normal,
					Abnormal:    s.abnormal,
					Dead:        s.dead,
					Collector:   "germinator-c",
				})
				if s.wantErr != "" {
					if derr == nil {
						t.Fatalf("step %d: expected %s, got nil", i, s.wantErr)
					}
					if derr.Code != s.wantErr {
						t.Fatalf("step %d: expected %s, got %s", i, s.wantErr, derr.Code)
					}
				} else if derr != nil {
					t.Fatalf("step %d: unexpected error: %v", i, derr)
				}

				if s.wantErr == "" {
					if resp.Status != s.wantStatus {
						t.Fatalf("step %d: expected status %s, got %s", i, s.wantStatus, resp.Status)
					}
					if resp.Advanced != s.wantAdvanced {
						t.Fatalf("step %d: expected advanced=%v, got %v", i, s.wantAdvanced, resp.Advanced)
					}
					if s.checkMissing && !slices.Equal(resp.MissingCells, s.wantMissing) {
						t.Fatalf("step %d: expected missing cells %v, got %v", i, s.wantMissing, resp.MissingCells)
					}
					if resp.EnergyBp != s.wantEnergyBp || resp.RateBp != s.wantRateBp {
						t.Fatalf("step %d: expected energy/rate %d/%d, got %d/%d",
							i, s.wantEnergyBp, s.wantRateBp, resp.EnergyBp, resp.RateBp)
					}
				}

				if s.checkCells {
					view, derr := svc.GetTask(taskID)
					if derr != nil {
						t.Fatalf("step %d: get task: %v", i, derr)
					}
					if len(view.Germinations) != s.wantCellCount {
						t.Fatalf("step %d: expected %d valid germination cells, got %d",
							i, s.wantCellCount, len(view.Germinations))
					}
					if s.wantStatus != "" && view.Task.Status != s.wantStatus {
						t.Fatalf("step %d: expected stored status %s, got %s", i, s.wantStatus, view.Task.Status)
					}
				}
			}
		})
	}
}

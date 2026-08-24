package api_test

import (
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

func TestModel_PathogenCoverageRequiresEachLockedBlindCode(t *testing.T) {
	type pathogenReading struct {
		blindCode string
		well      string
	}

	cases := []struct {
		name                 string
		readings             []pathogenReading
		wantStatus           inspection.TaskStatus
		wantPathogenCovered  bool
		wantMoistureRejected bool
	}{
		{
			name: "one blind code across all wells is incomplete",
			readings: []pathogenReading{
				{blindCode: "b1", well: "w1"},
				{blindCode: "b1", well: "w2"},
			},
			wantStatus:           inspection.StatusPathogen,
			wantPathogenCovered:  false,
			wantMoistureRejected: true,
		},
		{
			name: "each blind code with a locked well completes coverage",
			readings: []pathogenReading{
				{blindCode: "b1", well: "w1"},
				{blindCode: "b2", well: "w2"},
			},
			wantStatus:          inspection.StatusMoisture,
			wantPathogenCovered: true,
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, roles := catalog.Seed()
			svc := api.NewService(c, roles, store.NewMemory(), pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())
			op := "model-pathogen-coverage-" + strconv.Itoa(i)

			created, derr := svc.CreateTask(api.CreateTaskRequest{
				OperationID: op + "-create",
				SeedLot:     "lot-1001",
				Field:       "field-01",
				Variety:     "xiangliangyou-900",
				FemaleCert:  3,
				MaleCert:    3,
				BlindAllocs: []api.BlindAllocInput{
					{Code: "b1", Germination: 100, Pathogen: 50, Moisture: 30},
					{Code: "b2", Germination: 100, Pathogen: 50, Moisture: 30},
				},
				Chamber:        "ch-1",
				ChamberStart:   100,
				ChamberEnd:     200,
				Plate:          "p-1",
				Wells:          []string{"w1", "w2"},
				ReviewerRoster: []string{"reviewer-f", "reviewer-g"},
			})
			if derr != nil {
				t.Fatalf("create: %v", derr)
			}
			id := created.TaskID

			for n, reviewer := range []string{"sampler-a", "sampler-b"} {
				if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
					OperationID: op + "-sampling-" + strconv.Itoa(n),
					Reviewer:    reviewer,
					Field:       "field-01",
					SeedLot:     "lot-1001",
					BlindSeal:   "seal-1",
					SampleCount: 360,
				}); derr != nil {
					t.Fatalf("sampling %d: %v", n, derr)
				}
			}
			if _, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: op + "-split"}); derr != nil {
				t.Fatalf("split: %v", derr)
			}
			if _, derr := svc.Occupy(id, api.OccupyRequest{OperationID: op + "-occupy"}); derr != nil {
				t.Fatalf("occupy: %v", derr)
			}
			for _, blindCode := range []string{"b1", "b2"} {
				for _, day := range []int32{2, 5, 8} {
					if _, derr := svc.RecordGermination(id, api.GerminationRequest{
						OperationID: op + "-germ-" + blindCode + "-" + strconv.Itoa(int(day)),
						BlindCode:   blindCode,
						DayAge:      day,
						Normal:      95,
						Abnormal:    3,
						Dead:        2,
						Collector:   "germinator-c",
					}); derr != nil {
						t.Fatalf("germination %s day %d: %v", blindCode, day, derr)
					}
				}
			}
			ready, derr := svc.GetTask(id)
			if derr != nil {
				t.Fatalf("get pathogen-ready task: %v", derr)
			}
			if ready.Task.Status != inspection.StatusPathogen {
				t.Fatalf("expected pathogen state before readings, got %s", ready.Task.Status)
			}

			for n, r := range tc.readings {
				reading := int32(10)
				resp, derr := svc.RecordPathogen(id, api.PathogenRequest{
					OperationID: op + "-pathogen-" + strconv.Itoa(n),
					BlindCode:   r.blindCode,
					Plate:       "p-1",
					Well:        r.well,
					Verifier:    "pathologist-d",
					Reading:     &reading,
				})
				if derr != nil {
					t.Fatalf("pathogen %d: %v", n, derr)
				}
				if n == len(tc.readings)-1 && resp.Status != tc.wantStatus {
					t.Fatalf("last pathogen response status = %s, want %s", resp.Status, tc.wantStatus)
				}
			}

			view, derr := svc.GetTask(id)
			if derr != nil {
				t.Fatalf("get task after pathogen readings: %v", derr)
			}
			if view.Task.Status != tc.wantStatus {
				t.Fatalf("task status = %s, want %s", view.Task.Status, tc.wantStatus)
			}
			if view.Summary.PathogenCovered != tc.wantPathogenCovered {
				t.Fatalf("summary pathogen coverage = %t, want %t", view.Summary.PathogenCovered, tc.wantPathogenCovered)
			}

			moisture, derr := svc.RecordMoisture(id, api.MoistureRequest{
				OperationID:   op + "-moisture",
				Moisture:      "12.50",
				PurityGrains:  98,
				TotalGrains:   100,
				ThousandGrain: 25000,
				Collector:     "metrologist-e",
			})
			if tc.wantMoistureRejected {
				if derr == nil {
					t.Fatal("expected moisture rejection before every blind code had pathogen evidence")
				}
				if derr.Code != domain.CodeBadRequest {
					t.Fatalf("moisture rejection code = %s, want %s", derr.Code, domain.CodeBadRequest)
				}
				return
			}
			if derr != nil {
				t.Fatalf("moisture: %v", derr)
			}
			if moisture.Status != inspection.StatusPendingReview {
				t.Fatalf("moisture status = %s, want %s", moisture.Status, inspection.StatusPendingReview)
			}
		})
	}
}

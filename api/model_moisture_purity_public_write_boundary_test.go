package api_test

import (
	"fmt"
	"math"
	"testing"

	"riceguard/api"
	"riceguard/domain"
	"riceguard/inspection"
)

func TestModel_MoisturePurityPublicWriteBoundary(t *testing.T) {
	overflowGrains := int64(math.MaxInt64/10000 + 1)

	driveToMoisture := func(t *testing.T, svc *api.Service, op string) string {
		t.Helper()

		create, derr := svc.CreateTask(validCreate(op + "-create"))
		if derr != nil {
			t.Fatalf("create: %v", derr)
		}
		id := create.TaskID

		for i, reviewer := range []string{"sampler-a", "sampler-b"} {
			if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
				OperationID: fmt.Sprintf("%s-sampling-%d", op, i),
				Reviewer:    reviewer,
				Field:       "field-01",
				SeedLot:     "lot-1001",
				BlindSeal:   "seal-1",
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
		for _, dayAge := range []int32{2, 5, 8} {
			if _, derr := svc.RecordGermination(id, api.GerminationRequest{
				OperationID: fmt.Sprintf("%s-germination-%d", op, dayAge),
				BlindCode:   "b1",
				DayAge:      dayAge,
				Normal:      95,
				Abnormal:    3,
				Dead:        2,
				Collector:   "germinator-c",
			}); derr != nil {
				t.Fatalf("germination day %d: %v", dayAge, derr)
			}
		}

		reading := int32(10)
		if _, derr := svc.RecordPathogen(id, api.PathogenRequest{
			OperationID: op + "-pathogen",
			BlindCode:   "b1",
			Plate:       "p-1",
			Well:        "w1",
			Verifier:    "pathologist-d",
			Reading:     &reading,
		}); derr != nil {
			t.Fatalf("pathogen: %v", derr)
		}

		return id
	}

	countMoistureSuccessAudits := func(view api.TaskView) int {
		count := 0
		for _, event := range view.Audit {
			if event.Action == "moisture_purity_measurement" && event.Code == domain.CodeNone {
				count++
			}
		}
		return count
	}

	cases := []struct {
		name                 string
		request              api.MoistureRequest
		wantErr              domain.ErrorCode
		wantMoistureBp       int64
		wantDerivedPurityBp  int64
		wantPass             bool
		wantAdvancedStatus   inspection.TaskStatus
		wantUnchangedOnError bool
	}{
		{
			name: "normal_98_of_100_derives_9800bp",
			request: api.MoistureRequest{
				OperationID:   "moisture-normal",
				Moisture:      "12.50",
				PurityGrains:  98,
				TotalGrains:   100,
				ThousandGrain: 25000,
				Collector:     "metrologist-e",
			},
			wantMoistureBp:      1250,
			wantDerivedPurityBp: 9800,
			wantPass:            true,
			wantAdvancedStatus:  inspection.StatusPendingReview,
		},
		{
			name: "moisture_equal_to_locked_maximum_passes",
			request: api.MoistureRequest{
				OperationID:   "moisture-at-upper-bound",
				Moisture:      "13.00",
				PurityGrains:  98,
				TotalGrains:   100,
				ThousandGrain: 25000,
				Collector:     "metrologist-e",
			},
			wantMoistureBp:      1300,
			wantDerivedPurityBp: 9800,
			wantPass:            true,
			wantAdvancedStatus:  inspection.StatusPendingReview,
		},
		{
			name: "negative_pure_grains_rejected_before_persisting_evidence",
			request: api.MoistureRequest{
				OperationID:   "moisture-negative-pure",
				Moisture:      "12.50",
				PurityGrains:  -1,
				TotalGrains:   100,
				ThousandGrain: 25000,
				Collector:     "metrologist-e",
			},
			wantErr:              domain.CodeFixedPointOverflow,
			wantUnchangedOnError: true,
		},
		{
			name: "zero_total_grains_rejected_before_persisting_evidence",
			request: api.MoistureRequest{
				OperationID:   "moisture-zero-total",
				Moisture:      "12.50",
				PurityGrains:  50,
				TotalGrains:   0,
				ThousandGrain: 25000,
				Collector:     "metrologist-e",
			},
			wantErr:              domain.CodeBadRequest,
			wantUnchangedOnError: true,
		},
		{
			name: "pure_grains_above_total_stays_bad_request_without_persisting_evidence",
			request: api.MoistureRequest{
				OperationID:   "moisture-pure-above-total",
				Moisture:      "12.50",
				PurityGrains:  101,
				TotalGrains:   100,
				ThousandGrain: 25000,
				Collector:     "metrologist-e",
			},
			wantErr:              domain.CodeBadRequest,
			wantUnchangedOnError: true,
		},
		{
			name: "purity_scale_multiplication_overflow_rejected_before_persisting_evidence",
			request: api.MoistureRequest{
				OperationID:   "moisture-overflow",
				Moisture:      "12.50",
				PurityGrains:  overflowGrains,
				TotalGrains:   overflowGrains,
				ThousandGrain: 25000,
				Collector:     "metrologist-e",
			},
			wantErr:              domain.CodeFixedPointOverflow,
			wantUnchangedOnError: true,
		},
	}

	for i, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			svc := seedService()
			id := driveToMoisture(t, svc, fmt.Sprintf("model-moisture-%d", i))

			before, derr := svc.GetTask(id)
			if derr != nil {
				t.Fatalf("get task before moisture: %v", derr)
			}
			if before.Task.Status != inspection.StatusMoisture {
				t.Fatalf("expected moisture state before write, got %s", before.Task.Status)
			}
			beforeSuccessAudits := countMoistureSuccessAudits(before)

			resp, gotErr := svc.RecordMoisture(id, tc.request)
			if tc.wantErr != "" {
				if gotErr == nil {
					t.Fatalf("expected %s, got nil response %+v", tc.wantErr, resp)
				}
				if gotErr.Code != tc.wantErr {
					t.Fatalf("expected %s, got %s", tc.wantErr, gotErr.Code)
				}

				after, derr := svc.GetTask(id)
				if derr != nil {
					t.Fatalf("get task after rejected moisture: %v", derr)
				}
				if tc.wantUnchangedOnError && after.Task.Status != before.Task.Status {
					t.Fatalf("status changed after rejected write: before %s after %s", before.Task.Status, after.Task.Status)
				}
				if tc.wantUnchangedOnError && after.Task.Generation != before.Task.Generation {
					t.Fatalf("generation changed after rejected write: before %d after %d", before.Task.Generation, after.Task.Generation)
				}
				if tc.wantUnchangedOnError && len(after.Moisture) != len(before.Moisture) {
					t.Fatalf("moisture evidence changed after rejected write: before %d after %d", len(before.Moisture), len(after.Moisture))
				}
				if got := countMoistureSuccessAudits(after); got != beforeSuccessAudits {
					t.Fatalf("moisture success audit count changed after rejected write: before %d after %d", beforeSuccessAudits, got)
				}
				return
			}

			if gotErr != nil {
				t.Fatalf("unexpected moisture error: %v", gotErr)
			}
			if resp.MoistureBp != tc.wantMoistureBp {
				t.Fatalf("expected moisture %d bp, got %d", tc.wantMoistureBp, resp.MoistureBp)
			}
			if resp.DerivedPurity != tc.wantDerivedPurityBp {
				t.Fatalf("expected derived purity %d bp, got %d", tc.wantDerivedPurityBp, resp.DerivedPurity)
			}
			if resp.Pass != tc.wantPass {
				t.Fatalf("expected pass %t, got %t", tc.wantPass, resp.Pass)
			}
			if resp.Status != tc.wantAdvancedStatus || !resp.Advanced {
				t.Fatalf("expected advance to %s, got status %s advanced=%t", tc.wantAdvancedStatus, resp.Status, resp.Advanced)
			}

			after, derr := svc.GetTask(id)
			if derr != nil {
				t.Fatalf("get task after accepted moisture: %v", derr)
			}
			if len(after.Moisture) != len(before.Moisture)+1 {
				t.Fatalf("expected one moisture evidence, got before %d after %d", len(before.Moisture), len(after.Moisture))
			}
			evidence := after.Moisture[len(after.Moisture)-1]
			if int64(evidence.DerivedPurity) != tc.wantDerivedPurityBp {
				t.Fatalf("persisted derived purity = %d, want %d", evidence.DerivedPurity, tc.wantDerivedPurityBp)
			}
			if evidence.PassThreshold != tc.wantPass {
				t.Fatalf("persisted pass threshold = %t, want %t", evidence.PassThreshold, tc.wantPass)
			}
		})
	}
}

package api_test

import (
	"testing"

	"riceguard/api"
	"riceguard/catalog"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/pathogen"
	"riceguard/store"
)

func TestModel_MoistureDeviceFaultsDoNotPersistEvidence(t *testing.T) {
	type want struct {
		errCode       domain.ErrorCode
		status        inspection.TaskStatus
		evidenceCount int
		moisture      measure.Fixed
	}

	driveToMoisture := func(t *testing.T, svc *api.Service, op string) string {
		t.Helper()
		resp, derr := svc.CreateTask(api.CreateTaskRequest{
			OperationID: op + "-create",
			SeedLot:     "lot-1001",
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
		})
		if derr != nil {
			t.Fatalf("create: %v", derr)
		}
		id := resp.TaskID
		for i, reviewer := range []string{"sampler-a", "sampler-b"} {
			if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
				OperationID: op + "-sampling-" + string(rune('0'+i)),
				Reviewer:    reviewer,
				Field:       "field-01",
				SeedLot:     "lot-1001",
				BlindSeal:   "seal-1",
				SampleCount: 180,
			}); derr != nil {
				t.Fatalf("sampling %s: %v", reviewer, derr)
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
		pathogenReading := int32(10)
		if _, derr := svc.RecordPathogen(id, api.PathogenRequest{
			OperationID: op + "-pathogen",
			BlindCode:   "b1",
			Plate:       "p-1",
			Well:        "w1",
			Verifier:    "pathologist-d",
			Reading:     &pathogenReading,
		}); derr != nil {
			t.Fatalf("pathogen: %v", derr)
		}
		return id
	}

	cases := []struct {
		name        string
		faults      []measure.MoistureDeviceStatus
		req         api.MoistureRequest
		want        want
		wantSuccess bool
	}{
		{
			name:   "refused_budget_exhausted",
			faults: []measure.MoistureDeviceStatus{measure.MoistureRefused, measure.MoistureRefused, measure.MoistureRefused},
			req: api.MoistureRequest{
				OperationID: "moist-refused", PurityGrains: 98, TotalGrains: 100,
				ThousandGrain: 25000, Collector: "metrologist-e",
			},
			want: want{errCode: domain.CodeDeviceRetryable, status: inspection.StatusMoisture},
		},
		{
			name:   "disconnect_budget_exhausted",
			faults: []measure.MoistureDeviceStatus{measure.MoistureDisconnect, measure.MoistureDisconnect, measure.MoistureDisconnect},
			req: api.MoistureRequest{
				OperationID: "moist-disconnect", PurityGrains: 98, TotalGrains: 100,
				ThousandGrain: 25000, Collector: "metrologist-e",
			},
			want: want{errCode: domain.CodeDeviceRetryable, status: inspection.StatusMoisture},
		},
		{
			name:   "timeout_budget_exhausted",
			faults: []measure.MoistureDeviceStatus{measure.MoistureTimeout, measure.MoistureTimeout, measure.MoistureTimeout},
			req: api.MoistureRequest{
				OperationID: "moist-timeout", PurityGrains: 98, TotalGrains: 100,
				ThousandGrain: 25000, Collector: "metrologist-e",
			},
			want: want{errCode: domain.CodeDeviceRetryable, status: inspection.StatusMoisture},
		},
		{
			name:   "bad_format_budget_exhausted",
			faults: []measure.MoistureDeviceStatus{measure.MoistureBadFormat, measure.MoistureBadFormat, measure.MoistureBadFormat},
			req: api.MoistureRequest{
				OperationID: "moist-bad-format", PurityGrains: 98, TotalGrains: 100,
				ThousandGrain: 25000, Collector: "metrologist-e",
			},
			want: want{errCode: domain.CodeDeviceRetryable, status: inspection.StatusMoisture},
		},
		{
			name:   "explicit_legal_moisture_bypasses_faulty_meter",
			faults: []measure.MoistureDeviceStatus{measure.MoistureTimeout, measure.MoistureTimeout, measure.MoistureTimeout},
			req: api.MoistureRequest{
				OperationID: "moist-explicit", Moisture: "12.50", PurityGrains: 98, TotalGrains: 100,
				ThousandGrain: 25000, Collector: "metrologist-e",
			},
			want:        want{status: inspection.StatusPendingReview, evidenceCount: 1, moisture: measure.Fixed(1250)},
			wantSuccess: true,
		},
		{
			name:   "transient_fault_then_meter_success",
			faults: []measure.MoistureDeviceStatus{measure.MoistureRefused, measure.MoistureTimeout},
			req: api.MoistureRequest{
				OperationID: "moist-transient", PurityGrains: 98, TotalGrains: 100,
				ThousandGrain: 25000, Collector: "metrologist-e",
			},
			want:        want{status: inspection.StatusPendingReview, evidenceCount: 1, moisture: measure.Fixed(1201)},
			wantSuccess: true,
		},
		{
			name: "invalid_purity_total_rejected_before_evidence",
			req: api.MoistureRequest{
				OperationID: "moist-bad-purity", Moisture: "12.50", PurityGrains: 98, TotalGrains: 0,
				ThousandGrain: 25000, Collector: "metrologist-e",
			},
			want: want{errCode: domain.CodeBadRequest, status: inspection.StatusMoisture},
		},
		{
			name: "invalid_thousand_grain_rejected_before_evidence",
			req: api.MoistureRequest{
				OperationID: "moist-bad-weight", Moisture: "12.50", PurityGrains: 98, TotalGrains: 100,
				ThousandGrain: 0, Collector: "metrologist-e",
			},
			want: want{errCode: domain.CodeFixedPointOverflow, status: inspection.StatusMoisture},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meter := measure.NewScriptedMeter()
			for _, fault := range tc.faults {
				meter.AddFault(fault)
			}
			cat, roles := catalog.Seed()
			svc := api.NewService(cat, roles, store.NewMemory(), pathogen.NewStaticAmplifier(), meter)
			id := driveToMoisture(t, svc, "model-"+tc.name)

			resp, derr := svc.RecordMoisture(id, tc.req)
			if tc.wantSuccess {
				if derr != nil {
					t.Fatalf("moisture: %v", derr)
				}
				if resp.Status != tc.want.status || !resp.Advanced {
					t.Fatalf("expected moisture response to advance to %s, got status=%s advanced=%v", tc.want.status, resp.Status, resp.Advanced)
				}
			} else {
				if derr == nil {
					t.Fatal("expected moisture rejection, got nil")
				}
				if derr.Code != tc.want.errCode {
					t.Fatalf("expected %s, got %s", tc.want.errCode, derr.Code)
				}
			}

			view, derr := svc.GetTask(id)
			if derr != nil {
				t.Fatalf("get task: %v", derr)
			}
			if view.Task.Status != tc.want.status {
				t.Fatalf("expected task status %s, got %s", tc.want.status, view.Task.Status)
			}
			if len(view.Moisture) != tc.want.evidenceCount {
				t.Fatalf("expected %d moisture evidence rows, got %d: %#v", tc.want.evidenceCount, len(view.Moisture), view.Moisture)
			}
			if tc.want.evidenceCount == 0 {
				return
			}
			got := view.Moisture[0]
			if got.Moisture != tc.want.moisture {
				t.Fatalf("expected moisture %d, got %d", tc.want.moisture, got.Moisture)
			}
			if !got.PassThreshold {
				t.Fatal("expected saved moisture/purity evidence to pass threshold")
			}
		})
	}
}

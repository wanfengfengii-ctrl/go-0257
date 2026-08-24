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

func TestModel_PathogenScriptedAmplifierRetryRecovery(t *testing.T) {
	read17 := int32(17)

	cases := []struct {
		name             string
		faults           []pathogen.DeviceStatus
		reading          *int32
		wantErr          domain.ErrorCode
		wantStatuses     []pathogen.DeviceStatus
		wantReading      int32
		wantPathogenRows int
		checkScriptHeld  bool
	}{
		{
			name:             "transient timeout is consumed then retry succeeds",
			faults:           []pathogen.DeviceStatus{pathogen.DeviceTimeout},
			wantStatuses:     []pathogen.DeviceStatus{pathogen.DeviceTimeout, pathogen.DeviceOk},
			wantPathogenRows: 1,
		},
		{
			name:             "distinct faults are consumed in script order",
			faults:           []pathogen.DeviceStatus{pathogen.DeviceTimeout, pathogen.DeviceDisconnect},
			wantStatuses:     []pathogen.DeviceStatus{pathogen.DeviceTimeout, pathogen.DeviceDisconnect, pathogen.DeviceOk},
			wantPathogenRows: 1,
		},
		{
			name:             "three real retryable faults remain retryable",
			faults:           []pathogen.DeviceStatus{pathogen.DeviceTimeout, pathogen.DeviceTimeout, pathogen.DeviceTimeout},
			wantErr:          domain.CodeDeviceRetryable,
			wantStatuses:     []pathogen.DeviceStatus{pathogen.DeviceTimeout, pathogen.DeviceTimeout, pathogen.DeviceTimeout},
			wantPathogenRows: 0,
		},
		{
			name:             "no fault reads successfully",
			wantStatuses:     []pathogen.DeviceStatus{pathogen.DeviceOk},
			wantPathogenRows: 1,
		},
		{
			name:             "explicit reading bypasses the device script",
			faults:           []pathogen.DeviceStatus{pathogen.DeviceTimeout},
			reading:          &read17,
			wantStatuses:     []pathogen.DeviceStatus{pathogen.DeviceOk},
			wantReading:      read17,
			wantPathogenRows: 1,
			checkScriptHeld:  true,
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat, roles := catalog.Seed()
			amp := pathogen.NewScriptedAmplifier()
			for _, fault := range tc.faults {
				amp.AddFault("p-1", "w1", fault)
			}
			st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "riceguard.db"))
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			t.Cleanup(func() {
				if err := st.Close(); err != nil {
					t.Fatalf("close sqlite: %v", err)
				}
			})
			svc := api.NewService(cat, roles, st, amp, measure.NewScriptedMeter())
			op := fmt.Sprintf("model-pathogen-%d", i)

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

			for j, reviewer := range []string{"sampler-a", "sampler-b"} {
				if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
					OperationID: fmt.Sprintf("%s-s%d", op, j+1),
					Reviewer:    reviewer,
					Field:       "field-01",
					SeedLot:     "lot-1001",
					BlindSeal:   "seal-1",
					SampleCount: 180,
				}); derr != nil {
					t.Fatalf("sampling %d: %v", j+1, derr)
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
					OperationID: fmt.Sprintf("%s-g%d", op, day),
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

			pathogenResp, gotErr := svc.RecordPathogen(id, api.PathogenRequest{
				OperationID: op + "-pathogen",
				BlindCode:   "b1",
				Plate:       "p-1",
				Well:        "w1",
				Verifier:    "pathologist-d",
				Reading:     tc.reading,
			})
			if tc.wantErr != "" {
				if gotErr == nil {
					t.Fatalf("expected %s, got nil", tc.wantErr)
				}
				if gotErr.Code != tc.wantErr {
					t.Fatalf("expected %s, got %s", tc.wantErr, gotErr.Code)
				}
			} else if gotErr != nil {
				t.Fatalf("pathogen read: %v", gotErr)
			} else if !pathogenResp.Advanced || pathogenResp.Status != inspection.StatusMoisture {
				t.Fatalf("expected pathogen read to advance to moisture, got advanced=%v status=%s", pathogenResp.Advanced, pathogenResp.Status)
			}

			attempts, derr := svc.ListAttempts(id)
			if derr != nil {
				t.Fatalf("list attempts: %v", derr)
			}
			if len(attempts) != len(tc.wantStatuses) {
				t.Fatalf("expected %d attempts, got %d", len(tc.wantStatuses), len(attempts))
			}
			for j, want := range tc.wantStatuses {
				if attempts[j].Attempt != j+1 {
					t.Fatalf("attempt %d has attempt number %d", j, attempts[j].Attempt)
				}
				if attempts[j].Status != want {
					t.Fatalf("attempt %d expected status %s, got %s", j+1, want, attempts[j].Status)
				}
				if (want != pathogen.DeviceOk) != attempts[j].Retryable {
					t.Fatalf("attempt %d retryable mismatch for status %s", j+1, attempts[j].Status)
				}
			}

			view, derr := svc.GetTask(id)
			if derr != nil {
				t.Fatalf("get task: %v", derr)
			}
			if len(view.Pathogen) != tc.wantPathogenRows {
				t.Fatalf("expected %d pathogen rows, got %d", tc.wantPathogenRows, len(view.Pathogen))
			}
			if tc.wantPathogenRows == 1 {
				if view.Pathogen[0].Reading <= 0 {
					t.Fatalf("expected a positive pathogen reading, got %d", view.Pathogen[0].Reading)
				}
				if tc.wantReading != 0 && view.Pathogen[0].Reading != tc.wantReading {
					t.Fatalf("expected pathogen reading %d, got %d", tc.wantReading, view.Pathogen[0].Reading)
				}
			}

			if tc.checkScriptHeld {
				_, err := amp.Read("p-1", "w1")
				if err == nil {
					t.Fatal("expected explicit reading to leave the scripted fault unconsumed")
				}
				if err.Code != domain.CodeDeviceRetryable || len(err.Reasons) == 0 || err.Reasons[0] != string(pathogen.DeviceTimeout) {
					t.Fatalf("expected held timeout fault, got %v", err)
				}
			}
		})
	}
}

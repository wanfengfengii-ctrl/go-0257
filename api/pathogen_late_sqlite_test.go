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

func TestModel_LatePathogenReadingDoesNotBlockCurrentSQLite(t *testing.T) {
	cases := []struct {
		name        string
		lateRead    int32
		currentRead int32
	}{
		{
			name:        "old generation read is isolated before current same well",
			lateRead:    12,
			currentRead: 10,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "riceguard.db")
			c, roles := catalog.Seed()
			st, err := store.OpenSQLite(dbPath)
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			defer st.Close()

			svc := api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())
			id := driveToPathogen(t, svc, "late-sqlite")
			before, derr := svc.GetTask(id)
			if derr != nil {
				t.Fatalf("get task before late read: %v", derr)
			}
			if before.Task.Generation < 2 {
				t.Fatalf("expected pathogen task generation >= 2, got %d", before.Task.Generation)
			}

			_, derr = svc.RecordPathogen(id, api.PathogenRequest{
				OperationID: "late-sqlite-old-pathogen",
				BlindCode:   "b1",
				Plate:       "p-1",
				Well:        "w1",
				Verifier:    "pathologist-d",
				Generation:  int64(before.Task.Generation - 1),
				Reading:     int32Ptr(tc.lateRead),
			})
			if derr != nil {
				t.Fatalf("late pathogen read should be isolated, got %s: %v", derr.Code, derr.Reasons)
			}

			pathogenRows, err := st.ListPathogen(inspection.TaskID(id))
			if err != nil {
				t.Fatalf("list pathogen after late read: %v", err)
			}
			if len(pathogenRows) != 1 || !pathogenRows[0].LateIsolated {
				t.Fatalf("expected one isolated late pathogen row, got %#v", pathogenRows)
			}

			currentResp, derr := svc.RecordPathogen(id, api.PathogenRequest{
				OperationID: "late-sqlite-current-pathogen",
				BlindCode:   "b1",
				Plate:       "p-1",
				Well:        "w1",
				Verifier:    "pathologist-d",
				Reading:     int32Ptr(tc.currentRead),
			})
			if derr != nil {
				if derr.Code == domain.CodeNotFound {
					t.Fatalf("current pathogen read reused the isolated late key and returned %s: %v", derr.Code, derr.Reasons)
				}
				t.Fatalf("current pathogen read should succeed after isolated late read, got %s: %v", derr.Code, derr.Reasons)
			}
			if !currentResp.Advanced || currentResp.Status != inspection.StatusMoisture {
				t.Fatalf("expected current read to advance to moisture, got advanced=%v status=%s", currentResp.Advanced, currentResp.Status)
			}

			pathogenRows, err = st.ListPathogen(inspection.TaskID(id))
			if err != nil {
				t.Fatalf("list pathogen after current read: %v", err)
			}
			var isolated, live int
			for _, row := range pathogenRows {
				if string(row.Plate) != "p-1" || string(row.Well) != "w1" {
					continue
				}
				if row.LateIsolated {
					isolated++
				} else {
					live++
				}
			}
			if isolated != 1 || live != 1 {
				t.Fatalf("expected isolated and live same-well rows to coexist, got isolated=%d live=%d rows=%#v", isolated, live, pathogenRows)
			}
		})
	}
}

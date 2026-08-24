package api_test

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"riceguard/api"
	"riceguard/catalog"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/occupancy"
	"riceguard/pathogen"
	"riceguard/store"
)

func TestModel_OccupancyLifecycleFreesSupersededSlots(t *testing.T) {
	newService := func(st store.Store) *api.Service {
		c, roles := catalog.Seed()
		return api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())
	}

	prepareForOccupy := func(t *testing.T, svc *api.Service, op, lot, blind, chamber string, start, end uint64, plate, well string) string {
		t.Helper()
		req := validCreate(op + "-create")
		req.SeedLot = lot
		req.BlindAllocs = []api.BlindAllocInput{{Code: blind, Germination: 100, Pathogen: 50, Moisture: 30}}
		req.Chamber = chamber
		req.ChamberStart = start
		req.ChamberEnd = end
		req.Plate = plate
		req.Wells = []string{well}

		resp, derr := svc.CreateTask(req)
		if derr != nil {
			t.Fatalf("%s create: %v", op, derr)
		}
		for i, reviewer := range []string{"sampler-a", "sampler-b"} {
			if _, derr := svc.ConfirmSampling(resp.TaskID, api.SamplingRequest{
				OperationID: fmt.Sprintf("%s-sampling-%d", op, i),
				Reviewer:    reviewer,
				Field:       req.Field,
				SeedLot:     lot,
				BlindSeal:   "seal-1",
				SampleCount: 180,
			}); derr != nil {
				t.Fatalf("%s sampling %d: %v", op, i, derr)
			}
		}
		if _, derr := svc.SplitBlindSamples(resp.TaskID, api.SplitRequest{OperationID: op + "-split"}); derr != nil {
			t.Fatalf("%s split: %v", op, derr)
		}
		return resp.TaskID
	}

	occupyTask := func(t *testing.T, svc *api.Service, id, op string) {
		t.Helper()
		resp, derr := svc.Occupy(id, api.OccupyRequest{OperationID: op + "-occupy"})
		if derr != nil {
			t.Fatalf("%s occupy: %v", op, derr)
		}
		if resp.Status != inspection.StatusGerminating {
			t.Fatalf("%s expected germinating after occupy, got %s", op, resp.Status)
		}
	}

	prepareAndOccupy := func(t *testing.T, svc *api.Service, op, lot, blind, chamber string, start, end uint64, plate, well string) string {
		t.Helper()
		id := prepareForOccupy(t, svc, op, lot, blind, chamber, start, end, plate, well)
		occupyTask(t, svc, id, op)
		return id
	}

	finishToReleasable := func(t *testing.T, svc *api.Service, id, op, blind, plate, well string) {
		t.Helper()
		for _, day := range []int32{2, 5, 8} {
			if _, derr := svc.RecordGermination(id, api.GerminationRequest{
				OperationID: fmt.Sprintf("%s-germination-%d", op, day),
				BlindCode:   blind,
				DayAge:      day,
				Normal:      95,
				Abnormal:    3,
				Dead:        2,
				Collector:   "germinator-c",
			}); derr != nil {
				t.Fatalf("%s germination day %d: %v", op, day, derr)
			}
		}
		reading := int32(10)
		if _, derr := svc.RecordPathogen(id, api.PathogenRequest{
			OperationID: op + "-pathogen",
			BlindCode:   blind,
			Plate:       plate,
			Well:        well,
			Verifier:    "pathologist-d",
			Reading:     &reading,
		}); derr != nil {
			t.Fatalf("%s pathogen: %v", op, derr)
		}
		if _, derr := svc.RecordMoisture(id, api.MoistureRequest{
			OperationID:   op + "-moisture",
			Moisture:      "12.50",
			PurityGrains:  98,
			TotalGrains:   100,
			ThousandGrain: 25000,
			Collector:     "metrologist-e",
		}); derr != nil {
			t.Fatalf("%s moisture: %v", op, derr)
		}
		for i, reviewer := range []string{"reviewer-f", "reviewer-g"} {
			if _, derr := svc.Review(id, api.ReviewRequest{
				OperationID: fmt.Sprintf("%s-review-%d", op, i),
				Reviewer:    reviewer,
				Conclusion:  "approve",
			}); derr != nil {
				t.Fatalf("%s review %d: %v", op, i, derr)
			}
		}
	}

	assertReleasedHistory := func(t *testing.T, svc *api.Service, st store.Store, id, reason string) {
		t.Helper()
		view, derr := svc.GetTask(id)
		if derr != nil {
			t.Fatalf("get released task %s: %v", id, derr)
		}
		released := 0
		for _, sl := range view.Occupancies {
			if occupancy.Active(sl) {
				t.Fatalf("task %s still has active occupancy in lifecycle view: %+v", id, sl)
			}
			if sl.Status == occupancy.StatusReleased && sl.ReleaseReason == reason {
				released++
			}
		}
		if released != 2 {
			t.Fatalf("task %s release history count = %d, want chamber and well releases", id, released)
		}
		open, err := st.ListOpenOccupancies()
		if err != nil {
			t.Fatalf("list open occupancies: %v", err)
		}
		for _, sl := range open {
			if sl.TaskID == inspection.TaskID(id) {
				t.Fatalf("released task %s is still reported as open: %+v", id, sl)
			}
		}
	}

	assertRechamberHistory := func(t *testing.T, svc *api.Service, st store.Store, id, oldChamber, newChamber string) {
		t.Helper()
		view, derr := svc.GetTask(id)
		if derr != nil {
			t.Fatalf("get rechambered task %s: %v", id, derr)
		}
		replaced := 0
		newOpen := 0
		wellOpen := 0
		for _, sl := range view.Occupancies {
			if sl.Chamber == occupancy.ChamberID(oldChamber) {
				if occupancy.Active(sl) {
					t.Fatalf("replaced chamber is still active in lifecycle view: %+v", sl)
				}
				if sl.Status == occupancy.StatusRechamber && sl.ReleaseReason == "rechambered" {
					replaced++
				}
			}
			if sl.Chamber == occupancy.ChamberID(newChamber) && occupancy.Active(sl) {
				newOpen++
			}
			if sl.Plate != "" && occupancy.Active(sl) {
				wellOpen++
			}
		}
		if replaced != 1 || newOpen != 1 || wellOpen != 1 {
			t.Fatalf("rechamber history = replaced:%d newOpen:%d wellOpen:%d, want 1/1/1", replaced, newOpen, wellOpen)
		}
		open, err := st.ListOpenOccupancies()
		if err != nil {
			t.Fatalf("list open occupancies: %v", err)
		}
		for _, sl := range open {
			if sl.TaskID == inspection.TaskID(id) && sl.Chamber == occupancy.ChamberID(oldChamber) {
				t.Fatalf("replaced chamber is still reported open: %+v", sl)
			}
		}
	}

	reuseOnce := func(t *testing.T, svc *api.Service, op, lot, blind, chamber string, start, end uint64, plate, well string) {
		t.Helper()
		prepareAndOccupy(t, svc, op, lot, blind, chamber, start, end, plate, well)
	}

	recontendFreedSlot := func(t *testing.T, svc *api.Service, prefix, chamber string, start, end uint64, plate, well string) {
		t.Helper()
		idA := prepareForOccupy(t, svc, prefix+"-a", "lot-"+prefix+"-a", "blind-"+prefix+"-a", chamber, start, end, plate, well)
		idB := prepareForOccupy(t, svc, prefix+"-b", "lot-"+prefix+"-b", "blind-"+prefix+"-b", chamber, start, end, plate, well)

		var wg sync.WaitGroup
		results := make([]*domain.Error, 2)
		startLine := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-startLine
			_, results[0] = svc.Occupy(idA, api.OccupyRequest{OperationID: prefix + "-a-occupy"})
		}()
		go func() {
			defer wg.Done()
			<-startLine
			_, results[1] = svc.Occupy(idB, api.OccupyRequest{OperationID: prefix + "-b-occupy"})
		}()
		close(startLine)
		wg.Wait()

		successes := 0
		conflicts := 0
		for _, result := range results {
			switch {
			case result == nil:
				successes++
			case result.Code == domain.CodeOccupancyConflict:
				conflicts++
			default:
				t.Fatalf("unexpected occupy result: %v", result)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("expected one concurrent winner and one conflict, got %d successes and %d conflicts", successes, conflicts)
		}
	}

	cases := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "cancelled task frees same chamber window and plate well for recontention",
			run: func(t *testing.T) {
				st := store.NewMemory()
				svc := newService(st)
				id := prepareAndOccupy(t, svc, "cancel-source", "lot-cancel-source", "blind-cancel-source", "ch-1", 100, 200, "p-1", "w1")
				if _, derr := svc.Cancel(id, api.CancelRequest{OperationID: "cancel-source-cancel", Reason: "operator cancelled"}); derr != nil {
					t.Fatalf("cancel source: %v", derr)
				}
				assertReleasedHistory(t, svc, st, id, "cancelled")
				recontendFreedSlot(t, svc, "cancel-reuse", "ch-1", 100, 200, "p-1", "w1")
			},
		},
		{
			name: "terminal release frees same chamber window and plate well",
			run: func(t *testing.T) {
				st := store.NewMemory()
				svc := newService(st)
				id := prepareAndOccupy(t, svc, "terminal-source", "lot-terminal-source", "blind-terminal-source", "ch-1", 100, 200, "p-1", "w1")
				finishToReleasable(t, svc, id, "terminal-source", "blind-terminal-source", "p-1", "w1")
				if _, derr := svc.Finalize(id, api.FinalizeRequest{OperationID: "terminal-source-finalize"}); derr != nil {
					t.Fatalf("finalize source: %v", derr)
				}
				assertReleasedHistory(t, svc, st, id, "released")
				reuseOnce(t, svc, "terminal-reuse", "lot-terminal-reuse", "blind-terminal-reuse", "ch-1", 100, 200, "p-1", "w1")
			},
		},
		{
			name: "rechamber frees replaced chamber while retaining current well",
			run: func(t *testing.T) {
				st := store.NewMemory()
				svc := newService(st)
				id := prepareAndOccupy(t, svc, "rechamber-source", "lot-rechamber-source", "blind-rechamber-source", "ch-1", 100, 200, "p-1", "w1")
				if _, derr := svc.Rechamber(id, api.RechamberRequest{
					OperationID:  "rechamber-source-move",
					Chamber:      "ch-2",
					ChamberStart: 300,
					ChamberEnd:   400,
				}); derr != nil {
					t.Fatalf("rechamber source: %v", derr)
				}
				assertRechamberHistory(t, svc, st, id, "ch-1", "ch-2")
				reuseOnce(t, svc, "rechamber-reuse", "lot-rechamber-reuse", "blind-rechamber-reuse", "ch-1", 100, 200, "p-2", "w2")
			},
		},
		{
			name: "sqlite restart keeps released slots closed before reuse",
			run: func(t *testing.T) {
				dbPath := filepath.Join(t.TempDir(), "riceguard.db")
				st, err := store.OpenSQLite(dbPath)
				if err != nil {
					t.Fatalf("open sqlite: %v", err)
				}
				svc := newService(st)
				id := prepareAndOccupy(t, svc, "sqlite-source", "lot-sqlite-source", "blind-sqlite-source", "ch-1", 100, 200, "p-1", "w1")
				if _, derr := svc.Cancel(id, api.CancelRequest{OperationID: "sqlite-source-cancel", Reason: "restart before reuse"}); derr != nil {
					t.Fatalf("cancel sqlite source: %v", derr)
				}
				if err := st.Close(); err != nil {
					t.Fatalf("close sqlite: %v", err)
				}

				reopened, err := store.OpenSQLite(dbPath)
				if err != nil {
					t.Fatalf("reopen sqlite: %v", err)
				}
				defer reopened.Close()
				recovered := newService(reopened)
				assertReleasedHistory(t, recovered, reopened, id, "cancelled")
				reuseOnce(t, recovered, "sqlite-reuse", "lot-sqlite-reuse", "blind-sqlite-reuse", "ch-1", 100, 200, "p-1", "w1")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

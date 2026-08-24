package api_test

import (
	"testing"

	"riceguard/api"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/occupancy"
)

func TestModel_RechamberPublicBoundary(t *testing.T) {
	type occupiedTask struct {
		op      string
		lot     string
		blind   string
		chamber string
		start   uint64
		end     uint64
		plate   string
		well    string
	}

	occupyTask := func(t *testing.T, svc *api.Service, spec occupiedTask) string {
		t.Helper()

		req := validCreate(spec.op + "-create")
		req.SeedLot = spec.lot
		req.BlindAllocs = []api.BlindAllocInput{
			{Code: spec.blind, Germination: 100, Pathogen: 50, Moisture: 30},
		}
		req.Chamber = spec.chamber
		req.ChamberStart = spec.start
		req.ChamberEnd = spec.end
		req.Plate = spec.plate
		req.Wells = []string{spec.well}

		resp, derr := svc.CreateTask(req)
		if derr != nil {
			t.Fatalf("create %s: %v", spec.op, derr)
		}
		id := resp.TaskID

		for i, reviewer := range []string{"sampler-a", "sampler-b"} {
			_, derr := svc.ConfirmSampling(id, api.SamplingRequest{
				OperationID: spec.op + "-sample-" + string(rune('0'+i)),
				Reviewer:    reviewer,
				Field:       "field-01",
				SeedLot:     spec.lot,
				BlindSeal:   "seal-" + spec.op,
				SampleCount: 180,
			})
			if derr != nil {
				t.Fatalf("sampling %s/%s: %v", spec.op, reviewer, derr)
			}
		}
		if _, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: spec.op + "-split"}); derr != nil {
			t.Fatalf("split %s: %v", spec.op, derr)
		}
		if _, derr := svc.Occupy(id, api.OccupyRequest{OperationID: spec.op + "-occupy"}); derr != nil {
			t.Fatalf("occupy %s: %v", spec.op, derr)
		}
		return id
	}

	reasonContains := func(reasons []string, want string) bool {
		for _, reason := range reasons {
			if reason == want {
				return true
			}
		}
		return false
	}

	activeChambers := func(view api.TaskView, chamber string, start, end uint64) int {
		count := 0
		for _, slot := range view.Occupancies {
			if !occupancy.Active(slot) || slot.Chamber == "" {
				continue
			}
			if string(slot.Chamber) == chamber && uint64(slot.Start) == start && uint64(slot.End) == end {
				count++
			}
		}
		return count
	}

	activeWells := func(view api.TaskView, plate, well string) int {
		count := 0
		for _, slot := range view.Occupancies {
			if !occupancy.Active(slot) || slot.Well == "" {
				continue
			}
			if string(slot.Plate) == plate && string(slot.Well) == well {
				count++
			}
		}
		return count
	}

	cases := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "conflicting move rejects and rolls back released slots task fields and operation record",
			run: func(t *testing.T) {
				svc := seedService()
				idA := occupyTask(t, svc, occupiedTask{
					op: "conflict-a", lot: "lot-conflict-a", blind: "blind-conflict-a",
					chamber: "ch-1", start: 100, end: 200, plate: "plate-conflict-a", well: "w1",
				})
				idB := occupyTask(t, svc, occupiedTask{
					op: "conflict-b", lot: "lot-conflict-b", blind: "blind-conflict-b",
					chamber: "ch-2", start: 100, end: 200, plate: "plate-conflict-b", well: "w2",
				})

				_, derr := svc.Rechamber(idA, api.RechamberRequest{
					OperationID: "conflict-move",
					Chamber:     "ch-2", ChamberStart: 100, ChamberEnd: 200,
				})
				if derr == nil {
					t.Fatal("expected occupied target chamber to reject rechamber")
				}
				if derr.Code != domain.CodeOccupancyConflict {
					t.Fatalf("expected CodeOccupancyConflict, got %s", derr.Code)
				}
				if !reasonContains(derr.Reasons, idB) || !reasonContains(derr.Reasons, "ch-2") {
					t.Fatalf("expected conflict reasons to include target chamber and other task, got %#v", derr.Reasons)
				}

				viewA, derr := svc.GetTask(idA)
				if derr != nil {
					t.Fatalf("get task A after conflict: %v", derr)
				}
				if viewA.Task.Chamber != "ch-1" || viewA.Task.ChamberStart != 100 || viewA.Task.ChamberEnd != 200 {
					t.Fatalf("task A chamber changed after rejected move: %#v", viewA.Task)
				}
				if got := activeChambers(viewA, "ch-1", 100, 200); got != 1 {
					t.Fatalf("expected task A old chamber slot to remain active once, got %d", got)
				}
				if got := activeChambers(viewA, "ch-2", 100, 200); got != 0 {
					t.Fatalf("expected no active target chamber slot for rejected move, got %d", got)
				}
				if got := activeWells(viewA, "plate-conflict-a", "w1"); got != 1 {
					t.Fatalf("expected task A well slot to remain active once, got %d", got)
				}

				viewB, derr := svc.GetTask(idB)
				if derr != nil {
					t.Fatalf("get task B after conflict: %v", derr)
				}
				if got := activeChambers(viewB, "ch-2", 100, 200); got != 1 {
					t.Fatalf("expected task B target chamber slot to remain active once, got %d", got)
				}
				if got := activeWells(viewB, "plate-conflict-b", "w2"); got != 1 {
					t.Fatalf("expected task B well slot to remain active once, got %d", got)
				}

				resp, derr := svc.Rechamber(idA, api.RechamberRequest{
					OperationID: "conflict-move",
					Chamber:     "ch-3", ChamberStart: 210, ChamberEnd: 260,
				})
				if derr != nil {
					t.Fatalf("expected failed rechamber operation id to be reusable, got %v", derr)
				}
				if resp.Chamber != "ch-3" {
					t.Fatalf("expected retry into idle chamber to report ch-3, got %s", resp.Chamber)
				}
			},
		},
		{
			name: "idle replacement succeeds",
			run: func(t *testing.T) {
				svc := seedService()
				id := occupyTask(t, svc, occupiedTask{
					op: "idle-a", lot: "lot-idle-a", blind: "blind-idle-a",
					chamber: "ch-idle-old", start: 300, end: 360, plate: "plate-idle-a", well: "w1",
				})

				resp, derr := svc.Rechamber(id, api.RechamberRequest{
					OperationID: "idle-move",
					Chamber:     "ch-idle-new", ChamberStart: 420, ChamberEnd: 480,
				})
				if derr != nil {
					t.Fatalf("idle rechamber should succeed: %v", derr)
				}
				if resp.Chamber != "ch-idle-new" {
					t.Fatalf("expected ch-idle-new, got %s", resp.Chamber)
				}

				view, derr := svc.GetTask(id)
				if derr != nil {
					t.Fatalf("get idle task: %v", derr)
				}
				if view.Task.Chamber != "ch-idle-new" || view.Task.ChamberStart != 420 || view.Task.ChamberEnd != 480 {
					t.Fatalf("task did not move to idle chamber window: %#v", view.Task)
				}
			},
		},
		{
			name: "same task overlapping its old chamber window does not self conflict",
			run: func(t *testing.T) {
				svc := seedService()
				id := occupyTask(t, svc, occupiedTask{
					op: "self-a", lot: "lot-self-a", blind: "blind-self-a",
					chamber: "ch-self", start: 500, end: 600, plate: "plate-self-a", well: "w1",
				})

				resp, derr := svc.Rechamber(id, api.RechamberRequest{
					OperationID: "self-move",
					Chamber:     "ch-self", ChamberStart: 550, ChamberEnd: 650,
				})
				if derr != nil {
					t.Fatalf("same task replacement should not conflict with its old slot: %v", derr)
				}
				if resp.Chamber != "ch-self" {
					t.Fatalf("expected ch-self, got %s", resp.Chamber)
				}

				view, derr := svc.GetTask(id)
				if derr != nil {
					t.Fatalf("get self task: %v", derr)
				}
				if view.Task.ChamberStart != 550 || view.Task.ChamberEnd != 650 {
					t.Fatalf("task did not replace its old chamber window: %#v", view.Task)
				}
			},
		},
		{
			name: "terminal task cannot rechamber",
			run: func(t *testing.T) {
				svc := seedService()
				id := driveToReleasable(t, svc, "terminal-rechamber")
				final, derr := svc.Finalize(id, api.FinalizeRequest{OperationID: "terminal-rechamber-final"})
				if derr != nil {
					t.Fatalf("finalize: %v", derr)
				}
				if final.Status != inspection.StatusReleased {
					t.Fatalf("expected released terminal status, got %s", final.Status)
				}

				_, derr = svc.Rechamber(id, api.RechamberRequest{
					OperationID: "terminal-rechamber-move",
					Chamber:     "ch-terminal", ChamberStart: 700, ChamberEnd: 760,
				})
				if derr == nil {
					t.Fatal("expected terminal rechamber rejection")
				}
				if derr.Code != domain.CodeFinalized {
					t.Fatalf("expected CodeFinalized, got %s", derr.Code)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

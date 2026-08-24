package api_test

import (
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

func TestModel_RechamberRejectsOccupiedTargetAndPreservesOpenSlots(t *testing.T) {
	newService := func() *api.Service {
		c, roles := catalog.Seed()
		return api.NewService(c, roles, store.NewMemory(), pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())
	}

	type taskSpec struct {
		op      string
		lot     string
		blind   string
		chamber string
		start   uint64
		end     uint64
		plate   string
		well    string
	}

	createOccupiedTask := func(t *testing.T, svc *api.Service, spec taskSpec) string {
		t.Helper()

		create := api.CreateTaskRequest{
			OperationID: spec.op + "-create",
			SeedLot:     spec.lot,
			Field:       "field-01",
			Variety:     "xiangliangyou-900",
			FemaleCert:  3,
			MaleCert:    3,
			BlindAllocs: []api.BlindAllocInput{
				{Code: spec.blind, Germination: 100, Pathogen: 50, Moisture: 30},
			},
			Chamber:        spec.chamber,
			ChamberStart:   spec.start,
			ChamberEnd:     spec.end,
			Plate:          spec.plate,
			Wells:          []string{spec.well},
			ReviewerRoster: []string{"reviewer-f", "reviewer-g"},
		}
		resp, derr := svc.CreateTask(create)
		if derr != nil {
			t.Fatalf("create %s: %v", spec.op, derr)
		}

		for _, reviewer := range []string{"sampler-a", "sampler-b"} {
			_, derr := svc.ConfirmSampling(resp.TaskID, api.SamplingRequest{
				OperationID: spec.op + "-sampling-" + reviewer,
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
		if _, derr := svc.SplitBlindSamples(resp.TaskID, api.SplitRequest{OperationID: spec.op + "-split"}); derr != nil {
			t.Fatalf("split %s: %v", spec.op, derr)
		}
		if _, derr := svc.Occupy(resp.TaskID, api.OccupyRequest{OperationID: spec.op + "-occupy"}); derr != nil {
			t.Fatalf("occupy %s: %v", spec.op, derr)
		}
		return resp.TaskID
	}

	activeChamberSlots := func(view api.TaskView, chamber string, start, end uint64) int {
		count := 0
		for _, slot := range view.Occupancies {
			if occupancy.Active(slot) && string(slot.Chamber) == chamber &&
				uint64(slot.Start) == start && uint64(slot.End) == end {
				count++
			}
		}
		return count
	}

	activeWellSlots := func(view api.TaskView, plate, well string) int {
		count := 0
		for _, slot := range view.Occupancies {
			if occupancy.Active(slot) && string(slot.Plate) == plate && string(slot.Well) == well {
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
			name: "conflicting target rolls back release write task fields and idempotency",
			run: func(t *testing.T) {
				svc := newService()
				movingID := createOccupiedTask(t, svc, taskSpec{
					op: "conflict-moving", lot: "lot-conflict-moving", blind: "blind-conflict-moving",
					chamber: "ch-source", start: 10, end: 20, plate: "plate-moving", well: "w1",
				})
				blockingID := createOccupiedTask(t, svc, taskSpec{
					op: "conflict-blocking", lot: "lot-conflict-blocking", blind: "blind-conflict-blocking",
					chamber: "ch-target", start: 10, end: 20, plate: "plate-blocking", well: "w2",
				})

				_, derr := svc.Rechamber(movingID, api.RechamberRequest{
					OperationID:  "conflict-rechamber",
					Chamber:      "ch-target",
					ChamberStart: 10,
					ChamberEnd:   20,
				})
				if derr == nil {
					t.Fatal("expected rechamber onto another open task to be rejected")
				}
				if derr.Code != domain.CodeOccupancyConflict {
					t.Fatalf("expected %s, got %s", domain.CodeOccupancyConflict, derr.Code)
				}

				movingView, derr := svc.GetTask(movingID)
				if derr != nil {
					t.Fatalf("get moving task after conflict: %v", derr)
				}
				if movingView.Task.Chamber != "ch-source" || movingView.Task.ChamberStart != 10 || movingView.Task.ChamberEnd != 20 {
					t.Fatalf("moving task chamber fields changed after rejection: %#v", movingView.Task)
				}
				if got := activeChamberSlots(movingView, "ch-source", 10, 20); got != 1 {
					t.Fatalf("expected old chamber slot to remain active once, got %d", got)
				}
				if got := activeChamberSlots(movingView, "ch-target", 10, 20); got != 0 {
					t.Fatalf("expected rejected target slot not to be written, got %d", got)
				}
				if got := activeWellSlots(movingView, "plate-moving", "w1"); got != 1 {
					t.Fatalf("expected moving task well slot to remain active once, got %d", got)
				}

				blockingView, derr := svc.GetTask(blockingID)
				if derr != nil {
					t.Fatalf("get blocking task after conflict: %v", derr)
				}
				if got := activeChamberSlots(blockingView, "ch-target", 10, 20); got != 1 {
					t.Fatalf("expected other task chamber slot to remain active once, got %d", got)
				}
				if got := activeWellSlots(blockingView, "plate-blocking", "w2"); got != 1 {
					t.Fatalf("expected other task well slot to remain active once, got %d", got)
				}

				if _, derr := svc.Rechamber(movingID, api.RechamberRequest{
					OperationID:  "conflict-rechamber",
					Chamber:      "ch-idle",
					ChamberStart: 30,
					ChamberEnd:   40,
				}); derr != nil {
					t.Fatalf("expected failed operation id to be reusable for an idle move, got %v", derr)
				}
			},
		},
		{
			name: "idle and self-overlapping replacement keep the task well occupied",
			run: func(t *testing.T) {
				svc := newService()
				id := createOccupiedTask(t, svc, taskSpec{
					op: "self-replace", lot: "lot-self-replace", blind: "blind-self-replace",
					chamber: "ch-self", start: 50, end: 70, plate: "plate-self", well: "w1",
				})

				if _, derr := svc.Rechamber(id, api.RechamberRequest{
					OperationID:  "self-replace-rechamber",
					Chamber:      "ch-self",
					ChamberStart: 60,
					ChamberEnd:   80,
				}); derr != nil {
					t.Fatalf("expected overlapping replacement of the same task's old chamber slot to succeed: %v", derr)
				}

				view, derr := svc.GetTask(id)
				if derr != nil {
					t.Fatalf("get self-replaced task: %v", derr)
				}
				if got := activeChamberSlots(view, "ch-self", 50, 70); got != 0 {
					t.Fatalf("expected old chamber window to be closed, got %d active slots", got)
				}
				if got := activeChamberSlots(view, "ch-self", 60, 80); got != 1 {
					t.Fatalf("expected replacement chamber window to be active once, got %d", got)
				}
				if got := activeWellSlots(view, "plate-self", "w1"); got != 1 {
					t.Fatalf("expected well slot to remain active after rechamber, got %d", got)
				}
			},
		},
		{
			name: "terminal tasks cannot rechamber",
			run: func(t *testing.T) {
				svc := newService()
				id := createOccupiedTask(t, svc, taskSpec{
					op: "terminal", lot: "lot-terminal", blind: "blind-terminal",
					chamber: "ch-terminal-old", start: 90, end: 100, plate: "plate-terminal", well: "w1",
				})
				cancelled, derr := svc.Cancel(id, api.CancelRequest{
					OperationID: "terminal-cancel",
					Reason:      "operator cancelled before release",
				})
				if derr != nil {
					t.Fatalf("cancel terminal case: %v", derr)
				}
				if cancelled.Status != inspection.StatusCancelled {
					t.Fatalf("expected cancelled status, got %s", cancelled.Status)
				}

				_, derr = svc.Rechamber(id, api.RechamberRequest{
					OperationID:  "terminal-rechamber",
					Chamber:      "ch-terminal-new",
					ChamberStart: 110,
					ChamberEnd:   120,
				})
				if derr == nil {
					t.Fatal("expected terminal rechamber to be rejected")
				}
				if derr.Code != domain.CodeFinalized {
					t.Fatalf("expected %s, got %s", domain.CodeFinalized, derr.Code)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

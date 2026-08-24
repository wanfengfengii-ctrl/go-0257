package api_test

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"riceguard/api"
	"riceguard/catalog"
	"riceguard/measure"
	"riceguard/pathogen"
	"riceguard/store"
)

func TestModel_SQLiteLogicalClockRestartRecovery(t *testing.T) {
	newService := func(t *testing.T, dbPath string) (*api.Service, *store.SQLite) {
		t.Helper()
		c, roles := catalog.Seed()
		st, err := store.OpenSQLite(dbPath)
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		return api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter()), st
	}

	createRequest := func(op, lot, blindCode, chamber string, start, end uint64, plate, well string) api.CreateTaskRequest {
		return api.CreateTaskRequest{
			OperationID: op,
			SeedLot:     lot,
			Field:       "field-01",
			Variety:     "xiangliangyou-900",
			FemaleCert:  3,
			MaleCert:    3,
			BlindAllocs: []api.BlindAllocInput{
				{Code: blindCode, Germination: 100, Pathogen: 50, Moisture: 30},
			},
			Chamber:        chamber,
			ChamberStart:   start,
			ChamberEnd:     end,
			Plate:          plate,
			Wells:          []string{well},
			ReviewerRoster: []string{"reviewer-f", "reviewer-g"},
		}
	}

	driveToOccupied := func(t *testing.T, svc *api.Service, id, opPrefix, lot string) {
		t.Helper()
		if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
			OperationID: opPrefix + "-sample-a",
			Reviewer:    "sampler-a",
			Field:       "field-01",
			SeedLot:     lot,
			BlindSeal:   opPrefix + "-seal",
			SampleCount: 180,
		}); derr != nil {
			t.Fatalf("sampling a: %v", derr)
		}
		if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
			OperationID: opPrefix + "-sample-b",
			Reviewer:    "sampler-b",
			Field:       "field-01",
			SeedLot:     lot,
			BlindSeal:   opPrefix + "-seal",
			SampleCount: 180,
		}); derr != nil {
			t.Fatalf("sampling b: %v", derr)
		}
		if _, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: opPrefix + "-split"}); derr != nil {
			t.Fatalf("split: %v", derr)
		}
		if _, derr := svc.Occupy(id, api.OccupyRequest{OperationID: opPrefix + "-occupy"}); derr != nil {
			t.Fatalf("occupy: %v", derr)
		}
	}

	createAndOccupy := func(t *testing.T, svc *api.Service, opPrefix, lot, blindCode, chamber string, start, end uint64, plate, well string) (string, api.TaskView) {
		t.Helper()
		resp, derr := svc.CreateTask(createRequest(opPrefix+"-create", lot, blindCode, chamber, start, end, plate, well))
		if derr != nil {
			t.Fatalf("create: %v", derr)
		}
		driveToOccupied(t, svc, resp.TaskID, opPrefix, lot)
		view, derr := svc.GetTask(resp.TaskID)
		if derr != nil {
			t.Fatalf("get occupied task: %v", derr)
		}
		return resp.TaskID, view
	}

	maxAuditTime := func(t *testing.T, svc *api.Service) (uint64, int) {
		t.Helper()
		events, derr := svc.ListAllAudit()
		if derr != nil {
			t.Fatalf("list audit: %v", derr)
		}
		var max uint64
		for _, event := range events {
			if logical := uint64(event.LogicalTime); logical > max {
				max = logical
			}
		}
		if len(events) == 0 || max == 0 {
			t.Fatalf("expected persisted audit history, got %d events with max %d", len(events), max)
		}
		return max, len(events)
	}

	maxChamberEnd := func(t *testing.T, view api.TaskView) uint64 {
		t.Helper()
		var max uint64
		for _, slot := range view.Occupancies {
			if slot.Chamber == "" {
				continue
			}
			if end := uint64(slot.End); end > max {
				max = end
			}
		}
		if max == 0 {
			t.Fatalf("expected chamber occupancy for %s", view.Task.ID)
		}
		return max
	}

	taskOrdinal := func(t *testing.T, id string) uint64 {
		t.Helper()
		raw, ok := strings.CutPrefix(id, "task-")
		if !ok {
			t.Fatalf("task id %q does not have task- prefix", id)
		}
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			t.Fatalf("parse task id %q: %v", id, err)
		}
		return n
	}

	cases := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "empty database initializes at zero clock",
			run: func(t *testing.T) {
				dbPath := filepath.Join(t.TempDir(), "empty.db")
				st, err := store.OpenSQLite(dbPath)
				if err != nil {
					t.Fatalf("open empty sqlite: %v", err)
				}
				defer st.Close()
				if got := uint64(st.NextTime()); got != 1 {
					t.Fatalf("first logical tick from empty sqlite = %d, want 1", got)
				}
			},
		},
		{
			name: "post restart create keeps task audit and occupancy history independent",
			run: func(t *testing.T) {
				dbPath := filepath.Join(t.TempDir(), "riceguard.db")
				svc, st := newService(t, dbPath)

				beforeID, beforeView := createAndOccupy(t, svc, "model-before", "lot-before-restart", "b-before", "ch-before", 100, 200, "p-before", "w1")
				beforeAuditMax, beforeAuditCount := maxAuditTime(t, svc)
				beforeOccupancyMax := maxChamberEnd(t, beforeView)
				if beforeAuditMax <= uint64(beforeView.Task.CreatedAt) {
					t.Fatalf("setup did not advance audit clock beyond create time: audit max %d created_at %d", beforeAuditMax, beforeView.Task.CreatedAt)
				}
				if rec, ok := st.FindOperation("model-before-create"); !ok || string(rec.TaskID) != beforeID {
					t.Fatalf("before create operation not recorded against %s: ok=%v rec=%+v", beforeID, ok, rec)
				}
				if err := st.Close(); err != nil {
					t.Fatalf("close before restart: %v", err)
				}

				svc2, st2 := newService(t, dbPath)
				defer st2.Close()
				afterStart := beforeOccupancyMax + 100
				afterResp, derr := svc2.CreateTask(createRequest("model-after-create", "lot-after-restart", "b-after", "ch-after", afterStart, afterStart+100, "p-after", "w2"))
				if derr != nil {
					t.Fatalf("create after restart: %v", derr)
				}
				if afterResp.TaskID == beforeID {
					t.Fatalf("post-restart create reused existing task id %s", afterResp.TaskID)
				}
				if got := taskOrdinal(t, afterResp.TaskID); got <= beforeAuditMax {
					t.Fatalf("post-restart task id %s did not continue after audit clock %d", afterResp.TaskID, beforeAuditMax)
				}

				afterCreateView, derr := svc2.GetTask(afterResp.TaskID)
				if derr != nil {
					t.Fatalf("get task after restart create: %v", derr)
				}
				if afterCreateView.Task.SeedLot != "lot-after-restart" {
					t.Fatalf("post-restart task points at seed lot %q, want lot-after-restart", afterCreateView.Task.SeedLot)
				}
				if uint64(afterCreateView.Task.CreatedAt) <= beforeAuditMax {
					t.Fatalf("post-restart created_at %d did not continue after audit clock %d", afterCreateView.Task.CreatedAt, beforeAuditMax)
				}

				beforeAgain, derr := svc2.GetTask(beforeID)
				if derr != nil {
					t.Fatalf("get pre-restart task: %v", derr)
				}
				if beforeAgain.Task.SeedLot != beforeView.Task.SeedLot || beforeAgain.Task.Status != beforeView.Task.Status {
					t.Fatalf("pre-restart task was overwritten: before lot/status %s/%s after %s/%s",
						beforeView.Task.SeedLot, beforeView.Task.Status, beforeAgain.Task.SeedLot, beforeAgain.Task.Status)
				}
				if rec, ok := st2.FindOperation("model-before-create"); !ok || string(rec.TaskID) != beforeID {
					t.Fatalf("before create operation changed after restart: ok=%v rec=%+v", ok, rec)
				}
				if rec, ok := st2.FindOperation("model-after-create"); !ok || string(rec.TaskID) != afterResp.TaskID {
					t.Fatalf("after create operation not recorded against %s: ok=%v rec=%+v", afterResp.TaskID, ok, rec)
				}

				driveToOccupied(t, svc2, afterResp.TaskID, "model-after", "lot-after-restart")
				afterView, derr := svc2.GetTask(afterResp.TaskID)
				if derr != nil {
					t.Fatalf("get post-restart occupied task: %v", derr)
				}
				foundChamber := false
				for _, slot := range afterView.Occupancies {
					if slot.Chamber == "" {
						continue
					}
					foundChamber = true
					if uint64(slot.Start) <= beforeOccupancyMax || uint64(slot.End) <= beforeOccupancyMax {
						t.Fatalf("post-restart occupancy window [%d,%d] did not continue after historical max %d", slot.Start, slot.End, beforeOccupancyMax)
					}
				}
				if !foundChamber {
					t.Fatalf("post-restart task %s has no chamber occupancy", afterResp.TaskID)
				}

				events, derr := svc2.ListAllAudit()
				if derr != nil {
					t.Fatalf("list audit after restart: %v", derr)
				}
				if len(events) <= beforeAuditCount {
					t.Fatalf("no post-restart audit events appended: before %d after %d", beforeAuditCount, len(events))
				}
				for _, event := range events[beforeAuditCount:] {
					if uint64(event.LogicalTime) <= beforeAuditMax {
						t.Fatalf("post-restart audit logical time %d did not continue after historical max %d", event.LogicalTime, beforeAuditMax)
					}
				}

				tasks, derr := svc2.ListTasks()
				if derr != nil {
					t.Fatalf("list tasks: %v", derr)
				}
				if len(tasks) != 2 {
					t.Fatalf("expected independent pre- and post-restart tasks, got %d", len(tasks))
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

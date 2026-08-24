package api_test

import (
	"errors"
	"path/filepath"
	"testing"

	"riceguard/api"
	"riceguard/blindcode"
	"riceguard/catalog"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/occupancy"
	"riceguard/pathogen"
	"riceguard/review"
	"riceguard/store"
)

func TestModel_SQLiteMutateRollsBackOnReturnedErrors(t *testing.T) {
	rollbackErr := errors.New("force sqlite rollback")

	newSQLite := func(t *testing.T) *store.SQLite {
		t.Helper()
		st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "riceguard.db"))
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		t.Cleanup(func() {
			if err := st.Close(); err != nil {
				t.Fatalf("close sqlite: %v", err)
			}
		})
		return st
	}

	newSQLiteService := func(t *testing.T) (*api.Service, *store.SQLite) {
		t.Helper()
		c, roles := catalog.Seed()
		st := newSQLite(t)
		return api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter()), st
	}

	seedTask := func(t *testing.T, st *store.SQLite, id inspection.TaskID, status inspection.TaskStatus) {
		t.Helper()
		err := st.Mutate(func(tx store.Tx) error {
			return tx.SaveTask(&inspection.InspectionTask{
				ID: id, SeedLot: string(id) + "-lot", Field: catalog.FieldID("field-01"),
				Variety: "xiangliangyou-900", FemaleParent: "female", MaleParent: "male",
				FemaleCert: 3, MaleCert: 3, CertSummary: "ok", Status: status, Generation: 7,
				MoistureMax: 1300, PathogenMax: 35, MinPurity: 9600, GrainCount: 100,
				Chamber: "ch-seed", ChamberStart: 10, ChamberEnd: 20, Plate: "p-seed",
				Wells: []string{"w1"}, DayAges: []int32{2, 5, 8},
				BlindAllocs: []inspection.BlindAllocation{
					{Code: "b1", Germination: 100, Pathogen: 50, Moisture: 30},
				},
				ReviewerRoster: []string{"reviewer-f", "reviewer-g"}, CreatedAt: 1,
			})
		})
		if err != nil {
			t.Fatalf("seed task: %v", err)
		}
	}

	assertRollbackErr := func(t *testing.T, err error) {
		t.Helper()
		if !errors.Is(err, rollbackErr) {
			t.Fatalf("expected rollback marker, got %v", err)
		}
	}

	assertNoOperation := func(t *testing.T, st *store.SQLite, op string) {
		t.Helper()
		if _, ok := st.FindOperation(op); ok {
			t.Fatalf("operation %q was committed despite rollback", op)
		}
	}

	cases := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "failed_rechamber_conflict_preserves_existing_chamber_history",
			run: func(t *testing.T) {
				svc, st := newSQLiteService(t)
				createOccupied := func(op, lot, blind, chamber, plate, well string) string {
					t.Helper()
					req := validCreate(op + "-create")
					req.SeedLot = lot
					req.BlindAllocs = []api.BlindAllocInput{
						{Code: blind, Germination: 100, Pathogen: 50, Moisture: 30},
					}
					req.Chamber = chamber
					req.Plate = plate
					req.Wells = []string{well}
					resp, derr := svc.CreateTask(req)
					if derr != nil {
						t.Fatalf("create %s: %v", op, derr)
					}
					for i, reviewer := range []string{"sampler-a", "sampler-b"} {
						_, derr = svc.ConfirmSampling(resp.TaskID, api.SamplingRequest{
							OperationID: op + "-sample-" + string(rune('0'+i)),
							Reviewer:    reviewer, Field: req.Field, SeedLot: req.SeedLot,
							BlindSeal: "seal-1", SampleCount: 180,
						})
						if derr != nil {
							t.Fatalf("sampling %s: %v", op, derr)
						}
					}
					if _, derr = svc.SplitBlindSamples(resp.TaskID, api.SplitRequest{OperationID: op + "-split"}); derr != nil {
						t.Fatalf("split %s: %v", op, derr)
					}
					if _, derr = svc.Occupy(resp.TaskID, api.OccupyRequest{OperationID: op + "-occupy"}); derr != nil {
						t.Fatalf("occupy %s: %v", op, derr)
					}
					return resp.TaskID
				}

				id := createOccupied("rch-a", "lot-rch-a", "blind-rch-a", "ch-rch-a", "plate-rch-a", "w-a")
				other := createOccupied("rch-b", "lot-rch-b", "blind-rch-b", "ch-rch-b", "plate-rch-b", "w-b")

				otherView, derr := svc.GetTask(other)
				if derr != nil {
					t.Fatalf("get conflicting task: %v", derr)
				}
				before, derr := svc.GetTask(id)
				if derr != nil {
					t.Fatalf("get task before rechamber: %v", derr)
				}

				_, derr = svc.Rechamber(id, api.RechamberRequest{
					OperationID:  "rch-conflict",
					Chamber:      otherView.Task.Chamber,
					ChamberStart: otherView.Task.ChamberStart,
					ChamberEnd:   otherView.Task.ChamberEnd,
				})
				if derr == nil || derr.Code != domain.CodeOccupancyConflict {
					t.Fatalf("expected occupancy conflict, got %v", derr)
				}

				after, derr := svc.GetTask(id)
				if derr != nil {
					t.Fatalf("get task after rechamber: %v", derr)
				}
				if after.Task.Chamber != before.Task.Chamber ||
					after.Task.ChamberStart != before.Task.ChamberStart ||
					after.Task.ChamberEnd != before.Task.ChamberEnd ||
					after.Task.Status != before.Task.Status ||
					after.Task.Generation != before.Task.Generation {
					t.Fatalf("failed rechamber changed task aggregate: before %+v after %+v", before.Task, after.Task)
				}
				if len(after.Occupancies) != len(before.Occupancies) {
					t.Fatalf("failed rechamber changed occupancy history length: before %d after %d", len(before.Occupancies), len(after.Occupancies))
				}
				for _, slot := range after.Occupancies {
					if slot.Status == occupancy.StatusRechamber || slot.ReleaseReason == "rechambered" {
						t.Fatalf("failed rechamber left a release record: %+v", slot)
					}
				}
				assertNoOperation(t, st, "rch-conflict")
			},
		},
		{
			name: "failed_evidence_mutation_rolls_back_readings_attempts_operations_and_audit",
			run: func(t *testing.T) {
				st := newSQLite(t)
				taskID := inspection.TaskID("task-evidence-rollback")
				seedTask(t, st, taskID, inspection.StatusPathogen)

				err := st.Mutate(func(tx store.Tx) error {
					if err := tx.SavePathogen(pathogen.PathogenEvidence{
						TaskID: taskID, BlindCode: blindcode.BlindCode("b1"), Plate: occupancy.PlateID("p-seed"),
						Well: "w1", Reading: 12, Verdict: pathogen.VerdictNegative,
						DeviceStatus: pathogen.DeviceOk, Verifier: "pathologist-d",
					}); err != nil {
						return err
					}
					if err := tx.SaveAttempt(pathogen.Attempt{
						TaskID: taskID, Plate: "p-seed", Well: "w1", Attempt: 1,
						Status: pathogen.DeviceOk, Reading: 12, LogicalSeq: 1,
					}); err != nil {
						return err
					}
					if err := tx.RecordOperation(inspection.NewRecord("evidence-rollback-op", taskID, 7, "request", "response")); err != nil {
						return err
					}
					if err := tx.AppendAudit(inspection.AuditEvent{
						TaskID: taskID, LogicalTime: 2, Actor: "pathologist-d",
						TaskStatus: inspection.StatusPathogen, Action: "pathogen_reading",
						Code: domain.CodeNone, BlindCodes: []string{"b1"}, PlateWells: []string{"p-seed/w1"},
					}); err != nil {
						return err
					}
					return rollbackErr
				})
				assertRollbackErr(t, err)

				if got, err := st.ListPathogen(taskID); err != nil || len(got) != 0 {
					t.Fatalf("pathogen evidence persisted after rollback: len=%d err=%v", len(got), err)
				}
				if got, err := st.ListAttempts(taskID); err != nil || len(got) != 0 {
					t.Fatalf("attempts persisted after rollback: len=%d err=%v", len(got), err)
				}
				if got, err := st.ListAudit(taskID); err != nil || len(got) != 0 {
					t.Fatalf("audit persisted after rollback: len=%d err=%v", len(got), err)
				}
				assertNoOperation(t, st, "evidence-rollback-op")
			},
		},
		{
			name: "failed_terminal_mutation_rolls_back_aggregate_release_records_operations_and_audit",
			run: func(t *testing.T) {
				st := newSQLite(t)
				taskID := inspection.TaskID("task-terminal-rollback")
				seedTask(t, st, taskID, inspection.StatusReleasable)
				err := st.Mutate(func(tx store.Tx) error {
					return tx.SaveOccupancy(occupancy.OccupancySlot{
						TaskID: taskID, Chamber: "ch-seed", Start: 10, End: 20,
						Generation: 7, Status: occupancy.StatusOccupied,
					})
				})
				if err != nil {
					t.Fatalf("seed occupancy: %v", err)
				}

				err = st.Mutate(func(tx store.Tx) error {
					slots, err := tx.ListOccupancies(taskID)
					if err != nil {
						return err
					}
					for _, slot := range slots {
						if occupancy.Active(slot) {
							if err := tx.SaveOccupancy(occupancy.Release(slot, "released")); err != nil {
								return err
							}
						}
					}
					task, err := tx.GetTask(taskID)
					if err != nil {
						return err
					}
					task.Status = inspection.StatusReleased
					task.TerminalVersion = 1
					task.TerminalOutcome = "released"
					if err := tx.SaveTask(task); err != nil {
						return err
					}
					if err := tx.SaveCredential(inspection.ReleaseCredential{TaskID: taskID, Credential: "credential", Version: 1}); err != nil {
						return err
					}
					if err := tx.SaveReview(review.ReviewAndFinal{TaskID: taskID, Outcome: review.OutcomeReleased, TerminalVersion: 1}); err != nil {
						return err
					}
					if err := tx.RecordOperation(inspection.NewRecord("terminal-rollback-op", taskID, 7, "request", "response")); err != nil {
						return err
					}
					if err := tx.AppendAudit(inspection.AuditEvent{
						TaskID: taskID, LogicalTime: 2, Actor: "system",
						TaskStatus: inspection.StatusReleased, Action: "finalize", Code: domain.CodeNone,
					}); err != nil {
						return err
					}
					return rollbackErr
				})
				assertRollbackErr(t, err)

				task, err := st.GetTask(taskID)
				if err != nil {
					t.Fatalf("get task: %v", err)
				}
				if task.Status != inspection.StatusReleasable || task.TerminalVersion != 0 || task.TerminalOutcome != "" {
					t.Fatalf("terminal rollback changed task aggregate: %+v", task)
				}
				if got, err := st.ListOccupancies(taskID); err != nil || len(got) != 1 ||
					got[0].Status != occupancy.StatusOccupied || got[0].ReleaseReason != "" {
					t.Fatalf("terminal rollback changed occupancies: got=%+v err=%v", got, err)
				}
				if got, err := st.ListReviews(taskID); err != nil || len(got) != 0 {
					t.Fatalf("terminal review persisted after rollback: len=%d err=%v", len(got), err)
				}
				if cred, err := st.GetCredential(taskID); err == nil || cred != nil {
					t.Fatalf("credential persisted after rollback: cred=%+v err=%v", cred, err)
				}
				if got, err := st.ListAudit(taskID); err != nil || len(got) != 0 {
					t.Fatalf("terminal audit persisted after rollback: len=%d err=%v", len(got), err)
				}
				assertNoOperation(t, st, "terminal-rollback-op")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

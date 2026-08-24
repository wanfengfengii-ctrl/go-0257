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

func TestModel_PathogenRetryableReplayPersistsState(t *testing.T) {
	cases := []struct {
		name                string
		restartBeforeReplay bool
	}{
		{name: "same_process_replay", restartBeforeReplay: false},
		{name: "restart_replay", restartBeforeReplay: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "riceguard.db")
			c, roles := catalog.Seed()

			st, err := store.OpenSQLite(dbPath)
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			amp := pathogen.NewScriptedAmplifier()
			for _, status := range []pathogen.DeviceStatus{
				pathogen.DeviceRefused,
				pathogen.DeviceDisconnect,
				pathogen.DeviceTimeout,
			} {
				amp.AddFault("p-1", "w1", status)
			}
			svc := api.NewService(c, roles, st, amp, measure.NewScriptedMeter())
			id := driveToPathogen(t, svc, "model-"+tc.name)

			req := api.PathogenRequest{
				OperationID: "model-" + tc.name + "-pathogen-read",
				BlindCode:   "b1",
				Plate:       "p-1",
				Well:        "w1",
				Verifier:    "pathologist-d",
			}
			if resp, derr := svc.RecordPathogen(id, req); derr == nil || derr.Code != domain.CodeDeviceRetryable {
				t.Fatalf("first exhausted amplifier call = (%+v, %v), want RICE_DEVICE_RETRYABLE", resp, derr)
			}

			before := readPathogenReplayState(t, st, id, req.OperationID)
			if before.operation.ResponseCode != domain.CodeDeviceRetryable {
				t.Fatalf("retryable operation code = %s, want %s", before.operation.ResponseCode, domain.CodeDeviceRetryable)
			}
			if got := len(before.pathogen); got != 0 {
				t.Fatalf("retryable failure persisted %d pathogen readings, want 0", got)
			}
			if got := len(before.attempts); got != 3 {
				t.Fatalf("retryable failure persisted %d attempts, want 3", got)
			}
			for i, want := range []pathogen.DeviceStatus{
				pathogen.DeviceRefused,
				pathogen.DeviceDisconnect,
				pathogen.DeviceTimeout,
			} {
				got := before.attempts[i]
				if got.Status != want || !got.Retryable || got.Attempt != i+1 {
					t.Fatalf("attempt %d = {status:%s retryable:%v attempt:%d}, want {status:%s retryable:true attempt:%d}",
						i, got.Status, got.Retryable, got.Attempt, want, i+1)
				}
			}
			if countAudit(before.allAudit, "pathogen_retryable_attempt", domain.CodeDeviceRetryable) != 1 {
				t.Fatalf("expected exactly one retryable pathogen audit event before replay")
			}

			activeStore := st
			if tc.restartBeforeReplay {
				if err := st.Close(); err != nil {
					t.Fatalf("close before replay: %v", err)
				}
				st, err = store.OpenSQLite(dbPath)
				if err != nil {
					t.Fatalf("reopen before replay: %v", err)
				}
				activeStore = st
				svc = api.NewService(c, roles, activeStore, pathogen.NewScriptedAmplifier(), measure.NewScriptedMeter())

				recovered := readPathogenReplayState(t, activeStore, id, req.OperationID)
				if recovered.operation.ResponseCode != before.operation.ResponseCode ||
					len(recovered.attempts) != len(before.attempts) ||
					len(recovered.pathogen) != len(before.pathogen) ||
					len(recovered.allAudit) != len(before.allAudit) {
					t.Fatalf("pending retry state changed across restart: before=%+v recovered=%+v", before, recovered)
				}
			}
			defer activeStore.Close()

			replayed, derr := svc.RecordPathogen(id, req)
			if derr != nil {
				t.Fatalf("retrying same operation after retryable state: %v", derr)
			}
			if replayed.TaskID != inspection.TaskID(id) || replayed.Status == "" || replayed.Verdict == "" {
				t.Fatalf("retrying same operation returned empty success: %+v", replayed)
			}

			after := readPathogenReplayState(t, activeStore, id, req.OperationID)
			if after.operation.ResponseCode != domain.CodeNone {
				t.Fatalf("replayed operation code = %s, want successful recorded operation", after.operation.ResponseCode)
			}
			if got := len(after.pathogen); got != 1 {
				t.Fatalf("replay persisted %d pathogen readings, want 1", got)
			}
			if got := len(after.attempts); got != 4 {
				t.Fatalf("replay persisted %d attempts, want 4", got)
			}
			lastAttempt := after.attempts[len(after.attempts)-1]
			if lastAttempt.Status != pathogen.DeviceOk || lastAttempt.Retryable {
				t.Fatalf("replay attempt = {status:%s retryable:%v}, want successful non-retryable attempt", lastAttempt.Status, lastAttempt.Retryable)
			}
			if countAudit(after.allAudit, "pathogen_retryable_attempt", domain.CodeDeviceRetryable) != 1 ||
				countAudit(after.allAudit, "pathogen_reading", domain.CodeNone) != 1 {
				t.Fatalf("unexpected pathogen audit counts after replay")
			}

			again, derr := svc.RecordPathogen(id, req)
			if derr != nil {
				t.Fatalf("successful operation replay returned error: %v", derr)
			}
			if again != replayed {
				t.Fatalf("successful operation replay changed response: first=%+v again=%+v", replayed, again)
			}
			stable := readPathogenReplayState(t, activeStore, id, req.OperationID)
			if len(stable.attempts) != len(after.attempts) ||
				len(stable.pathogen) != len(after.pathogen) ||
				len(stable.allAudit) != len(after.allAudit) {
				t.Fatalf("successful idempotent replay mutated state: after=%+v stable=%+v", after, stable)
			}
		})
	}
}

type pathogenReplayState struct {
	attempts  []pathogen.Attempt
	pathogen  []pathogen.PathogenEvidence
	allAudit  []inspection.AuditEvent
	operation inspection.IdempotencyRecord
}

func readPathogenReplayState(t *testing.T, st store.Store, id, operationID string) pathogenReplayState {
	t.Helper()
	taskID := inspection.TaskID(id)
	attempts, err := st.ListAttempts(taskID)
	if err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	evidence, err := st.ListPathogen(taskID)
	if err != nil {
		t.Fatalf("list pathogen: %v", err)
	}
	audit, err := st.ListAllAudit()
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	op, ok := st.FindOperation(operationID)
	if !ok {
		t.Fatalf("operation %q was not persisted", operationID)
	}
	return pathogenReplayState{
		attempts:  attempts,
		pathogen:  evidence,
		allAudit:  audit,
		operation: *op,
	}
}

func countAudit(events []inspection.AuditEvent, action string, code domain.ErrorCode) int {
	var count int
	for _, event := range events {
		if event.Action == action && event.Code == code {
			count++
		}
	}
	return count
}

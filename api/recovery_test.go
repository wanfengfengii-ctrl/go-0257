package api_test

import (
	"path/filepath"
	"testing"

	"riceguard/api"
	"riceguard/catalog"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/pathogen"
	"riceguard/store"
)

func TestRestartRecovery(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "riceguard.db")

	c, roles := catalog.Seed()
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	svc := api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())

	id := driveToReleasable(t, svc, "rec")
	if _, derr := svc.Finalize(id, api.FinalizeRequest{OperationID: "rec-final"}); derr != nil {
		t.Fatalf("finalize: %v", derr)
	}
	before, derr := svc.GetTask(id)
	if derr != nil {
		t.Fatalf("get task: %v", derr)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen and verify the aggregate is fully recovered.
	st2, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer st2.Close()
	svc2 := api.NewService(c, roles, st2, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())

	after, derr := svc2.GetTask(id)
	if derr != nil {
		t.Fatalf("get task after reopen: %v", derr)
	}
	if after.Task.Status != before.Task.Status {
		t.Fatalf("status changed across restart: %s vs %s", before.Task.Status, after.Task.Status)
	}
	if after.Credential == nil || before.Credential == nil {
		t.Fatal("expected recovered credential")
	}
	if after.Credential.Credential != before.Credential.Credential {
		t.Fatal("credential changed across restart")
	}
	if len(after.Occupancies) != len(before.Occupancies) {
		t.Fatal("occupancies not recovered")
	}
	if len(after.Germinations) != len(before.Germinations) {
		t.Fatal("germinations not recovered")
	}
}

func TestPendingRetryRecovered(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "riceguard.db")
	c, roles := catalog.Seed()

	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	amp := pathogen.NewScriptedAmplifier()
	amp.AddFault("p-1", "w1", pathogen.DeviceRefused)
	amp.AddFault("p-1", "w1", pathogen.DeviceDisconnect)
	amp.AddFault("p-1", "w1", pathogen.DeviceTimeout)
	svc := api.NewService(c, roles, st, amp, measure.NewScriptedMeter())

	id := driveToPathogen(t, svc, "pr")
	// Exhausts the retry budget, persisting three retryable attempts.
	_, derr := svc.RecordPathogen(id, api.PathogenRequest{
		OperationID: "pr-read", BlindCode: "b1", Plate: "p-1", Well: "w1", Verifier: "pathologist-d",
	})
	if derr == nil || derr.Code != "RICE_DEVICE_RETRYABLE" {
		t.Fatalf("expected device retryable, got %v", derr)
	}

	attempts, _ := st.ListAttempts(inspection.TaskID(id))
	if len(attempts) != 3 {
		t.Fatalf("expected 3 persisted attempts, got %d", len(attempts))
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	st2, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	attempts2, _ := st2.ListAttempts(inspection.TaskID(id))
	if len(attempts2) != 3 {
		t.Fatalf("expected attempts recovered, got %d", len(attempts2))
	}
}

func TestLogicalClockContinues(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "riceguard.db")
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	first := st.NextTime()
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	st2, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	second := st2.NextTime()
	if second <= first {
		t.Fatalf("logical clock must keep increasing across restart: %d <= %d", second, first)
	}
}

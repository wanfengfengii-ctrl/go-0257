package occupancy_test

import (
	"testing"

	"riceguard/domain"
	"riceguard/occupancy"
)

func TestReserveChamberNoOverlap(t *testing.T) {
	a := occupancy.NewArbiter(nil)
	if err := a.ReserveChamber("ch1", 10, 20); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReserveChamberOverlapConflict(t *testing.T) {
	slots := []occupancy.OccupancySlot{
		{Chamber: "ch1", Start: 10, End: 20, Status: occupancy.StatusOccupied, TaskID: "task-a"},
	}
	a := occupancy.NewArbiter(slots)
	err := a.ReserveChamber("ch1", 15, 25)
	if err == nil {
		t.Fatal("expected overlap conflict, got nil")
	}
	if err.Code != domain.CodeOccupancyConflict {
		t.Fatalf("expected CodeOccupancyConflict, got %s", err.Code)
	}
}

func TestReserveChamberBackToBackAllowed(t *testing.T) {
	slots := []occupancy.OccupancySlot{
		{Chamber: "ch1", Start: 10, End: 20, Status: occupancy.StatusOccupied},
	}
	a := occupancy.NewArbiter(slots)
	if err := a.ReserveChamber("ch1", 20, 30); err != nil {
		t.Fatalf("back-to-back windows should not conflict: %v", err)
	}
}

func TestReserveWellConflict(t *testing.T) {
	slots := []occupancy.OccupancySlot{
		{Plate: "p1", Well: "w1", Status: occupancy.StatusOccupied, TaskID: "task-a"},
	}
	a := occupancy.NewArbiter(slots)
	err := a.ReserveWell("p1", "w1")
	if err == nil {
		t.Fatal("expected well conflict, got nil")
	}
	if err.Code != domain.CodeOccupancyConflict {
		t.Fatalf("expected CodeOccupancyConflict, got %s", err.Code)
	}
}

func TestReleaseMarksSlot(t *testing.T) {
	s := occupancy.OccupancySlot{Status: occupancy.StatusOccupied}
	r := occupancy.Release(s, "quarantine")
	if r.Status != occupancy.StatusReleased {
		t.Fatal("expected released status")
	}
	if occupancy.Active(r) {
		t.Fatal("released slot must not be active")
	}
}

package pathogen_test

import (
	"testing"

	"riceguard/domain"
	"riceguard/occupancy"
	"riceguard/pathogen"
)

func TestAdjudicateNegative(t *testing.T) {
	if pathogen.Adjudicate(40, 40) != pathogen.VerdictNegative {
		t.Fatal("expected reading at threshold to be negative")
	}
	if pathogen.Adjudicate(41, 40) != pathogen.VerdictPositive {
		t.Fatal("expected reading above threshold to be positive")
	}
}

func TestNeedsRejudge(t *testing.T) {
	if !pathogen.NeedsRejudge(pathogen.VerdictPositive, false) {
		t.Fatal("positive should need rejudge")
	}
	if !pathogen.NeedsRejudge(pathogen.VerdictNegative, true) {
		t.Fatal("contamination should need rejudge")
	}
	if pathogen.NeedsRejudge(pathogen.VerdictNegative, false) {
		t.Fatal("negative non-contaminated should not need rejudge")
	}
}

func TestStaticAmplifierReturnsReading(t *testing.T) {
	amp := pathogen.NewStaticAmplifier()
	amp.Set("p1", "w1", 12)
	reading, err := amp.Read(occupancy.PlateID("p1"), occupancy.WellID("w1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reading != 12 {
		t.Fatalf("expected 12, got %d", reading)
	}
}

func TestRetryPolicyExhaustsBudget(t *testing.T) {
	amp := pathogen.NewScriptedAmplifier()
	for i := 0; i < 3; i++ {
		amp.AddFault("p1", "w1", pathogen.DeviceRefused)
	}
	_, attempts, err := pathogen.DefaultRetryPolicy.Run(amp, "p1", "w1", func() uint64 { return 0 })
	if err == nil {
		t.Fatal("expected budget exhaustion, got nil")
	}
	if err.Code != domain.CodeDeviceRetryable {
		t.Fatalf("expected CodeDeviceRetryable, got %s", err.Code)
	}
	if len(attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(attempts))
	}
	for _, a := range attempts {
		if !a.Retryable {
			t.Fatal("expected all attempts retryable")
		}
	}
}

func TestRetryPolicySucceedsWithinBudget(t *testing.T) {
	amp := pathogen.NewScriptedAmplifier()
	amp.AddFault("p1", "w1", pathogen.DeviceDisconnect)
	reading, attempts, err := pathogen.DefaultRetryPolicy.Run(amp, "p1", "w1", func() uint64 { return 0 })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reading <= 0 {
		t.Fatalf("expected positive reading, got %d", reading)
	}
	if len(attempts) != 2 {
		t.Fatalf("expected 2 attempts (1 fault + 1 success), got %d", len(attempts))
	}
}

func TestBuildRejudgeCoversScope(t *testing.T) {
	ev := []pathogen.PathogenEvidence{
		{BlindCode: "b1", Plate: "p1", Well: "w1", Verdict: pathogen.VerdictPositive},
		{BlindCode: "b2", Plate: "p1", Well: "w2", Contaminated: true},
	}
	r := pathogen.BuildRejudge("t1", 1, 1, ev)
	if !r.ScopeCovers("b1") || !r.ScopeCovers("b2") {
		t.Fatal("rejudge should cover affected blind codes")
	}
	if !r.WellCovers(occupancy.PlateID("p1"), occupancy.WellID("w1")) ||
		!r.WellCovers(occupancy.PlateID("p1"), occupancy.WellID("w2")) {
		t.Fatal("rejudge should cover affected wells")
	}
}

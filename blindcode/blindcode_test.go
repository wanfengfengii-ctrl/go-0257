package blindcode_test

import (
	"testing"

	"riceguard/blindcode"
	"riceguard/domain"
	"riceguard/inspection"
)

func allocs() []inspection.BlindAllocation {
	return []inspection.BlindAllocation{
		{Code: "b1", Germination: 100, Pathogen: 50, Moisture: 30},
		{Code: "b2", Germination: 100, Pathogen: 50, Moisture: 30},
	}
}

func TestBuildMatrixCoversThreeSplits(t *testing.T) {
	m, err := blindcode.BuildMatrix("t1", 1, allocs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Cells) != 6 {
		t.Fatalf("expected 6 cells (2 codes x 3 splits), got %d", len(m.Cells))
	}
	if m.Total(blindcode.SplitGermination) != 200 {
		t.Fatalf("expected germination total 200, got %d", m.Total(blindcode.SplitGermination))
	}
}

func TestBuildMatrixDuplicateRejected(t *testing.T) {
	a := allocs()
	a[1].Code = "b1"
	_, err := blindcode.BuildMatrix("t1", 1, a)
	if err == nil {
		t.Fatal("expected duplicate rejection, got nil")
	}
	if err.Code != domain.CodeBlindDuplicate {
		t.Fatalf("expected CodeBlindDuplicate, got %s", err.Code)
	}
}

func TestBuildMatrixNonPositiveQuantity(t *testing.T) {
	a := allocs()
	a[0].Germination = 0
	_, err := blindcode.BuildMatrix("t1", 1, a)
	if err == nil {
		t.Fatal("expected non-positive quantity rejection, got nil")
	}
	if err.Code != domain.CodeBadRequest {
		t.Fatalf("expected CodeBadRequest, got %s", err.Code)
	}
}

func TestConsistencyHashStable(t *testing.T) {
	m, _ := blindcode.BuildMatrix("t1", 1, allocs())
	h1 := blindcode.ConsistencyHash("b1", m.Cells)
	h2 := blindcode.ConsistencyHash("b1", m.Cells)
	if h1 != h2 {
		t.Fatal("consistency hash must be deterministic")
	}
}

func TestUnblindGateDuplicate(t *testing.T) {
	g := blindcode.NewMemoryGate()
	if _, err := g.Open("t1", 1, "b1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err := g.Open("t1", 1, "b1")
	if err == nil {
		t.Fatal("expected duplicate unblind rejection, got nil")
	}
	if err.Code != domain.CodeBlindDuplicate {
		t.Fatalf("expected CodeBlindDuplicate, got %s", err.Code)
	}
}

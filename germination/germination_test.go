package germination_test

import (
	"testing"

	"riceguard/blindcode"
	"riceguard/domain"
	"riceguard/germination"
	"riceguard/inspection"
)

func TestConservedTrue(t *testing.T) {
	cell := cell(92, 5, 3)
	if !germination.Conserved(100, cell) {
		t.Fatal("expected conserved counts")
	}
}

func TestConservedFalse(t *testing.T) {
	cell := cell(91, 5, 3)
	if germination.Conserved(100, cell) {
		t.Fatal("expected count drift to fail conservation")
	}
}

func TestValidateCellDriftRejected(t *testing.T) {
	err := germination.ValidateCell(100, cell(90, 5, 4))
	if err == nil {
		t.Fatal("expected drift rejection, got nil")
	}
	if err.Code != domain.CodeGerminationDrift {
		t.Fatalf("expected CodeGerminationDrift, got %s", err.Code)
	}
}

func TestDuplicateReadingDetected(t *testing.T) {
	cells := []germination.GerminationCell{
		{BlindCode: "b1", DayAge: 5, Valid: true, Normal: 92, Abnormal: 5, Dead: 3},
	}
	if !germination.Duplicate(cells, "b1", 5) {
		t.Fatal("expected duplicate day-age reading to be detected")
	}
}

func TestMissingCells(t *testing.T) {
	cells := []germination.GerminationCell{
		{BlindCode: "b1", DayAge: 2, Valid: true},
	}
	missing := germination.MissingCells(cells, []string{"b1"}, []int32{2, 5, 8})
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing cells, got %d", len(missing))
	}
}

func TestCoveredFalseThenTrue(t *testing.T) {
	cells := []germination.GerminationCell{
		{BlindCode: "b1", DayAge: 2, Valid: true},
		{BlindCode: "b1", DayAge: 5, Valid: true},
		{BlindCode: "b1", DayAge: 8, Valid: true},
	}
	if !germination.Covered(cells, []string{"b1"}, []int32{2, 5, 8}) {
		t.Fatal("expected grid covered")
	}
}

func TestEnergyAndRate(t *testing.T) {
	cells := []germination.GerminationCell{
		{BlindCode: "b1", DayAge: 2, Valid: true, Normal: 80},
		{BlindCode: "b1", DayAge: 5, Valid: true, Normal: 90},
		{BlindCode: "b1", DayAge: 8, Valid: true, Normal: 95},
	}
	energy, err := germination.Energy(cells, "b1", []int32{2, 5, 8}, 100)
	if err != nil {
		t.Fatalf("energy: %v", err)
	}
	if energy != 8000 {
		t.Fatalf("expected energy 8000bp, got %d", energy)
	}
	rate, err := germination.Rate(cells, "b1", []int32{2, 5, 8}, 100)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}
	if rate != 9500 {
		t.Fatalf("expected rate 9500bp, got %d", rate)
	}
}

func cell(normal, abnormal, dead int) germination.GerminationCell {
	return germination.GerminationCell{
		TaskID:    inspection.TaskID("t1"),
		BlindCode: blindcode.BlindCode("b1"),
		Split:     blindcode.SplitGermination,
		DayAge:    5,
		Normal:    normal,
		Abnormal:  abnormal,
		Dead:      dead,
		Valid:     true,
	}
}

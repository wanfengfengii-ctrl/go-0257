package measure_test

import (
	"testing"

	"riceguard/domain"
	"riceguard/measure"
)

func TestMulBasic(t *testing.T) {
	got, err := measure.Mul(measure.Fixed(200), measure.Fixed(50))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != measure.Fixed(100) {
		t.Fatalf("expected 100, got %d", got)
	}
}

func TestMulOverflow(t *testing.T) {
	_, err := measure.Mul(measure.Fixed(1)<<62, measure.Fixed(2))
	if err == nil {
		t.Fatal("expected overflow error, got nil")
	}
	if err.Code != domain.CodeFixedPointOverflow {
		t.Fatalf("expected CodeFixedPointOverflow, got %s", err.Code)
	}
}

func TestDivByZero(t *testing.T) {
	_, err := measure.Div(measure.Fixed(10000), measure.Fixed(0))
	if err == nil {
		t.Fatal("expected division-by-zero error, got nil")
	}
	if err.Code != domain.CodeFixedPointOverflow {
		t.Fatalf("expected CodeFixedPointOverflow, got %s", err.Code)
	}
}

func TestDivBasic(t *testing.T) {
	got, err := measure.Div(measure.Fixed(100), measure.Fixed(200))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != measure.Fixed(50) {
		t.Fatalf("expected 50, got %d", got)
	}
}

func TestParsePercentBasic(t *testing.T) {
	got, err := measure.ParsePercent("12.50")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != measure.Fixed(1250) {
		t.Fatalf("expected 1250 basis points, got %d", got)
	}
}

func TestParseNegativeRejected(t *testing.T) {
	_, err := measure.ParsePercent("-12.50")
	if err == nil {
		t.Fatal("expected negative literal rejection, got nil")
	}
	if err.Code != domain.CodeFixedPointOverflow {
		t.Fatalf("expected CodeFixedPointOverflow, got %s", err.Code)
	}
}

func TestParseTooManyDecimalsRejected(t *testing.T) {
	_, err := measure.ParsePercent("13.0001")
	if err == nil {
		t.Fatal("expected too-many-decimals rejection, got nil")
	}
	if err.Code != domain.CodeBadRequest {
		t.Fatalf("expected CodeBadRequest, got %s", err.Code)
	}
}

func TestParseTooLongRejected(t *testing.T) {
	_, err := measure.ParsePercent("1234567890.12")
	if err == nil {
		t.Fatal("expected over-long literal rejection, got nil")
	}
	if err.Code != domain.CodeFixedPointOverflow {
		t.Fatalf("expected CodeFixedPointOverflow, got %s", err.Code)
	}
}

func TestDecideMoistureAtBoundary(t *testing.T) {
	m, err := measure.ParsePercent("13.00")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d, err := measure.DecideMoisture(m, 1300)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if !d.Pass {
		t.Fatal("expected moisture at boundary to pass")
	}
}

func TestDecideMoistureAboveBoundary(t *testing.T) {
	m, _ := measure.ParsePercent("13.01")
	d, err := measure.DecideMoisture(m, 1300)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if d.Pass {
		t.Fatal("expected moisture above boundary to fail")
	}
}

func TestPurityDeriveDivideByZero(t *testing.T) {
	_, err := measure.PurityDerive(50, 0)
	if err == nil {
		t.Fatal("expected division-by-zero, got nil")
	}
	if err.Code != domain.CodeBadRequest {
		t.Fatalf("expected CodeBadRequest, got %s", err.Code)
	}
}

func TestPurityDeriveBasic(t *testing.T) {
	got, err := measure.PurityDerive(98, 100)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if got != measure.Fixed(9800) {
		t.Fatalf("expected 9800 basis points, got %d", got)
	}
}

func TestThousandGrainNegative(t *testing.T) {
	err := measure.ThousandGrainValidate(-1)
	if err == nil {
		t.Fatal("expected negative thousand-grain rejection, got nil")
	}
	if err.Code != domain.CodeFixedPointOverflow {
		t.Fatalf("expected CodeFixedPointOverflow, got %s", err.Code)
	}
}

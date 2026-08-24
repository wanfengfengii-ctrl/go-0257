package catalog_test

import (
	"testing"

	"riceguard/catalog"
	"riceguard/domain"
)

func seed() *catalog.Memory {
	c := catalog.NewMemory()
	c.Register(catalog.CatalogVariety{
		ID:            "xiangliangyou-900",
		FemaleParent:  "xiang-a",
		MaleParent:    "r-900",
		AllowedFields: []catalog.FieldID{"field-01", "field-02"},
		MinPurity:     9800,
		CertRevision:  3,
		DayAges:       []int32{2, 5, 8},
		MoistureMax:   1300,
		PathogenMax:   0,
	})
	c.RegisterCert(catalog.ParentCert{Parent: "xiang-a", Revision: 3, Purity: 9950})
	c.RegisterCert(catalog.ParentCert{Parent: "r-900", Revision: 3, Purity: 9920})
	return c
}

func TestValidateFieldMatch(t *testing.T) {
	c := seed()
	if err := c.ValidateField("xiangliangyou-900", "field-01"); err != nil {
		t.Fatalf("expected compatible field, got %v", err)
	}
}

func TestValidateFieldMismatch(t *testing.T) {
	c := seed()
	err := c.ValidateField("xiangliangyou-900", "field-99")
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
	if err.Code != domain.CodeVarietyMismatch {
		t.Fatalf("expected CodeVarietyMismatch, got %s", err.Code)
	}
}

func TestValidateParentCertStale(t *testing.T) {
	c := seed()
	err := c.ValidateParentCert("xiang-a", 2)
	if err == nil {
		t.Fatal("expected stale cert error, got nil")
	}
	if err.Code != domain.CodeStaleParentCert {
		t.Fatalf("expected CodeStaleParentCert, got %s", err.Code)
	}
}

func TestValidateParentCertCurrent(t *testing.T) {
	c := seed()
	if err := c.ValidateParentCert("r-900", 3); err != nil {
		t.Fatalf("expected current cert, got %v", err)
	}
}

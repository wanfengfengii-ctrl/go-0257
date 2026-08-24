package catalog

// Seed builds a deterministic catalog of fictitious hybrid rice variety
// combinations, their parental purity certificates and qualified personnel.
// It is used by the production entry point and by deterministic tests so that
// both exercise the same rule templates.
func Seed() (*Memory, *MemoryRoles) {
	c := NewMemory()

	// xiangliangyou-900: a two-line hybrid with a three-observation schedule.
	c.Register(CatalogVariety{
		ID:                 "xiangliangyou-900",
		FemaleParent:       "xiang-a",
		MaleParent:         "r-900",
		AllowedFields:      []FieldID{"field-01", "field-02"},
		MinPurity:          9800,
		CertRevision:       3,
		DayAges:            []int32{2, 5, 8},
		GrainCount:         100,
		MoistureMax:        1300,
		PathogenMax:        40,
		GerminationRateMin: 8500,
	})

	// wuyou-631: a three-line hybrid with a longer observation schedule.
	c.Register(CatalogVariety{
		ID:                 "wuyou-631",
		FemaleParent:       "wu-ii-32a",
		MaleParent:         "hui-631",
		AllowedFields:      []FieldID{"field-03"},
		MinPurity:          9850,
		CertRevision:       5,
		DayAges:            []int32{3, 6, 10},
		GrainCount:         120,
		MoistureMax:        1250,
		PathogenMax:        35,
		GerminationRateMin: 8500,
	})

	// longjing-158: a two-line hybrid with a strict purity floor.
	c.Register(CatalogVariety{
		ID:                 "longjing-158",
		FemaleParent:       "lj-1s",
		MaleParent:         "lj-158",
		AllowedFields:      []FieldID{"field-04", "field-05"},
		MinPurity:          9900,
		CertRevision:       7,
		DayAges:            []int32{2, 4, 7, 9},
		GrainCount:         100,
		MoistureMax:        1280,
		PathogenMax:        30,
		GerminationRateMin: 9000,
	})

	// Parental purity certificates for every referenced parent.
	c.RegisterCert(ParentCert{Parent: "xiang-a", Revision: 3, Purity: 9950})
	c.RegisterCert(ParentCert{Parent: "r-900", Revision: 3, Purity: 9920})
	c.RegisterCert(ParentCert{Parent: "wu-ii-32a", Revision: 5, Purity: 9960})
	c.RegisterCert(ParentCert{Parent: "hui-631", Revision: 5, Purity: 9930})
	c.RegisterCert(ParentCert{Parent: "lj-1s", Revision: 7, Purity: 9970})
	c.RegisterCert(ParentCert{Parent: "lj-158", Revision: 7, Purity: 9940})

	// ganxin-203: a two-line hybrid with a strict pathogen threshold.
	c.Register(CatalogVariety{
		ID:                 "ganxin-203",
		FemaleParent:       "gx-2s",
		MaleParent:         "gx-203",
		AllowedFields:      []FieldID{"field-06"},
		MinPurity:          9880,
		CertRevision:       4,
		DayAges:            []int32{2, 5, 8, 11},
		GrainCount:         110,
		MoistureMax:        1320,
		PathogenMax:        25,
		GerminationRateMin: 8600,
	})

	// yongyou-12: a three-line hybrid with a long observation schedule.
	c.Register(CatalogVariety{
		ID:                 "yongyou-12",
		FemaleParent:       "yy-12a",
		MaleParent:         "yy-hui-12",
		AllowedFields:      []FieldID{"field-07", "field-08"},
		MinPurity:          9900,
		CertRevision:       6,
		DayAges:            []int32{3, 5, 7, 10, 12},
		GrainCount:         100,
		MoistureMax:        1260,
		PathogenMax:        38,
		GerminationRateMin: 8800,
	})

	// Additional parental certificates for the new parents.
	c.RegisterCert(ParentCert{Parent: "gx-2s", Revision: 4, Purity: 9965})
	c.RegisterCert(ParentCert{Parent: "gx-203", Revision: 4, Purity: 9935})
	c.RegisterCert(ParentCert{Parent: "yy-12a", Revision: 6, Purity: 9975})
	c.RegisterCert(ParentCert{Parent: "yy-hui-12", Revision: 6, Purity: 9945})

	r := NewMemoryRoles()
	r.Register(Personnel{ID: "sampler-a", Roles: []RoleID{RoleSampler, RoleReviewer}})
	r.Register(Personnel{ID: "sampler-b", Roles: []RoleID{RoleSampler}})
	r.Register(Personnel{ID: "germinator-c", Roles: []RoleID{RoleGerminator}})
	r.Register(Personnel{ID: "pathologist-d", Roles: []RoleID{RolePathologist}})
	r.Register(Personnel{ID: "metrologist-e", Roles: []RoleID{RoleMetrologist}})
	r.Register(Personnel{ID: "reviewer-f", Roles: []RoleID{RoleReviewer}})
	r.Register(Personnel{ID: "reviewer-g", Roles: []RoleID{RoleReviewer}})
	r.Register(Personnel{ID: "sampler-h", Roles: []RoleID{RoleSampler}})
	r.Register(Personnel{ID: "pathologist-i", Roles: []RoleID{RolePathologist, RoleReviewer}})
	r.Register(Personnel{ID: "metrologist-j", Roles: []RoleID{RoleMetrologist}})

	return c, r
}

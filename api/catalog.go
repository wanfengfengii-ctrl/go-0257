package api

import (
	"riceguard/catalog"
	"riceguard/domain"
	"riceguard/inspection"
)

// CatalogInfo is the read-only rule catalog and personnel directory exposed
// to the console so it can render variety pickers and reviewer selectors.
type CatalogInfo struct {
	Varieties []catalog.CatalogVariety `json:"varieties"`
	People    []catalog.Personnel      `json:"people"`
}

// Catalog returns the full rule catalog and personnel directory.
func (s *Service) Catalog() CatalogInfo {
	return CatalogInfo{
		Varieties: s.catalog.ListVarieties(),
		People:    s.roles.ListPeople(),
	}
}

// ListOpenTasks returns every non-terminal task ordered by creation. It is
// used by the console and by restart-recovery verification to enumerate the
// open inspection work still in flight.
func (s *Service) ListOpenTasks() ([]inspection.InspectionTask, *domain.Error) {
	tasks, err := s.store.ListTasks()
	if err != nil {
		return nil, asDomain(err)
	}
	out := make([]inspection.InspectionTask, 0, len(tasks))
	for _, t := range tasks {
		if !t.IsTerminal() {
			out = append(out, *t)
		}
	}
	return out, nil
}

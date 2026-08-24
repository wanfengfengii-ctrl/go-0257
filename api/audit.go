package api

import (
	"riceguard/domain"
	"riceguard/inspection"
)

// ListAudit returns the audit trail for a single task, ordered by persisted
// sequence number.
func (s *Service) ListAudit(id string) ([]inspection.AuditEvent, *domain.Error) {
	events, err := s.store.ListAudit(inspection.TaskID(id))
	if err != nil {
		return nil, asDomain(err)
	}
	if events == nil {
		events = []inspection.AuditEvent{}
	}
	return events, nil
}

// ListAllAudit returns the global append-only audit sequence across all
// tasks, ordered by persisted sequence number. It powers the monitoring view.
func (s *Service) ListAllAudit() ([]inspection.AuditEvent, *domain.Error) {
	events, err := s.store.ListAllAudit()
	if err != nil {
		return nil, asDomain(err)
	}
	if events == nil {
		events = []inspection.AuditEvent{}
	}
	return events, nil
}

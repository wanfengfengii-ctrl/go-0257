package api

import (
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/pathogen"
)

// ListAttempts returns the persisted instrument invocation attempts for a
// task, ordered by their logical sequence. It exposes the pending-retry audit
// trail to the console.
func (s *Service) ListAttempts(id string) ([]pathogen.Attempt, *domain.Error) {
	attempts, err := s.store.ListAttempts(inspection.TaskID(id))
	if err != nil {
		return nil, asDomain(err)
	}
	if attempts == nil {
		attempts = []pathogen.Attempt{}
	}
	return attempts, nil
}

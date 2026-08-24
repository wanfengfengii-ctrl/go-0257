package measure

import "riceguard/domain"

// MeterAttempt drives a moisture-meter invocation under a bounded retry
// budget, returning the first successful reading together with the number of
// attempts consumed.
func MeterAttempt(m MoistureMeter, attemptID string, max int) (Fixed, int, *domain.Error) {
	for i := 1; i <= max; i++ {
		reading, derr := m.Read(attemptID)
		if derr == nil {
			return reading, i, nil
		}
	}
	return 0, max, nil
}

// DefaultMeterAttempts is the standard moisture-meter retry budget.
const DefaultMeterAttempts = 3

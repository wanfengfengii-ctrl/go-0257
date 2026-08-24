package measure

import "riceguard/domain"

// MeterAttempt drives a moisture-meter invocation under a bounded retry
// budget, returning the first successful reading together with the number of
// attempts consumed. If the attempt budget is exhausted it returns
// CodeDeviceRetryable so a refused, disconnected, timed-out or malformed meter
// never produces a valid moisture reading; the caller must stay in the
// moisture state rather than persisting a zero result as qualified evidence.
func MeterAttempt(m MoistureMeter, attemptID string, max int) (Fixed, int, *domain.Error) {
	for i := 1; i <= max; i++ {
		reading, derr := m.Read(attemptID)
		if derr == nil {
			return reading, i, nil
		}
	}
	return 0, max, domain.NewError(domain.CodeDeviceRetryable, "moisture meter attempt budget exhausted", attemptID)
}

// DefaultMeterAttempts is the standard moisture-meter retry budget.
const DefaultMeterAttempts = 3

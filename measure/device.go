package measure

import "riceguard/domain"

// MoistureDeviceStatus is the result of a moisture-meter invocation.
type MoistureDeviceStatus string

const (
	MoistureOk         MoistureDeviceStatus = "ok"
	MoistureRefused    MoistureDeviceStatus = "refused"
	MoistureDisconnect MoistureDeviceStatus = "disconnect"
	MoistureTimeout    MoistureDeviceStatus = "timeout"
	MoistureBadFormat  MoistureDeviceStatus = "bad_format"
)

// MoistureMeter is the moisture/purity instrument adapter. Faulty invocations
// are returned as retryable attempts and must never be written as valid
// moisture or purity evidence.
type MoistureMeter interface {
	// Read attempts one moisture reading. A non-nil error wraps a
	// domain.Error with CodeDeviceRetryable for retryable faults.
	Read(attempt string) (Fixed, *domain.Error)
}

// ScriptedMeter replays a deterministic fault script for a moisture meter.
type ScriptedMeter struct {
	faults []MoistureDeviceStatus
	next   int64
}

// NewScriptedMeter builds an empty scripted meter.
func NewScriptedMeter() *ScriptedMeter {
	return &ScriptedMeter{}
}

// AddFault appends a fault to the meter's replay script.
func (s *ScriptedMeter) AddFault(st MoistureDeviceStatus) {
	s.faults = append(s.faults, st)
}

// Read implements MoistureMeter. Each call consumes the next scripted fault;
// an exhausted script returns a deterministic successful reading.
func (s *ScriptedMeter) Read(attempt string) (Fixed, *domain.Error) {
	if len(s.faults) > 0 {
		f := s.faults[0]
		s.faults = s.faults[1:]
		return 0, domain.NewError(domain.CodeDeviceRetryable, string(f), attempt)
	}
	s.next++
	// Return a realistic moisture around 12.00% (1200 basis points), drifting
	// slightly upward with each successful read.
	return Fixed(1200 + s.next), nil
}

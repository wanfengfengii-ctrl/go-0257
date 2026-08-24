package pathogen

import (
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/occupancy"
)

// RetryPolicy is the deterministic retry policy for instrument invocations.
// It bounds the number of retryable attempts per well so a refused,
// disconnected, timed-out or malformed device never produces a premature
// negative, qualified or terminal conclusion.
type RetryPolicy struct {
	MaxAttempts int
}

// DefaultRetryPolicy is the standard policy used by the production service.
var DefaultRetryPolicy = RetryPolicy{MaxAttempts: 3}

// Attempt is a single audited instrument invocation attempt.
type Attempt struct {
	TaskID     inspection.TaskID
	Plate      occupancy.PlateID
	Well       occupancy.WellID
	Attempt    int
	Status     DeviceStatus
	Reading    int32
	Retryable  bool
	LogicalSeq uint64
}

// Run drives an amplifier invocation under the retry policy. It consumes the
// scripted faults, recording every retryable attempt, and returns the first
// successful reading together with the full attempt history. If the attempt
// budget is exhausted it returns CodeDeviceRetryable.
func (p RetryPolicy) Run(amp Amplifier, plate occupancy.PlateID, well occupancy.WellID, seq func() uint64) (int32, []Attempt, *domain.Error) {
	var attempts []Attempt
	for i := 1; i <= p.MaxAttempts; i++ {
		reading, derr := amp.Read(plate, well)
		if derr == nil {
			attempts = append(attempts, Attempt{
				Plate: plate, Well: well, Attempt: i, Status: DeviceOk,
				Reading: reading, Retryable: false, LogicalSeq: seq(),
			})
			return reading, attempts, nil
		}
		status := deviceStatusFromError(derr)
		attempts = append(attempts, Attempt{
			Plate: plate, Well: well, Attempt: i, Status: status,
			Retryable: true, LogicalSeq: seq(),
		})
	}
	return 0, attempts, domain.NewError(domain.CodeDeviceRetryable,
		"attempt budget exhausted", string(plate), string(well))
}

func deviceStatusFromError(err *domain.Error) DeviceStatus {
	for _, r := range err.Reasons {
		switch r {
		case string(DeviceRefused):
			return DeviceRefused
		case string(DeviceDisconnect):
			return DeviceDisconnect
		case string(DeviceTimeout):
			return DeviceTimeout
		case string(DeviceBadFormat):
			return DeviceBadFormat
		}
	}
	return DeviceBadFormat
}

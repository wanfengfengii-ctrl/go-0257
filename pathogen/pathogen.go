// Package pathogen owns the amplification readout, threshold adjudication,
// contamination re-judgment generations and late-reading isolation.
package pathogen

import (
	"riceguard/blindcode"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/occupancy"
)

// DeviceStatus is the result of an amplification instrument invocation.
type DeviceStatus string

const (
	DeviceOk         DeviceStatus = "ok"
	DeviceRefused    DeviceStatus = "refused"
	DeviceDisconnect DeviceStatus = "disconnect"
	DeviceTimeout    DeviceStatus = "timeout"
	DeviceBadFormat  DeviceStatus = "bad_format"
)

// Verdict is the threshold boundary adjudication for a single well reading.
type Verdict string

const (
	VerdictNegative     Verdict = "negative"
	VerdictPositive     Verdict = "positive"
	VerdictContaminated Verdict = "contaminated"
	VerdictPending      Verdict = "pending"
)

// PathogenEvidence records an amplification readout for a well bound to a
// blind code, its threshold verdict, re-judgment generation and late-reading
// isolation flags.
type PathogenEvidence struct {
	TaskID       inspection.TaskID
	BlindCode    blindcode.BlindCode
	Plate        occupancy.PlateID
	Well         occupancy.WellID
	Reading      int32
	Verdict      Verdict
	DeviceStatus DeviceStatus
	Verifier     string
	RejudgeGen   inspection.Generation
	Contaminated bool
	LateIsolated bool
}

// Amplifier is the amplification instrument adapter. Faulty invocations
// (refused, disconnected, timed out or malformed) are returned as retryable
// attempts and must never be written as valid readings.
type Amplifier interface {
	// Read attempts one amplification read for a plate well. A non-nil
	// error wraps a domain.Error with CodeDeviceRetryable for retryable
	// faults.
	Read(plate occupancy.PlateID, well occupancy.WellID) (int32, *domain.Error)
}

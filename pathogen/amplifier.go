package pathogen

import (
	"riceguard/domain"
	"riceguard/occupancy"
)

// StaticAmplifier is a deterministic amplifier adapter that returns a fixed
// reading for every well. It is used in tests and for the "successful" path.
type StaticAmplifier struct {
	Readings map[string]int32
}

// NewStaticAmplifier builds an empty static amplifier.
func NewStaticAmplifier() *StaticAmplifier {
	return &StaticAmplifier{Readings: make(map[string]int32)}
}

// Set registers a fixed reading for a plate well.
func (s *StaticAmplifier) Set(plate occupancy.PlateID, well occupancy.WellID, v int32) {
	s.Readings[wellKey(plate, well)] = v
}

// Read implements Amplifier, returning the registered reading or zero.
func (s *StaticAmplifier) Read(plate occupancy.PlateID, well occupancy.WellID) (int32, *domain.Error) {
	return s.Readings[wellKey(plate, well)], nil
}

func wellKey(plate occupancy.PlateID, well occupancy.WellID) string {
	return string(plate) + "/" + string(well)
}

// ScriptedFault is a single instrument fault in a replayable script.
type ScriptedFault struct {
	Plate  occupancy.PlateID
	Well   occupancy.WellID
	Status DeviceStatus
}

// ScriptedAmplifier replays a deterministic sequence of faults for specific
// plate wells. Each well has a scripted status; once the script is exhausted
// the amplifier returns a successful reading. It models refused, disconnected,
// timed-out and malformed instrument invocations without any real device.
type ScriptedAmplifier struct {
	faults map[string][]DeviceStatus
	next   int32
}

// NewScriptedAmplifier builds an empty scripted amplifier.
func NewScriptedAmplifier() *ScriptedAmplifier {
	return &ScriptedAmplifier{faults: make(map[string][]DeviceStatus)}
}

// AddFault appends a fault to a well's replay script.
func (s *ScriptedAmplifier) AddFault(plate occupancy.PlateID, well occupancy.WellID, st DeviceStatus) {
	k := wellKey(plate, well)
	s.faults[k] = append(s.faults[k], st)
}

// Read implements Amplifier. Each call consumes the next scripted fault for
// the well; a fault returns a CodeDeviceRetryable rejection, while an
// exhausted script returns a deterministic successful reading derived from a
// monotonically increasing counter.
func (s *ScriptedAmplifier) Read(plate occupancy.PlateID, well occupancy.WellID) (int32, *domain.Error) {
	k := wellKey(plate, well)
	if len(s.faults[k]) > 0 {
		f := s.faults[k][0]
		if f != DeviceTimeout {
			s.faults[k] = s.faults[k][1:]
		}
		return 0, faultError(f, plate, well)
	}
	s.next++
	return s.next%100 + 1, nil
}

func faultError(st DeviceStatus, plate occupancy.PlateID, well occupancy.WellID) *domain.Error {
	return domain.NewError(domain.CodeDeviceRetryable,
		string(st), string(plate), string(well))
}

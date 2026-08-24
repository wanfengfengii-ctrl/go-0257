package occupancy

import (
	"riceguard/blindcode"
	"riceguard/domain"
	"riceguard/inspection"
)

// Arbiter decides whether a requested chamber time window or plate well can
// be occupied given the current open slots. It is the pure concurrency
// arbitration logic: the store runs the underlying transaction, and the
// arbiter returns a deterministic verdict with stable, sorted reasons.
type Arbiter struct {
	Slots []OccupancySlot
}

// NewArbiter builds an arbiter over the current open slots.
func NewArbiter(slots []OccupancySlot) *Arbiter {
	return &Arbiter{Slots: slots}
}

// open reports whether a slot is still actively binding a resource.
func open(s OccupancySlot) bool {
	return s.Status == StatusReserved || s.Status == StatusOccupied
}

// ReserveChamber validates a chamber time window. It rejects any overlap with
// an existing open slot on the same chamber. Overlap is exclusive on the
// endpoints so back-to-back windows do not conflict.
func (a *Arbiter) ReserveChamber(chamber ChamberID, start, end domain.LogicalTime) *domain.Error {
	if end <= start {
		return domain.NewError(domain.CodeBadRequest, "chamber window end before start", string(chamber))
	}
	var conflicts []string
	for _, s := range a.Slots {
		if !open(s) || s.Chamber != chamber {
			continue
		}
		if overlaps(start, end, s.Start, s.End) {
			conflicts = append(conflicts, string(s.TaskID))
		}
	}
	if len(conflicts) > 0 {
		return domain.NewError(domain.CodeOccupancyConflict,
			domain.JoinReasons(conflicts, []string{string(chamber)})...)
	}
	return nil
}

// ReserveWell validates a plate well. A well may only be bound to one open
// task at a time.
func (a *Arbiter) ReserveWell(plate PlateID, well WellID) *domain.Error {
	var conflicts []string
	for _, s := range a.Slots {
		if !open(s) || s.Plate != plate || s.Well != well {
			continue
		}
		conflicts = append(conflicts, string(s.TaskID))
	}
	if len(conflicts) > 0 {
		return domain.NewError(domain.CodeOccupancyConflict,
			domain.JoinReasons(conflicts, []string{string(plate), string(well)})...)
	}
	return nil
}

// overlaps reports whether [a1,a2) and [b1,b2) intersect.
func overlaps(a1, a2, b1, b2 domain.LogicalTime) bool {
	return a1 < b2 && b1 < a2
}

// BuildSlot assembles a chamber occupancy slot record.
func BuildSlot(chamber ChamberID, start, end domain.LogicalTime, task inspection.TaskID, code blindcode.BlindCode, gen inspection.Generation) OccupancySlot {
	return OccupancySlot{
		Chamber:    chamber,
		Start:      start,
		End:        end,
		TaskID:     task,
		BlindCode:  code,
		Generation: gen,
		Status:     StatusOccupied,
	}
}

// BuildWell assembles a plate-well occupancy slot record.
func BuildWell(plate PlateID, well WellID, task inspection.TaskID, code blindcode.BlindCode, gen inspection.Generation) OccupancySlot {
	return OccupancySlot{
		Plate:      plate,
		Well:       well,
		TaskID:     task,
		BlindCode:  code,
		Generation: gen,
		Status:     StatusOccupied,
	}
}

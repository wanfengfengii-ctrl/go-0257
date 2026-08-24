// Package occupancy owns the constant-temperature germination chamber
// time-slot and pathogen plate-well occupancy records, including the atomic
// concurrency arbitration performed inside a single transaction.
package occupancy

import (
	"riceguard/blindcode"
	"riceguard/domain"
	"riceguard/inspection"
)

// ChamberID identifies a constant-temperature germination chamber.
type ChamberID string

// PlateID identifies a pathogen detection plate.
type PlateID string

// WellID identifies a well on a detection plate.
type WellID string

// OccupancyStatus is the lifecycle of an occupied slot.
type OccupancyStatus string

const (
	StatusReserved  OccupancyStatus = "reserved"
	StatusOccupied  OccupancyStatus = "occupied"
	StatusReleased  OccupancyStatus = "released"
	StatusRechamber OccupancyStatus = "rechambered"
)

// OccupancySlot records a chamber time window and/or a plate well bound to a
// task, blind code and generation. A released slot keeps its release reason
// for the audit trail. Seq is the persisted row identity assigned by the store
// on first save; a non-zero Seq means SaveOccupancy updates the existing row in
// place rather than appending a duplicate, so releasing or re-chambering a
// slot truly closes the original active row instead of leaving it bound.
type OccupancySlot struct {
	Seq           uint64
	Chamber       ChamberID
	Start         domain.LogicalTime
	End           domain.LogicalTime
	Plate         PlateID
	Well          WellID
	TaskID        inspection.TaskID
	BlindCode     blindcode.BlindCode
	Generation    inspection.Generation
	Status        OccupancyStatus
	ReleaseReason string
}

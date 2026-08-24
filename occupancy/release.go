package occupancy

// Release marks an occupied slot as released with a reason. Release only
// happens at a legal terminal outcome or inside an explicit re-chamber
// transaction; the arbiter never releases a slot on its own.
func Release(s OccupancySlot, reason string) OccupancySlot {
	s.Status = StatusReleased
	s.ReleaseReason = reason
	return s
}

// Rechamber transitions a slot to the rechambered state, recording the
// reason. The old slot remains immutable for the audit trail; a new slot is
// written for the replacement window.
func Rechamber(s OccupancySlot, reason string) OccupancySlot {
	s.Status = StatusRechamber
	s.ReleaseReason = reason
	return s
}

// Active reports whether a slot is actively binding its resource.
func Active(s OccupancySlot) bool {
	return s.Status == StatusReserved || s.Status == StatusOccupied
}

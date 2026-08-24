package domain

// LogicalTime is the persisted logical clock used to order task creation,
// occupancy windows and audit events deterministically across restarts. It
// keeps increasing from the value recovered from WAL after a restart.
type LogicalTime uint64

// TimeSource yields monotonically increasing LogicalTime values. The SQLite
// implementation persists the last issued value so it continues after a
// restart instead of resetting to zero.
type TimeSource interface {
	// Next returns the next logical time strictly greater than all
	// previously issued values.
	Next() LogicalTime
}

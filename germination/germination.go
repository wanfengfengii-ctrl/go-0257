// Package germination owns the day-age observation grid: normal, abnormal and
// dead counts, missing/retest markers, count conservation and the fixed-point
// integer rules that derive germination energy and germination rate.
package germination

import (
	"riceguard/blindcode"
	"riceguard/inspection"
)

// DayAge is an observation day age from the locked schedule.
type DayAge int32

// GerminationCell is a single valid observation cell keyed by task, blind
// code, aliquot and day age.
type GerminationCell struct {
	TaskID      inspection.TaskID
	BlindCode   blindcode.BlindCode
	Split       blindcode.SplitType
	DayAge      DayAge
	Normal      int
	Abnormal    int
	Dead        int
	Retest      bool
	Collector   string
	OperationID string
	Valid       bool
}

// Conserved reports whether the observed counts sum exactly to the locked
// grain count, enforcing the count-conservation invariant.
func Conserved(locked int, c GerminationCell) bool {
	return c.Normal+c.Abnormal+c.Dead == locked
}

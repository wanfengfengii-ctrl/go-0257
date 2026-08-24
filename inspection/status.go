package inspection

// Description returns a stable, human-readable label for a task status. It is
// used by the console to render the state machine progress bar.
func (s TaskStatus) Description() string {
	switch s {
	case StatusPendingCreate:
		return "待建检"
	case StatusPendingSampling:
		return "待抽样确认"
	case StatusBlindSplit:
		return "盲码分管中"
	case StatusOccupying:
		return "舱位占用中"
	case StatusGerminating:
		return "发芽观察中"
	case StatusPathogen:
		return "病原核验中"
	case StatusMoisture:
		return "含水复测中"
	case StatusPendingReview:
		return "待独立复核"
	case StatusReleasable:
		return "可放播"
	case StatusReleased:
		return "已放播"
	case StatusQuarantined:
		return "污染隔离"
	case StatusCancelled:
		return "已取消"
	default:
		return "未知"
	}
}

// Progress returns the zero-based index of the status in the ordered
// inspection pipeline. Terminal statuses share the highest index.
func (s TaskStatus) Progress() int {
	return StatusOrder[s]
}

// Next returns the legal next status for a non-terminal status, or an empty
// status when the status is terminal.
func (s TaskStatus) Next() TaskStatus {
	return advanceTarget[s]
}

// TotalStages is the number of non-terminal pipeline stages plus one terminal
// stage, used to normalize progress display.
const TotalStages = 9

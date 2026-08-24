package review

// OutcomeText renders a terminal outcome as a stable label.
func OutcomeText(o FinalOutcome) string {
	switch o {
	case OutcomeReleased:
		return "released"
	case OutcomeQuarantined:
		return "quarantined"
	case OutcomeCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

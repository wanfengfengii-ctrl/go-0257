package pathogen

// VerdictText renders a threshold verdict as a stable, human-readable label
// for the console and audit trail.
func VerdictText(v Verdict) string {
	switch v {
	case VerdictNegative:
		return "negative"
	case VerdictPositive:
		return "positive"
	case VerdictContaminated:
		return "contaminated"
	case VerdictPending:
		return "pending"
	default:
		return "unknown"
	}
}

// DeviceText renders an instrument device status as a stable label.
func DeviceText(s DeviceStatus) string {
	switch s {
	case DeviceOk:
		return "ok"
	case DeviceRefused:
		return "refused"
	case DeviceDisconnect:
		return "disconnected"
	case DeviceTimeout:
		return "timed_out"
	case DeviceBadFormat:
		return "bad_format"
	default:
		return "unknown"
	}
}

// ParseVerdict parses a verdict label back into a Verdict, defaulting to
// pending for unknown input.
func ParseVerdict(s string) Verdict {
	switch s {
	case "negative":
		return VerdictNegative
	case "positive":
		return VerdictPositive
	case "contaminated":
		return VerdictContaminated
	default:
		return VerdictPending
	}
}

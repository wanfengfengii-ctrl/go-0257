package pathogen

import "riceguard/domain"

// Adjudicate maps an amplification reading to a threshold verdict using the
// locked pathogen threshold. Readings strictly above the threshold are
// positive; readings at or below the threshold are negative.
func Adjudicate(reading, threshold int32) Verdict {
	if reading > threshold {
		return VerdictPositive
	}
	return VerdictNegative
}

// Contaminated reports whether a verifier flagged the sample as contaminated.
// Contamination is an orthogonal axis to the numeric threshold: a
// contaminated well must trigger a re-judgment even if its reading is
// numerically negative.
func Contaminated(evidence PathogenEvidence) bool {
	return evidence.Contaminated
}

// NeedsRejudge reports whether a reading requires a generation-scoped
// re-judgment: any positive, contaminated or pending verdict does.
func NeedsRejudge(v Verdict, contaminated bool) bool {
	if contaminated {
		return true
	}
	return v == VerdictPositive || v == VerdictPending
}

// ValidateReading rejects a malformed reading. Readings must be non-negative;
// the amplifier adapter already turns refused/disconnected/timed-out/malformed
// invocations into retryable attempts, so this guard only protects against
// out-of-range numeric values.
func ValidateReading(reading int32) *domain.Error {
	if reading < 0 {
		return domain.NewError(domain.CodeBadRequest, "negative amplification reading")
	}
	return nil
}

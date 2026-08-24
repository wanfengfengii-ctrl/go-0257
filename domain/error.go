// Package domain holds shared stable value types used across the RiceGuard
// bounded contexts: rejection codes, sorted rejection reasons and the
// persisted logical clock.
package domain

// ErrorCode is a stable, machine-readable rejection code. It is part of the
// public contract so callers can branch on it without parsing messages.
type ErrorCode string

// Stable rejection codes from the approved project document.
const (
	CodeNone                ErrorCode = ""
	CodeVarietyMismatch     ErrorCode = "RICE_VARIETY_MISMATCH"
	CodeStaleParentCert     ErrorCode = "RICE_STALE_PARENT_CERT"
	CodeBlindDuplicate      ErrorCode = "RICE_BLIND_DUPLICATE"
	CodeOccupancyConflict   ErrorCode = "RICE_OCCUPANCY_CONFLICT"
	CodeGerminationDrift    ErrorCode = "RICE_GERMINATION_COUNT_DRIFT"
	CodeFixedPointOverflow  ErrorCode = "RICE_FIXEDPOINT_OVERFLOW"
	CodeDeviceRetryable     ErrorCode = "RICE_DEVICE_RETRYABLE"
	CodeFinalized           ErrorCode = "RICE_FINALIZED"
	CodeIdempotencyConflict ErrorCode = "RICE_IDEMPOTENCY_CONFLICT"
	CodeGenerationStale     ErrorCode = "RICE_GENERATION_STALE"
	CodeNotFound            ErrorCode = "RICE_NOT_FOUND"
	CodeBadRequest          ErrorCode = "RICE_BAD_REQUEST"
)

// Error is a domain rejection carrying a stable code and a deterministically
// sorted set of reasons. Reason ordering must remain stable across runs so
// that identical conflicts produce identical audit text.
type Error struct {
	Code    ErrorCode
	Reasons []string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Code)
}

// NewError builds a domain rejection with the supplied reasons in order.
func NewError(code ErrorCode, reasons ...string) *Error {
	return &Error{Code: code, Reasons: reasons}
}

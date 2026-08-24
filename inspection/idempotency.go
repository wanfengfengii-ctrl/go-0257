package inspection

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"riceguard/domain"
)

// Digest computes a canonical SHA-256 request digest from an ordered list of
// field values. Identical requests produce identical digests; any content
// difference produces a different digest and therefore a conflict.
func Digest(parts ...string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%q", parts)))
	return hex.EncodeToString(sum[:])
}

// CheckIdempotent resolves an idempotent operation against a previously
// recorded result. It returns:
//   - (record, true)  when the operation was seen with identical content;
//   - (nil, false)    when the operation is new;
//   - a CodeIdempotencyConflict rejection when the operation was seen with
//     different content.
func CheckIdempotent(rec *IdempotencyRecord, digest string) (*IdempotencyRecord, *domain.Error, bool) {
	if rec == nil {
		return nil, nil, false
	}
	if rec.RequestDigest != digest {
		return nil, domain.NewError(domain.CodeIdempotencyConflict,
			"operation content conflict", rec.OperationID), true
	}
	return rec, nil, true
}

// NewRecord builds an idempotency record for a successful operation.
func NewRecord(operationID string, task TaskID, gen Generation, digest string, result string) IdempotencyRecord {
	return IdempotencyRecord{
		OperationID:   operationID,
		TaskID:        task,
		Generation:    gen,
		RequestDigest: digest,
		ResponseCode:  domain.CodeNone,
		ResultDigest:  result,
	}
}

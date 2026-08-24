package inspection

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewCredential mints a unique release credential for a task. It embeds the
// task ID and terminal version plus 16 bytes of cryptographic randomness so
// the credential is unique even for a retried finalization of the same task.
func NewCredential(task TaskID, version int64) ReleaseCredential {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	return ReleaseCredential{
		TaskID:     task,
		Credential: fmt.Sprintf("RG-%s-v%d-%s", task, version, hex.EncodeToString(raw)),
		Version:    version,
	}
}

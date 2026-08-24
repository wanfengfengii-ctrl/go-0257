package blindcode

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"riceguard/inspection"
)

// ConsistencyHash computes a deterministic hash covering the triple-split
// matrix cells for a blind code. It is stored on each BlindSample so that any
// later tampering with the matrix can be detected by re-hashing.
func ConsistencyHash(code BlindCode, cells []TripleSplit) string {
	// Collect only the cells belonging to this code, ordered by split type.
	var parts []string
	for _, c := range cells {
		if c.Code == code {
			parts = append(parts, fmt.Sprintf("%s=%d", c.Split, c.Quantity))
		}
	}
	sort.Strings(parts)
	all := append([]string{string(code)}, parts...)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%q", all)))
	return hex.EncodeToString(sum[:])
}

// BuildSamples materializes BlindSample records from the triple-split matrix,
// binding each blind code to the task, generation and a consistency hash.
func BuildSamples(task inspection.TaskID, gen inspection.Generation, m SplitMatrix) []BlindSample {
	var samples []BlindSample
	for _, code := range m.Codes() {
		samples = append(samples, BlindSample{
			Code:            BlindCode(code),
			TaskID:          task,
			Generation:      gen,
			Unblinded:       false,
			GerminationQty:  quantity(m, BlindCode(code), SplitGermination),
			PathogenQty:     quantity(m, BlindCode(code), SplitPathogen),
			MoistureQty:     quantity(m, BlindCode(code), SplitMoisture),
			ConsistencyHash: ConsistencyHash(BlindCode(code), m.Cells),
		})
	}
	return samples
}

func quantity(m SplitMatrix, code BlindCode, s SplitType) int {
	for _, c := range m.Cells {
		if c.Code == code && c.Split == s {
			return c.Quantity
		}
	}
	return 0
}

package blindcode

import (
	"strconv"

	"riceguard/domain"
	"riceguard/inspection"
)

// MemoryGate is a one-way unblinding gate. It rejects early, duplicate and
// cross-generation unblinding and permanently marks a code as opened, so the
// batch blind-code mapping can never be mutated after a reveal.
type MemoryGate struct {
	opened map[string]inspection.Generation
}

// NewMemoryGate builds an empty gate.
func NewMemoryGate() *MemoryGate {
	return &MemoryGate{opened: make(map[string]inspection.Generation)}
}

// Open performs the one-way unblinding for a code at the given generation.
//   - A code already opened at the same generation is a duplicate reveal.
//   - A code already opened at a different generation is a cross-generation
//     reveal.
func (g *MemoryGate) Open(task inspection.TaskID, gen inspection.Generation, code BlindCode) (BlindSample, *domain.Error) {
	if prev, ok := g.opened[string(code)]; ok {
		if prev == gen {
			return BlindSample{}, domain.NewError(domain.CodeBlindDuplicate,
				"blind code already opened", string(code))
		}
		return BlindSample{}, domain.NewError(domain.CodeGenerationStale,
			"blind code already opened at generation", strconv.FormatInt(int64(prev), 10), string(code))
	}
	g.opened[string(code)] = gen
	return BlindSample{
		Code:       code,
		TaskID:     task,
		Generation: gen,
		Unblinded:  true,
	}, nil
}

// Opened reports whether the code has already passed through the gate.
func (g *MemoryGate) Opened(code BlindCode) bool {
	_, ok := g.opened[string(code)]
	return ok
}

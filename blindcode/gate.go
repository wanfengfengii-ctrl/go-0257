package blindcode

import (
	"strconv"

	"riceguard/domain"
	"riceguard/inspection"
)

// gateKey is the per-task scoping of a blind code. A code is only ever opened
// once per task: the one-way reveal is a single-task invariant (rule 3), not a
// global ban on reusing the same code across different batches. A batch that
// has reached a terminal outcome releases its code, so a later batch on another
// field may reuse the same code without being blocked at finalize. This keeps
// the finalize-time gate in agreement with the create-time assertion, which
// already skips terminal tasks, so the verdict is stable across restarts.
type gateKey struct {
	task inspection.TaskID
	code BlindCode
}

// MemoryGate is a one-way unblinding gate. It rejects early, duplicate and
// cross-generation unblinding within a single task and permanently marks a
// code as opened for that task, so the batch blind-code mapping can never be
// mutated after a reveal. The reveal is scoped per task so a reused code on a
// different batch is never mistaken for a duplicate reveal.
type MemoryGate struct {
	opened map[gateKey]inspection.Generation
}

// NewMemoryGate builds an empty gate.
func NewMemoryGate() *MemoryGate {
	return &MemoryGate{opened: make(map[gateKey]inspection.Generation)}
}

// Open performs the one-way unblinding for a code at the given generation.
//   - A code already opened by the same task at the same generation is a
//     duplicate reveal.
//   - A code already opened by the same task at a different generation is a
//     cross-generation reveal.
//
// A code opened by a different task never collides: the reveal is a per-task
// invariant, so a terminal batch's code is reusable by a later batch.
func (g *MemoryGate) Open(task inspection.TaskID, gen inspection.Generation, code BlindCode) (BlindSample, *domain.Error) {
	key := gateKey{task: task, code: code}
	if prev, ok := g.opened[key]; ok {
		if prev == gen {
			return BlindSample{}, domain.NewError(domain.CodeBlindDuplicate,
				"blind code already opened", string(code))
		}
		return BlindSample{}, domain.NewError(domain.CodeGenerationStale,
			"blind code already opened at generation", strconv.FormatInt(int64(prev), 10), string(code))
	}
	g.opened[key] = gen
	return BlindSample{
		Code:       code,
		TaskID:     task,
		Generation: gen,
		Unblinded:  true,
	}, nil
}

// Opened reports whether the code has already passed through the gate for the
// given task.
func (g *MemoryGate) Opened(task inspection.TaskID, code BlindCode) bool {
	_, ok := g.opened[gateKey{task: task, code: code}]
	return ok
}

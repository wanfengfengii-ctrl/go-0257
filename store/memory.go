package store

import (
	"sort"
	"sync"

	"riceguard/blindcode"
	"riceguard/domain"
	"riceguard/germination"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/occupancy"
	"riceguard/pathogen"
	"riceguard/review"
)

// memState is the full in-memory snapshot of the store.
type memState struct {
	tasks         map[inspection.TaskID]*inspection.InspectionTask
	order         []inspection.TaskID
	byLot         map[string]inspection.TaskID
	confirmations map[inspection.TaskID][]inspection.SamplingConfirmation
	blindSamples  map[inspection.TaskID][]blindcode.BlindSample
	splits        map[inspection.TaskID][]blindcode.TripleSplit
	occupancies   map[inspection.TaskID][]occupancy.OccupancySlot
	germinations  map[inspection.TaskID][]germination.GerminationCell
	moisture      map[inspection.TaskID][]measure.MoisturePurityEvidence
	pathogen      map[inspection.TaskID][]pathogen.PathogenEvidence
	attempts      map[inspection.TaskID][]pathogen.Attempt
	reviews       map[inspection.TaskID][]review.ReviewAndFinal
	credentials   map[inspection.TaskID]*inspection.ReleaseCredential
	audit         map[inspection.TaskID][]inspection.AuditEvent
	auditSeq      uint64
	ops           map[string]*inspection.IdempotencyRecord
}

func newMemState() *memState {
	return &memState{
		tasks:         make(map[inspection.TaskID]*inspection.InspectionTask),
		byLot:         make(map[string]inspection.TaskID),
		confirmations: make(map[inspection.TaskID][]inspection.SamplingConfirmation),
		blindSamples:  make(map[inspection.TaskID][]blindcode.BlindSample),
		splits:        make(map[inspection.TaskID][]blindcode.TripleSplit),
		occupancies:   make(map[inspection.TaskID][]occupancy.OccupancySlot),
		germinations:  make(map[inspection.TaskID][]germination.GerminationCell),
		moisture:      make(map[inspection.TaskID][]measure.MoisturePurityEvidence),
		pathogen:      make(map[inspection.TaskID][]pathogen.PathogenEvidence),
		attempts:      make(map[inspection.TaskID][]pathogen.Attempt),
		reviews:       make(map[inspection.TaskID][]review.ReviewAndFinal),
		credentials:   make(map[inspection.TaskID]*inspection.ReleaseCredential),
		audit:         make(map[inspection.TaskID][]inspection.AuditEvent),
		ops:           make(map[string]*inspection.IdempotencyRecord),
	}
}

func cloneMemState(src *memState) *memState {
	dst := newMemState()
	for k, v := range src.tasks {
		cp := *v
		dst.tasks[k] = &cp
	}
	dst.order = append(dst.order, src.order...)
	for k, v := range src.byLot {
		dst.byLot[k] = v
	}
	copySliceMap(dst.confirmations, src.confirmations)
	copySliceMap(dst.blindSamples, src.blindSamples)
	copySliceMap(dst.splits, src.splits)
	copySliceMap(dst.occupancies, src.occupancies)
	copySliceMap(dst.germinations, src.germinations)
	copySliceMap(dst.moisture, src.moisture)
	copySliceMap(dst.pathogen, src.pathogen)
	copySliceMap(dst.attempts, src.attempts)
	copySliceMap(dst.reviews, src.reviews)
	for k, v := range src.credentials {
		cp := *v
		dst.credentials[k] = &cp
	}
	for k, v := range src.audit {
		dst.audit[k] = append([]inspection.AuditEvent(nil), v...)
	}
	dst.auditSeq = src.auditSeq
	for k, v := range src.ops {
		cp := *v
		dst.ops[k] = &cp
	}
	return dst
}

func copySliceMap[T any](dst map[inspection.TaskID][]T, src map[inspection.TaskID][]T) {
	for k, v := range src {
		dst[k] = append([]T(nil), v...)
	}
}

// Memory is an in-memory, mutex-guarded Store implementation used in
// deterministic tests and as the stable foundation. Its Mutate clones the
// state, runs the transaction against the clone and swaps it back only on a
// successful commit, giving real atomicity.
type Memory struct {
	mu    sync.Mutex
	state *memState
	clock domain.LogicalTime
}

// NewMemory builds an empty in-memory store with a zero logical clock.
func NewMemory() *Memory {
	return &Memory{state: newMemState()}
}

// --- Reader (read under lock) ---

func (m *Memory) GetTask(id inspection.TaskID) (*inspection.InspectionTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.getTask(id)
}

func (m *Memory) ListTasks() ([]*inspection.InspectionTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.listTasks(), nil
}

func (m *Memory) ListConfirmations(id inspection.TaskID) ([]inspection.SamplingConfirmation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.confirmations[id], nil
}

func (m *Memory) ListBlindSamples(id inspection.TaskID) ([]blindcode.BlindSample, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.blindSamples[id], nil
}

func (m *Memory) ListSplits(id inspection.TaskID) ([]blindcode.TripleSplit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.splits[id], nil
}

func (m *Memory) ListOccupancies(id inspection.TaskID) ([]occupancy.OccupancySlot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.occupancies[id], nil
}

func (m *Memory) ListOpenOccupancies() ([]occupancy.OccupancySlot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.openOccupancies(), nil
}

func (m *Memory) ListGerminations(id inspection.TaskID) ([]germination.GerminationCell, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.germinations[id], nil
}

func (m *Memory) ListMoisture(id inspection.TaskID) ([]measure.MoisturePurityEvidence, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.moisture[id], nil
}

func (m *Memory) ListPathogen(id inspection.TaskID) ([]pathogen.PathogenEvidence, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.pathogen[id], nil
}

func (m *Memory) ListAttempts(id inspection.TaskID) ([]pathogen.Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.attempts[id], nil
}

func (m *Memory) ListReviews(id inspection.TaskID) ([]review.ReviewAndFinal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.reviews[id], nil
}

func (m *Memory) GetCredential(id inspection.TaskID) (*inspection.ReleaseCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.getCredential(id)
}

func (m *Memory) ListAudit(id inspection.TaskID) ([]inspection.AuditEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.audit[id], nil
}

func (m *Memory) ListAllAudit() ([]inspection.AuditEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.allAudit(), nil
}

func (m *Memory) FindOperation(op string) (*inspection.IdempotencyRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.state.ops[op]
	if !ok {
		return nil, false
	}
	cp := *r
	return &cp, true
}

// --- Store ---

func (m *Memory) Mutate(fn func(Tx) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := cloneMemState(m.state)
	tx := &memTx{state: snapshot, clock: m.clock}
	if err := fn(tx); err != nil {
		return err
	}
	m.state = snapshot
	m.clock = tx.clock
	return nil
}

func (m *Memory) NextTime() domain.LogicalTime {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clock++
	return m.clock
}

func (m *Memory) Close() error { return nil }

// --- memState read helpers ---

func (s *memState) getTask(id inspection.TaskID) (*inspection.InspectionTask, error) {
	t, ok := s.tasks[id]
	if !ok {
		return nil, domain.NewError(domain.CodeNotFound, string(id))
	}
	cp := *t
	return &cp, nil
}

func (s *memState) listTasks() []*inspection.InspectionTask {
	out := make([]*inspection.InspectionTask, 0, len(s.order))
	for _, id := range s.order {
		cp := *s.tasks[id]
		out = append(out, &cp)
	}
	return out
}

func (s *memState) openOccupancies() []occupancy.OccupancySlot {
	var out []occupancy.OccupancySlot
	for _, slots := range s.occupancies {
		for _, sl := range slots {
			if occupancy.Active(sl) {
				out = append(out, sl)
			}
		}
	}
	return out
}

func (s *memState) allAudit() []inspection.AuditEvent {
	var out []inspection.AuditEvent
	for _, events := range s.audit {
		out = append(out, events...)
	}
	// Stable ordering by persisted sequence number.
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

func (s *memState) getCredential(id inspection.TaskID) (*inspection.ReleaseCredential, error) {
	c, ok := s.credentials[id]
	if !ok {
		return nil, domain.NewError(domain.CodeNotFound, string(id))
	}
	cp := *c
	return &cp, nil
}

// memTx is a transaction over a cloned state snapshot.
type memTx struct {
	state *memState
	clock domain.LogicalTime
}

// --- memTx Reader ---

func (t *memTx) GetTask(id inspection.TaskID) (*inspection.InspectionTask, error) {
	return t.state.getTask(id)
}
func (t *memTx) ListTasks() ([]*inspection.InspectionTask, error) {
	return t.state.listTasks(), nil
}
func (t *memTx) ListConfirmations(id inspection.TaskID) ([]inspection.SamplingConfirmation, error) {
	return t.state.confirmations[id], nil
}
func (t *memTx) ListBlindSamples(id inspection.TaskID) ([]blindcode.BlindSample, error) {
	return t.state.blindSamples[id], nil
}
func (t *memTx) ListSplits(id inspection.TaskID) ([]blindcode.TripleSplit, error) {
	return t.state.splits[id], nil
}
func (t *memTx) ListOccupancies(id inspection.TaskID) ([]occupancy.OccupancySlot, error) {
	return t.state.occupancies[id], nil
}
func (t *memTx) ListOpenOccupancies() ([]occupancy.OccupancySlot, error) {
	return t.state.openOccupancies(), nil
}
func (t *memTx) ListGerminations(id inspection.TaskID) ([]germination.GerminationCell, error) {
	return t.state.germinations[id], nil
}
func (t *memTx) ListMoisture(id inspection.TaskID) ([]measure.MoisturePurityEvidence, error) {
	return t.state.moisture[id], nil
}
func (t *memTx) ListPathogen(id inspection.TaskID) ([]pathogen.PathogenEvidence, error) {
	return t.state.pathogen[id], nil
}
func (t *memTx) ListReviews(id inspection.TaskID) ([]review.ReviewAndFinal, error) {
	return t.state.reviews[id], nil
}
func (t *memTx) ListAttempts(id inspection.TaskID) ([]pathogen.Attempt, error) {
	return t.state.attempts[id], nil
}
func (t *memTx) GetCredential(id inspection.TaskID) (*inspection.ReleaseCredential, error) {
	return t.state.getCredential(id)
}
func (t *memTx) ListAudit(id inspection.TaskID) ([]inspection.AuditEvent, error) {
	return t.state.audit[id], nil
}
func (t *memTx) ListAllAudit() ([]inspection.AuditEvent, error) {
	return t.state.allAudit(), nil
}
func (t *memTx) FindOperation(op string) (*inspection.IdempotencyRecord, bool) {
	r, ok := t.state.ops[op]
	if !ok {
		return nil, false
	}
	cp := *r
	return &cp, true
}

// --- memTx writes ---

func (t *memTx) SaveTask(task *inspection.InspectionTask) error {
	if _, exists := t.state.byLot[task.SeedLot]; exists {
		if t.state.byLot[task.SeedLot] != task.ID {
			return domain.NewError(domain.CodeOccupancyConflict, "seed lot already bound", task.SeedLot)
		}
	}
	if _, exists := t.state.tasks[task.ID]; !exists {
		t.state.order = append(t.state.order, task.ID)
		t.state.byLot[task.SeedLot] = task.ID
	}
	cp := *task
	t.state.tasks[task.ID] = &cp
	return nil
}

func (t *memTx) SaveConfirmation(c inspection.SamplingConfirmation) error {
	t.state.confirmations[c.TaskID] = append(t.state.confirmations[c.TaskID], c)
	return nil
}

func (t *memTx) SaveBlindSample(b blindcode.BlindSample) error {
	t.state.blindSamples[b.TaskID] = append(t.state.blindSamples[b.TaskID], b)
	return nil
}

func (t *memTx) SaveSplit(s blindcode.TripleSplit) error {
	t.state.splits[s.TaskID] = append(t.state.splits[s.TaskID], s)
	return nil
}

func (t *memTx) MarkBlindUnblinded(task inspection.TaskID, code blindcode.BlindCode) error {
	samples := t.state.blindSamples[task]
	for i := range samples {
		if samples[i].Code == code {
			samples[i].Unblinded = true
		}
	}
	return nil
}

func (t *memTx) SaveOccupancy(o occupancy.OccupancySlot) error {
	t.state.occupancies[o.TaskID] = append(t.state.occupancies[o.TaskID], o)
	return nil
}

func (t *memTx) SaveGermination(g germination.GerminationCell) error {
	t.state.germinations[g.TaskID] = append(t.state.germinations[g.TaskID], g)
	return nil
}

func (t *memTx) SaveMoisture(m measure.MoisturePurityEvidence) error {
	t.state.moisture[m.TaskID] = append(t.state.moisture[m.TaskID], m)
	return nil
}

func (t *memTx) SavePathogen(p pathogen.PathogenEvidence) error {
	t.state.pathogen[p.TaskID] = append(t.state.pathogen[p.TaskID], p)
	return nil
}

func (t *memTx) SaveAttempt(a pathogen.Attempt) error {
	t.state.attempts[a.TaskID] = append(t.state.attempts[a.TaskID], a)
	return nil
}

func (t *memTx) SaveReview(r review.ReviewAndFinal) error {
	t.state.reviews[r.TaskID] = append(t.state.reviews[r.TaskID], r)
	return nil
}

func (t *memTx) SaveCredential(c inspection.ReleaseCredential) error {
	cp := c
	t.state.credentials[c.TaskID] = &cp
	return nil
}

func (t *memTx) RecordOperation(r inspection.IdempotencyRecord) error {
	cp := r
	t.state.ops[r.OperationID] = &cp
	return nil
}

func (t *memTx) AppendAudit(e inspection.AuditEvent) error {
	t.state.auditSeq++
	cp := e
	cp.Seq = t.state.auditSeq
	t.state.audit[e.TaskID] = append(t.state.audit[e.TaskID], cp)
	return nil
}

func (t *memTx) NextTime() domain.LogicalTime {
	t.clock++
	return t.clock
}

func (t *memTx) Commit() error   { return nil }
func (t *memTx) Rollback() error { return nil }

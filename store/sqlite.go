package store

import (
	"database/sql"
	"encoding/json"

	_ "modernc.org/sqlite"

	"riceguard/blindcode"
	"riceguard/domain"
	"riceguard/germination"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/occupancy"
	"riceguard/pathogen"
	"riceguard/review"
)

const clockKey = "logical_clock"

// SQLite is the production SQLite WAL store. It persists the task aggregate,
// occupancy records, evidence tables, idempotency operations, instrument
// attempts (encoded in evidence), audit events and terminal credentials, and
// supports deterministic restart recovery by reopening the WAL database and
// continuing the persisted logical clock.
type SQLite struct {
	db *sql.DB
}

// OpenSQLite opens (or creates) a SQLite database at path, enables WAL
// journal mode and applies the schema. WAL mode makes restart recovery
// automatic: an unclean shutdown replays the write-ahead log on reopen.
func OpenSQLite(path string) (*SQLite, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // serialize writes; matches single-node design
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(schemaDDL); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureClock(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLite{db: db}, nil
}

func ensureClock(db *sql.DB) error {
	// The logical clock backs task-id allocation (task-<n>) and must keep
	// increasing across restarts so a fresh post-restart task never reuses a
	// task id that still belongs to an existing batch. Recover the clock to a
	// value at least as large as the greatest persisted timestamp: the next
	// NextTime() then yields a strictly greater value and the new task id is
	// distinct from every existing one. created_at is the logical time each
	// task was minted with, so MAX(created_at) is the safe floor.
	var floor uint64
	if err := db.QueryRow(`SELECT COALESCE(MAX(created_at), 0) FROM tasks`).Scan(&floor); err != nil {
		return err
	}
	var cur uint64
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = ?`, clockKey).Scan(&cur); err != nil {
		if err == sql.ErrNoRows {
			cur = 0
		} else {
			return err
		}
	}
	if floor > cur {
		cur = floor
	}
	if cur == 0 {
		_, err := db.Exec(`INSERT INTO meta(key, value) VALUES (?, 0)`, clockKey)
		return err
	}
	_, err := db.Exec(`INSERT INTO meta(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, clockKey, cur)
	return err
}

func (s *SQLite) Close() error { return s.db.Close() }

// --- Reader ---

func (s *SQLite) GetTask(id inspection.TaskID) (*inspection.InspectionTask, error) {
	return scanTask(s.db.QueryRow(`SELECT id, seed_lot, field, variety, female_parent, male_parent,
		female_cert, male_cert, cert_summary, status, generation, moisture_max, pathogen_max,
		min_purity, grain_count, chamber, chamber_start, chamber_end, plate, wells, day_ages,
		blind_allocs, reviewer_roster, terminal_version, terminal_outcome, created_at
		FROM tasks WHERE id = ?`, id))
}

func (s *SQLite) ListTasks() ([]*inspection.InspectionTask, error) {
	rows, err := s.db.Query(`SELECT id, seed_lot, field, variety, female_parent, male_parent,
		female_cert, male_cert, cert_summary, status, generation, moisture_max, pathogen_max,
		min_purity, grain_count, chamber, chamber_start, chamber_end, plate, wells, day_ages,
		blind_allocs, reviewer_roster, terminal_version, terminal_outcome, created_at
		FROM tasks ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*inspection.InspectionTask
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *SQLite) ListConfirmations(id inspection.TaskID) ([]inspection.SamplingConfirmation, error) {
	rows, err := s.db.Query(`SELECT task_id, reviewer, field, seed_lot, blind_seal, sample_count, generation, operation_id
		FROM confirmations WHERE task_id = ? ORDER BY rowid`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []inspection.SamplingConfirmation
	for rows.Next() {
		var c inspection.SamplingConfirmation
		if err := rows.Scan(&c.TaskID, &c.Reviewer, &c.Field, &c.SeedLot, &c.BlindSeal, &c.SampleCount, &c.Generation, &c.OperationID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLite) ListBlindSamples(id inspection.TaskID) ([]blindcode.BlindSample, error) {
	rows, err := s.db.Query(`SELECT task_id, code, generation, unblinded, germination_qty, pathogen_qty, moisture_qty, consistency_hash
		FROM blind_samples WHERE task_id = ? ORDER BY code`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []blindcode.BlindSample
	for rows.Next() {
		var b blindcode.BlindSample
		var unblinded int
		if err := rows.Scan(&b.TaskID, &b.Code, &b.Generation, &unblinded, &b.GerminationQty, &b.PathogenQty, &b.MoistureQty, &b.ConsistencyHash); err != nil {
			return nil, err
		}
		b.Unblinded = unblinded != 0
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *SQLite) ListSplits(id inspection.TaskID) ([]blindcode.TripleSplit, error) {
	rows, err := s.db.Query(`SELECT task_id, code, split, quantity FROM splits WHERE task_id = ? ORDER BY code, split`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []blindcode.TripleSplit
	for rows.Next() {
		var sp blindcode.TripleSplit
		if err := rows.Scan(&sp.TaskID, &sp.Code, &sp.Split, &sp.Quantity); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func (s *SQLite) ListOccupancies(id inspection.TaskID) ([]occupancy.OccupancySlot, error) {
	rows, err := s.db.Query(`SELECT task_id, chamber, start, end, plate, well, blind_code, generation, status, release_reason
		FROM occupancies WHERE task_id = ? ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOccupancies(rows)
}

func (s *SQLite) ListOpenOccupancies() ([]occupancy.OccupancySlot, error) {
	rows, err := s.db.Query(`SELECT task_id, chamber, start, end, plate, well, blind_code, generation, status, release_reason
		FROM occupancies WHERE status IN ('reserved','occupied') ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOccupancies(rows)
}

func scanOccupancies(rows *sql.Rows) ([]occupancy.OccupancySlot, error) {
	var out []occupancy.OccupancySlot
	for rows.Next() {
		var o occupancy.OccupancySlot
		if err := rows.Scan(&o.TaskID, &o.Chamber, &o.Start, &o.End, &o.Plate, &o.Well, &o.BlindCode, &o.Generation, &o.Status, &o.ReleaseReason); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *SQLite) ListGerminations(id inspection.TaskID) ([]germination.GerminationCell, error) {
	rows, err := s.db.Query(`SELECT task_id, blind_code, split, day_age, normal, abnormal, dead, retest, collector, operation_id, valid
		FROM germinations WHERE task_id = ? ORDER BY blind_code, day_age`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []germination.GerminationCell
	for rows.Next() {
		var g germination.GerminationCell
		var retest, valid int
		if err := rows.Scan(&g.TaskID, &g.BlindCode, &g.Split, &g.DayAge, &g.Normal, &g.Abnormal, &g.Dead, &retest, &g.Collector, &g.OperationID, &valid); err != nil {
			return nil, err
		}
		g.Retest = retest != 0
		g.Valid = valid != 0
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *SQLite) ListMoisture(id inspection.TaskID) ([]measure.MoisturePurityEvidence, error) {
	rows, err := s.db.Query(`SELECT task_id, moisture, purity_grains, thousand_grain, derived_purity, pass_threshold, attempt_id, collector, version
		FROM moisture WHERE task_id = ? ORDER BY version`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []measure.MoisturePurityEvidence
	for rows.Next() {
		var m measure.MoisturePurityEvidence
		var pass int
		if err := rows.Scan(&m.TaskID, &m.Moisture, &m.PurityGrains, &m.ThousandGrain, &m.DerivedPurity, &pass, &m.AttemptID, &m.Collector, &m.Version); err != nil {
			return nil, err
		}
		m.PassThreshold = pass != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *SQLite) ListPathogen(id inspection.TaskID) ([]pathogen.PathogenEvidence, error) {
	rows, err := s.db.Query(`SELECT task_id, blind_code, plate, well, reading, verdict, device_status, verifier, rejudge_gen, contaminated, late_isolated
		FROM pathogen WHERE task_id = ? ORDER BY plate, well`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pathogen.PathogenEvidence
	for rows.Next() {
		var p pathogen.PathogenEvidence
		var cont, late int
		if err := rows.Scan(&p.TaskID, &p.BlindCode, &p.Plate, &p.Well, &p.Reading, &p.Verdict, &p.DeviceStatus, &p.Verifier, &p.RejudgeGen, &cont, &late); err != nil {
			return nil, err
		}
		p.Contaminated = cont != 0
		p.LateIsolated = late != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) ListAttempts(id inspection.TaskID) ([]pathogen.Attempt, error) {
	rows, err := s.db.Query(`SELECT task_id, plate, well, attempt, status, reading, retryable, logical_seq
		FROM attempts WHERE task_id = ? ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pathogen.Attempt
	for rows.Next() {
		var a pathogen.Attempt
		var retryable int
		if err := rows.Scan(&a.TaskID, &a.Plate, &a.Well, &a.Attempt, &a.Status, &a.Reading, &retryable, &a.LogicalSeq); err != nil {
			return nil, err
		}
		a.Retryable = retryable != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *SQLite) ListReviews(id inspection.TaskID) ([]review.ReviewAndFinal, error) {
	rows, err := s.db.Query(`SELECT task_id, reviewer, scope, qualified, conclusion, outcome, isolation_reason, cancel_reason, terminal_version
		FROM reviews WHERE task_id = ? ORDER BY rowid`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []review.ReviewAndFinal
	for rows.Next() {
		var r review.ReviewAndFinal
		var qual int
		if err := rows.Scan(&r.TaskID, &r.Reviewer, &r.Scope, &qual, &r.Conclusion, &r.Outcome, &r.IsolationReason, &r.CancelReason, &r.TerminalVersion); err != nil {
			return nil, err
		}
		r.Qualified = qual != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLite) GetCredential(id inspection.TaskID) (*inspection.ReleaseCredential, error) {
	return scanCredential(s.db.QueryRow(`SELECT task_id, credential, version FROM credentials WHERE task_id = ?`, id))
}

func scanCredential(row interface{ Scan(...any) error }) (*inspection.ReleaseCredential, error) {
	var c inspection.ReleaseCredential
	if err := row.Scan(&c.TaskID, &c.Credential, &c.Version); err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.NewError(domain.CodeNotFound)
		}
		return nil, err
	}
	return &c, nil
}

func (s *SQLite) ListAudit(id inspection.TaskID) ([]inspection.AuditEvent, error) {
	rows, err := s.db.Query(`SELECT seq, logical_time, actor, task_status, action, code, reasons, blind_codes, day_ages, plate_wells
		FROM audit WHERE task_id = ? ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAudit(rows)
}

func scanAudit(rows *sql.Rows) ([]inspection.AuditEvent, error) {
	var out []inspection.AuditEvent
	for rows.Next() {
		var a inspection.AuditEvent
		var reasons, codes, days, wells string
		if err := rows.Scan(&a.Seq, &a.LogicalTime, &a.Actor, &a.TaskStatus, &a.Action, &a.Code, &reasons, &codes, &days, &wells); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(reasons), &a.Reasons)
		_ = json.Unmarshal([]byte(codes), &a.BlindCodes)
		_ = json.Unmarshal([]byte(days), &a.DayAges)
		_ = json.Unmarshal([]byte(wells), &a.PlateWells)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *SQLite) ListAllAudit() ([]inspection.AuditEvent, error) {
	rows, err := s.db.Query(`SELECT seq, logical_time, actor, task_status, action, code, reasons, blind_codes, day_ages, plate_wells
		FROM audit ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAudit(rows)
}

func (s *SQLite) FindOperation(op string) (*inspection.IdempotencyRecord, bool) {
	row := s.db.QueryRow(`SELECT operation_id, task_id, generation, request_digest, response_code, reasons, result_digest
		FROM operations WHERE operation_id = ?`, op)
	var r inspection.IdempotencyRecord
	var reasons string
	if err := row.Scan(&r.OperationID, &r.TaskID, &r.Generation, &r.RequestDigest, &r.ResponseCode, &reasons, &r.ResultDigest); err != nil {
		if err == sql.ErrNoRows {
			return nil, false
		}
		return nil, false
	}
	_ = json.Unmarshal([]byte(reasons), &r.Reasons)
	return &r, true
}

// --- Store ---

func (s *SQLite) Mutate(fn func(Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stx := &sqliteTx{tx: tx}
	if err := fn(stx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *SQLite) NextTime() domain.LogicalTime {
	// Outside a transaction, issue a time via an atomic upsert of the clock.
	tx, err := s.db.Begin()
	if err != nil {
		return 0
	}
	defer tx.Rollback()
	var v uint64
	err = tx.QueryRow(`SELECT value FROM meta WHERE key = ?`, clockKey).Scan(&v)
	if err != nil {
		return 0
	}
	v++
	if _, err := tx.Exec(`UPDATE meta SET value = ? WHERE key = ?`, v, clockKey); err != nil {
		return 0
	}
	if err := tx.Commit(); err != nil {
		return 0
	}
	return domain.LogicalTime(v)
}

// --- sqliteTx ---

type sqliteTx struct {
	tx *sql.Tx
}

func (t *sqliteTx) NextTime() domain.LogicalTime {
	var v uint64
	if err := t.tx.QueryRow(`SELECT value FROM meta WHERE key = ?`, clockKey).Scan(&v); err != nil {
		return 0
	}
	v++
	if _, err := t.tx.Exec(`UPDATE meta SET value = ? WHERE key = ?`, v, clockKey); err != nil {
		return 0
	}
	return domain.LogicalTime(v)
}

func (t *sqliteTx) Commit() error   { return t.tx.Commit() }
func (t *sqliteTx) Rollback() error { return t.tx.Rollback() }

// Reader implementations delegate to the store read helpers but through the
// transaction handle for a consistent snapshot.

func (t *sqliteTx) GetTask(id inspection.TaskID) (*inspection.InspectionTask, error) {
	return scanTask(t.tx.QueryRow(`SELECT id, seed_lot, field, variety, female_parent, male_parent,
		female_cert, male_cert, cert_summary, status, generation, moisture_max, pathogen_max,
		min_purity, grain_count, chamber, chamber_start, chamber_end, plate, wells, day_ages,
		blind_allocs, reviewer_roster, terminal_version, terminal_outcome, created_at
		FROM tasks WHERE id = ?`, id))
}

func (t *sqliteTx) ListTasks() ([]*inspection.InspectionTask, error) {
	rows, err := t.tx.Query(`SELECT id, seed_lot, field, variety, female_parent, male_parent,
		female_cert, male_cert, cert_summary, status, generation, moisture_max, pathogen_max,
		min_purity, grain_count, chamber, chamber_start, chamber_end, plate, wells, day_ages,
		blind_allocs, reviewer_roster, terminal_version, terminal_outcome, created_at
		FROM tasks ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*inspection.InspectionTask
	for rows.Next() {
		tt, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tt)
	}
	return out, rows.Err()
}

func (t *sqliteTx) ListConfirmations(id inspection.TaskID) ([]inspection.SamplingConfirmation, error) {
	rows, err := t.tx.Query(`SELECT task_id, reviewer, field, seed_lot, blind_seal, sample_count, generation, operation_id
		FROM confirmations WHERE task_id = ? ORDER BY rowid`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []inspection.SamplingConfirmation
	for rows.Next() {
		var c inspection.SamplingConfirmation
		if err := rows.Scan(&c.TaskID, &c.Reviewer, &c.Field, &c.SeedLot, &c.BlindSeal, &c.SampleCount, &c.Generation, &c.OperationID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (t *sqliteTx) ListBlindSamples(id inspection.TaskID) ([]blindcode.BlindSample, error) {
	rows, err := t.tx.Query(`SELECT task_id, code, generation, unblinded, germination_qty, pathogen_qty, moisture_qty, consistency_hash
		FROM blind_samples WHERE task_id = ? ORDER BY code`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []blindcode.BlindSample
	for rows.Next() {
		var b blindcode.BlindSample
		var unblinded int
		if err := rows.Scan(&b.TaskID, &b.Code, &b.Generation, &unblinded, &b.GerminationQty, &b.PathogenQty, &b.MoistureQty, &b.ConsistencyHash); err != nil {
			return nil, err
		}
		b.Unblinded = unblinded != 0
		out = append(out, b)
	}
	return out, rows.Err()
}

func (t *sqliteTx) ListSplits(id inspection.TaskID) ([]blindcode.TripleSplit, error) {
	rows, err := t.tx.Query(`SELECT task_id, code, split, quantity FROM splits WHERE task_id = ? ORDER BY code, split`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []blindcode.TripleSplit
	for rows.Next() {
		var sp blindcode.TripleSplit
		if err := rows.Scan(&sp.TaskID, &sp.Code, &sp.Split, &sp.Quantity); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func (t *sqliteTx) ListOccupancies(id inspection.TaskID) ([]occupancy.OccupancySlot, error) {
	rows, err := t.tx.Query(`SELECT task_id, chamber, start, end, plate, well, blind_code, generation, status, release_reason
		FROM occupancies WHERE task_id = ? ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOccupancies(rows)
}

func (t *sqliteTx) ListOpenOccupancies() ([]occupancy.OccupancySlot, error) {
	rows, err := t.tx.Query(`SELECT task_id, chamber, start, end, plate, well, blind_code, generation, status, release_reason
		FROM occupancies WHERE status IN ('reserved','occupied') ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOccupancies(rows)
}

func (t *sqliteTx) ListGerminations(id inspection.TaskID) ([]germination.GerminationCell, error) {
	rows, err := t.tx.Query(`SELECT task_id, blind_code, split, day_age, normal, abnormal, dead, retest, collector, operation_id, valid
		FROM germinations WHERE task_id = ? ORDER BY blind_code, day_age`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []germination.GerminationCell
	for rows.Next() {
		var g germination.GerminationCell
		var retest, valid int
		if err := rows.Scan(&g.TaskID, &g.BlindCode, &g.Split, &g.DayAge, &g.Normal, &g.Abnormal, &g.Dead, &retest, &g.Collector, &g.OperationID, &valid); err != nil {
			return nil, err
		}
		g.Retest = retest != 0
		g.Valid = valid != 0
		out = append(out, g)
	}
	return out, rows.Err()
}

func (t *sqliteTx) ListMoisture(id inspection.TaskID) ([]measure.MoisturePurityEvidence, error) {
	rows, err := t.tx.Query(`SELECT task_id, moisture, purity_grains, thousand_grain, derived_purity, pass_threshold, attempt_id, collector, version
		FROM moisture WHERE task_id = ? ORDER BY version`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []measure.MoisturePurityEvidence
	for rows.Next() {
		var m measure.MoisturePurityEvidence
		var pass int
		if err := rows.Scan(&m.TaskID, &m.Moisture, &m.PurityGrains, &m.ThousandGrain, &m.DerivedPurity, &pass, &m.AttemptID, &m.Collector, &m.Version); err != nil {
			return nil, err
		}
		m.PassThreshold = pass != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

func (t *sqliteTx) ListPathogen(id inspection.TaskID) ([]pathogen.PathogenEvidence, error) {
	rows, err := t.tx.Query(`SELECT task_id, blind_code, plate, well, reading, verdict, device_status, verifier, rejudge_gen, contaminated, late_isolated
		FROM pathogen WHERE task_id = ? ORDER BY plate, well`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pathogen.PathogenEvidence
	for rows.Next() {
		var p pathogen.PathogenEvidence
		var cont, late int
		if err := rows.Scan(&p.TaskID, &p.BlindCode, &p.Plate, &p.Well, &p.Reading, &p.Verdict, &p.DeviceStatus, &p.Verifier, &p.RejudgeGen, &cont, &late); err != nil {
			return nil, err
		}
		p.Contaminated = cont != 0
		p.LateIsolated = late != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func (t *sqliteTx) ListAttempts(id inspection.TaskID) ([]pathogen.Attempt, error) {
	rows, err := t.tx.Query(`SELECT task_id, plate, well, attempt, status, reading, retryable, logical_seq
		FROM attempts WHERE task_id = ? ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pathogen.Attempt
	for rows.Next() {
		var a pathogen.Attempt
		var retryable int
		if err := rows.Scan(&a.TaskID, &a.Plate, &a.Well, &a.Attempt, &a.Status, &a.Reading, &retryable, &a.LogicalSeq); err != nil {
			return nil, err
		}
		a.Retryable = retryable != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

func (t *sqliteTx) ListReviews(id inspection.TaskID) ([]review.ReviewAndFinal, error) {
	rows, err := t.tx.Query(`SELECT task_id, reviewer, scope, qualified, conclusion, outcome, isolation_reason, cancel_reason, terminal_version
		FROM reviews WHERE task_id = ? ORDER BY rowid`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []review.ReviewAndFinal
	for rows.Next() {
		var r review.ReviewAndFinal
		var qual int
		if err := rows.Scan(&r.TaskID, &r.Reviewer, &r.Scope, &qual, &r.Conclusion, &r.Outcome, &r.IsolationReason, &r.CancelReason, &r.TerminalVersion); err != nil {
			return nil, err
		}
		r.Qualified = qual != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (t *sqliteTx) GetCredential(id inspection.TaskID) (*inspection.ReleaseCredential, error) {
	return scanCredential(t.tx.QueryRow(`SELECT task_id, credential, version FROM credentials WHERE task_id = ?`, id))
}

func (t *sqliteTx) ListAudit(id inspection.TaskID) ([]inspection.AuditEvent, error) {
	rows, err := t.tx.Query(`SELECT seq, logical_time, actor, task_status, action, code, reasons, blind_codes, day_ages, plate_wells
		FROM audit WHERE task_id = ? ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAudit(rows)
}

func (t *sqliteTx) ListAllAudit() ([]inspection.AuditEvent, error) {
	rows, err := t.tx.Query(`SELECT seq, logical_time, actor, task_status, action, code, reasons, blind_codes, day_ages, plate_wells
		FROM audit ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAudit(rows)
}

func (t *sqliteTx) FindOperation(op string) (*inspection.IdempotencyRecord, bool) {
	row := t.tx.QueryRow(`SELECT operation_id, task_id, generation, request_digest, response_code, reasons, result_digest
		FROM operations WHERE operation_id = ?`, op)
	var r inspection.IdempotencyRecord
	var reasons string
	if err := row.Scan(&r.OperationID, &r.TaskID, &r.Generation, &r.RequestDigest, &r.ResponseCode, &reasons, &r.ResultDigest); err != nil {
		return nil, false
	}
	_ = json.Unmarshal([]byte(reasons), &r.Reasons)
	return &r, true
}

// --- writes ---

func (t *sqliteTx) SaveTask(task *inspection.InspectionTask) error {
	wells, _ := json.Marshal(task.Wells)
	dayAges, _ := json.Marshal(task.DayAges)
	allocs, _ := json.Marshal(task.BlindAllocs)
	roster, _ := json.Marshal(task.ReviewerRoster)
	_, err := t.tx.Exec(`INSERT INTO tasks (id, seed_lot, field, variety, female_parent, male_parent,
		female_cert, male_cert, cert_summary, status, generation, moisture_max, pathogen_max,
		min_purity, grain_count, chamber, chamber_start, chamber_end, plate, wells, day_ages,
		blind_allocs, reviewer_roster, terminal_version, terminal_outcome, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			status=excluded.status, generation=excluded.generation,
			terminal_version=excluded.terminal_version, terminal_outcome=excluded.terminal_outcome`,
		task.ID, task.SeedLot, task.Field, task.Variety, task.FemaleParent, task.MaleParent,
		task.FemaleCert, task.MaleCert, task.CertSummary, task.Status, task.Generation, task.MoistureMax,
		task.PathogenMax, task.MinPurity, task.GrainCount, task.Chamber, task.ChamberStart, task.ChamberEnd,
		task.Plate, string(wells), string(dayAges), string(allocs), string(roster),
		task.TerminalVersion, task.TerminalOutcome, task.CreatedAt)
	return err
}

func (t *sqliteTx) SaveConfirmation(c inspection.SamplingConfirmation) error {
	_, err := t.tx.Exec(`INSERT INTO confirmations (task_id, reviewer, field, seed_lot, blind_seal, sample_count, generation, operation_id)
		VALUES (?,?,?,?,?,?,?,?)`,
		c.TaskID, c.Reviewer, c.Field, c.SeedLot, c.BlindSeal, c.SampleCount, c.Generation, c.OperationID)
	return err
}

func (t *sqliteTx) SaveBlindSample(b blindcode.BlindSample) error {
	unblinded := 0
	if b.Unblinded {
		unblinded = 1
	}
	_, err := t.tx.Exec(`INSERT INTO blind_samples (task_id, code, generation, unblinded, germination_qty, pathogen_qty, moisture_qty, consistency_hash)
		VALUES (?,?,?,?,?,?,?,?)`,
		b.TaskID, b.Code, b.Generation, unblinded, b.GerminationQty, b.PathogenQty, b.MoistureQty, b.ConsistencyHash)
	return err
}

func (t *sqliteTx) SaveSplit(sp blindcode.TripleSplit) error {
	_, err := t.tx.Exec(`INSERT INTO splits (task_id, code, split, quantity) VALUES (?,?,?,?)`,
		sp.TaskID, sp.Code, sp.Split, sp.Quantity)
	return err
}

func (t *sqliteTx) MarkBlindUnblinded(task inspection.TaskID, code blindcode.BlindCode) error {
	_, err := t.tx.Exec(`UPDATE blind_samples SET unblinded = 1 WHERE task_id = ? AND code = ?`, task, code)
	return err
}

func (t *sqliteTx) SaveOccupancy(o occupancy.OccupancySlot) error {
	_, err := t.tx.Exec(`INSERT INTO occupancies (task_id, chamber, start, end, plate, well, blind_code, generation, status, release_reason)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		o.TaskID, o.Chamber, o.Start, o.End, o.Plate, o.Well, o.BlindCode, o.Generation, o.Status, o.ReleaseReason)
	return err
}

func (t *sqliteTx) SaveGermination(g germination.GerminationCell) error {
	retest, valid := 0, 0
	if g.Retest {
		retest = 1
	}
	if g.Valid {
		valid = 1
	}
	_, err := t.tx.Exec(`INSERT INTO germinations (task_id, blind_code, split, day_age, normal, abnormal, dead, retest, collector, operation_id, valid)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		g.TaskID, g.BlindCode, g.Split, g.DayAge, g.Normal, g.Abnormal, g.Dead, retest, g.Collector, g.OperationID, valid)
	return err
}

func (t *sqliteTx) SaveMoisture(m measure.MoisturePurityEvidence) error {
	pass := 0
	if m.PassThreshold {
		pass = 1
	}
	_, err := t.tx.Exec(`INSERT INTO moisture (task_id, moisture, purity_grains, thousand_grain, derived_purity, pass_threshold, attempt_id, collector, version)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		m.TaskID, m.Moisture, m.PurityGrains, m.ThousandGrain, m.DerivedPurity, pass, m.AttemptID, m.Collector, m.Version)
	return err
}

func (t *sqliteTx) SavePathogen(p pathogen.PathogenEvidence) error {
	cont, late := 0, 0
	if p.Contaminated {
		cont = 1
	}
	if p.LateIsolated {
		late = 1
	}
	_, err := t.tx.Exec(`INSERT INTO pathogen (task_id, blind_code, plate, well, reading, verdict, device_status, verifier, rejudge_gen, contaminated, late_isolated)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		p.TaskID, p.BlindCode, p.Plate, p.Well, p.Reading, p.Verdict, p.DeviceStatus, p.Verifier, p.RejudgeGen, cont, late)
	return err
}

func (t *sqliteTx) SaveAttempt(a pathogen.Attempt) error {
	retryable := 0
	if a.Retryable {
		retryable = 1
	}
	_, err := t.tx.Exec(`INSERT INTO attempts (task_id, plate, well, attempt, status, reading, retryable, logical_seq)
		VALUES (?,?,?,?,?,?,?,?)`,
		a.TaskID, a.Plate, a.Well, a.Attempt, a.Status, a.Reading, retryable, a.LogicalSeq)
	return err
}

func (t *sqliteTx) SaveReview(r review.ReviewAndFinal) error {
	qual := 0
	if r.Qualified {
		qual = 1
	}
	_, err := t.tx.Exec(`INSERT INTO reviews (task_id, reviewer, scope, qualified, conclusion, outcome, isolation_reason, cancel_reason, terminal_version)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		r.TaskID, r.Reviewer, r.Scope, qual, r.Conclusion, r.Outcome, r.IsolationReason, r.CancelReason, r.TerminalVersion)
	return err
}

func (t *sqliteTx) SaveCredential(c inspection.ReleaseCredential) error {
	_, err := t.tx.Exec(`INSERT INTO credentials (task_id, credential, version) VALUES (?,?,?)
		ON CONFLICT(task_id) DO UPDATE SET credential=excluded.credential, version=excluded.version`,
		c.TaskID, c.Credential, c.Version)
	return err
}

func (t *sqliteTx) RecordOperation(r inspection.IdempotencyRecord) error {
	reasons, _ := json.Marshal(r.Reasons)
	_, err := t.tx.Exec(`INSERT INTO operations (operation_id, task_id, generation, request_digest, response_code, reasons, result_digest)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(operation_id) DO UPDATE SET task_id=excluded.task_id, generation=excluded.generation,
			request_digest=excluded.request_digest, response_code=excluded.response_code, reasons=excluded.reasons, result_digest=excluded.result_digest`,
		r.OperationID, r.TaskID, r.Generation, r.RequestDigest, r.ResponseCode, string(reasons), r.ResultDigest)
	return err
}

func (t *sqliteTx) AppendAudit(e inspection.AuditEvent) error {
	reasons, _ := json.Marshal(e.Reasons)
	codes, _ := json.Marshal(e.BlindCodes)
	days, _ := json.Marshal(e.DayAges)
	wells, _ := json.Marshal(e.PlateWells)
	_, err := t.tx.Exec(`INSERT INTO audit (task_id, logical_time, actor, task_status, action, code, reasons, blind_codes, day_ages, plate_wells)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		e.TaskID, e.LogicalTime, e.Actor, e.TaskStatus, e.Action, e.Code, string(reasons), string(codes), string(days), string(wells))
	return err
}

// scanTask decodes a task row. When rows is non-nil it advances the cursor,
// otherwise it scans from the supplied row.
type rowScanner interface {
	Scan(...any) error
}

func scanTask(row rowScanner) (*inspection.InspectionTask, error) {
	var t inspection.InspectionTask
	var wells, dayAges, allocs, roster string
	if err := row.Scan(&t.ID, &t.SeedLot, &t.Field, &t.Variety, &t.FemaleParent, &t.MaleParent,
		&t.FemaleCert, &t.MaleCert, &t.CertSummary, &t.Status, &t.Generation, &t.MoistureMax,
		&t.PathogenMax, &t.MinPurity, &t.GrainCount, &t.Chamber, &t.ChamberStart, &t.ChamberEnd,
		&t.Plate, &wells, &dayAges, &allocs, &roster, &t.TerminalVersion, &t.TerminalOutcome, &t.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.NewError(domain.CodeNotFound)
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(wells), &t.Wells)
	_ = json.Unmarshal([]byte(dayAges), &t.DayAges)
	_ = json.Unmarshal([]byte(allocs), &t.BlindAllocs)
	_ = json.Unmarshal([]byte(roster), &t.ReviewerRoster)
	return &t, nil
}

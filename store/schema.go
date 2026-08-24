package store

// schemaDDL is the SQLite schema for the RiceGuard store. It uses WAL journal
// mode (set at open time) and partial unique indexes to enforce resource
// uniqueness only for open tasks.
const schemaDDL = `
CREATE TABLE IF NOT EXISTS tasks (
	id               TEXT PRIMARY KEY,
	seed_lot         TEXT NOT NULL,
	field            TEXT NOT NULL,
	variety          TEXT NOT NULL,
	female_parent    TEXT NOT NULL,
	male_parent      TEXT NOT NULL,
	female_cert      INTEGER NOT NULL,
	male_cert        INTEGER NOT NULL,
	cert_summary     TEXT NOT NULL,
	status           TEXT NOT NULL,
	generation       INTEGER NOT NULL,
	moisture_max     INTEGER NOT NULL,
	pathogen_max     INTEGER NOT NULL,
	min_purity       INTEGER NOT NULL,
	grain_count      INTEGER NOT NULL,
	chamber          TEXT NOT NULL,
	chamber_start    INTEGER NOT NULL,
	chamber_end      INTEGER NOT NULL,
	plate            TEXT NOT NULL,
	wells            TEXT NOT NULL,
	day_ages         TEXT NOT NULL,
	blind_allocs     TEXT NOT NULL,
	reviewer_roster  TEXT NOT NULL,
	terminal_version INTEGER NOT NULL DEFAULT 0,
	terminal_outcome TEXT NOT NULL DEFAULT '',
	created_at       INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_seed_lot_open
	ON tasks(seed_lot) WHERE status NOT IN ('released','quarantined','cancelled');

CREATE TABLE IF NOT EXISTS confirmations (
	task_id      TEXT NOT NULL,
	reviewer     TEXT NOT NULL,
	field        TEXT NOT NULL,
	seed_lot     TEXT NOT NULL,
	blind_seal   TEXT NOT NULL,
	sample_count INTEGER NOT NULL,
	generation   INTEGER NOT NULL,
	operation_id TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_confirmations_task ON confirmations(task_id);

CREATE TABLE IF NOT EXISTS blind_samples (
	task_id         TEXT NOT NULL,
	code            TEXT NOT NULL,
	generation      INTEGER NOT NULL,
	unblinded       INTEGER NOT NULL,
	germination_qty INTEGER NOT NULL,
	pathogen_qty    INTEGER NOT NULL,
	moisture_qty    INTEGER NOT NULL,
	consistency_hash TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_blind_samples_task_code ON blind_samples(task_id, code);

CREATE TABLE IF NOT EXISTS splits (
	task_id  TEXT NOT NULL,
	code     TEXT NOT NULL,
	split    TEXT NOT NULL,
	quantity INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_splits_task ON splits(task_id);

CREATE TABLE IF NOT EXISTS occupancies (
	seq            INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id        TEXT NOT NULL,
	chamber        TEXT NOT NULL DEFAULT '',
	start          INTEGER NOT NULL DEFAULT 0,
	end            INTEGER NOT NULL DEFAULT 0,
	plate          TEXT NOT NULL DEFAULT '',
	well           TEXT NOT NULL DEFAULT '',
	blind_code     TEXT NOT NULL DEFAULT '',
	generation     INTEGER NOT NULL,
	status         TEXT NOT NULL,
	release_reason TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_occupancies_task ON occupancies(task_id);
CREATE INDEX IF NOT EXISTS idx_occupancies_chamber ON occupancies(chamber);
CREATE INDEX IF NOT EXISTS idx_occupancies_well ON occupancies(plate, well);

CREATE TABLE IF NOT EXISTS germinations (
	task_id      TEXT NOT NULL,
	blind_code   TEXT NOT NULL,
	split        TEXT NOT NULL,
	day_age      INTEGER NOT NULL,
	normal       INTEGER NOT NULL,
	abnormal     INTEGER NOT NULL,
	dead         INTEGER NOT NULL,
	retest       INTEGER NOT NULL,
	collector    TEXT NOT NULL,
	operation_id TEXT NOT NULL,
	valid        INTEGER NOT NULL
);
-- The observation cell is keyed by (task_id, blind_code, day_age): each blind
-- code must be able to record a reading for every locked day age, so the
-- uniqueness constraint must include blind_code. Without it, a second blind
-- code's reading for the same day age collides with the first. Drop any index
-- created under the previous, too-broad (task_id, day_age) definition before
-- recreating, since CREATE ... IF NOT EXISTS leaves an existing index untouched.
DROP INDEX IF EXISTS idx_germinations_cell;
CREATE UNIQUE INDEX IF NOT EXISTS idx_germinations_cell
	ON germinations(task_id, blind_code, day_age);
CREATE INDEX IF NOT EXISTS idx_germinations_task ON germinations(task_id);

CREATE TABLE IF NOT EXISTS moisture (
	task_id         TEXT NOT NULL,
	moisture        INTEGER NOT NULL,
	purity_grains   INTEGER NOT NULL,
	thousand_grain  INTEGER NOT NULL,
	derived_purity  INTEGER NOT NULL,
	pass_threshold  INTEGER NOT NULL,
	attempt_id      TEXT NOT NULL,
	collector       TEXT NOT NULL DEFAULT '',
	version         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_moisture_task ON moisture(task_id);

CREATE TABLE IF NOT EXISTS pathogen (
	task_id       TEXT NOT NULL,
	blind_code    TEXT NOT NULL,
	plate         TEXT NOT NULL,
	well          TEXT NOT NULL,
	reading       INTEGER NOT NULL,
	verdict       TEXT NOT NULL,
	device_status TEXT NOT NULL,
	verifier      TEXT NOT NULL,
	rejudge_gen   INTEGER NOT NULL,
	contaminated  INTEGER NOT NULL,
	late_isolated INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_pathogen_well
	ON pathogen(task_id, plate, well);
CREATE INDEX IF NOT EXISTS idx_pathogen_task ON pathogen(task_id);

CREATE TABLE IF NOT EXISTS attempts (
	seq        INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id    TEXT NOT NULL,
	plate      TEXT NOT NULL,
	well       TEXT NOT NULL,
	attempt    INTEGER NOT NULL,
	status     TEXT NOT NULL,
	reading    INTEGER NOT NULL,
	retryable  INTEGER NOT NULL,
	logical_seq INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_attempts_task ON attempts(task_id);

CREATE TABLE IF NOT EXISTS reviews (
	task_id          TEXT NOT NULL,
	reviewer         TEXT NOT NULL,
	scope            TEXT NOT NULL,
	qualified        INTEGER NOT NULL,
	conclusion       TEXT NOT NULL,
	outcome          TEXT NOT NULL DEFAULT '',
	isolation_reason TEXT NOT NULL DEFAULT '',
	cancel_reason    TEXT NOT NULL DEFAULT '',
	terminal_version INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_reviews_task ON reviews(task_id);

CREATE TABLE IF NOT EXISTS credentials (
	task_id    TEXT PRIMARY KEY,
	credential TEXT NOT NULL,
	version    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS operations (
	operation_id   TEXT PRIMARY KEY,
	task_id        TEXT NOT NULL,
	generation     INTEGER NOT NULL,
	request_digest TEXT NOT NULL,
	response_code  TEXT NOT NULL,
	reasons        TEXT NOT NULL,
	result_digest  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS audit (
	seq          INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id      TEXT NOT NULL,
	logical_time INTEGER NOT NULL,
	actor        TEXT NOT NULL,
	task_status  TEXT NOT NULL,
	action       TEXT NOT NULL,
	code         TEXT NOT NULL,
	reasons      TEXT NOT NULL,
	blind_codes  TEXT NOT NULL,
	day_ages     TEXT NOT NULL,
	plate_wells  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_task ON audit(task_id);

CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value INTEGER NOT NULL
);
`

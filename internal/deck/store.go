package deck

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store keeps the keys, their runs, the cooldowns and the sync mark in SQLite.
type Store struct {
	db  *sql.DB
	Now func() time.Time
}

const schema = `
CREATE TABLE IF NOT EXISTS keys (
	slot TEXT PRIMARY KEY,
	label TEXT NOT NULL DEFAULT '',
	mode TEXT NOT NULL DEFAULT 'off',
	action TEXT NOT NULL DEFAULT '',
	steps TEXT NOT NULL DEFAULT '[]',
	app_scope TEXT NOT NULL DEFAULT '',
	cooldown_seconds INTEGER NOT NULL DEFAULT 0,
	single_instance INTEGER NOT NULL DEFAULT 1,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS runs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	slot TEXT NOT NULL,
	status TEXT NOT NULL,
	exit_code INTEGER,
	output TEXT NOT NULL DEFAULT '',
	started_at TEXT NOT NULL,
	finished_at TEXT
);
CREATE INDEX IF NOT EXISTS runs_slot ON runs (slot, id);
CREATE TABLE IF NOT EXISTS cooldowns (slot TEXT PRIMARY KEY, until TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`

// Open opens (and creates) the database at path; ":memory:" is for tests.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if path == ":memory:" {
		// Every pooled connection would otherwise get its own empty database.
		db.SetMaxOpenConns(1)
	}
	if _, err := db.Exec("PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000;"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := addIconColumn(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate icon: %w", err)
	}
	if err := migrateMacros(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate macros: %w", err)
	}
	if err := migrateProfiles(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate profiles: %w", err)
	}
	return &Store{db: db, Now: time.Now}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// addIconColumn adds keys.icon to databases made before icons. Key 1 gets
// the skull, which the real pad's first keycap has printed on it.
func addIconColumn(db *sql.DB) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('keys') WHERE name = 'icon'`).Scan(&n); err != nil || n > 0 {
		return err
	}
	if _, err := db.Exec(`ALTER TABLE keys ADD COLUMN icon TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE keys SET icon = 'icon:skull' WHERE slot = 'key1'`)
	return err
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseStamp(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

const keyColumns = `profile_id, slot, label, icon, mode, action, steps, macro_id, app_scope, cooldown_seconds, single_instance, updated_at`

// profileOrPersistent turns 0 into the persistent profile and checks the rest.
func (s *Store) profileOrPersistent(pid int64) (int64, error) {
	if pid == 0 {
		return s.PersistentProfile()
	}
	if _, err := s.Profile(pid); err != nil {
		return 0, err
	}
	return pid, nil
}

// Keys returns every slot of the persistent profile in pad order.
func (s *Store) Keys() ([]Key, error) { return s.KeysIn(0) }

// KeysIn returns every slot of a profile (0 = persistent) in pad order,
// creating the rows that do not exist.
func (s *Store) KeysIn(pid int64) ([]Key, error) {
	pid, err := s.profileOrPersistent(pid)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT `+keyColumns+` FROM profile_keys WHERE profile_id = ?`, pid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := map[Slot]Key{}
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		found[k.Slot] = k
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Key, 0, len(slots))
	for _, slot := range slots {
		k, ok := found[slot]
		if !ok {
			k = Key{ProfileID: pid, Slot: slot, Mode: ModeOff, SingleInstance: true, Steps: []Step{}, UpdatedAt: s.Now()}
			if slot == Key1 {
				k.Icon = "icon:skull" // printed on the real pad's first keycap
			}
			if _, err := s.db.Exec(`INSERT INTO profile_keys (profile_id, slot, icon, updated_at) VALUES (?, ?, ?, ?)`, pid, string(slot), k.Icon, stamp(k.UpdatedAt)); err != nil {
				return nil, err
			}
		}
		out = append(out, k)
	}
	return out, nil
}

type scanner interface{ Scan(dest ...any) error }

func scanKey(r scanner) (Key, error) {
	var k Key
	var slot, mode, steps, updated string
	var single int
	var macroID sql.NullInt64
	if err := r.Scan(&k.ProfileID, &slot, &k.Label, &k.Icon, &mode, &k.Action, &steps, &macroID, &k.AppScope, &k.CooldownSeconds, &single, &updated); err != nil {
		return k, err
	}
	k.MacroID = macroID.Int64
	k.Slot, k.Mode, k.SingleInstance, k.UpdatedAt = Slot(slot), Mode(mode), single == 1, parseStamp(updated)
	if err := json.Unmarshal([]byte(steps), &k.Steps); err != nil {
		return k, fmt.Errorf("steps of %s: %w", slot, err)
	}
	if k.Steps == nil {
		k.Steps = []Step{}
	}
	return k, nil
}

// Key is one slot of the persistent profile.
func (s *Store) Key(slot Slot) (Key, error) { return s.KeyIn(0, slot) }

// KeyIn is one slot of a profile (0 = persistent).
func (s *Store) KeyIn(pid int64, slot Slot) (Key, error) {
	keys, err := s.KeysIn(pid)
	if err != nil {
		return Key{}, err
	}
	for _, k := range keys {
		if k.Slot == slot {
			return k, nil
		}
	}
	return Key{}, fmt.Errorf("unknown slot %q", slot)
}

// SaveKey validates and stores the key in its profile (0 = persistent).
func (s *Store) SaveKey(k Key) error {
	if err := k.Validate(); err != nil {
		return err
	}
	pid, err := s.profileOrPersistent(k.ProfileID)
	if err != nil {
		return err
	}
	if k.Steps == nil {
		k.Steps = []Step{}
	}
	var macroID any // NULL unless macro mode
	if k.Mode == ModeMacro {
		if k.MacroID == 0 {
			// Steps without a macro (MCP set_key before the library): the key
			// gets a macro of its own.
			name := k.Label
			if strings.TrimSpace(name) == "" {
				name = k.Slot.Name() + " macro"
			}
			id, err := s.SaveMacro(Macro{Name: name, Kind: MacroOnce, Steps: k.Steps})
			if err != nil {
				return err
			}
			k.MacroID = id
		} else if _, err := s.Macro(k.MacroID); err != nil {
			return err
		}
		macroID = k.MacroID
	}
	steps, err := json.Marshal(k.Steps)
	if err != nil {
		return err
	}
	single := 0
	if k.SingleInstance {
		single = 1
	}
	_, err = s.db.Exec(`INSERT INTO profile_keys (`+keyColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(profile_id, slot) DO UPDATE SET label = excluded.label, icon = excluded.icon, mode = excluded.mode, action = excluded.action,
		steps = excluded.steps, macro_id = excluded.macro_id, app_scope = excluded.app_scope, cooldown_seconds = excluded.cooldown_seconds,
		single_instance = excluded.single_instance, updated_at = excluded.updated_at`,
		pid, string(k.Slot), k.Label, k.Icon, string(k.Mode), k.Action, string(steps), macroID, k.AppScope, k.CooldownSeconds, single, stamp(s.Now()))
	return err
}

// ChangedAt is when any key last changed.
func (s *Store) ChangedAt() (time.Time, error) {
	var v sql.NullString
	if err := s.db.QueryRow(`SELECT MAX(updated_at) FROM profile_keys`).Scan(&v); err != nil {
		return time.Time{}, err
	}
	if !v.Valid {
		return time.Time{}, nil
	}
	return parseStamp(v.String), nil
}

func (s *Store) CreateRun(slot Slot) (Run, error) {
	now := s.Now()
	res, err := s.db.Exec(`INSERT INTO runs (slot, status, started_at) VALUES (?, 'running', ?)`, string(slot), stamp(now))
	if err != nil {
		return Run{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Run{}, err
	}
	return Run{ID: id, Slot: slot, Status: "running", StartedAt: now}, nil
}

func (s *Store) FinishRun(id int64, status string, exitCode *int, output string) error {
	var code any
	if exitCode != nil {
		code = *exitCode
	}
	_, err := s.db.Exec(`UPDATE runs SET status = ?, exit_code = ?, output = ?, finished_at = ? WHERE id = ?`,
		status, code, output, stamp(s.Now()), id)
	return err
}

func scanRun(r scanner) (Run, error) {
	var run Run
	var slot, started string
	var code sql.NullInt64
	var finished sql.NullString
	if err := r.Scan(&run.ID, &slot, &run.Status, &code, &run.Output, &started, &finished); err != nil {
		return run, err
	}
	run.Slot, run.StartedAt = Slot(slot), parseStamp(started)
	if code.Valid {
		c := int(code.Int64)
		run.ExitCode = &c
	}
	if finished.Valid {
		run.FinishedAt = parseStamp(finished.String)
	}
	return run, nil
}

const runColumns = `id, slot, status, exit_code, output, started_at, finished_at`

// LastRun is the newest run of the slot, nil when it never ran.
func (s *Store) LastRun(slot Slot) (*Run, error) {
	row := s.db.QueryRow(`SELECT `+runColumns+` FROM runs WHERE slot = ? ORDER BY id DESC LIMIT 1`, string(slot))
	run, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// Runs lists the slot's runs, newest first.
func (s *Store) Runs(slot Slot, limit int) ([]Run, error) {
	rows, err := s.db.Query(`SELECT `+runColumns+` FROM runs WHERE slot = ? ORDER BY id DESC LIMIT ?`, string(slot), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *Store) CooldownUntil(slot Slot) (time.Time, error) {
	var v string
	err := s.db.QueryRow(`SELECT until FROM cooldowns WHERE slot = ?`, string(slot)).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return parseStamp(v), nil
}

func (s *Store) SetCooldownUntil(slot Slot, until time.Time) error {
	_, err := s.db.Exec(`INSERT INTO cooldowns (slot, until) VALUES (?, ?) ON CONFLICT(slot) DO UPDATE SET until = excluded.until`,
		string(slot), stamp(until))
	return err
}

// SyncedAt is when the pad was last written from this configuration.
func (s *Store) SyncedAt() (time.Time, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'synced_at'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return parseStamp(v), true, nil
}

func (s *Store) MarkSynced(at time.Time) error {
	_, err := s.db.Exec(`INSERT INTO meta (key, value) VALUES ('synced_at', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, stamp(at))
	return err
}

// Debug reports whether debug mode is on (off until it is turned on).
func (s *Store) Debug() (bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'debug'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return v == "1", err
}

func (s *Store) SetDebug(on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	_, err := s.db.Exec(`INSERT INTO meta (key, value) VALUES ('debug', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, v)
	return err
}

// Views is every key with its last run, the shape the API and the page use.
// Views is every key of the persistent profile with its last run.
func (s *Store) Views(modifiers string) ([]View, error) { return s.ViewsIn(0, modifiers) }

// ViewsIn is every key of a profile (0 = persistent) with its last run.
func (s *Store) ViewsIn(pid int64, modifiers string) ([]View, error) {
	keys, err := s.KeysIn(pid)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(keys))
	for _, k := range keys {
		last, err := s.LastRun(k.Slot)
		if err != nil {
			return nil, err
		}
		v := k.View(modifiers, last)
		if k.Mode == ModeMacro && k.MacroID != 0 {
			if m, err := s.Macro(k.MacroID); err == nil {
				v.Macro = &MacroSummary{ID: m.ID, Name: m.Name, Kind: m.Kind, Color: m.Color}
				v.Steps = m.Steps
			}
		}
		out = append(out, v)
	}
	return out, nil
}

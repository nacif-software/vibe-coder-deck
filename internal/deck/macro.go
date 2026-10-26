package deck

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MacroKind is how a macro plays while its key is pressed, like G HUB's
// macro types.
type MacroKind string

const (
	MacroOnce     MacroKind = "once"     // plays once per press
	MacroRepeat   MacroKind = "repeat"   // repeats while the key is held
	MacroToggle   MacroKind = "toggle"   // a press starts repeating, the next stops it
	MacroSequence MacroKind = "sequence" // press steps, hold steps on repeat, release steps
)

var macroKinds = map[MacroKind]bool{MacroOnce: true, MacroRepeat: true, MacroToggle: true, MacroSequence: true}

// Macro is a named list of steps in the library; keys in macro mode point at one.
type Macro struct {
	ID    int64     `json:"id"`
	Name  string    `json:"name"`
	Kind  MacroKind `json:"kind"`
	Steps []Step    `json:"steps"` // what plays; for a sequence, on press
	// For a sequence only: looped while the key is held, then played on release.
	HoldSteps    []Step `json:"hold_steps"`
	ReleaseSteps []Step `json:"release_steps"`
	// StandardDelayMs > 0 replaces the recorded waits with this one between actions.
	StandardDelayMs int       `json:"standard_delay_ms"`
	Color           string    `json:"color"`
	UpdatedAt       time.Time `json:"updated_at"`
	AssignedTo      []Slot    `json:"assigned_to"` // filled by Store.Macros
}

// MacroSummary is what a key's view shows of its macro.
type MacroSummary struct {
	ID    int64     `json:"id"`
	Name  string    `json:"name"`
	Kind  MacroKind `json:"kind"`
	Color string    `json:"color"`
}

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (m Macro) Validate() error {
	if strings.TrimSpace(m.Name) == "" || len(m.Name) > 60 {
		return errors.New("name: 1 to 60 characters")
	}
	if !macroKinds[m.Kind] {
		return fmt.Errorf("kind: %q is not once, repeat, toggle or sequence", m.Kind)
	}
	if m.Kind == MacroSequence {
		if len(m.Steps)+len(m.HoldSteps)+len(m.ReleaseSteps) == 0 {
			return errors.New("steps: a sequence needs press, hold or release steps")
		}
	} else if len(m.Steps) == 0 {
		return errors.New("steps: a macro needs at least one step")
	}
	for name, list := range map[string][]Step{"steps": m.Steps, "hold_steps": m.HoldSteps, "release_steps": m.ReleaseSteps} {
		if err := validateSteps(name, list); err != nil {
			return err
		}
	}
	if m.StandardDelayMs < 0 || m.StandardDelayMs > 60000 {
		return errors.New("standard_delay_ms: between 0 and 60000")
	}
	if m.Color != "" && !colorPattern.MatchString(m.Color) {
		return errors.New("color: #rrggbb")
	}
	return nil
}

func validateSteps(field string, steps []Step) error {
	for i, s := range steps {
		if !stepTypes[s.Type] {
			return fmt.Errorf("%s.%d.type: %q is not keys, text, delay, keydown, keyup, click or launch", field, i, s.Type)
		}
		if strings.TrimSpace(s.Value) == "" {
			return fmt.Errorf("%s.%d.value: required", field, i)
		}
		if s.Type == "delay" {
			if _, err := strconv.ParseFloat(s.Value, 64); err != nil {
				return fmt.Errorf("%s.%d.value: delay must be milliseconds", field, i)
			}
		}
		if s.Type == "click" && !mouseButtons[strings.ToLower(s.Value)] {
			return fmt.Errorf("%s.%d.value: click must be left, right or middle", field, i)
		}
	}
	return nil
}

// WithStandardDelay drops the recorded waits and puts ms between actions,
// which is what "use standard delays" means; ms <= 0 keeps the steps as they are.
func WithStandardDelay(steps []Step, ms int) []Step {
	if ms <= 0 {
		return steps
	}
	out := make([]Step, 0, len(steps)*2)
	for _, s := range steps {
		if s.Type == "delay" {
			continue
		}
		if len(out) > 0 {
			out = append(out, Step{Type: "delay", Value: strconv.Itoa(ms)})
		}
		out = append(out, s)
	}
	return out
}

const macroColumns = `id, name, kind, steps, hold_steps, release_steps, standard_delay_ms, color, updated_at`

func scanMacro(r scanner) (Macro, error) {
	var m Macro
	var kind, steps, hold, release, updated string
	if err := r.Scan(&m.ID, &m.Name, &kind, &steps, &hold, &release, &m.StandardDelayMs, &m.Color, &updated); err != nil {
		return m, err
	}
	m.Kind, m.UpdatedAt = MacroKind(kind), parseStamp(updated)
	for _, f := range []struct {
		raw string
		to  *[]Step
	}{{steps, &m.Steps}, {hold, &m.HoldSteps}, {release, &m.ReleaseSteps}} {
		if err := json.Unmarshal([]byte(f.raw), f.to); err != nil {
			return m, fmt.Errorf("macro %d: %w", m.ID, err)
		}
		if *f.to == nil {
			*f.to = []Step{}
		}
	}
	m.AssignedTo = []Slot{}
	return m, nil
}

// Macros lists the library by name, with the keys of the persistent profile
// each macro is assigned to.
func (s *Store) Macros() ([]Macro, error) { return s.MacrosIn(0) }

// MacrosIn lists the library with the keys of a profile each one is assigned to.
func (s *Store) MacrosIn(pid int64) ([]Macro, error) {
	rows, err := s.db.Query(`SELECT ` + macroColumns + ` FROM macros ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Macro
	index := map[int64]int{}
	for rows.Next() {
		m, err := scanMacro(rows)
		if err != nil {
			return nil, err
		}
		index[m.ID] = len(out)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	keys, err := s.KeysIn(pid)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		if i, ok := index[k.MacroID]; ok && k.Mode == ModeMacro {
			out[i].AssignedTo = append(out[i].AssignedTo, k.Slot)
		}
	}
	if out == nil {
		out = []Macro{}
	}
	return out, nil
}

var ErrNoMacro = errors.New("no such macro")

func (s *Store) Macro(id int64) (Macro, error) {
	m, err := scanMacro(s.db.QueryRow(`SELECT `+macroColumns+` FROM macros WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, fmt.Errorf("%w: %d", ErrNoMacro, id)
	}
	return m, err
}

// SaveMacro validates and stores the macro (a new one when ID is 0) and
// returns its id. Keys using it pick up the change at once.
func (s *Store) SaveMacro(m Macro) (int64, error) {
	m.Name = strings.TrimSpace(m.Name)
	if err := m.Validate(); err != nil {
		return 0, err
	}
	enc := func(steps []Step) string {
		if steps == nil {
			steps = []Step{}
		}
		b, _ := json.Marshal(steps)
		return string(b)
	}
	now := stamp(s.Now())
	if m.ID == 0 {
		res, err := s.db.Exec(`INSERT INTO macros (name, kind, steps, hold_steps, release_steps, standard_delay_ms, color, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			m.Name, string(m.Kind), enc(m.Steps), enc(m.HoldSteps), enc(m.ReleaseSteps), m.StandardDelayMs, m.Color, now)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	res, err := s.db.Exec(`UPDATE macros SET name = ?, kind = ?, steps = ?, hold_steps = ?, release_steps = ?, standard_delay_ms = ?, color = ?, updated_at = ? WHERE id = ?`,
		m.Name, string(m.Kind), enc(m.Steps), enc(m.HoldSteps), enc(m.ReleaseSteps), m.StandardDelayMs, m.Color, now, m.ID)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, fmt.Errorf("%w: %d", ErrNoMacro, m.ID)
	}
	// The keys' configuration changed too: the pad view shows them as behind.
	_, err = s.db.Exec(`UPDATE profile_keys SET updated_at = ? WHERE macro_id = ?`, now, m.ID)
	return m.ID, err
}

// DeleteMacro removes a macro and turns off the keys that used it.
func (s *Store) DeleteMacro(id int64) error {
	if _, err := s.Macro(id); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE profile_keys SET mode = 'off', macro_id = NULL, updated_at = ? WHERE macro_id = ?`, stamp(s.Now()), id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM macros WHERE id = ?`, id)
	return err
}

// migrateMacros adds the macro library to databases made before it: the
// table, keys.macro_id, and one macro per key that held its own steps.
func migrateMacros(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS macros (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		kind TEXT NOT NULL DEFAULT 'once',
		steps TEXT NOT NULL DEFAULT '[]',
		hold_steps TEXT NOT NULL DEFAULT '[]',
		release_steps TEXT NOT NULL DEFAULT '[]',
		standard_delay_ms INTEGER NOT NULL DEFAULT 0,
		color TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('keys') WHERE name = 'macro_id'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := db.Exec(`ALTER TABLE keys ADD COLUMN macro_id INTEGER`); err != nil {
			return err
		}
	}
	rows, err := db.Query(`SELECT slot, label, steps FROM keys WHERE mode = 'macro' AND macro_id IS NULL AND steps NOT IN ('', '[]')`)
	if err != nil {
		return err
	}
	type legacy struct{ slot, label, steps string }
	var found []legacy
	for rows.Next() {
		var l legacy
		if err := rows.Scan(&l.slot, &l.label, &l.steps); err != nil {
			rows.Close()
			return err
		}
		found = append(found, l)
	}
	rows.Close()
	for _, l := range found {
		name := l.label
		if strings.TrimSpace(name) == "" {
			name = Slot(l.slot).Name() + " macro"
		}
		res, err := db.Exec(`INSERT INTO macros (name, kind, steps, updated_at) VALUES (?, 'once', ?, ?)`, name, l.steps, stamp(time.Now()))
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if _, err := db.Exec(`UPDATE keys SET macro_id = ? WHERE slot = ?`, id, l.slot); err != nil {
			return err
		}
	}
	return nil
}

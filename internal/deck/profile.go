package deck

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Profiles work like G HUB's: the Desktop and every app you add have
// profiles, each a full set of the nine controls. With profile switching on,
// the app in front picks its active profile (Desktop's when it has none);
// with it off, the persistent profile is used everywhere.

type AppKind string

const (
	AppDesktop AppKind = "desktop"
	AppProgram AppKind = "app"
)

type App struct {
	ID       int64   `json:"id"`
	Kind     AppKind `json:"kind"`
	BundleID string  `json:"bundle_id"` // empty until seen in front, for apps added by name
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	Enabled  bool    `json:"enabled"`
	// ActiveProfileID is the profile this app switches to.
	ActiveProfileID int64     `json:"active_profile_id"`
	Profiles        []Profile `json:"profiles"`
}

type Profile struct {
	ID    int64  `json:"id"`
	AppID int64  `json:"app_id"`
	Name  string `json:"name"`
}

var ErrNoProfile = errors.New("no such profile")

const profileSchema = `
CREATE TABLE IF NOT EXISTS apps (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	kind TEXT NOT NULL DEFAULT 'app',
	bundle_id TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL,
	path TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1,
	active_profile_id INTEGER,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS profiles (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	app_id INTEGER NOT NULL,
	name TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS profile_keys (
	profile_id INTEGER NOT NULL,
	slot TEXT NOT NULL,
	label TEXT NOT NULL DEFAULT '',
	icon TEXT NOT NULL DEFAULT '',
	mode TEXT NOT NULL DEFAULT 'off',
	action TEXT NOT NULL DEFAULT '',
	steps TEXT NOT NULL DEFAULT '[]',
	macro_id INTEGER,
	app_scope TEXT NOT NULL DEFAULT '',
	cooldown_seconds INTEGER NOT NULL DEFAULT 0,
	single_instance INTEGER NOT NULL DEFAULT 1,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (profile_id, slot)
);
`

const keyCopyColumns = `slot, label, icon, mode, action, steps, macro_id, app_scope, cooldown_seconds, single_instance, updated_at`

// migrateProfiles moves the keys of databases made before profiles into
// Desktop: Default. A key limited to one app in front ("app scope") moves to
// a profile of that app, so it still only runs there, and switching is on.
func migrateProfiles(db *sql.DB) error {
	if _, err := db.Exec(profileSchema); err != nil {
		return err
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM apps`).Scan(&n); err != nil || n > 0 {
		return err
	}
	now := stamp(time.Now())
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	newApp := func(kind AppKind, name string) (int64, int64, error) {
		res, err := tx.Exec(`INSERT INTO apps (kind, name, created_at) VALUES (?, ?, ?)`, string(kind), name, now)
		if err != nil {
			return 0, 0, err
		}
		app, _ := res.LastInsertId()
		res, err = tx.Exec(`INSERT INTO profiles (app_id, name, created_at) VALUES (?, 'Default', ?)`, app, now)
		if err != nil {
			return 0, 0, err
		}
		profile, _ := res.LastInsertId()
		_, err = tx.Exec(`UPDATE apps SET active_profile_id = ? WHERE id = ?`, profile, app)
		return app, profile, err
	}
	_, desktop, err := newApp(AppDesktop, "Desktop")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO profile_keys (profile_id, `+keyCopyColumns+`) SELECT ?, `+keyCopyColumns+` FROM keys`, desktop); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT DISTINCT app_scope FROM keys WHERE app_scope != ''`)
	if err != nil {
		return err
	}
	var scopes []string
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			rows.Close()
			return err
		}
		scopes = append(scopes, scope)
	}
	rows.Close()
	for _, scope := range scopes {
		_, profile, err := newApp(AppProgram, scope)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO profile_keys (profile_id, `+keyCopyColumns+`) SELECT ?, `+keyCopyColumns+` FROM profile_keys WHERE profile_id = ?`, profile, desktop); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE profile_keys SET mode = 'off', action = '', macro_id = NULL WHERE profile_id = ? AND slot IN (SELECT slot FROM keys WHERE app_scope = ?)`, desktop, scope); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE profile_keys SET app_scope = ''`); err != nil {
		return err
	}
	switching := "0"
	if len(scopes) > 0 {
		switching = "1"
	}
	if _, err := tx.Exec(`INSERT INTO meta (key, value) VALUES ('profile_switching', ?), ('persistent_profile', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, switching, fmt.Sprint(desktop)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) meta(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) setMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) ProfileSwitching() (bool, error) {
	v, err := s.meta("profile_switching")
	return v == "1", err
}

func (s *Store) SetProfileSwitching(on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	return s.setMeta("profile_switching", v)
}

// PersistentProfile is the profile used when switching is off, and what
// "the keys" means wherever no profile is named.
func (s *Store) PersistentProfile() (int64, error) {
	v, err := s.meta("persistent_profile")
	if err != nil {
		return 0, err
	}
	var id int64
	fmt.Sscan(v, &id)
	if _, err := s.Profile(id); err == nil {
		return id, nil
	}
	// Gone or unset: Desktop's active profile.
	err = s.db.QueryRow(`SELECT active_profile_id FROM apps WHERE kind = 'desktop'`).Scan(&id)
	return id, err
}

func (s *Store) SetPersistentProfile(id int64) error {
	if _, err := s.Profile(id); err != nil {
		return err
	}
	return s.setMeta("persistent_profile", fmt.Sprint(id))
}

func (s *Store) Profile(id int64) (Profile, error) {
	var p Profile
	err := s.db.QueryRow(`SELECT id, app_id, name FROM profiles WHERE id = ?`, id).Scan(&p.ID, &p.AppID, &p.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("%w: %d", ErrNoProfile, id)
	}
	return p, err
}

// Apps lists the Desktop first, then the apps by name, each with its profiles.
func (s *Store) Apps() ([]App, error) {
	rows, err := s.db.Query(`SELECT id, kind, bundle_id, name, path, enabled, COALESCE(active_profile_id, 0) FROM apps ORDER BY kind = 'app', name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	var apps []App
	index := map[int64]int{}
	for rows.Next() {
		var a App
		var kind string
		var enabled int
		if err := rows.Scan(&a.ID, &kind, &a.BundleID, &a.Name, &a.Path, &enabled, &a.ActiveProfileID); err != nil {
			rows.Close()
			return nil, err
		}
		a.Kind, a.Enabled, a.Profiles = AppKind(kind), enabled == 1, []Profile{}
		index[a.ID] = len(apps)
		apps = append(apps, a)
	}
	rows.Close()
	prows, err := s.db.Query(`SELECT id, app_id, name FROM profiles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer prows.Close()
	for prows.Next() {
		var p Profile
		if err := prows.Scan(&p.ID, &p.AppID, &p.Name); err != nil {
			return nil, err
		}
		if i, ok := index[p.AppID]; ok {
			apps[i].Profiles = append(apps[i].Profiles, p)
		}
	}
	return apps, prows.Err()
}

func (s *Store) app(id int64) (App, error) {
	apps, err := s.Apps()
	if err != nil {
		return App{}, err
	}
	for _, a := range apps {
		if a.ID == id {
			return a, nil
		}
	}
	return App{}, fmt.Errorf("no such app: %d", id)
}

// AddApp adds an app with a Default profile copied from the Desktop's active
// one, so it behaves the same until you change it.
func (s *Store) AddApp(bundleID, name, path string) (App, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return App{}, errors.New("name: required")
	}
	apps, err := s.Apps()
	if err != nil {
		return App{}, err
	}
	var from int64
	for _, a := range apps {
		if a.Kind == AppDesktop {
			from = a.ActiveProfileID
		} else if (bundleID != "" && a.BundleID == bundleID) || strings.EqualFold(a.Name, name) {
			return App{}, fmt.Errorf("%s is already in the list", a.Name)
		}
	}
	res, err := s.db.Exec(`INSERT INTO apps (kind, bundle_id, name, path, created_at) VALUES ('app', ?, ?, ?, ?)`, bundleID, name, path, stamp(s.Now()))
	if err != nil {
		return App{}, err
	}
	id, _ := res.LastInsertId()
	p, err := s.AddProfile(id, "Default", from)
	if err != nil {
		return App{}, err
	}
	if err := s.SetAppActiveProfile(id, p.ID); err != nil {
		return App{}, err
	}
	return s.app(id)
}

// AddProfile adds a profile to an app, copying the keys of copyFrom (0 = all off).
func (s *Store) AddProfile(appID int64, name string, copyFrom int64) (Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 60 {
		return Profile{}, errors.New("name: 1 to 60 characters")
	}
	if _, err := s.app(appID); err != nil {
		return Profile{}, err
	}
	res, err := s.db.Exec(`INSERT INTO profiles (app_id, name, created_at) VALUES (?, ?, ?)`, appID, name, stamp(s.Now()))
	if err != nil {
		return Profile{}, err
	}
	id, _ := res.LastInsertId()
	if copyFrom != 0 {
		if _, err := s.db.Exec(`INSERT INTO profile_keys (profile_id, `+keyCopyColumns+`) SELECT ?, `+keyCopyColumns+` FROM profile_keys WHERE profile_id = ?`, id, copyFrom); err != nil {
			return Profile{}, err
		}
	}
	return s.Profile(id)
}

func (s *Store) RenameProfile(id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 60 {
		return errors.New("name: 1 to 60 characters")
	}
	if _, err := s.Profile(id); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE profiles SET name = ? WHERE id = ?`, name, id)
	return err
}

// DeleteProfile removes a profile and its keys; an app keeps at least one.
func (s *Store) DeleteProfile(id int64) error {
	p, err := s.Profile(id)
	if err != nil {
		return err
	}
	a, err := s.app(p.AppID)
	if err != nil {
		return err
	}
	if len(a.Profiles) <= 1 {
		return errors.New("an app keeps at least one profile: delete the app instead")
	}
	if _, err := s.db.Exec(`DELETE FROM profile_keys WHERE profile_id = ?`, id); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM profiles WHERE id = ?`, id); err != nil {
		return err
	}
	if a.ActiveProfileID == id {
		for _, other := range a.Profiles {
			if other.ID != id {
				return s.SetAppActiveProfile(a.ID, other.ID)
			}
		}
	}
	return nil
}

func (s *Store) SetAppActiveProfile(appID, profileID int64) error {
	p, err := s.Profile(profileID)
	if err != nil {
		return err
	}
	if p.AppID != appID {
		return fmt.Errorf("profile %d belongs to another app", profileID)
	}
	_, err = s.db.Exec(`UPDATE apps SET active_profile_id = ? WHERE id = ?`, profileID, appID)
	return err
}

func (s *Store) SetAppEnabled(appID int64, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE apps SET enabled = ? WHERE id = ? AND kind = 'app'`, v, appID)
	return err
}

// SetAppSeen records what an app looked like in front: the bundle id of an
// app added by name and the path of its bundle (for its icon), each only
// while unknown.
func (s *Store) SetAppSeen(appID int64, bundleID, path string) error {
	_, err := s.db.Exec(`UPDATE apps SET bundle_id = CASE WHEN bundle_id = '' THEN ? ELSE bundle_id END, path = CASE WHEN path = '' THEN ? ELSE path END WHERE id = ?`, bundleID, path, appID)
	return err
}

// DeleteApp removes an app (not the Desktop) with its profiles and keys.
func (s *Store) DeleteApp(appID int64) error {
	a, err := s.app(appID)
	if err != nil {
		return err
	}
	if a.Kind == AppDesktop {
		return errors.New("the Desktop cannot be removed")
	}
	if _, err := s.db.Exec(`DELETE FROM profile_keys WHERE profile_id IN (SELECT id FROM profiles WHERE app_id = ?)`, appID); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM profiles WHERE app_id = ?`, appID); err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM apps WHERE id = ?`, appID)
	return err
}

// ResolveProfile is the profile to use with this app in front: the
// persistent one when switching is off; otherwise the active profile of an
// enabled app matching by bundle id (or by name, for apps added by name),
// else the Desktop's. The app's bundle id and path are recorded when unknown.
func (s *Store) ResolveProfile(bundleID, name, path string) int64 {
	persistent, _ := s.PersistentProfile()
	if on, _ := s.ProfileSwitching(); !on {
		return persistent
	}
	apps, err := s.Apps()
	if err != nil {
		return persistent
	}
	var desktop int64
	for _, a := range apps {
		if a.Kind == AppDesktop {
			desktop = a.ActiveProfileID
			continue
		}
		if !a.Enabled {
			continue
		}
		if (bundleID != "" && a.BundleID == bundleID) || (a.BundleID == "" && name != "" && strings.EqualFold(a.Name, name)) {
			if (a.BundleID == "" && bundleID != "") || (a.Path == "" && path != "") {
				s.SetAppSeen(a.ID, bundleID, path)
			}
			return a.ActiveProfileID
		}
	}
	if desktop != 0 {
		return desktop
	}
	return persistent
}

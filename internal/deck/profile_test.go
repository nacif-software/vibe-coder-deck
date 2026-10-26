package deck

import "testing"

func TestDesktopDefaultProfileExists(t *testing.T) {
	s := newStore(t)
	apps, err := s.Apps()
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || apps[0].Kind != AppDesktop || len(apps[0].Profiles) != 1 || apps[0].Profiles[0].Name != "Default" {
		t.Fatalf("apps = %+v", apps)
	}
	if p, _ := s.PersistentProfile(); p != apps[0].Profiles[0].ID {
		t.Fatalf("persistent = %d", p)
	}
}

func TestProfilesKeepTheirOwnKeys(t *testing.T) {
	s := newStore(t)
	desktop, _ := s.PersistentProfile()
	s.SaveKey(Key{Slot: Key2, Mode: ModeKeys, Action: "cmd+c", SingleInstance: true})
	app, err := s.AddApp("com.apple.Safari", "Safari", "/Applications/Safari.app")
	if err != nil {
		t.Fatal(err)
	}
	safari := app.ActiveProfileID
	// A new app starts as a copy of Desktop: Default.
	if k, _ := s.KeyIn(safari, Key2); k.Action != "cmd+c" {
		t.Fatalf("copied key2 = %+v", k)
	}
	if err := s.SaveKey(Key{ProfileID: safari, Slot: Key2, Mode: ModeKeys, Action: "cmd+t", SingleInstance: true}); err != nil {
		t.Fatal(err)
	}
	if k, _ := s.KeyIn(desktop, Key2); k.Action != "cmd+c" {
		t.Fatalf("desktop key2 changed: %+v", k)
	}
	if k, _ := s.KeyIn(safari, Key2); k.Action != "cmd+t" {
		t.Fatalf("safari key2 = %+v", k)
	}
	second, err := s.AddProfile(app.ID, "Writing", safari)
	if err != nil || second.Name != "Writing" {
		t.Fatalf("second profile = %+v %v", second, err)
	}
	if k, _ := s.KeyIn(second.ID, Key2); k.Action != "cmd+t" {
		t.Fatalf("copy of safari key2 = %+v", k)
	}
	if err := s.DeleteProfile(desktop); err == nil {
		t.Fatal("the last Desktop profile must not be deleted")
	}
	if err := s.DeleteApp(app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.KeyIn(safari, Key2); err == nil {
		t.Fatal("the app's profiles should be gone")
	}
}

func TestResolveProfile(t *testing.T) {
	s := newStore(t)
	desktop, _ := s.PersistentProfile()
	app, _ := s.AddApp("com.apple.Safari", "Safari", "")
	orca, _ := s.AddApp("", "Orca", "") // known by name until its bundle id is seen
	// Switching off: always the persistent profile.
	if got := s.ResolveProfile("com.apple.Safari", "Safari", ""); got != desktop {
		t.Fatalf("switching off = %d", got)
	}
	s.SetProfileSwitching(true)
	if got := s.ResolveProfile("com.apple.Safari", "Safari", ""); got != app.ActiveProfileID {
		t.Fatalf("safari = %d", got)
	}
	if got := s.ResolveProfile("com.stablyso.orca", "Orca", ""); got != orca.ActiveProfileID {
		t.Fatalf("orca by name = %d", got)
	}
	if got := s.ResolveProfile("com.apple.finder", "Finder", ""); got != desktop {
		t.Fatalf("an app without profiles = %d", got)
	}
	s.SetAppEnabled(app.ID, false)
	if got := s.ResolveProfile("com.apple.Safari", "Safari", ""); got != desktop {
		t.Fatalf("disabled app = %d", got)
	}
}

func TestAppScopesMigrateToAppProfiles(t *testing.T) {
	s := newStore(t)
	// A database from before profiles: key1 only ran with Orca in front.
	if _, err := s.db.Exec(`DROP TABLE profile_keys; DROP TABLE profiles; DROP TABLE apps; DELETE FROM meta WHERE key IN ('profile_switching', 'persistent_profile')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM keys; INSERT INTO keys (slot, label, mode, action, app_scope, updated_at) VALUES
		('key1', 'Handoff', 'script', '/h.sh', 'Orca', '2026-01-01T00:00:00Z'),
		('key2', 'Shot', 'keys', 'cmd+shift+4', '', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := migrateProfiles(s.db); err != nil {
		t.Fatal(err)
	}
	desktop, _ := s.PersistentProfile()
	if k, _ := s.KeyIn(desktop, Key1); k.Mode != ModeOff {
		t.Fatalf("desktop key1 should be off: %+v", k)
	}
	if k, _ := s.KeyIn(desktop, Key2); k.Action != "cmd+shift+4" {
		t.Fatalf("desktop key2 = %+v", k)
	}
	apps, _ := s.Apps()
	if len(apps) != 2 || apps[1].Name != "Orca" {
		t.Fatalf("apps = %+v", apps)
	}
	orca := apps[1].ActiveProfileID
	if k, _ := s.KeyIn(orca, Key1); k.Action != "/h.sh" || k.AppScope != "" {
		t.Fatalf("orca key1 = %+v", k)
	}
	if k, _ := s.KeyIn(orca, Key2); k.Action != "cmd+shift+4" {
		t.Fatalf("orca key2 = %+v", k)
	}
	if on, _ := s.ProfileSwitching(); !on {
		t.Fatal("switching should be on to keep key1 working in Orca")
	}
}

func TestResolveProfileRecordsWhereTheAppLives(t *testing.T) {
	s := newStore(t)
	s.SetProfileSwitching(true)
	orca, _ := s.AddApp("", "Orca", "")                     // added by name: no bundle id, no path
	safari, _ := s.AddApp("com.apple.Safari", "Safari", "") // bundle id known, no path
	s.ResolveProfile("com.stablyso.orca", "Orca", "/Applications/Orca.app")
	s.ResolveProfile("com.apple.Safari", "Safari", "/Applications/Safari.app")
	s.ResolveProfile("com.apple.Safari", "Safari", "/Volumes/Other/Safari.app") // a known path is kept
	byID := map[int64]App{}
	apps, _ := s.Apps()
	for _, a := range apps {
		byID[a.ID] = a
	}
	if a := byID[orca.ID]; a.BundleID != "com.stablyso.orca" || a.Path != "/Applications/Orca.app" {
		t.Fatalf("orca = %+v", a)
	}
	if a := byID[safari.ID]; a.Path != "/Applications/Safari.app" {
		t.Fatalf("safari = %+v", a)
	}
}

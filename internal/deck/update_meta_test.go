package deck

import "testing"

func TestAutoUpdateIsOnUntilTurnedOff(t *testing.T) {
	s := newStore(t)
	if on, err := s.AutoUpdate(); err != nil || !on {
		t.Fatalf("default = %v %v", on, err)
	}
	if err := s.SetAutoUpdate(false); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.AutoUpdate(); on {
		t.Fatal("still on")
	}
	s.SetAutoUpdate(true)
	if on, _ := s.AutoUpdate(); !on {
		t.Fatal("not back on")
	}
}

func TestLastSeenVersionRemembersWhatsNewWasShown(t *testing.T) {
	s := newStore(t)
	if v, err := s.LastSeenVersion(); err != nil || v != "" {
		t.Fatalf("default = %q %v", v, err)
	}
	if err := s.SetLastSeenVersion("v0.2.0"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.LastSeenVersion(); v != "v0.2.0" {
		t.Fatalf("seen = %q", v)
	}
}

func TestGitHubTokenIsKeptForPrivateReleases(t *testing.T) {
	s := newStore(t)
	if tok, err := s.GitHubToken(); err != nil || tok != "" {
		t.Fatalf("default = %q %v", tok, err)
	}
	s.SetGitHubToken("ghp_x")
	if tok, _ := s.GitHubToken(); tok != "ghp_x" {
		t.Fatalf("token = %q", tok)
	}
	s.SetGitHubToken("")
	if tok, _ := s.GitHubToken(); tok != "" {
		t.Fatal("token not cleared")
	}
}

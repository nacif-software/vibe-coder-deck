package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nacif-software/vibe-coder-deck/internal/deck"
	"github.com/nacif-software/vibe-coder-deck/internal/update"
)

type fakeUpdates struct {
	mu                                    sync.Mutex
	st                                    update.Status
	checks, downloads, installs, relCalls int
	rels                                  []update.Release
}

func (f *fakeUpdates) Status() update.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.st
}

func (f *fakeUpdates) Check(context.Context) (update.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	f.st.State, f.st.Latest = update.StateAvailable, &update.Release{Version: "v0.3.0", Notes: "notes"}
	return f.st, nil
}

func (f *fakeUpdates) Download(context.Context) (update.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloads++
	f.st.State, f.st.Progress = update.StateReady, 1
	return f.st, nil
}

func (f *fakeUpdates) Install() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installs++
	f.st.State = update.StateInstalled
	return nil
}

func (f *fakeUpdates) SetAuto(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.st.Auto = on
}

func (f *fakeUpdates) Releases(context.Context, int) ([]update.Release, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.relCalls++
	return f.rels, "github", nil
}

func updatesServer(t *testing.T, current string) (*Server, *deck.Store, *fakeUpdates) {
	t.Helper()
	s, err := deck.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	srv := New(s, "ctrl+opt+cmd", func(bool) (deck.Plan, error) { return nil, nil })
	f := &fakeUpdates{
		st:   update.Status{State: update.StateIdle, Current: current, Release: update.IsRelease(current), Target: "/Applications/X.app", Auto: true},
		rels: []update.Release{{Version: "v0.2.0", Notes: "## Added\n- things"}},
	}
	srv.Updates, srv.Version = f, current
	return srv, s, f
}

func updReq(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out := map[string]any{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func status(out map[string]any) map[string]any {
	st, _ := out["status"].(map[string]any)
	return st
}

func TestUpdateStatusShowsWhatsNewOnce(t *testing.T) {
	srv, store, _ := updatesServer(t, "v0.2.0")
	code, out := updReq(t, srv, "GET", "/api/update", "")
	if code != 200 || status(out)["state"] != "idle" || status(out)["current"] != "v0.2.0" || out["whats_new"] != true {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ := updReq(t, srv, "POST", "/api/update/seen", ""); code != 200 {
		t.Fatalf("seen = %d", code)
	}
	if _, out := updReq(t, srv, "GET", "/api/update", ""); out["whats_new"] != false {
		t.Fatalf("still new: %v", out)
	}
	if v, _ := store.LastSeenVersion(); v != "v0.2.0" {
		t.Fatalf("seen = %q", v)
	}
}

func TestUpdateStatusWithoutAnUpdater(t *testing.T) {
	h, _, _ := newServer(t)
	code, out := updReq(t, h, "GET", "/api/update", "")
	if code != 200 || status(out)["state"] != "unavailable" || out["whats_new"] != false {
		t.Fatalf("%d %v", code, out)
	}
	if _, out := updReq(t, h, "GET", "/api/app", ""); out["version"] != "" {
		t.Fatalf("app = %v", out)
	}
}

func TestUpdateCheckDownloadInstallThroughTheAPI(t *testing.T) {
	srv, _, f := updatesServer(t, "v0.2.0")
	restarts := 0
	srv.Restart = func() error { restarts++; return nil }
	code, out := updReq(t, srv, "POST", "/api/update/check", "")
	if code != 200 || status(out)["state"] != "available" || f.checks != 1 {
		t.Fatalf("check: %d %v", code, out)
	}
	if latest, _ := status(out)["latest"].(map[string]any); latest["version"] != "v0.3.0" {
		t.Fatalf("latest = %v", status(out)["latest"])
	}
	if code, _ := updReq(t, srv, "POST", "/api/update/download", ""); code != 200 {
		t.Fatalf("download = %d", code)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, out := updReq(t, srv, "GET", "/api/update", "")
		if status(out)["state"] == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never ready: %v", out)
		}
		time.Sleep(10 * time.Millisecond)
	}
	code, out = updReq(t, srv, "POST", "/api/update/install", "")
	if code != 200 || out["installed"] != true || out["restarting"] != true || f.installs != 1 || restarts != 1 {
		t.Fatalf("install: %d %v installs %d restarts %d", code, out, f.installs, restarts)
	}
}

func TestUpdateAutoIsRemembered(t *testing.T) {
	srv, store, f := updatesServer(t, "v0.2.0")
	code, out := updReq(t, srv, "PUT", "/api/update/auto", `{"on":false}`)
	if code != 200 || out["auto"] != false {
		t.Fatalf("%d %v", code, out)
	}
	if on, _ := store.AutoUpdate(); on || f.Status().Auto {
		t.Fatal("auto still on")
	}
}

func TestReleasesAreCachedForAWhile(t *testing.T) {
	srv, _, f := updatesServer(t, "v0.2.0")
	for i := 0; i < 2; i++ {
		code, out := updReq(t, srv, "GET", "/api/releases", "")
		rels, _ := out["releases"].([]any)
		if code != 200 || out["source"] != "github" || len(rels) != 1 || rels[0].(map[string]any)["version"] != "v0.2.0" {
			t.Fatalf("%d %v", code, out)
		}
	}
	if f.relCalls != 1 {
		t.Fatalf("GitHub asked %d times", f.relCalls)
	}
}

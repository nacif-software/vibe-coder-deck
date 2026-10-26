package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRelease serves v0.2.0 with a bundle zip and a checksums.txt. checksum
// overrides the real hash of the zip when set.
func fakeRelease(t *testing.T, zipPath, checksum string, withChecksums bool) (Source, *int) {
	t.Helper()
	zipBytes, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if checksum == "" {
		sum := sha256.Sum256(zipBytes)
		checksum = hex.EncodeToString(sum[:])
	}
	downloads := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/nacif-software/vibe-coder-deck/releases/latest":
			assets := fmt.Sprintf(`{"name":"Vibe-Coder-Deck-v0.2.0-macos.zip","url":"%s/assets/mac","size":%d}`, srv.URL, len(zipBytes))
			if withChecksums {
				assets += fmt.Sprintf(`,{"name":"checksums.txt","url":"%s/assets/sums","size":10}`, srv.URL)
			}
			fmt.Fprintf(w, `{"tag_name":"v0.2.0","name":"v0.2.0","body":"notes","published_at":"2026-11-02T10:00:00Z","assets":[%s]}`, assets)
		case "/assets/mac":
			downloads++
			w.Write(zipBytes)
		case "/assets/sums":
			fmt.Fprintf(w, "%s  Vibe-Coder-Deck-v0.2.0-macos.zip\n", checksum)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return Source{Repo: "nacif-software/vibe-coder-deck", API: srv.URL, Client: srv.Client()}, &downloads
}

func newUpdater(t *testing.T, current, checksum string, withChecksums bool) (*Updater, string) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir()) // Download's os.MkdirTemp lands here, not in the real temp dir
	dir := t.TempDir()
	app := oldBundle(t, dir)
	src, _ := fakeRelease(t, bundleZip(t, dir, "new"), checksum, withChecksums)
	u := New(src, current, app)
	u.GOOS = "darwin"
	return u, app
}

func TestCheckFindsANewerRelease(t *testing.T) {
	u, _ := newUpdater(t, "v0.1.0", "", true)
	var seen []State
	u.OnChange = func(s Status) { seen = append(seen, s.State) }
	st, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateAvailable || st.Latest == nil || st.Latest.Version != "v0.2.0" || st.Current != "v0.1.0" || !st.Release || st.CheckedAt.IsZero() {
		t.Fatalf("status = %+v", st)
	}
	if len(seen) < 2 || seen[0] != StateChecking || seen[len(seen)-1] != StateAvailable {
		t.Fatalf("changes = %v", seen)
	}
}

func TestCheckSaysUpToDate(t *testing.T) {
	u, _ := newUpdater(t, "v0.2.0", "", true)
	if st, _ := u.Check(context.Background()); st.State != StateUpToDate || st.Latest == nil {
		t.Fatalf("status = %+v", st)
	}
}

func TestCheckReportsAnAPIError(t *testing.T) {
	u, _ := newUpdater(t, "v0.1.0", "", true)
	u.Source.Repo = "nobody/nothing"
	st, err := u.Check(context.Background())
	if err == nil || st.State != StateError || !strings.Contains(st.Error, "no release") {
		t.Fatalf("status = %+v err %v", st, err)
	}
}

func TestDownloadVerifiesTheChecksum(t *testing.T) {
	u, _ := newUpdater(t, "v0.1.0", "", true)
	u.Check(context.Background())
	var progress []float64
	u.OnChange = func(s Status) {
		if s.State == StateDownloading {
			progress = append(progress, s.Progress)
		}
	}
	st, err := u.Download(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateReady || st.Progress != 1 {
		t.Fatalf("status = %+v", st)
	}
	if len(progress) == 0 || progress[len(progress)-1] != 1 {
		t.Fatalf("progress = %v", progress)
	}
	if _, err := os.Stat(u.Downloaded()); err != nil {
		t.Fatalf("no zip: %v", err)
	}
}

func TestDownloadRefusesAWrongChecksum(t *testing.T) {
	u, _ := newUpdater(t, "v0.1.0", strings.Repeat("0", 64), true)
	u.Check(context.Background())
	st, err := u.Download(context.Background())
	if err == nil || st.State != StateError || !strings.Contains(st.Error, "checksum") {
		t.Fatalf("status = %+v err %v", st, err)
	}
	if u.Downloaded() != "" {
		t.Fatal("kept a bad download")
	}
}

func TestDownloadRefusesAReleaseWithoutChecksums(t *testing.T) {
	u, _ := newUpdater(t, "v0.1.0", "", false)
	u.Check(context.Background())
	if st, err := u.Download(context.Background()); err == nil || !strings.Contains(st.Error, "checksums.txt") {
		t.Fatalf("status = %+v err %v", st, err)
	}
}

func TestDownloadNeedsAnInstalledApp(t *testing.T) {
	u, _ := newUpdater(t, "v0.1.0", "", true)
	u.Target = ""
	u.Check(context.Background())
	if _, err := u.Download(context.Background()); err == nil || !strings.Contains(err.Error(), "installed app") {
		t.Fatalf("err = %v", err)
	}
}

func TestInstallSwapsTheBundle(t *testing.T) {
	u, app := newUpdater(t, "v0.1.0", "", true)
	u.Check(context.Background())
	u.Download(context.Background())
	if err := u.Install(); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(app, "Contents", "MacOS", "vibe-coder-deck")) != "new" {
		t.Fatal("not installed")
	}
	if st := u.Status(); st.State != StateInstalled {
		t.Fatalf("state = %s", st.State)
	}
}

func TestInstallNeedsADownload(t *testing.T) {
	u, _ := newUpdater(t, "v0.1.0", "", true)
	if err := u.Install(); err == nil {
		t.Fatal("installed nothing")
	}
}

func settle(t *testing.T, u *Updater, want State) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st := u.Status(); st.State == want {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("state never became %s: %+v", want, u.Status())
	return Status{}
}

func TestRunDownloadsAutomaticallyOnlyForReleaseBuilds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rel, _ := newUpdater(t, "v0.1.0", "", true)
	rel.SetAuto(true)
	go rel.Run(ctx, 0, time.Hour)
	settle(t, rel, StateReady)

	dev, _ := newUpdater(t, "v0.1.0-3-gabc1234", "", true)
	dev.SetAuto(true)
	go dev.Run(ctx, 0, time.Hour)
	if st := settle(t, dev, StateAvailable); st.Release {
		t.Fatalf("dev build reported as a release: %+v", st)
	}
	time.Sleep(50 * time.Millisecond)
	if st := dev.Status(); st.State != StateAvailable {
		t.Fatalf("dev build downloaded: %+v", st)
	}

	off, _ := newUpdater(t, "v0.1.0", "", true)
	go off.Run(ctx, 0, time.Hour)
	settle(t, off, StateAvailable)
	time.Sleep(50 * time.Millisecond)
	if st := off.Status(); st.State != StateAvailable || st.Auto {
		t.Fatalf("auto off still downloaded: %+v", st)
	}
}

func TestReleasesFallBackToTheChangelog(t *testing.T) {
	u, _ := newUpdater(t, "v0.1.0", "", true)
	u.Changelog = sampleChangelog
	u.Source.Repo = "nobody/nothing"
	rels, source, err := u.Releases(context.Background(), 10)
	if err != nil || source != "changelog" || len(rels) != 2 || rels[0].Version != "v0.2.0" {
		t.Fatalf("rels %+v source %q err %v", rels, source, err)
	}
}

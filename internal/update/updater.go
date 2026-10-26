package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// State is where the updater is.
type State string

const (
	StateIdle        State = "idle"
	StateChecking    State = "checking"
	StateUpToDate    State = "up-to-date"
	StateAvailable   State = "available" // a newer release exists
	StateDownloading State = "downloading"
	StateReady       State = "ready" // downloaded and verified; install when the user says so
	StateInstalling  State = "installing"
	StateInstalled   State = "installed" // the app on disk is the new one; restart to run it
	StateError       State = "error"
)

// Status is what the page and the menu show.
type Status struct {
	State     State     `json:"state"`
	Current   string    `json:"current"`
	Release   bool      `json:"release"` // the current build is an exact release, not a dev build
	Target    string    `json:"target,omitempty"`
	Latest    *Release  `json:"latest,omitempty"`
	Progress  float64   `json:"progress"` // 0..1 while downloading
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at,omitzero"`
	Auto      bool      `json:"auto"` // download newer releases by itself
}

// Updater finds, downloads, verifies and installs a newer release of the
// program. Downloads happen by themselves when Auto is on and the running
// build is a release; nothing is installed without Install.
type Updater struct {
	Source    Source
	Current   string // the running version
	Target    string // the .app bundle or .exe to replace; "" outside an installed app
	GOOS      string // runtime.GOOS unless a test says otherwise
	GOARCH    string // runtime.GOARCH unless a test says otherwise
	Changelog string // the embedded CHANGELOG.md, the notes when GitHub cannot be reached
	OnChange  func(Status)

	mu         sync.Mutex
	st         Status
	zip        string // the verified download
	zipVersion string
}

// New makes an updater for the running version and the app it would replace.
func New(src Source, current, target string) *Updater {
	return &Updater{
		Source: src, Current: current, Target: target, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		st: Status{State: StateIdle, Current: current, Release: IsRelease(current), Target: target},
	}
}

// Status is a copy of the current state.
func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.st
}

// Downloaded is the verified zip waiting to be installed, "" when none.
func (u *Updater) Downloaded() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.zip
}

// SetAuto turns automatic downloads on or off.
func (u *Updater) SetAuto(on bool) { u.set(func(s *Status) { s.Auto = on }) }

func (u *Updater) set(mut func(*Status)) Status {
	u.mu.Lock()
	mut(&u.st)
	st, cb := u.st, u.OnChange
	u.mu.Unlock()
	if cb != nil {
		cb(st)
	}
	return st
}

func (u *Updater) fail(err error) Status {
	return u.set(func(s *Status) { s.State, s.Error, s.Progress = StateError, err.Error(), 0 })
}

// Check asks GitHub for the newest release and compares it with the running version.
func (u *Updater) Check(ctx context.Context) (Status, error) {
	if st := u.Status(); st.State == StateDownloading || st.State == StateInstalling || st.State == StateInstalled {
		return st, nil
	}
	u.set(func(s *Status) { s.State, s.Error = StateChecking, "" })
	rel, err := u.Source.Latest(ctx)
	if err != nil {
		return u.fail(err), err
	}
	return u.set(func(s *Status) {
		s.Latest, s.CheckedAt = &rel, time.Now()
		switch {
		case u.zip != "" && u.zipVersion == rel.Version:
			s.State, s.Progress = StateReady, 1
		case Newer(u.Current, rel.Version):
			s.State, s.Progress = StateAvailable, 0
		default:
			s.State = StateUpToDate
		}
	}), nil
}

// Download fetches the latest release's file for this system and verifies
// its SHA-256 against the release's checksums.txt.
func (u *Updater) Download(ctx context.Context) (Status, error) {
	st := u.Status()
	if u.Target == "" {
		return st, errors.New("this build cannot update itself: it is not the installed app")
	}
	if st.Latest == nil {
		return st, errors.New("check for updates first")
	}
	if st.State == StateDownloading || st.State == StateInstalling || st.State == StateInstalled {
		return st, nil
	}
	if u.Downloaded() != "" && st.State == StateReady {
		return st, nil
	}
	latest := *st.Latest
	asset, ok := PickAsset(latest.Assets, u.GOOS, u.GOARCH)
	if !ok {
		err := fmt.Errorf("release %s has no file for %s/%s", latest.Version, u.GOOS, u.GOARCH)
		return u.fail(err), err
	}
	var sums Asset
	for _, a := range latest.Assets {
		if a.Name == "checksums.txt" {
			sums = a
		}
	}
	if sums.URL == "" {
		err := fmt.Errorf("release %s has no checksums.txt; refusing to install it", latest.Version)
		return u.fail(err), err
	}
	text, err := u.Source.Text(ctx, sums.URL)
	if err != nil {
		return u.fail(fmt.Errorf("reading checksums.txt: %w", err)), err
	}
	want, ok := ChecksumFor(text, asset.Name)
	if !ok {
		err := fmt.Errorf("checksums.txt does not list %s", asset.Name)
		return u.fail(err), err
	}
	u.set(func(s *Status) { s.State, s.Progress, s.Error = StateDownloading, 0, "" })
	dir, err := os.MkdirTemp("", "vibe-coder-deck-update-")
	if err != nil {
		return u.fail(err), err
	}
	path := filepath.Join(dir, asset.Name)
	got, err := u.download(ctx, asset, path)
	if err != nil {
		os.RemoveAll(dir)
		return u.fail(fmt.Errorf("downloading %s: %w", asset.Name, err)), err
	}
	if got != want {
		os.RemoveAll(dir)
		err := fmt.Errorf("checksum mismatch for %s: the download is corrupt or not the published file", asset.Name)
		return u.fail(err), err
	}
	u.mu.Lock()
	old := u.zip
	u.zip, u.zipVersion = path, latest.Version
	u.mu.Unlock()
	if old != "" {
		os.RemoveAll(filepath.Dir(old))
	}
	return u.set(func(s *Status) { s.State, s.Progress = StateReady, 1 }), nil
}

// download streams an asset to path and returns its SHA-256.
func (u *Updater) download(ctx context.Context, asset Asset, path string) (string, error) {
	req, err := u.Source.Request(ctx, asset.URL, "application/octet-stream")
	if err != nil {
		return "", err
	}
	res, err := u.Source.client().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub %d", res.StatusCode)
	}
	total := res.ContentLength
	if total <= 0 {
		total = asset.Size
	}
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 256<<10)
	var done int64
	last := -1.0
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return "", err
			}
			h.Write(buf[:n])
			done += int64(n)
			if total > 0 {
				if p := float64(done) / float64(total); p-last >= 0.01 {
					last = p
					u.set(func(s *Status) { s.Progress = min(p, 1) })
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	u.set(func(s *Status) { s.Progress = 1 })
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Install replaces the app on disk with the downloaded release. The caller
// restarts the program afterwards.
func (u *Updater) Install() error {
	zip := u.Downloaded()
	if zip == "" {
		return errors.New("no update downloaded")
	}
	if st := u.Status(); st.State == StateInstalling || st.State == StateInstalled {
		return nil
	}
	u.set(func(s *Status) { s.State, s.Error = StateInstalling, "" })
	if err := Install(zip, u.Target); err != nil {
		u.fail(err)
		return err
	}
	os.RemoveAll(filepath.Dir(zip))
	u.mu.Lock()
	u.zip = ""
	u.mu.Unlock()
	u.set(func(s *Status) { s.State = StateInstalled })
	return nil
}

// Run checks after `first`, then every `every`, until ctx ends; with Auto on
// and a release build running, a newer release is downloaded right away.
func (u *Updater) Run(ctx context.Context, first, every time.Duration) {
	t := time.NewTimer(first)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if st, err := u.Check(ctx); err == nil && st.State == StateAvailable && st.Auto && st.Release {
			u.Download(ctx)
		}
		t.Reset(every)
	}
}

// Releases are the published versions with their notes, from GitHub when it
// answers and from the embedded changelog otherwise (source: "github" or
// "changelog").
func (u *Updater) Releases(ctx context.Context, n int) ([]Release, string, error) {
	rels, err := u.Source.Releases(ctx, n)
	if err == nil && len(rels) > 0 {
		return rels, "github", nil
	}
	if u.Changelog != "" {
		return ParseChangelog(u.Changelog), "changelog", nil
	}
	return nil, "", err
}

// Watch sets the function told about every change of status (the menu bar
// item); safe while Run is going.
func (u *Updater) Watch(fn func(Status)) {
	u.mu.Lock()
	u.OnChange = fn
	u.mu.Unlock()
}

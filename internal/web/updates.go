package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/nacif-software/vibe-coder-deck/internal/update"
)

// Updates is the program's updater (an *update.Updater); nil when this
// build cannot look for releases.
type Updates interface {
	Status() update.Status
	Check(ctx context.Context) (update.Status, error)
	Download(ctx context.Context) (update.Status, error)
	Install() error
	SetAuto(on bool)
	Releases(ctx context.Context, n int) ([]update.Release, string, error)
}

// releaseCache keeps the patch notes for a while: the drawer opens often and
// GitHub's API is rate limited.
type releaseCache struct {
	mu     sync.Mutex
	at     time.Time
	rels   []update.Release
	source string
}

func (s *Server) routeUpdates() {
	s.mux.HandleFunc("GET /api/update", s.updateStatus)
	s.mux.HandleFunc("POST /api/update/check", s.updateCheck)
	s.mux.HandleFunc("POST /api/update/download", s.updateDownload)
	s.mux.HandleFunc("POST /api/update/install", s.updateInstall)
	s.mux.HandleFunc("PUT /api/update/auto", s.updateAuto)
	s.mux.HandleFunc("POST /api/update/seen", s.updateSeen)
	s.mux.HandleFunc("GET /api/releases", s.releases)
}

// updateView is what GET /api/update answers: the updater's status plus
// whether the running version's notes are still unseen.
func (s *Server) updateView() map[string]any {
	st := update.Status{State: "unavailable", Current: s.Version}
	if s.Updates != nil {
		st = s.Updates.Status()
	}
	seen, _ := s.store.LastSeenVersion()
	return map[string]any{
		"status":    st,
		"whats_new": s.Updates != nil && update.IsRelease(st.Current) && seen != st.Current,
	}
}

func (s *Server) writeUpdate(w http.ResponseWriter) { writeJSON(w, 200, s.updateView()) }

func (s *Server) noUpdater(w http.ResponseWriter) bool {
	if s.Updates == nil {
		writeError(w, 501, errors.New("this build cannot look for updates"))
		return true
	}
	return false
}

func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) { s.writeUpdate(w) }

func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	if s.noUpdater(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	s.Updates.Check(ctx) // an error lands in the status
	s.writeUpdate(w)
}

func (s *Server) updateDownload(w http.ResponseWriter, r *http.Request) {
	if s.noUpdater(w) {
		return
	}
	st := s.Updates.Status()
	if st.Latest == nil {
		writeError(w, 409, errors.New("check for updates first"))
		return
	}
	if st.State != update.StateDownloading {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			s.Updates.Download(ctx)
		}()
	}
	s.writeUpdate(w)
}

func (s *Server) updateInstall(w http.ResponseWriter, r *http.Request) {
	if s.noUpdater(w) {
		return
	}
	if err := s.Updates.Install(); err != nil {
		writeError(w, 500, err)
		return
	}
	restarting := false
	if s.Restart != nil {
		restarting = s.Restart() == nil
	}
	writeJSON(w, 200, map[string]bool{"installed": true, "restarting": restarting})
}

func (s *Server) updateAuto(w http.ResponseWriter, r *http.Request) {
	var in struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, err)
		return
	}
	if err := s.store.SetAutoUpdate(in.On); err != nil {
		writeError(w, 500, err)
		return
	}
	if s.Updates != nil {
		s.Updates.SetAuto(in.On)
	}
	writeJSON(w, 200, map[string]bool{"auto": in.On})
}

func (s *Server) updateSeen(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SetLastSeenVersion(s.Version); err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]string{"seen": s.Version})
}

func (s *Server) releases(w http.ResponseWriter, r *http.Request) {
	if s.noUpdater(w) {
		return
	}
	c := &s.relCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rels == nil || time.Since(c.at) > 10*time.Minute {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		rels, source, err := s.Updates.Releases(ctx, 10)
		if err != nil {
			writeError(w, 502, err)
			return
		}
		if rels == nil {
			rels = []update.Release{}
		}
		c.rels, c.source, c.at = rels, source, time.Now()
	}
	writeJSON(w, 200, map[string]any{"source": c.source, "releases": c.rels, "current": s.Version})
}

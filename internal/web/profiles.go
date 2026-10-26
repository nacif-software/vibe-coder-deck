package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/nacif-software/vibe-coder-deck/internal/deck"
)

// AppInfo is an app bundle on this machine.
type AppInfo struct {
	Name     string `json:"name"`
	BundleID string `json:"bundle_id"`
	Path     string `json:"path"`
}

// AppsProvider reads the apps on this machine for the Profiles page.
type AppsProvider interface {
	Installed() []AppInfo
	Info(path string) (AppInfo, error)
	Choose() (AppInfo, bool, error) // the OS's own "choose an app" dialog
	Icon(path string) ([]byte, error)
}

// ActiveFunc reports the active profile and the app in front.
type ActiveFunc func() (profileID int64, bundleID, name string)

func (s *Server) routeProfiles() {
	s.mux.HandleFunc("GET /api/profiles", s.profiles)
	s.mux.HandleFunc("PUT /api/profiles/settings", s.profileSettings)
	s.mux.HandleFunc("POST /api/profiles", s.addProfile)
	s.mux.HandleFunc("PATCH /api/profiles/{id}", s.renameProfile)
	s.mux.HandleFunc("DELETE /api/profiles/{id}", s.deleteProfile)
	s.mux.HandleFunc("POST /api/apps", s.addApp)
	s.mux.HandleFunc("POST /api/apps/browse", s.browseApp)
	s.mux.HandleFunc("PATCH /api/apps/{id}", s.updateApp)
	s.mux.HandleFunc("DELETE /api/apps/{id}", s.deleteApp)
	s.mux.HandleFunc("GET /api/installed-apps", s.installedApps)
	s.mux.HandleFunc("GET /api/app-icon", s.appIcon)
}

// profileParam is ?profile=<id> (0 = the persistent profile).
func profileParam(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.URL.Query().Get("profile"), 10, 64)
	return id
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func (s *Server) profiles(w http.ResponseWriter, r *http.Request) {
	apps, err := s.store.Apps()
	if err != nil {
		writeError(w, 500, err)
		return
	}
	switching, _ := s.store.ProfileSwitching()
	persistent, _ := s.store.PersistentProfile()
	out := map[string]any{"apps": apps, "switching": switching, "persistent_profile_id": persistent, "active_profile_id": persistent}
	if s.Active != nil {
		pid, bundle, name := s.Active()
		out["active_profile_id"] = pid
		out["frontmost"] = map[string]string{"bundle_id": bundle, "name": name}
	}
	writeJSON(w, 200, out)
}

// afterProfileChange rewrites the pad when what it should hold may differ.
func (s *Server) afterProfileChange(w http.ResponseWriter, out map[string]any) {
	if _, err := s.sync(false); err != nil {
		out["sync_error"] = err.Error()
	} else {
		out["synced"] = true
	}
	writeJSON(w, 200, out)
}

func (s *Server) profileSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Switching    *bool  `json:"switching"`
		PersistentID *int64 `json:"persistent_profile_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, err)
		return
	}
	if in.PersistentID != nil {
		if err := s.store.SetPersistentProfile(*in.PersistentID); err != nil {
			writeError(w, 422, err)
			return
		}
	}
	if in.Switching != nil {
		if err := s.store.SetProfileSwitching(*in.Switching); err != nil {
			writeError(w, 500, err)
			return
		}
	}
	s.afterProfileChange(w, map[string]any{"ok": true})
}

func (s *Server) addProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AppID    int64  `json:"app_id"`
		Name     string `json:"name"`
		CopyFrom int64  `json:"copy_from"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, err)
		return
	}
	p, err := s.store.AddProfile(in.AppID, in.Name, in.CopyFrom)
	if err != nil {
		writeError(w, 422, err)
		return
	}
	writeJSON(w, 201, p)
}

func (s *Server) renameProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, err)
		return
	}
	if err := s.store.RenameProfile(pathID(r), in.Name); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) deleteProfile(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteProfile(pathID(r)); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	s.afterProfileChange(w, map[string]any{"deleted": true})
}

func statusFor(err error) int {
	if errors.Is(err, deck.ErrNoProfile) {
		return 404
	}
	return 422
}

func (s *Server) addApp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, err)
		return
	}
	info := AppInfo{Name: in.Name, Path: in.Path}
	if in.Path != "" && s.Apps != nil {
		got, err := s.Apps.Info(in.Path)
		if err != nil {
			writeError(w, 422, err)
			return
		}
		info = got
	} else if s.Apps != nil {
		// Added by name: an installed app of that name gives its bundle id and icon.
		for _, a := range s.Apps.Installed() {
			if strings.EqualFold(a.Name, strings.TrimSpace(in.Name)) {
				info = a
				break
			}
		}
	}
	s.storeApp(w, info)
}

func (s *Server) browseApp(w http.ResponseWriter, r *http.Request) {
	if s.Apps == nil {
		writeError(w, 501, errors.New("no app picker here"))
		return
	}
	info, ok, err := s.Apps.Choose()
	if err != nil {
		writeError(w, 422, err)
		return
	}
	if !ok {
		writeJSON(w, 200, map[string]bool{"cancelled": true})
		return
	}
	s.storeApp(w, info)
}

func (s *Server) storeApp(w http.ResponseWriter, info AppInfo) {
	app, err := s.store.AddApp(info.BundleID, info.Name, info.Path)
	if err != nil {
		writeError(w, 422, err)
		return
	}
	writeJSON(w, 201, app)
}

func (s *Server) updateApp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled         *bool  `json:"enabled"`
		ActiveProfileID *int64 `json:"active_profile_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, err)
		return
	}
	id := pathID(r)
	if in.Enabled != nil {
		if err := s.store.SetAppEnabled(id, *in.Enabled); err != nil {
			writeError(w, 422, err)
			return
		}
	}
	if in.ActiveProfileID != nil {
		if err := s.store.SetAppActiveProfile(id, *in.ActiveProfileID); err != nil {
			writeError(w, statusFor(err), err)
			return
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) deleteApp(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteApp(pathID(r)); err != nil {
		writeError(w, 422, err)
		return
	}
	s.afterProfileChange(w, map[string]any{"deleted": true})
}

func (s *Server) installedApps(w http.ResponseWriter, r *http.Request) {
	list := []AppInfo{}
	if s.Apps != nil {
		list = append(list, s.Apps.Installed()...)
	}
	writeJSON(w, 200, map[string]any{"apps": list})
}

var iconCache sync.Map // path → []byte

func (s *Server) appIcon(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if s.Apps == nil || path == "" {
		http.NotFound(w, r)
		return
	}
	png, ok := iconCache.Load(path)
	if !ok {
		b, err := s.Apps.Icon(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		iconCache.Store(path, b)
		png = b
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "max-age=86400")
	w.Write(png.([]byte))
}

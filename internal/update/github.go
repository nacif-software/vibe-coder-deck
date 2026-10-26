package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNoRelease is returned when the repository has published no release.
var ErrNoRelease = errors.New("no release found: none is published, or the repository is private and needs a token")

// Source is the GitHub repository that publishes the releases.
type Source struct {
	Repo   string       // owner/name
	API    string       // https://api.github.com
	Token  string       // optional; needed for a private repository
	Client *http.Client // nil: a client with a 30 s timeout
}

func (s Source) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (s Source) api() string {
	if s.API == "" {
		return "https://api.github.com"
	}
	return strings.TrimRight(s.API, "/")
}

// Request builds a GitHub request with the token and the given Accept type.
func (s Source) Request(ctx context.Context, url, accept string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "vibe-coder-deck")
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}
	return req, nil
}

// the shape GitHub returns for a release
type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func (g ghRelease) release() Release {
	r := Release{Version: g.TagName, Name: g.Name, Notes: g.Body, PublishedAt: g.PublishedAt, URL: g.HTMLURL}
	if r.Name == "" {
		r.Name = g.TagName
	}
	for _, a := range g.Assets {
		r.Assets = append(r.Assets, Asset{Name: a.Name, URL: a.URL, Size: a.Size})
	}
	return r
}

func (s Source) getJSON(ctx context.Context, path string, out any) error {
	req, err := s.Request(ctx, s.api()+path, "application/vnd.github+json")
	if err != nil {
		return err
	}
	res, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusNotFound {
		return ErrNoRelease
	}
	if res.StatusCode != http.StatusOK {
		var gh struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &gh)
		if gh.Message == "" {
			gh.Message = strings.TrimSpace(string(body))
		}
		return fmt.Errorf("GitHub %d: %s", res.StatusCode, gh.Message)
	}
	return json.Unmarshal(body, out)
}

// Latest is the newest published release (drafts and prereleases excluded).
func (s Source) Latest(ctx context.Context) (Release, error) {
	var g ghRelease
	if err := s.getJSON(ctx, "/repos/"+s.Repo+"/releases/latest", &g); err != nil {
		return Release{}, err
	}
	return g.release(), nil
}

// Releases is up to n published releases, newest first.
func (s Source) Releases(ctx context.Context, n int) ([]Release, error) {
	var gs []ghRelease
	if err := s.getJSON(ctx, fmt.Sprintf("/repos/%s/releases?per_page=%d", s.Repo, n), &gs); err != nil {
		return nil, err
	}
	rels := make([]Release, 0, len(gs))
	for _, g := range gs {
		if g.Draft || g.Prerelease {
			continue
		}
		rels = append(rels, g.release())
	}
	return rels, nil
}

// Text downloads a small asset (checksums.txt) through the API.
func (s Source) Text(ctx context.Context, url string) (string, error) {
	req, err := s.Request(ctx, url, "application/octet-stream")
	if err != nil {
		return "", err
	}
	res, err := s.client().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub %d fetching %s", res.StatusCode, url)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return string(b), err
}

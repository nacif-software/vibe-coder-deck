package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
)

// Reading a public repository's releases without the REST API: the tag from
// github.com's /releases/latest redirect, the files from the release's
// checksums.txt (it names every one, and /releases/download/ serves them),
// the notes from the tag's CHANGELOG.md (the release notes are its section).

func (s Source) web() string {
	if s.Web == "" {
		return "https://github.com"
	}
	return strings.TrimRight(s.Web, "/")
}

func (s Source) raw() string {
	if s.Raw == "" {
		return "https://raw.githubusercontent.com"
	}
	return strings.TrimRight(s.Raw, "/")
}

// latestTag follows nothing: /releases/latest redirects to the newest
// release's page (.../releases/tag/v1.2.3), or to /releases when there is none.
func (s Source) latestTag(ctx context.Context) (string, error) {
	req, err := s.Request(ctx, s.web()+"/"+s.Repo+"/releases/latest", "text/html")
	if err != nil {
		return "", err
	}
	c := *s.client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := c.Do(req)
	if err != nil {
		return "", err
	}
	res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return "", ErrNoRelease
	}
	loc := res.Header.Get("Location")
	if res.StatusCode/100 != 3 || loc == "" {
		return "", fmt.Errorf("GitHub %d for the latest release", res.StatusCode)
	}
	if !strings.Contains(loc, "/releases/tag/") {
		return "", ErrNoRelease
	}
	return path.Base(loc), nil
}

func (s Source) getText(ctx context.Context, url string) (string, error) {
	req, err := s.Request(ctx, url, "text/plain")
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
	b, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	return string(b), err
}

func (s Source) releasePage(tag string) string {
	return s.web() + "/" + s.Repo + "/releases/tag/" + tag
}

func (s Source) latestWeb(ctx context.Context) (Release, error) {
	tag, err := s.latestTag(ctx)
	if err != nil {
		return Release{}, err
	}
	download := s.web() + "/" + s.Repo + "/releases/download/" + tag + "/"
	sums, err := s.getText(ctx, download+"checksums.txt")
	if err != nil {
		return Release{}, fmt.Errorf("release %s: %w", tag, err)
	}
	rel := Release{Version: tag, Name: tag, URL: s.releasePage(tag)}
	for _, line := range strings.Split(sums, "\n") {
		if f := strings.Fields(line); len(f) >= 2 {
			name := path.Base(strings.TrimPrefix(f[len(f)-1], "*"))
			rel.Assets = append(rel.Assets, Asset{Name: name, URL: download + name})
		}
	}
	rel.Assets = append(rel.Assets, Asset{Name: "checksums.txt", URL: download + "checksums.txt"})
	// The notes are a nicety: a release without them still updates.
	if md, err := s.getText(ctx, s.raw()+"/"+s.Repo+"/"+tag+"/CHANGELOG.md"); err == nil {
		for _, r := range ParseChangelog(md) {
			if r.Version == "v"+strings.TrimPrefix(tag, "v") {
				rel.Notes, rel.PublishedAt = r.Notes, r.PublishedAt
			}
		}
	}
	return rel, nil
}

func (s Source) releasesWeb(ctx context.Context, n int) ([]Release, error) {
	tag, err := s.latestTag(ctx)
	if err != nil {
		return nil, err
	}
	md, err := s.getText(ctx, s.raw()+"/"+s.Repo+"/"+tag+"/CHANGELOG.md")
	if err != nil {
		return nil, err
	}
	rels := ParseChangelog(md)
	if len(rels) > n {
		rels = rels[:n]
	}
	for i := range rels {
		rels[i].URL = s.releasePage(rels[i].Version)
	}
	return rels, nil
}

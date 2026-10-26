package update

import (
	"regexp"
	"strings"
	"time"
)

// Release is a published version: its tag, its notes and its files.
type Release struct {
	Version     string    `json:"version"` // the tag: v1.2.3
	Name        string    `json:"name"`    // the release title
	Notes       string    `json:"notes"`   // markdown
	PublishedAt time.Time `json:"published_at"`
	URL         string    `json:"url,omitempty"` // the release page
	Assets      []Asset   `json:"assets,omitempty"`
}

// Asset is a file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"` // the API url of the file (works for private repos too)
	Size int64  `json:"size"`
}

// A changelog heading in the Keep a Changelog shape: "## [1.2.3] - 2026-10-26".
var changelogHeading = regexp.MustCompile(`^## \[([^\]]+)\](?:\s*-\s*(\d{4}-\d{2}-\d{2}))?`)

// ParseChangelog reads the released versions of a CHANGELOG.md, newest first
// as written, each with the notes under its heading. "[Unreleased]" is skipped.
func ParseChangelog(md string) []Release {
	var rels []Release
	var cur *Release
	var body []string
	flush := func() {
		if cur != nil {
			cur.Notes = strings.TrimSpace(strings.Join(body, "\n"))
			rels = append(rels, *cur)
		}
		cur, body = nil, nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		if m := changelogHeading.FindStringSubmatch(line); m != nil {
			flush()
			if strings.EqualFold(m[1], "unreleased") {
				continue
			}
			tag := "v" + strings.TrimPrefix(m[1], "v")
			cur = &Release{Version: tag, Name: tag}
			if m[2] != "" {
				cur.PublishedAt, _ = time.Parse("2006-01-02", m[2])
			}
			continue
		}
		if cur != nil {
			body = append(body, line)
		}
	}
	flush()
	return rels
}

// ChangelogSection is the notes of one version of a changelog ("v1.2.3" or "1.2.3").
func ChangelogSection(md, version string) (string, bool) {
	want := "v" + strings.TrimPrefix(version, "v")
	for _, r := range ParseChangelog(md) {
		if r.Version == want {
			return r.Notes, true
		}
	}
	return "", false
}

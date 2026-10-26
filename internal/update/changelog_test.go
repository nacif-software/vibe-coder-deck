package update

import (
	"strings"
	"testing"
)

const sampleChangelog = `# Changelog

All notable changes to Vibe Coder Deck.

## [Unreleased]

- Something coming.

## [0.2.0] - 2026-11-02

### Added
- Profiles per app.

### Fixed
- Key 1 ran twice.

## [0.1.0] - 2026-10-26

First release.
`

func TestParseChangelogReadsTheReleasesAndTheirNotes(t *testing.T) {
	rels := ParseChangelog(sampleChangelog)
	if len(rels) != 2 || rels[0].Version != "v0.2.0" || rels[1].Version != "v0.1.0" {
		t.Fatalf("releases = %+v", rels)
	}
	if got := rels[0].PublishedAt.Format("2006-01-02"); got != "2026-11-02" {
		t.Fatalf("date = %s", got)
	}
	if !strings.HasPrefix(rels[0].Notes, "### Added") || !strings.Contains(rels[0].Notes, "Key 1 ran twice.") || strings.Contains(rels[0].Notes, "0.1.0") {
		t.Fatalf("notes = %q", rels[0].Notes)
	}
	if rels[1].Notes != "First release." || rels[1].Name != "v0.1.0" {
		t.Fatalf("first = %+v", rels[1])
	}
}

func TestChangelogSectionFindsOneVersion(t *testing.T) {
	if s, ok := ChangelogSection(sampleChangelog, "v0.1.0"); !ok || s != "First release." {
		t.Fatalf("section = %q, %v", s, ok)
	}
	if s, ok := ChangelogSection(sampleChangelog, "0.2.0"); !ok || !strings.HasPrefix(s, "### Added") {
		t.Fatalf("section = %q, %v", s, ok)
	}
	if _, ok := ChangelogSection(sampleChangelog, "v9.9.9"); ok {
		t.Fatal("unknown version found")
	}
}

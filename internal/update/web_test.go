package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const webChangelog = "# Changelog\n\n## [0.2.0] - 2026-11-02\n\n### Added\n- Profiles\n\n## [0.1.0] - 2026-10-26\n\nThe first release.\n"

const webChecksums = "aaa  Vibe-Coder-Deck-v0.2.0-macos.zip\nbbb  vibe-coder-deck-v0.2.0-windows-amd64.zip\n"

// gitHubWeb fakes github.com and raw.githubusercontent.com for a public
// repository read without a token; any call to the REST API fails the test.
func gitHubWeb(t *testing.T, latest string) (Source, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/nacif-software/vibe-coder-deck/releases/latest":
			w.Header().Set("Location", latest)
			w.WriteHeader(http.StatusFound)
		case "/nacif-software/vibe-coder-deck/releases/download/v0.2.0/checksums.txt":
			fmt.Fprint(w, webChecksums)
		case "/raw/nacif-software/vibe-coder-deck/v0.2.0/CHANGELOG.md":
			fmt.Fprint(w, webChangelog)
		default:
			if strings.HasPrefix(r.URL.Path, "/repos/") {
				t.Errorf("the REST API was called: %s", r.URL.Path)
			}
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return Source{Repo: "nacif-software/vibe-coder-deck", Web: srv.URL, Raw: srv.URL + "/raw", Client: srv.Client()}, &paths
}

func TestLatestWithoutATokenReadsGitHubsPagesNotTheAPI(t *testing.T) {
	src, _ := gitHubWeb(t, "https://github.com/nacif-software/vibe-coder-deck/releases/tag/v0.2.0")
	rel, err := src.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "v0.2.0" || rel.Notes != "### Added\n- Profiles" || rel.PublishedAt.Format("2006-01-02") != "2026-11-02" {
		t.Fatalf("release = %+v", rel)
	}
	if rel.URL != src.Web+"/nacif-software/vibe-coder-deck/releases/tag/v0.2.0" {
		t.Fatalf("url = %q", rel.URL)
	}
	want := []string{"Vibe-Coder-Deck-v0.2.0-macos.zip", "vibe-coder-deck-v0.2.0-windows-amd64.zip", "checksums.txt"}
	if len(rel.Assets) != len(want) {
		t.Fatalf("assets = %+v", rel.Assets)
	}
	for i, a := range rel.Assets {
		if a.Name != want[i] || a.URL != src.Web+"/nacif-software/vibe-coder-deck/releases/download/v0.2.0/"+want[i] {
			t.Errorf("asset %d = %+v", i, a)
		}
	}
	if text, err := src.Text(context.Background(), rel.Assets[2].URL); err != nil || text != webChecksums {
		t.Fatalf("checksums = %q, %v", text, err)
	}
}

func TestLatestWithoutATokenAndNoRelease(t *testing.T) {
	src, _ := gitHubWeb(t, "https://github.com/nacif-software/vibe-coder-deck/releases")
	if _, err := src.Latest(context.Background()); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("err = %v, want ErrNoRelease", err)
	}
}

func TestReleasesWithoutATokenComeFromTheLatestChangelog(t *testing.T) {
	src, _ := gitHubWeb(t, "https://github.com/nacif-software/vibe-coder-deck/releases/tag/v0.2.0")
	rels, err := src.Releases(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 2 || rels[0].Version != "v0.2.0" || rels[1].Version != "v0.1.0" || rels[1].Notes != "The first release." {
		t.Fatalf("releases = %+v", rels)
	}
	if rels[1].URL != src.Web+"/nacif-software/vibe-coder-deck/releases/tag/v0.1.0" {
		t.Fatalf("url = %q", rels[1].URL)
	}
	if rels, _ := src.Releases(context.Background(), 1); len(rels) != 1 {
		t.Fatalf("n=1 gave %d releases", len(rels))
	}
}

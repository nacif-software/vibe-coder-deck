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

const latestJSON = `{"tag_name":"v0.2.0","name":"Vibe Coder Deck v0.2.0","body":"### Added\n- Profiles","html_url":"https://github.com/nacif-software/vibe-coder-deck/releases/tag/v0.2.0","published_at":"2026-11-02T10:00:00Z","draft":false,"prerelease":false,"assets":[{"name":"Vibe-Coder-Deck-v0.2.0-macos.zip","url":"https://api.example/assets/1","browser_download_url":"https://example/mac.zip","size":123},{"name":"checksums.txt","url":"https://api.example/assets/2","browser_download_url":"https://example/checksums.txt","size":10}]}`

func gitHub(t *testing.T, handler http.HandlerFunc) Source {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return Source{Repo: "nacif-software/vibe-coder-deck", API: srv.URL, Token: "tok", Client: srv.Client()}
}

func TestLatestReadsTheNewestReleaseFromGitHub(t *testing.T) {
	var auth, accept string
	src := gitHub(t, func(w http.ResponseWriter, r *http.Request) {
		auth, accept = r.Header.Get("Authorization"), r.Header.Get("Accept")
		if r.URL.Path != "/repos/nacif-software/vibe-coder-deck/releases/latest" {
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, latestJSON)
	})
	rel, err := src.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "v0.2.0" || rel.Name != "Vibe Coder Deck v0.2.0" || rel.Notes != "### Added\n- Profiles" || rel.URL != "https://github.com/nacif-software/vibe-coder-deck/releases/tag/v0.2.0" {
		t.Fatalf("release = %+v", rel)
	}
	if rel.PublishedAt.Format("2006-01-02") != "2026-11-02" {
		t.Fatalf("published = %v", rel.PublishedAt)
	}
	if len(rel.Assets) != 2 || rel.Assets[0].Name != "Vibe-Coder-Deck-v0.2.0-macos.zip" || rel.Assets[0].URL != "https://api.example/assets/1" || rel.Assets[0].Size != 123 {
		t.Fatalf("assets = %+v", rel.Assets)
	}
	if auth != "Bearer tok" || !strings.Contains(accept, "application/vnd.github+json") {
		t.Fatalf("headers: auth %q accept %q", auth, accept)
	}
}

func TestLatestReportsWhenThereIsNoRelease(t *testing.T) {
	src := gitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})
	if _, err := src.Latest(context.Background()); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("err = %v", err)
	}
}

func TestLatestFailsWithGitHubsMessage(t *testing.T) {
	src := gitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, `{"message":"API rate limit exceeded for 1.2.3.4."}`)
	})
	_, err := src.Latest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "API rate limit exceeded") {
		t.Fatalf("err = %v", err)
	}
}

func TestReleasesSkipsDraftsAndPrereleases(t *testing.T) {
	src := gitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/nacif-software/vibe-coder-deck/releases" || r.URL.Query().Get("per_page") != "10" {
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, `[{"tag_name":"v0.3.0","draft":true},{"tag_name":"v0.3.0-rc1","prerelease":true},{"tag_name":"v0.2.0","body":"two","published_at":"2026-11-02T10:00:00Z"},{"tag_name":"v0.1.0","body":"one","published_at":"2026-10-26T10:00:00Z"}]`)
	})
	rels, err := src.Releases(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 2 || rels[0].Version != "v0.2.0" || rels[1].Notes != "one" {
		t.Fatalf("releases = %+v", rels)
	}
}

func TestTextFetchesASmallAssetAsOctetStream(t *testing.T) {
	var accept string
	src := gitHub(t, func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		fmt.Fprint(w, "abc  file.zip\n")
	})
	text, err := src.Text(context.Background(), src.API+"/assets/2")
	if err != nil || text != "abc  file.zip\n" || accept != "application/octet-stream" {
		t.Fatalf("text %q err %v accept %q", text, err, accept)
	}
}

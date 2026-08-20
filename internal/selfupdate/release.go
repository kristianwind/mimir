// Package selfupdate checks for new releases and, where the deployment allows
// it, installs one.
//
// Two things shape the design.
//
// A container cannot replace its own image, and Mimir ships as a Yggdrasil
// rune. So the updater detects how it is deployed and refuses honestly rather
// than half-succeeding: in a container it reports the update and names the
// rune action that applies it, and nothing else.
//
// And an update that bricks an install is worse than no updater at all. The
// candidate binary is checksum-verified and then actually run — it must serve
// a health check on this machine before anything is replaced — and a watchdog
// spawned from the known-good backup restores it if the new one fails to come
// up afterwards. Neither step is a promise about a file; both are observations
// about a process that ran.
package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Release is the subset of a GitHub release Mimir uses.
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []Asset   `json:"assets"`
}

// Asset is one downloadable file on a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// AssetName is the binary this build would install: one artefact per
// platform, named so the wrong architecture cannot be downloaded by accident.
func AssetName() string {
	return fmt.Sprintf("mimir_%s_%s", runtime.GOOS, runtime.GOARCH)
}

// ChecksumsName is the file listing SHA-256 sums for a release's assets.
const ChecksumsName = "checksums.txt"

// Find returns the asset with a name, if the release carries it.
func (r Release) Find(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// latest fetches the newest published release.
func (u *Updater) latest(ctx context.Context) (Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", u.Repo)
	if u.releaseURL != "" {
		url = u.releaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mimir/"+u.Version)

	resp, err := u.client().Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("selfupdate: could not reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// GitHub answers 404 both for a repository with no releases and for
		// one an anonymous request cannot see. The updater carries no
		// credentials on purpose, so a private repository lands here with a
		// perfectly published release — and saying "no releases yet" would
		// be confidently wrong.
		return Release{}, fmt.Errorf(
			"selfupdate: fandt ingen udgivelser i %s. Enten er der ingen endnu, "+
				"eller også er repoet privat — Mimir henter uden login, så et privat "+
				"repos udgivelser er usynlige for den", u.Repo)
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("selfupdate: GitHub returned %s", strings.TrimSpace(resp.Status))
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Release{}, fmt.Errorf("selfupdate: decode release: %w", err)
	}
	return rel, nil
}

// Newer reports whether b is a later version than a.
//
// Both must be vMAJOR.MINOR.PATCH. Anything else — a dev build, a dirty tag —
// is never "older than" a release, because comparing an unversioned build
// against a tag is a question with no answer, and guessing it would offer an
// update that replaces something the operator built on purpose.
func Newer(a, b string) bool {
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	if !aok || !bok {
		return false
	}
	for i := 0; i < 3; i++ {
		if av[i] != bv[i] {
			return bv[i] > av[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "v") {
		return out, false
	}
	// A pre-release or build suffix is not something to compare numerically.
	if strings.ContainsAny(v, "-+") {
		return out, false
	}
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

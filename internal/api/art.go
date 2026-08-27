package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kristianwind/mimir/internal/model"
)

// Character art.
//
// The pictures are the game's own, served by Enka alongside the data. Mimir
// fetches each one once and keeps it, rather than pointing the browser at
// enka.network:
//
//   - A page that loads eight images from a third party tells that third party
//     which characters this household plays, every time it is opened. This is
//     a product with no telemetry in it; borrowing someone else's would be an
//     odd exception.
//   - Enka is a volunteer service that already gives Mimir its data. Serving a
//     megabyte of art per page view off their bandwidth is not how to treat
//     that.
//   - A cached picture survives Enka being down, and the roster page is not
//     worth a dependency on anyone's uptime.
//
// The only input is a character key, which must exist in the active snapshot
// before anything is fetched. The URL is then built from mined data, so this
// cannot be pointed at an arbitrary host — it is a cache with a fixed source,
// not a proxy.

// artCandidates are the image names to try for one character, best first.
//
// The namecard leads because it is the one asset the game itself designed as a
// backdrop: a 420×200 banner in the character's own colours, about a tenth the
// weight of the splash. The obvious alternative was tried and is worse — the
// square card portrait carries the game's decorative frame, which crops into a
// wide card as a visible border around a washed-out figure.
//
// The splash is the fallback for characters with no namecard (the Travelers),
// and the plain icon for the handful with neither.
func artCandidates(base string) []string {
	// An artifact icon arrives as the whole name because that is how the
	// datamine writes it. Only a character's portrait is a suffix with three
	// prefixes worth trying.
	if strings.HasPrefix(base, "UI_") {
		return []string{base}
	}

	return []string{
		"UI_NameCardPic_" + base + "_P",
		"UI_Gacha_AvatarImg_" + base,
		"UI_AvatarIcon_" + base,
	}
}

// artSource is a var rather than a constant so a test can point it at a local
// server. Nothing outside a test writes it.
var artSource = "https://enka.network/ui/"

// artFetches serialises the work per character, so eight cards rendering at
// once fetch one picture rather than eight copies of it.
var artFetches sync.Map // base -> *sync.Mutex

// handleCharacterArt serves one character's backdrop.
// handleArtifactArt serves the picture for one set and slot.
//
// The same cache and the same rule as a character's portrait: fetched once,
// kept, and served from here afterwards. A page that pulled two hundred
// artifact icons from somebody else's server would hand them a picture of the
// household's whole inventory, one request at a time.
func (s *Server) handleArtifactArt(w http.ResponseWriter, r *http.Request) {
	setKey := chi.URLParam(r, "setKey")
	slot := model.Slot(chi.URLParam(r, "slot"))

	snap, err := s.GameData.Current()
	if err != nil {
		writeDomainError(w, err)
		return
	}
	set, ok := snap.ArtifactSets[setKey]
	if !ok {
		writeError(w, http.StatusNotFound, "no such artifact set", "")
		return
	}
	icon := set.Icons[slot]
	if icon == "" {
		writeError(w, http.StatusNotFound, "that set has no picture for that slot", "")
		return
	}
	s.serveArt(w, r, icon)
}

func (s *Server) handleCharacterArt(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "characterKey")

	snap, err := s.GameData.Current()
	if err != nil {
		writeDomainError(w, err)
		return
	}
	def, err := snap.Char(key)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such character", "")
		return
	}
	if def.Art == "" {
		// Travelers and a few trial entries have no picture in the store.
		// That is a fact about the game, not a failure.
		writeError(w, http.StatusNotFound, "that character has no artwork", "")
		return
	}

	s.serveArt(w, r, def.Art)
}

// serveArt fetches a picture once and serves it from disk thereafter.
func (s *Server) serveArt(w http.ResponseWriter, r *http.Request, base string) {
	path, err := s.artFile(r.Context(), base)
	if err != nil {
		if errors.Is(err, errNoArt) {
			writeError(w, http.StatusNotFound, "that character has no artwork", "")
			return
		}
		if s.Log != nil {
			s.Log.Warn("could not fetch art", "image", base, "error", err)
		}
		writeError(w, http.StatusBadGateway, "the artwork could not be fetched", "")
		return
	}

	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "that character has no artwork", "")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the artwork could not be read", "")
		return
	}

	// A week: the art only changes when the game does, and by then the whole
	// snapshot has been resynced anyway.
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

// errNoArt means every candidate name 404ed. Cached like a picture is, because
// re-asking Enka on every page load for something that does not exist is worse
// than remembering that it does not.
var errNoArt = errors.New("api: this character has no artwork")

// errNotFound is the source saying the image does not exist, as opposed to
// the source not answering. Only the first is worth remembering.
var errNotFound = errors.New("api: no such image")

// artFile returns the path to a cached picture, fetching it the first time.
func (s *Server) artFile(ctx context.Context, base string) (string, error) {
	dir := s.artDir()
	if dir == "" {
		return "", fmt.Errorf("api: no data directory for the art cache")
	}
	path := filepath.Join(dir, base+".png")
	missing := path + ".none"

	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if _, err := os.Stat(missing); err == nil {
		return "", errNoArt
	}

	lock, _ := artFetches.LoadOrStore(base, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	// Another request may have finished while this one waited.
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if _, err := os.Stat(missing); err == nil {
		return "", errNoArt
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}

	// A transient failure is not evidence about the character.
	//
	// The marker below is permanent, so writing one because a request timed
	// out or the CDN answered 502 blanks that picture for good — and blanks
	// the whole roster if it happens while the page is first loading. Only a
	// source that answered "no such image" gets remembered as one.
	var reachable bool
	for _, name := range artCandidates(base) {
		body, err := s.fetchArt(ctx, name)
		if err == nil {
			if err := writeFileAtomic(path, body); err != nil {
				return "", err
			}
			return path, nil
		}
		if errors.Is(err, errNotFound) {
			reachable = true
		}
	}
	if !reachable {
		return "", fmt.Errorf("api: could not reach the art source for %q", base)
	}

	_ = os.WriteFile(missing, nil, 0o640)
	return "", errNoArt
}

func (s *Server) artDir() string {
	if s.Config == nil || s.Config.DataDir == "" {
		return ""
	}
	return filepath.Join(s.Config.DataDir, "art")
}

// fetchArt gets one image, identifying Mimir the way Enka asks integrators to.
func (s *Server) fetchArt(ctx context.Context, name string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artSource+name+".png", nil)
	if err != nil {
		return nil, err
	}
	agent := "mimir"
	if s.Config != nil && s.Config.UserAgent != "" {
		agent = s.Config.UserAgent
	}
	req.Header.Set("User-Agent", agent)
	req.Header.Set("Accept", "image/png,image/*")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", errNotFound, name)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api: %s answered %s", name, res.Status)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		return nil, fmt.Errorf("api: %s is not an image (%s)", name, ct)
	}

	// Four megabytes is well past the largest splash the game ships; anything
	// bigger is not the file we asked for.
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("api: %s came back empty", name)
	}
	return body, nil
}

// writeFileAtomic keeps a half-written picture out of the cache: a truncated
// PNG would be served forever, since the cache only checks that a file exists.
func writeFileAtomic(path string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".art-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o640); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

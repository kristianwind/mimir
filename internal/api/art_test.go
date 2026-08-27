package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kristianwind/mimir/internal/config"
	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// fakeEnka stands in for the picture host and counts what was asked for.
type fakeEnka struct {
	mu      sync.Mutex
	asked   []string
	serve   map[string][]byte
	latency func()
}

func (f *fakeEnka) start(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.asked = append(f.asked, r.URL.Path)
		body, ok := f.serve[r.URL.Path]
		f.mu.Unlock()

		if f.latency != nil {
			f.latency()
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	old := artSource
	artSource = srv.URL + "/ui/"
	t.Cleanup(func() { artSource = old })
}

func (f *fakeEnka) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

// artServer wires a server with a snapshot and a writable cache directory.
func artServer(t *testing.T, chars map[string]gamedata.Character) (*Server, func(as, method, path, body string) *httptest.ResponseRecorder) { //nolint:unparam // the server is handy in new tests
	t.Helper()
	s, do := newServer(t)
	s.Config = &config.Config{DataDir: t.TempDir(), UserAgent: "mimir/test"}
	s.GameData = gamedata.NewStore(s.DB)
	if err := s.GameData.Save(&gamedata.Snapshot{Version: "test", Characters: chars}); err != nil {
		t.Fatal(err)
	}
	if err := s.GameData.Load(); err != nil {
		t.Fatal(err)
	}
	// A fresh lock table per test, or the serialisation test inherits the
	// mutex a previous one left behind.
	artFetches = sync.Map{}
	return s, do
}

func TestArtIsFetchedOnceAndThenServedLocally(t *testing.T) {
	enka := &fakeEnka{serve: map[string][]byte{
		"/ui/UI_NameCardPic_Shougun_P.png": []byte("\x89PNG namecard"),
	}}
	enka.start(t)

	_, do := artServer(t, map[string]gamedata.Character{
		"RaidenShogun": {Key: "RaidenShogun", Art: "Shougun"},
	})

	res := do("member", "GET", "/api/art/RaidenShogun", "")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if res.Body.String() != "\x89PNG namecard" {
		t.Errorf("body = %q", res.Body.String())
	}
	if ct := res.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type = %q", ct)
	}
	if cc := res.Header().Get("Cache-Control"); cc == "" {
		t.Error("no Cache-Control: the browser will re-ask for a picture that never changes")
	}

	// The whole point: the second view costs the picture host nothing.
	before := len(enka.calls())
	if res = do("member", "GET", "/api/art/RaidenShogun", ""); res.Code != http.StatusOK {
		t.Fatalf("second request: %d", res.Code)
	}
	if got := len(enka.calls()); got != before {
		t.Errorf("the host was asked again on a cache hit (%d calls, was %d)", got, before)
	}
}

// The namecard is the one that suits a card background, but not every
// character has one. The fallbacks are what keep the roster from having holes
// in it.
func TestArtFallsBackThroughTheCandidates(t *testing.T) {
	enka := &fakeEnka{serve: map[string][]byte{
		"/ui/UI_Gacha_AvatarImg_Ayaka.png": []byte("\x89PNG splash"),
	}}
	enka.start(t)

	_, do := artServer(t, map[string]gamedata.Character{
		"KamisatoAyaka": {Key: "KamisatoAyaka", Art: "Ayaka"},
	})

	res := do("member", "GET", "/api/art/KamisatoAyaka", "")
	if res.Code != http.StatusOK || res.Body.String() != "\x89PNG splash" {
		t.Fatalf("status = %d, body = %q", res.Code, res.Body.String())
	}
	if calls := enka.calls(); len(calls) != 2 || calls[0] != "/ui/UI_NameCardPic_Ayaka_P.png" {
		t.Errorf("candidates were tried in the wrong order: %v", calls)
	}
}

// A character with no picture must not send a request to the host on every
// page view for the rest of the instance's life.
func TestAMissingPictureIsRememberedToo(t *testing.T) {
	enka := &fakeEnka{serve: map[string][]byte{}}
	enka.start(t)

	_, do := artServer(t, map[string]gamedata.Character{
		"Traveler": {Key: "Traveler", Art: "PlayerBoy"},
	})

	if res := do("member", "GET", "/api/art/Traveler", ""); res.Code != http.StatusNotFound {
		t.Fatalf("status = %d", res.Code)
	}
	after := len(enka.calls())
	if res := do("member", "GET", "/api/art/Traveler", ""); res.Code != http.StatusNotFound {
		t.Fatalf("second status = %d", res.Code)
	}
	if got := len(enka.calls()); got != after {
		t.Errorf("a known-missing picture was looked up again (%d calls, was %d)", got, after)
	}
}

// The URL is built from mined data and the key is looked up first, so this
// cannot be turned into a fetcher for arbitrary names.
func TestArtRefusesWhatIsNotInTheSnapshot(t *testing.T) {
	enka := &fakeEnka{serve: map[string][]byte{}}
	enka.start(t)

	_, do := artServer(t, map[string]gamedata.Character{
		"RaidenShogun": {Key: "RaidenShogun", Art: "Shougun"},
	})

	for _, key := range []string{"NotACharacter", "..%2f..%2fetc%2fpasswd"} {
		if res := do("member", "GET", "/api/art/"+key, ""); res.Code != http.StatusNotFound {
			t.Errorf("%s gave %d, want 404", key, res.Code)
		}
	}
	if calls := enka.calls(); len(calls) != 0 {
		t.Errorf("an unknown key reached the picture host: %v", calls)
	}
}

// A character in the snapshot with no art name is a fact about the game, not a
// reason to ask the host about a name built from an empty string.
func TestACharacterWithNoArtNameIsNotFetched(t *testing.T) {
	enka := &fakeEnka{serve: map[string][]byte{}}
	enka.start(t)

	_, do := artServer(t, map[string]gamedata.Character{
		"Traveler": {Key: "Traveler"},
	})

	if res := do("member", "GET", "/api/art/Traveler", ""); res.Code != http.StatusNotFound {
		t.Fatalf("status = %d", res.Code)
	}
	if calls := enka.calls(); len(calls) != 0 {
		t.Errorf("a character with no art name still hit the host: %v", calls)
	}
}

// Eight cards render at once. They must not become eight downloads of the same
// picture, which is exactly what a cache without a lock does.
func TestConcurrentViewsFetchOnce(t *testing.T) {
	ready := make(chan struct{})
	enka := &fakeEnka{
		serve:   map[string][]byte{"/ui/UI_NameCardPic_Shougun_P.png": []byte("\x89PNG")},
		latency: func() { <-ready },
	}
	enka.start(t)

	_, do := artServer(t, map[string]gamedata.Character{
		"RaidenShogun": {Key: "RaidenShogun", Art: "Shougun"},
	})

	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = do("member", "GET", "/api/art/RaidenShogun", "").Code
		}(i)
	}
	close(ready)
	wg.Wait()

	for i, c := range codes {
		if c != http.StatusOK {
			t.Errorf("request %d gave %d", i, c)
		}
	}
	if got := len(enka.calls()); got != 1 {
		t.Errorf("%d requests to the picture host for one picture", got)
	}
}

// A body that is not an image must not be cached and then served as one
// forever after — an error page from a proxy is the realistic case.
func TestANonImageIsNotCached(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>502</html>")
	}))
	defer srv.Close()
	old := artSource
	artSource = srv.URL + "/ui/"
	defer func() { artSource = old }()
	artFetches = sync.Map{}

	_, do := artServer(t, map[string]gamedata.Character{
		"RaidenShogun": {Key: "RaidenShogun", Art: "Shougun"},
	})

	if res := do("member", "GET", "/api/art/RaidenShogun", ""); res.Code == http.StatusOK {
		t.Fatalf("an HTML error page was served as a picture: %q", res.Body.String())
	}
}

// The bug this exists to stop: a picture that failed to arrive once was
// remembered as a picture that does not exist, for good. One bad minute while
// a page was loading left the whole roster grey, and no amount of reloading
// brought it back — the negative marker on disk is checked before anything
// else and never expires.
func TestAnOutageIsNotRememberedAsAMissingPicture(t *testing.T) {
	// A source that is up but answering 500, which is not the same claim as
	// answering 404.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream is having a moment", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	old := artSource
	artSource = srv.URL + "/ui/"
	t.Cleanup(func() { artSource = old })

	s, do := artServer(t, map[string]gamedata.Character{
		"RaidenShogun": {Key: "RaidenShogun", Art: "Shougun"},
	})

	if res := do("member", "GET", "/api/art/RaidenShogun", ""); res.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want a gateway error rather than a verdict about the character", res.Code)
	}

	// Nothing may have been written down about it.
	marker := filepath.Join(s.Config.DataDir, "art", "Shougun.png.none")
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("an outage was recorded as the character having no picture")
	}

	// And when the source comes back, so does the picture.
	enka := &fakeEnka{serve: map[string][]byte{
		"/ui/UI_NameCardPic_Shougun_P.png": []byte("\x89PNG namecard"),
	}}
	enka.start(t)
	artFetches = sync.Map{}

	res := do("member", "GET", "/api/art/RaidenShogun", "")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d after the source recovered, body = %s", res.Code, res.Body.String())
	}
}

// A source that genuinely has no such image is worth remembering, or every
// page load asks again for something that will never be there.
func TestAGenuineMissIsRemembered(t *testing.T) {
	enka := &fakeEnka{serve: map[string][]byte{}}
	enka.start(t)

	s, do := artServer(t, map[string]gamedata.Character{
		"Nobody": {Key: "Nobody", Art: "Nobody"},
	})

	if res := do("member", "GET", "/api/art/Nobody", ""); res.Code != http.StatusNotFound {
		t.Fatalf("status = %d", res.Code)
	}
	marker := filepath.Join(s.Config.DataDir, "art", "Nobody.png.none")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("a real miss was not remembered: %v", err)
	}

	before := len(enka.calls())
	do("member", "GET", "/api/art/Nobody", "")
	if len(enka.calls()) != before {
		t.Error("the source was asked again for something it had already said does not exist")
	}
}

// An artifact's picture goes through the same cache, and its name arrives
// complete rather than as a suffix with prefixes to try.
func TestArtifactArtIsServedFromTheSameCache(t *testing.T) {
	enka := &fakeEnka{serve: map[string][]byte{
		"/ui/UI_RelicIcon_15020_3.png": []byte("\x89PNG circlet"),
	}}
	enka.start(t)

	s, do := artServer(t, nil)
	if err := s.GameData.Save(&gamedata.Snapshot{
		Version: "test",
		ArtifactSets: map[string]gamedata.ArtifactSet{
			"EmblemOfSeveredFate": {Key: "EmblemOfSeveredFate", Icons: map[model.Slot]string{
				model.Circlet: "UI_RelicIcon_15020_3",
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.GameData.Load(); err != nil {
		t.Fatal(err)
	}

	res := do("member", "GET", "/api/art/set/EmblemOfSeveredFate/circlet", "")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if got := enka.calls(); len(got) != 1 || got[0] != "/ui/UI_RelicIcon_15020_3.png" {
		t.Errorf("asked for %v; a complete icon name must not have prefixes tried against it", got)
	}

	// A slot the set has no picture for is a fact, not a failure.
	if res := do("member", "GET", "/api/art/set/EmblemOfSeveredFate/flower", ""); res.Code != http.StatusNotFound {
		t.Errorf("status = %d for a slot with no picture", res.Code)
	}
}

package mine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestSource returns a Source that retries without the real backoff.
func newTestSource(t *testing.T) *Source {
	t.Helper()
	s := NewSource(t.TempDir())
	s.RetryWait = time.Millisecond
	return s
}

// A sync used to die on a single blip out of several hundred requests. This is
// the exact failure seen in production: raw.githubusercontent.com answered one
// ordinary GET with a bare "400 Bad Request" and the whole mine was abandoned.
func TestGetRetriesASpurious400(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) < 3 {
			http.Error(w, "400 Bad Request", http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"name":"Kaveh"}`))
	}))
	defer srv.Close()

	raw, err := newTestSource(t).Get(context.Background(), srv.URL+"/kaveh.json")
	if err != nil {
		t.Fatalf("gave up on a transient 400: %v", err)
	}
	if string(raw) != `{"name":"Kaveh"}` {
		t.Fatalf("body = %q", raw)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

// A missing file is a real signal — the upstream layout changed — and retrying
// it would turn a breakage into a slow breakage.
func TestGetDoesNotRetryA404(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if _, err := newTestSource(t).Get(context.Background(), srv.URL+"/gone.json"); err == nil {
		t.Fatal("want an error")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("asked %d times for a 404, want 1", got)
	}
}

func TestGetGivesUpAndSaysHowOften(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := newTestSource(t).Get(context.Background(), srv.URL+"/x.json")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "after 4 attempts") {
		t.Fatalf("error does not say how hard it tried: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != fetchAttempts {
		t.Fatalf("attempts = %d, want %d", got, fetchAttempts)
	}
}

// Retrying must not outlive the caller: a cancelled sync stops promptly.
func TestGetStopsWhenTheContextIsCancelled(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := newTestSource(t)
	s.RetryWait = time.Minute // long enough that only cancellation ends it
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	done := make(chan struct{})
	go func() { s.Get(ctx, srv.URL+"/x.json"); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("kept retrying after the sync was cancelled")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

// A retried file must still land in the cache, or the next run re-fetches it.
func TestARetriedFileIsStillCached(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) < 2 {
			http.Error(w, "400 Bad Request", http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	s := newTestSource(t)
	if _, err := s.Get(context.Background(), srv.URL+"/a.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(context.Background(), srv.URL+"/a.json"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("hit upstream %d times, want 2 (one failure, one success, then cache)", got)
	}
}

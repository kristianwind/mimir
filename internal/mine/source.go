package mine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Source fetches upstream files, caching them on disk.
//
// The cache is not an optimisation. A full mine pulls a few hundred small
// files from GitHub; re-running it after a code change should not re-download
// them, both to stay well inside anonymous rate limits and so that a mine can
// be re-run offline while debugging the mapping.
type Source struct {
	CacheDir string
	HTTP     *http.Client
	// MaxAge is how long a cached file is considered fresh. Zero means
	// forever, which is what a pinned ref wants.
	MaxAge time.Duration
	// Concurrency caps parallel downloads.
	Concurrency int
	// RetryWait is the first backoff between attempts; each further attempt
	// waits three times as long. Zero means the default.
	RetryWait time.Duration
	// Log receives one line per network fetch.
	Log func(format string, args ...any)
}

// NewSource returns a source caching under dir.
func NewSource(dir string) *Source {
	return &Source{
		CacheDir:    dir,
		HTTP:        &http.Client{Timeout: 5 * time.Minute},
		Concurrency: 12,
		RetryWait:   300 * time.Millisecond,
		Log:         func(string, ...any) {},
	}
}

// fetchAttempts is how many times one file is tried before the sync gives up.
//
// A mine pulls a few hundred files and swaps nothing unless every one of them
// arrives, so the whole run is only as reliable as its unluckiest request.
// Four attempts turns a one-in-a-few-hundred blip into a one-in-a-few-billion
// one, at the cost of a few seconds on the rare occasion it is needed.
const fetchAttempts = 4

// retryable reports whether a status is worth trying again.
//
// 404 is not: the URLs are generated from a fixed pattern against a pinned
// ref, so a missing file means the upstream layout changed, and quietly
// retrying that would turn a real breakage into a slow one. 401 and 403 are
// not either — credentials do not improve by asking again.
//
// 400 is, which is the unusual entry. It normally means the request was
// malformed and will never succeed. But raw.githubusercontent.com is fronted
// by a CDN that intermittently answers a perfectly ordinary GET with a bare
// "400 Bad Request" body, and that is what was seen in production: a sync
// died on characters/kaveh.json while the same URL returned 200 from the
// host, from inside the container, and 366 times in a row from the real
// client. A URL this code built itself is not malformed; if the far end says
// it is, the far end is having a moment.
func retryable(status int) bool {
	switch status {
	case http.StatusNotFound, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusGone:
		return false
	}
	return true
}

// Get fetches a URL, returning its body.
//
// Transient failures are retried, because the alternative is what used to
// happen: one unlucky request out of several hundred abandoned a thirty-second
// sync and showed the admin an error naming a URL that works perfectly when
// they click it.
func (s *Source) Get(ctx context.Context, url string) ([]byte, error) {
	path := s.cachePath(url)
	if raw, ok := s.fromCache(path); ok {
		return raw, nil
	}

	wait := s.RetryWait
	if wait <= 0 {
		wait = 300 * time.Millisecond
	}

	var lastErr error
	for attempt := 1; attempt <= fetchAttempts; attempt++ {
		if attempt > 1 {
			// Jittered, because twelve downloads run in parallel and a
			// failure upstream tends to hit all of them at once. Retrying in
			// lockstep would rebuild the very burst that was refused.
			d := wait/2 + time.Duration(rand.Int63n(int64(wait)))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(d):
			}
			wait *= 3
			s.Log("retry %d/%d %s", attempt, fetchAttempts, url)
		}

		raw, again, err := s.getOnce(ctx, url)
		if err == nil {
			if err := s.store(path, raw); err != nil {
				return nil, err
			}
			return raw, nil
		}
		lastErr = err
		if !again || ctx.Err() != nil {
			break
		}
	}
	return nil, fmt.Errorf("after %d attempts: %w", fetchAttempts, lastErr)
}

// getOnce makes one request. It reports whether the failure is worth another
// attempt, so Get can tell a blip from a breakage.
func (s *Source) getOnce(ctx context.Context, url string) (raw []byte, again bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", "mimir-mine/0.1 (+https://github.com/kristianwind/mimir)")
	req.Header.Set("Accept", "application/json, */*")

	s.Log("fetch %s", url)
	resp, err := s.HTTP.Do(req)
	if err != nil {
		// A refused connection, a reset, a timeout: all worth another go,
		// unless the caller has given up.
		return nil, ctx.Err() == nil, fmt.Errorf("mine: get %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, retryable(resp.StatusCode),
			fmt.Errorf("mine: get %s: status %d: %s", url, resp.StatusCode, body)
	}
	raw, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, ctx.Err() == nil, fmt.Errorf("mine: read %s: %w", url, err)
	}
	return raw, false, nil
}

// GetJSON fetches a URL and decodes it into v.
func (s *Source) GetJSON(ctx context.Context, url string, v any) error {
	raw, err := s.Get(ctx, url)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("mine: decode %s: %w", url, err)
	}
	return nil
}

// GetManyJSON fetches many URLs concurrently, calling decode for each body.
// decode is invoked under a lock, so it may write to shared state.
func (s *Source) GetManyJSON(ctx context.Context, urls []string, decode func(url string, raw []byte) error) error {
	conc := s.Concurrency
	if conc < 1 {
		conc = 1
	}
	sem := make(chan struct{}, conc)

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		firstEr error
	)
	fail := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if firstEr == nil {
			firstEr = err
		}
	}

	for _, url := range urls {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			mu.Lock()
			stop := firstEr != nil
			mu.Unlock()
			if stop || ctx.Err() != nil {
				return
			}

			raw, err := s.Get(ctx, url)
			if err != nil {
				fail(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if err := decode(url, raw); err != nil {
				if firstEr == nil {
					firstEr = err
				}
			}
		}(url)
	}
	wg.Wait()
	return firstEr
}

func (s *Source) cachePath(url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(s.CacheDir, hex.EncodeToString(sum[:])+".cache")
}

func (s *Source) fromCache(path string) ([]byte, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if s.MaxAge > 0 && time.Since(info.ModTime()) > s.MaxAge {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return raw, true
}

func (s *Source) store(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	// Write-then-rename: an interrupted mine must not leave a truncated file
	// that the next run happily treats as a cache hit.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

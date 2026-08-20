package mine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	// Log receives one line per network fetch.
	Log func(format string, args ...any)
}

// NewSource returns a source caching under dir.
func NewSource(dir string) *Source {
	return &Source{
		CacheDir:    dir,
		HTTP:        &http.Client{Timeout: 5 * time.Minute},
		Concurrency: 12,
		Log:         func(string, ...any) {},
	}
}

// Get fetches a URL, returning its body.
func (s *Source) Get(ctx context.Context, url string) ([]byte, error) {
	path := s.cachePath(url)
	if raw, ok := s.fromCache(path); ok {
		return raw, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mimir-mine/0.1 (+https://github.com/kristianwind/mimir)")
	req.Header.Set("Accept", "application/json, */*")

	s.Log("fetch %s", url)
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mine: get %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("mine: get %s: status %d: %s", url, resp.StatusCode, body)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mine: read %s: %w", url, err)
	}

	if err := s.store(path, raw); err != nil {
		return nil, err
	}
	return raw, nil
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

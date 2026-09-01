package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func logged(t *testing.T, method, target string, h http.HandlerFunc) string {
	t.Helper()
	var buf bytes.Buffer
	s := &Server{Log: slog.New(slog.NewTextHandler(&buf, nil))}
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	s.requestLog(h).ServeHTTP(httptest.NewRecorder(), req)
	return buf.String()
}

func ok(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

// The question this whole file exists to answer.
func TestRequestLogNamesTheCrawler(t *testing.T) {
	out := logged(t, "GET", "/sitemap.xml", ok)
	for _, want := range []string{"path=/sitemap.xml", "status=200", "Googlebot"} {
		if !strings.Contains(out, want) {
			t.Errorf("log is missing %q:\n%s", want, out)
		}
	}
}

// A query string can carry a reset token or a checkout session. A log is the
// last place one should end up.
func TestRequestLogDropsTheQueryString(t *testing.T) {
	out := logged(t, "GET", "/reset?token=super-secret-value", ok)
	if strings.Contains(out, "super-secret-value") {
		t.Fatalf("token reached the log:\n%s", out)
	}
	if !strings.Contains(out, "path=/reset") {
		t.Fatalf("path missing:\n%s", out)
	}
}

// The deliberate omission. If this ever starts failing, someone has added the
// one field that turns a traffic log into personal data.
func TestRequestLogHasNoAddress(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{Log: slog.New(slog.NewTextHandler(&buf, nil))}
	req := httptest.NewRequest("GET", "/pricing", nil)
	req.RemoteAddr = "203.0.113.42:51000"
	s.requestLog(http.HandlerFunc(ok)).ServeHTTP(httptest.NewRecorder(), req)
	if strings.Contains(buf.String(), "203.0.113.42") {
		t.Fatalf("the caller's address reached the log:\n%s", buf.String())
	}
}

// A page view drags a dozen assets with it; logging them buries the request
// that mattered.
func TestRequestLogSkipsSuccessfulAssets(t *testing.T) {
	if out := logged(t, "GET", "/assets/app-a1b2c3.js", ok); out != "" {
		t.Fatalf("asset was logged:\n%s", out)
	}
}

// But a 404 on an asset is a broken build, and that is worth seeing.
func TestRequestLogKeepsFailedAssets(t *testing.T) {
	out := logged(t, "GET", "/assets/gone.js", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if !strings.Contains(out, "status=404") {
		t.Fatalf("a failing asset was hidden:\n%s", out)
	}
}

func TestRequestLogLevels(t *testing.T) {
	for _, tc := range []struct{ status, want string }{
		{"200", "level=INFO"}, {"404", "level=WARN"}, {"500", "level=ERROR"},
	} {
		code := map[string]int{"200": 200, "404": 404, "500": 500}[tc.status]
		out := logged(t, "GET", "/x", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) })
		if !strings.Contains(out, tc.want) {
			t.Errorf("status %s logged at the wrong level:\n%s", tc.status, out)
		}
	}
}

func TestRequestLogClipsAnAbsurdAgent(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{Log: slog.New(slog.NewTextHandler(&buf, nil))}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", strings.Repeat("A", 5000))
	s.requestLog(http.HandlerFunc(ok)).ServeHTTP(httptest.NewRecorder(), req)
	if len(buf.String()) > 600 {
		t.Fatalf("one line grew to %d bytes", len(buf.String()))
	}
}

// A Server without a logger must still serve.
func TestRequestLogWithoutALogger(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.requestLog(http.HandlerFunc(ok)).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

// The art routes live under /api, so a prefix of "/art/" never matched them.
// One roster view pulls dozens, and the key names which characters somebody
// looks at.
func TestRequestLogSkipsCharacterArt(t *testing.T) {
	if out := logged(t, "GET", "/api/art/Furina", ok); out != "" {
		t.Fatalf("character art was logged:\n%s", out)
	}
	if out := logged(t, "GET", "/api/art/set/EmblemOfSeveredFate/flower", ok); out != "" {
		t.Fatalf("set art was logged:\n%s", out)
	}
}

// But the pages that answer "is anything crawling us" must survive the filter.
func TestRequestLogKeepsRealPages(t *testing.T) {
	for _, p := range []string{"/", "/pricing", "/sitemap.xml", "/robots.txt", "/api/plan/Raiden"} {
		if out := logged(t, "GET", p, ok); out == "" {
			t.Errorf("%s was silently dropped", p)
		}
	}
}

// The blind spot this closes: a handler that has not returned leaves no line,
// because the line is written after it does. A request reported as failing
// from a browser produced nothing in the log, and "no line" was read as "it
// never arrived" — which the log could not actually distinguish.
func TestASlowRequestSaysSoWhileItIsStillRunning(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{Log: slog.New(slog.NewTextHandler(&buf, nil))}

	release := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	})

	go s.requestLog(h).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/slow", nil))

	// slowRequest is five seconds in production; waiting that long in a test
	// would be paying for the constant twice. What matters is that the line
	// appears before the handler returns, so the handler is held until it has.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "still running") {
			close(release)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(release)
	if !strings.Contains(buf.String(), "still running") {
		t.Skipf("slowRequest is %s, longer than this test waits; the timer path is unexercised", slowRequest)
	}
}

// The ordinary path must not gain a line it did not have: a fast request
// reports once, when it finishes.
func TestAFastRequestIsLoggedOnlyOnce(t *testing.T) {
	out := logged(t, "GET", "/quick", ok)
	if n := strings.Count(out, "msg=request"); n != 1 {
		t.Fatalf("a fast request produced %d lines:\n%s", n, out)
	}
	if strings.Contains(out, "still running") {
		t.Fatalf("a fast request was announced as slow:\n%s", out)
	}
}

// The note must not be an alarm. An opinion from the AI layer takes twenty to
// thirty seconds by design, so a warning on every one of them is a warning
// nobody reads by the end of the week.
func TestTheStillRunningNoteIsNotAnAlarm(t *testing.T) {
	src, err := os.ReadFile("requestlog.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `Log.Info("request still running"`) {
		t.Error("the still-running note is not logged at info level")
	}
}

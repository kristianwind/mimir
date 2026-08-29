package api

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kristianwind/mimir/internal/billing"
	"github.com/kristianwind/mimir/internal/config"
	"github.com/kristianwind/mimir/internal/db"
)

// The one that must never break: a Mimir somebody runs at home has nothing to
// sell and refuses nothing. Decide answers self-hosted before it looks at any
// subscription, so the middleware is inert there rather than a switch that
// could be flipped by a stray environment variable.
func TestASelfHostedInstanceRefusesNothing(t *testing.T) {
	for _, rec := range []billing.Record{
		{},                     // never subscribed
		{Status: "canceled"},   // stopped paying
		{Status: "incomplete"}, // never started
	} {
		created := time.Now().Add(-5 * 365 * 24 * time.Hour) // years past any trial
		got := billing.Decide(false, rec, created, time.Now())
		if !got.Allowed {
			t.Errorf("a self-hosted instance refused %+v: %+v", rec, got)
		}
		if got.Reason != billing.ReasonSelfHosted {
			t.Errorf("reason = %q, want self-hosted", got.Reason)
		}
	}
}

// And on the hosted one, an expired trial is refused with a status that says
// "pay and this resumes" rather than "you may never have this".
func TestAnEndedTrialIsRefusedWithPaymentRequired(t *testing.T) {
	created := time.Now().Add(-time.Duration(billing.TrialDays+1) * 24 * time.Hour)
	access := billing.Decide(true, billing.Record{}, created, time.Now())
	if access.Allowed {
		t.Fatal("an expired trial was allowed on the hosted instance")
	}

	w := httptest.NewRecorder()
	writeError(w, http.StatusPaymentRequired, paywallMessage(access.Reason),
		"Your account, your imported data and your goals are all still here, "+
			"and subscribing turns the rest back on.")

	if w.Code != http.StatusPaymentRequired {
		t.Errorf("status = %d, want 402", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "trial has ended") {
		t.Errorf("the reply does not say why: %s", body)
	}
	// It must say the data is safe. Somebody reading this is deciding whether
	// to pay, and "your inventory is gone" would be both untrue and the worst
	// possible thing to imply at that moment.
	if !strings.Contains(body, "still here") {
		t.Errorf("the reply does not say the data is still there: %s", body)
	}
}

// A database fault is not evidence of not paying. Refusing on an error would
// turn an outage into a billing accusation.
func TestADatabaseFaultIsNotTreatedAsNonPayment(t *testing.T) {
	conn, err := db.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	// Closed, so every query answers with an error rather than a verdict.
	conn.Close()

	s := &Server{
		Config:  &config.Config{Hosted: true},
		DB:      conn,
		Billing: &billing.Store{DB: conn},
		Log:     slog.New(slog.DiscardHandler),
	}

	called := false
	h := s.requirePaid(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/accounts/1/plan", nil))

	if called {
		t.Error("the handler ran despite the entitlement check failing")
	}
	if w.Code == http.StatusPaymentRequired {
		t.Error("a fault was reported to the user as a payment problem")
	}
}

package api

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/billing"
)

// Stripe's errors are written for whoever wired the integration up: request
// ids, dashboard links, the internal id of a price. Somebody trying to pay
// should see none of it — it tells them nothing they can act on, and it reads
// like the service is broken in a way that makes handing over a card feel
// unwise.
func TestACustomerNeverSeesStripesInternals(t *testing.T) {
	s := &Server{Log: slog.New(slog.DiscardHandler)}
	raw := errors.New(`billing: create checkout session: {"code":"resource_missing",` +
		`"doc_url":"https://stripe.com/docs/error-codes/resource-missing","status":400,` +
		`"message":"No such price: 'price_1U9NyTJ1lNX36YXYQkuDjTfO'; a similar object exists ` +
		`in test mode, but a live mode key was used","request_id":"req_RApDKIU9Raj2TP"}`)

	w := httptest.NewRecorder()
	s.writeStripeError(w, httptest.NewRequest("POST", "/x", nil), "create checkout session", raw)

	body := w.Body.String()
	for _, leak := range []string{"price_1U9NyTJ", "req_RApDKIU9Raj2TP", "resource_missing",
		"dashboard.stripe.com", "live mode key"} {
		if strings.Contains(body, leak) {
			t.Errorf("the reply leaks %q to the customer: %s", leak, body)
		}
	}
	// And it says the one thing they actually need to know.
	if !strings.Contains(body, "Nothing was charged") {
		t.Errorf("the reply does not say nothing was charged: %s", body)
	}
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

// An instance that sells nothing says so plainly rather than blaming Stripe
// for being unreachable.
func TestAnUnconfiguredInstanceSaysSoRatherThanBlamingStripe(t *testing.T) {
	s := &Server{Log: slog.New(slog.DiscardHandler)}
	w := httptest.NewRecorder()
	s.writeStripeError(w, httptest.NewRequest("POST", "/x", nil), "checkout", billing.ErrNotConfigured)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if strings.Contains(w.Body.String(), "could not be reached") {
		t.Errorf("an unconfigured instance blamed the network: %s", w.Body.String())
	}
}

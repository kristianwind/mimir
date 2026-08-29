package api

// Subscriptions.
//
// The shape of this file follows one rule: nothing about what a customer is
// entitled to is ever decided from something the customer's browser said.
//
// Stripe's redirect back to the site after checkout is a URL, and a URL is
// something anybody can be talked into visiting. So the redirect changes
// nothing here — it only takes the reader back to a page, which then asks the
// server what is true. Entitlement moves exclusively on the webhook, whose
// signature has been checked.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/kristianwind/mimir/internal/auth"
	"github.com/kristianwind/mimir/internal/billing"
)

// maxWebhookBody caps what will be read from an unauthenticated endpoint.
// Stripe events are kilobytes; anything approaching this is not one.
const maxWebhookBody = 256 << 10

// handleBillingStatus reports what this user is entitled to and why.
func (s *Server) handleBillingStatus(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	access, rec, err := s.access(r.Context(), u.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access": access,
		// Whether this instance can take money at all. A self-hosted one
		// cannot, and its interface must not offer to.
		"sellable": s.Config.Hosted && s.Stripe.Configured(),
		// Present only once they have a Stripe customer, because the portal
		// is meaningless without one.
		"hasBilling":     rec.StripeCustomerID != "",
		"publishableKey": s.Config.StripePublishableKey,
		"trialDays":      billing.TrialDays,
	})
}

// handleCheckout sends a customer to Stripe to pay.
func (s *Server) handleCheckout(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	if !s.Config.Hosted || !s.Stripe.Configured() {
		writeError(w, http.StatusNotFound, "this instance does not sell subscriptions", "")
		return
	}
	var body struct {
		Plan billing.Plan `json:"plan"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request", "")
		return
	}

	rec, _, err := s.Billing.Get(r.Context(), u.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	// One Stripe customer per user, forever. Making a second one would split
	// somebody's payment history in half and leave the webhook updating a
	// row nobody is reading.
	customerID, err := s.Stripe.Customer(r.Context(), rec.StripeCustomerID, u.ID, u.Username, s.emailOf(r, u.ID))
	if err != nil {
		s.writeStripeError(w, r, "create customer", err)
		return
	}
	if customerID != rec.StripeCustomerID {
		if err := s.Billing.SetCustomer(r.Context(), u.ID, customerID); err != nil {
			writeDomainError(w, err)
			return
		}
	}

	url, err := s.Stripe.Checkout(r.Context(), customerID, body.Plan)
	if errors.Is(err, billing.ErrCustomerGone) {
		// The recorded customer is from another mode, or was deleted in the
		// dashboard. Make a fresh one and go again — once. A stale id is a
		// thing to recover from, not a dead end to show somebody who is
		// trying to pay.
		s.Log.Warn("stored Stripe customer is gone; making a new one",
			"user", u.Username, "was", customerID)
		customerID, err = s.Stripe.Customer(r.Context(), "", u.ID, u.Username, s.emailOf(r, u.ID))
		if err == nil {
			err = s.Billing.SetCustomer(r.Context(), u.ID, customerID)
		}
		if err == nil {
			url, err = s.Stripe.Checkout(r.Context(), customerID, body.Plan)
		}
	}
	if err != nil {
		s.writeStripeError(w, r, "create checkout session", err)
		return
	}
	s.audit(r, "billing.checkout", u.Username, map[string]any{"plan": body.Plan})
	writeJSON(w, http.StatusOK, map[string]any{"url": url})
}

// handlePortal opens Stripe's own billing page, where cancelling, changing
// plan and replacing a card all happen.
func (s *Server) handlePortal(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	rec, _, err := s.Billing.Get(r.Context(), u.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if rec.StripeCustomerID == "" {
		writeError(w, http.StatusBadRequest, "there is nothing to manage yet", "")
		return
	}
	url, err := s.Stripe.Portal(r.Context(), rec.StripeCustomerID)
	if err != nil {
		s.writeStripeError(w, r, "create portal session", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url})
}

// handleStripeWebhook is where entitlement actually changes.
//
// Unauthenticated by necessity — Stripe has no session — so the signature is
// the whole of the security. Everything below it runs only after the payload
// has been proved to come from Stripe.
func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "unreadable body", "")
		return
	}
	ev, err := s.Stripe.Verify(payload, r.Header.Get("Stripe-Signature"))
	if err != nil {
		// Logged as a warning rather than an error: an unsigned POST to a
		// public endpoint is the internet doing what it does, and drowning
		// the log in it helps nobody.
		s.Log.Warn("rejected a Stripe webhook", "error", err)
		writeError(w, http.StatusBadRequest, "signature", "")
		return
	}
	if !ev.Actionable {
		// Acknowledged. Refusing an event this service does not care about
		// makes Stripe retry it for days.
		w.WriteHeader(http.StatusOK)
		return
	}

	err = s.Billing.SetSubscription(r.Context(), ev.CustomerID, ev.SubscriptionID,
		ev.Status, ev.CurrentPeriodEnd, ev.CancelAtPeriodEnd)
	switch {
	case errors.Is(err, billing.ErrUnknownCustomer):
		// A customer this instance has never seen — most often the test and
		// live dashboards pointing at the same endpoint. Accepted so Stripe
		// stops retrying, and recorded so it is not invisible.
		s.Log.Warn("Stripe event for an unknown customer",
			"customer", ev.CustomerID, "type", ev.Type)
	case err != nil:
		// A real failure. Refused, so Stripe retries and the change is not
		// silently lost.
		s.Log.Error("could not record a subscription change", "error", err, "type", ev.Type)
		writeError(w, http.StatusInternalServerError, "could not record", "")
		return
	default:
		s.Log.Info("subscription changed", "type", ev.Type, "status", ev.Status)
	}
	w.WriteHeader(http.StatusOK)
}

// handleComp grants or withdraws free access. Administrators only.
//
// Comping is deliberately not a role. Somebody given free access is a user of
// the product, not an operator of the machine, and the two must never be the
// same switch.
func (s *Server) handleComp(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body struct {
		UserID int64  `json:"userId"`
		Comped bool   `json:"comped"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request", "")
		return
	}
	if err := s.Billing.SetComped(r.Context(), body.UserID, body.Comped, body.Note); err != nil {
		writeDomainError(w, err)
		return
	}
	me, _ := auth.FromContext(r.Context())
	s.audit(r, "billing.comp", me.Username, map[string]any{
		"userId": body.UserID, "comped": body.Comped, "note": body.Note,
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// writeStripeError turns a failure from Stripe into something a customer can
// read, and puts the detail in the log where it belongs.
//
// Stripe's errors are written for whoever wired the integration up: they
// carry request ids, dashboard links and the internal id of a price. A person
// trying to pay for something should never see any of that — it tells them
// nothing they can act on, and it reads like the service is broken in a way
// that makes handing over a card feel unwise.
//
// The operator still gets the whole thing, because the operator is the one
// who can fix it.
func (s *Server) writeStripeError(w http.ResponseWriter, r *http.Request, what string, err error) {
	if errors.Is(err, billing.ErrNotConfigured) {
		writeError(w, http.StatusNotFound, "this instance does not sell subscriptions", "")
		return
	}
	s.Log.Error("stripe", "doing", what, "error", err)
	writeError(w, http.StatusBadGateway,
		"the payment provider could not be reached just now",
		"Nothing was charged. Try again in a moment; if it keeps happening the operator has been told.")
}

// access is the one place the entitlement question is asked.
func (s *Server) access(ctx context.Context, userID int64) (billing.Access, billing.Record, error) {
	rec, made, err := s.Billing.Get(ctx, userID)
	if err != nil {
		return billing.Access{}, billing.Record{}, err
	}
	return billing.Decide(s.Config.Hosted, rec, made, time.Now()), rec, nil
}

// emailOf reads a user's email, which Stripe wants for receipts. Absent is
// fine; Stripe simply has nowhere to send one.
func (s *Server) emailOf(r *http.Request, userID int64) string {
	var email *string
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT email FROM users WHERE id = ?`, userID).Scan(&email); err != nil || email == nil {
		return ""
	}
	return *email
}

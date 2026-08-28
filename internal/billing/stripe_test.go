package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"
)

// sign builds the Stripe-Signature header the way Stripe does, so these tests
// exercise the real verification path without a network or an account. The
// signed material is "timestamp.payload"; getting that wrong is the classic
// way a hand-rolled verifier ends up accepting anything.
func sign(t *testing.T, payload, secret string, at time.Time) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.%s", at.Unix(), payload)
	return fmt.Sprintf("t=%d,v1=%s", at.Unix(), hex.EncodeToString(mac.Sum(nil)))
}

func subscriptionEvent(kind, customer, status string, periodEnd time.Time, cancelAtEnd bool) string {
	return fmt.Sprintf(`{
	  "id": "evt_test",
	  "api_version": %q,
	  "type": %q,
	  "data": {"object": {
	     "id": "sub_test",
	     "object": "subscription",
	     "customer": %q,
	     "status": %q,
	     "cancel_at_period_end": %t,
	     "items": {"object": "list", "data": [
	        {"id": "si_test", "object": "subscription_item", "current_period_end": %d}
	     ]}
	  }}
	}`, APIVersion(), kind, customer, status, cancelAtEnd, periodEnd.Unix())
}

// The one that matters. Anybody on the internet can POST to a webhook
// endpoint; the signature is the only thing stopping them granting themselves
// a subscription.
func TestAnUnsignedEventIsRefused(t *testing.T) {
	s := &Stripe{WebhookSecret: "whsec_test"}
	body := subscriptionEvent("customer.subscription.updated", "cus_1", "active", time.Now(), false)

	for name, sig := range map[string]string{
		"no signature at all":        "",
		"nonsense":                   "t=1,v1=deadbeef",
		"signed with another secret": sign(t, body, "whsec_someone_else", time.Now()),
	} {
		if _, err := s.Verify([]byte(body), sig); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// A payload that was signed correctly and then edited must not verify — that
// is the whole point of signing the body rather than a header.
func TestATamperedPayloadIsRefused(t *testing.T) {
	const secret = "whsec_test"
	s := &Stripe{WebhookSecret: secret}
	body := subscriptionEvent("customer.subscription.updated", "cus_1", "canceled", time.Now(), false)
	sig := sign(t, body, secret, time.Now())

	// Somebody upgrading themselves from canceled to active.
	tampered := strings.Replace(body, `"canceled"`, `"active"`, 1)
	if _, err := s.Verify([]byte(tampered), sig); err == nil {
		t.Fatal("an edited payload verified against the original signature")
	}
}

// An old signature must not work forever. Stripe includes the timestamp in
// the signed material and its library enforces a tolerance; without that, one
// captured request is a replay attack with no expiry.
func TestAnAncientSignatureIsRefused(t *testing.T) {
	const secret = "whsec_test"
	s := &Stripe{WebhookSecret: secret}
	body := subscriptionEvent("customer.subscription.updated", "cus_1", "active", time.Now(), false)
	old := time.Now().Add(-24 * time.Hour)

	if _, err := s.Verify([]byte(body), sign(t, body, secret, old)); err == nil {
		t.Fatal("a signature from yesterday was accepted")
	}
}

// Refusing to verify at all is safer than verifying against nothing. An
// instance with no webhook secret configured must not treat every event as
// genuine.
func TestNoSecretMeansNoTrust(t *testing.T) {
	s := &Stripe{}
	body := subscriptionEvent("customer.subscription.updated", "cus_1", "active", time.Now(), false)
	if _, err := s.Verify([]byte(body), sign(t, body, "", time.Now())); err == nil {
		t.Fatal("an event was trusted on an instance with no webhook secret")
	}
}

// The happy path, and the fields the rest of the service acts on.
func TestAGenuineEventIsReadCorrectly(t *testing.T) {
	const secret = "whsec_test"
	s := &Stripe{WebhookSecret: secret}
	ends := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	body := subscriptionEvent("customer.subscription.updated", "cus_42", "active", ends, true)

	got, err := s.Verify([]byte(body), sign(t, body, secret, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Actionable {
		t.Fatal("a subscription event was not treated as actionable")
	}
	if got.CustomerID != "cus_42" {
		t.Errorf("customer = %q", got.CustomerID)
	}
	if got.Status != "active" {
		t.Errorf("status = %q", got.Status)
	}
	if !got.CancelAtPeriodEnd {
		t.Error("cancel_at_period_end was lost")
	}
	// Stripe moved this onto the items; reading only the old top-level field
	// would silently give the zero time and expire everybody immediately.
	if !got.CurrentPeriodEnd.Equal(ends.UTC()) {
		t.Errorf("period end = %v, want %v", got.CurrentPeriodEnd, ends.UTC())
	}
}

// A deletion is the end of it whatever the object says, because Stripe sends
// the subscription as it was at the moment it went away.
func TestADeletionIsAlwaysCancelled(t *testing.T) {
	const secret = "whsec_test"
	s := &Stripe{WebhookSecret: secret}
	body := subscriptionEvent("customer.subscription.deleted", "cus_1", "active", time.Now(), false)

	got, err := s.Verify([]byte(body), sign(t, body, secret, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "canceled" {
		t.Errorf("a deleted subscription reported status %q", got.Status)
	}
}

// Stripe sends dozens of event types. The ones this service does not act on
// have to verify and then be ignored — rejecting them makes Stripe retry
// them forever.
func TestUnrelatedEventsAreAcknowledgedAndIgnored(t *testing.T) {
	const secret = "whsec_test"
	s := &Stripe{WebhookSecret: secret}
	body := fmt.Sprintf(
		`{"id":"evt_x","api_version":%q,"type":"payment_intent.succeeded","data":{"object":{"id":"pi_x"}}}`,
		APIVersion())

	got, err := s.Verify([]byte(body), sign(t, body, secret, time.Now()))
	if err != nil {
		t.Fatalf("an unrelated event failed verification: %v", err)
	}
	if got.Actionable {
		t.Error("an unrelated event was treated as actionable")
	}
}

// An instance that sells nothing must say so rather than half-working.
func TestAnUnconfiguredInstanceRefusesToTakeMoney(t *testing.T) {
	for _, s := range []*Stripe{
		{},
		{SecretKey: "sk_test"},
		{SecretKey: "sk_test", PriceMonthly: "price_m"},
	} {
		if s.Configured() {
			t.Errorf("%+v reported itself configured", s)
		}
		if _, err := s.Checkout(t.Context(), "cus_1", Monthly); err != ErrNotConfigured {
			t.Errorf("Checkout gave %v, want ErrNotConfigured", err)
		}
		if _, err := s.Portal(t.Context(), "cus_1"); err != ErrNotConfigured {
			t.Errorf("Portal gave %v, want ErrNotConfigured", err)
		}
	}
}

// An endpoint configured on a different Stripe API version sends objects of a
// different shape, and this very version moved current_period_end off the
// subscription onto its items. Waving that through would not fail loudly; it
// would read as "no subscription" and lock a paying customer out. So it is
// refused, and the refusal has to name the fix.
func TestAVersionMismatchIsRefusedAndSaysWhy(t *testing.T) {
	const secret = "whsec_test"
	s := &Stripe{WebhookSecret: secret}
	body := `{"id":"evt_x","api_version":"2019-01-01","type":"customer.subscription.updated",` +
		`"data":{"object":{"id":"sub_x","object":"subscription","customer":"cus_x","status":"active"}}}`

	_, err := s.Verify([]byte(body), sign(t, body, secret, time.Now()))
	if err == nil {
		t.Fatal("an event from another API version was accepted")
	}
	if !strings.Contains(err.Error(), APIVersion()) {
		t.Errorf("the error does not name the version this build reads: %v", err)
	}
	if !strings.Contains(err.Error(), "dashboard") {
		t.Errorf("the error does not say where to fix it: %v", err)
	}
}

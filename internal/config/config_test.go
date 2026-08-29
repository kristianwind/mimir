package config

import (
	"strings"
	"testing"
)

// Five Stripe credentials sit in five similar-looking boxes on a settings
// form. Kristian put the webhook secret in the secret-key box, and the only
// sign was Stripe answering "Invalid API Key provided: whsec_Ma…" to a
// customer trying to pay — naming neither the field nor the product.
func TestAStripeValueInTheWrongBoxIsNamed(t *testing.T) {
	c := &Config{
		StripeSecretKey:      "whsec_MaSomethingSomethingHpA8",
		StripeWebhookSecret:  "sk_live_theOtherWayRound",
		StripePublishableKey: "pk_live_fine",
		StripePriceMonthly:   "price_fine",
		StripePriceYearly:    "price_fine",
	}
	got := c.checkStripe()
	if len(got) != 2 {
		t.Fatalf("complaints = %d, want 2:\n%v", len(got), got)
	}

	joined := strings.Join(got, "\n")
	for _, want := range []string{"MIMIR_STRIPE_SECRET_KEY", "MIMIR_STRIPE_WEBHOOK_SECRET"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the complaint does not name %s:\n%s", want, joined)
		}
	}
	// The secret itself must never reach a log line. The prefix is enough to
	// say which box it belongs in.
	if strings.Contains(joined, "MaSomethingSomethingHpA8") ||
		strings.Contains(joined, "theOtherWayRound") {
		t.Errorf("a credential was quoted back into the complaint:\n%s", joined)
	}
}

// And a correct configuration says nothing at all, or the warning becomes
// noise nobody reads.
func TestAGoodStripeConfigurationIsSilent(t *testing.T) {
	c := &Config{
		StripeSecretKey:      "sk_live_x",
		StripeWebhookSecret:  "whsec_x",
		StripePublishableKey: "pk_live_x",
		StripePriceMonthly:   "price_x",
		StripePriceYearly:    "price_x",
	}
	if got := c.checkStripe(); len(got) != 0 {
		t.Errorf("complained about a good configuration: %v", got)
	}
	// So does an instance that sells nothing.
	if got := (&Config{}).checkStripe(); len(got) != 0 {
		t.Errorf("complained about an unconfigured instance: %v", got)
	}
}

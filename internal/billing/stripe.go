package billing

// Talking to Stripe.
//
// Deliberately small. Four operations — start a checkout, open the customer's
// own billing page, read a subscription, and verify an incoming event — and
// nothing else, because every additional call is another place where this
// service's idea of the truth can drift from Stripe's.
//
// The direction of trust runs one way. Stripe is the source of truth for
// whether somebody is paying, and this instance never decides that for
// itself; it records what it is told, by a webhook whose signature it has
// checked. Nothing about entitlement is computed from a redirect back to the
// site, because a redirect is a URL the customer's own browser can be talked
// into visiting.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v82"
	billingportal "github.com/stripe/stripe-go/v82/billingportal/session"
	checkout "github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/customer"
	"github.com/stripe/stripe-go/v82/webhook"
)

// ErrNotConfigured is returned when this instance sells nothing.
var ErrNotConfigured = errors.New("billing: Stripe is not configured on this instance")

// Stripe is the client.
type Stripe struct {
	SecretKey     string
	WebhookSecret string
	PriceMonthly  string
	PriceYearly   string
	// BaseURL is where Stripe sends the customer back to.
	BaseURL string
}

// Configured reports whether this instance can take money at all.
func (s *Stripe) Configured() bool {
	return s != nil && s.SecretKey != "" && s.PriceMonthly != "" && s.PriceYearly != ""
}

func (s *Stripe) key() string { return s.SecretKey }

// Plan is which of the two prices a customer chose.
type Plan string

const (
	Monthly Plan = "monthly"
	Yearly  Plan = "yearly"
)

func (s *Stripe) price(plan Plan) (string, error) {
	switch plan {
	case Monthly:
		return s.PriceMonthly, nil
	case Yearly:
		return s.PriceYearly, nil
	}
	return "", fmt.Errorf("billing: unknown plan %q", plan)
}

// Customer returns the Stripe customer for a user, making one if needed.
//
// The user's id is written into the customer's metadata as well as being
// stored here, so that an operator staring at the Stripe dashboard can tell
// who a customer is without a second window open.
func (s *Stripe) Customer(ctx context.Context, existing string, userID int64, username, email string) (string, error) {
	if !s.Configured() {
		return "", ErrNotConfigured
	}
	if existing != "" {
		return existing, nil
	}
	params := &stripe.CustomerParams{
		Name: stripe.String(username),
	}
	if email != "" {
		params.Email = stripe.String(email)
	}
	params.AddMetadata("mimir_user_id", fmt.Sprint(userID))
	params.Context = ctx

	stripe.Key = s.key()
	c, err := customer.New(params)
	if err != nil {
		return "", fmt.Errorf("billing: create customer: %w", err)
	}
	return c.ID, nil
}

// Checkout starts a hosted checkout and returns the URL to send the customer
// to.
//
// No trial is attached. The trial happens before Stripe is involved at all —
// it asks for no card, so there is nothing for Stripe to hold — and somebody
// arriving here has decided to pay. Adding trial days on top would give
// fourteen more free days to the one person who already said yes.
func (s *Stripe) Checkout(ctx context.Context, customerID string, plan Plan) (string, error) {
	if !s.Configured() {
		return "", ErrNotConfigured
	}
	price, err := s.price(plan)
	if err != nil {
		return "", err
	}
	stripe.Key = s.key()
	params := &stripe.CheckoutSessionParams{
		Mode:     stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		Customer: stripe.String(customerID),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(price), Quantity: stripe.Int64(1)},
		},
		SuccessURL: stripe.String(s.BaseURL + "/settings?checkout=done"),
		CancelURL:  stripe.String(s.BaseURL + "/settings?checkout=cancelled"),
	}
	params.Context = ctx
	sess, err := checkout.New(params)
	if err != nil {
		return "", fmt.Errorf("billing: create checkout session: %w", err)
	}
	return sess.URL, nil
}

// Portal opens Stripe's own billing page for a customer.
//
// Cancelling, changing plan and replacing a card all happen there rather than
// here. That is not laziness: those flows involve proration, tax and card
// authentication, and reimplementing them would mean reimplementing the parts
// of Stripe most likely to take somebody's money wrongly.
func (s *Stripe) Portal(ctx context.Context, customerID string) (string, error) {
	if !s.Configured() {
		return "", ErrNotConfigured
	}
	stripe.Key = s.key()
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(s.BaseURL + "/settings"),
	}
	params.Context = ctx
	sess, err := billingportal.New(params)
	if err != nil {
		return "", fmt.Errorf("billing: create portal session: %w", err)
	}
	return sess.URL, nil
}

// Event is the part of a Stripe webhook this service acts on.
type Event struct {
	// Type is Stripe's event name, kept for logging.
	Type string
	// CustomerID identifies whose subscription changed.
	CustomerID        string
	SubscriptionID    string
	Status            string
	CurrentPeriodEnd  time.Time
	CancelAtPeriodEnd bool
	// Actionable is false for the many events that are none of this
	// service's business. They are acknowledged rather than rejected, or
	// Stripe retries them forever.
	Actionable bool
}

// APIVersion is the Stripe API version this build understands.
//
// It is exposed because the webhook endpoint in the Stripe dashboard has its
// own version, and an endpoint on a different one sends objects of a
// different shape. Verification refuses those rather than guessing, so an
// operator needs to be able to read this number and match it.
func APIVersion() string { return stripe.APIVersion }

// Verify checks a webhook's signature and extracts what matters.
//
// The signature is the only thing standing between this endpoint and anybody
// on the internet granting themselves a subscription, so an unverifiable
// payload is an error and never a best-effort parse.
//
// A version mismatch is refused for the same reason rather than being waved
// through with IgnoreAPIVersionMismatch. Stripe genuinely moves fields
// between versions — current_period_end left the subscription for its items
// in this very one — and a misparsed object does not fail, it quietly reads
// as "no subscription" and locks a paying customer out. A loud refusal that
// names the fix is the better failure.
func (s *Stripe) Verify(payload []byte, signature string) (Event, error) {
	if s.WebhookSecret == "" {
		return Event{}, errors.New("billing: no webhook secret; refusing to trust an unverified event")
	}
	ev, err := webhook.ConstructEvent(payload, signature, s.WebhookSecret)
	if err != nil {
		if strings.Contains(err.Error(), "API version") {
			return Event{}, fmt.Errorf("billing: this build reads Stripe API version %s, and the "+
				"webhook endpoint is sending another. Set the endpoint's version to match in the "+
				"Stripe dashboard: %w", stripe.APIVersion, err)
		}
		return Event{}, fmt.Errorf("billing: webhook signature: %w", err)
	}

	out := Event{Type: string(ev.Type)}
	switch ev.Type {
	case "customer.subscription.created",
		"customer.subscription.updated",
		"customer.subscription.deleted":
		var sub stripe.Subscription
		if err := jsonUnmarshal(ev.Data.Raw, &sub); err != nil {
			return Event{}, fmt.Errorf("billing: %s: %w", ev.Type, err)
		}
		out.Actionable = true
		out.SubscriptionID = sub.ID
		out.Status = string(sub.Status)
		out.CancelAtPeriodEnd = sub.CancelAtPeriodEnd
		if sub.Customer != nil {
			out.CustomerID = sub.Customer.ID
		}
		out.CurrentPeriodEnd = periodEnd(&sub)
		// A deletion is the end of it whatever the payload says the status
		// is, because Stripe sends the object as it was at the moment it
		// went away.
		if ev.Type == "customer.subscription.deleted" {
			out.Status = "canceled"
		}
	}
	return out, nil
}

// periodEnd reads when the current billing period ends.
//
// Stripe moved this from the subscription to its items in a recent API
// version, and the subscription-level field is absent on new accounts. Taking
// the latest of the items is what the subscription's own end date means when
// there is more than one.
func periodEnd(sub *stripe.Subscription) time.Time {
	var latest int64
	if sub.Items != nil {
		for _, item := range sub.Items.Data {
			if item.CurrentPeriodEnd > latest {
				latest = item.CurrentPeriodEnd
			}
		}
	}
	if latest == 0 {
		return time.Time{}
	}
	return time.Unix(latest, 0).UTC()
}

// jsonUnmarshal is a named indirection so the import stays visible next to
// the one place it is used.
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

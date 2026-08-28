// Package billing decides what a user on the hosted instance is entitled to.
//
// The question "may this person use Mimir?" has exactly one answer function,
// and it is pure. Everything that could make it complicated — Stripe, the
// clock, the database — is passed in, so the rules can be read in one place
// and tested without any of them.
//
// Three things grant access, in this order:
//
//	comped        somebody was given it for free
//	trial         the first fourteen days, from the day the account was made
//	subscription  Stripe says trialing or active
//
// And one thing that looks like a fourth but is not: this package does
// nothing at all unless the instance is the hosted one. A self-hosted Mimir
// has no billing, no trial and no expiry, because the software is free and
// the whole promise is that running it yourself holds nothing back.
package billing

import (
	"time"
)

// TrialDays is how long the free trial runs. It asks for no card, which is
// what keeps it out of Stripe entirely: there is nothing to charge, so there
// is nothing to create.
const TrialDays = 14

// Reason names why access was granted or refused. It travels with the
// decision so the interface can say something true rather than "no".
type Reason string

const (
	// ReasonSelfHosted is the answer on every install that is not the one
	// being sold. There is nothing to pay for.
	ReasonSelfHosted Reason = "self-hosted"
	// ReasonComped is free access, given deliberately.
	ReasonComped Reason = "comped"
	// ReasonTrial is the first fourteen days.
	ReasonTrial Reason = "trial"
	// ReasonSubscribed is a live Stripe subscription.
	ReasonSubscribed Reason = "subscribed"
	// ReasonTrialOver is the trial having run out with nothing after it.
	ReasonTrialOver Reason = "trial-over"
	// ReasonLapsed is a subscription that has stopped paying.
	ReasonLapsed Reason = "lapsed"
)

// Record is what the database holds about one user's billing. The zero value
// is the normal state — no row — and means "on trial".
type Record struct {
	Comped bool
	// Status is Stripe's own word, stored verbatim.
	Status            string
	CurrentPeriodEnd  time.Time
	CancelAtPeriodEnd bool
	StripeCustomerID  string
	SubscriptionID    string
}

// Access is the decision.
type Access struct {
	Allowed bool   `json:"allowed"`
	Reason  Reason `json:"reason"`
	// TrialEndsAt is set while the trial is what is granting access, so the
	// interface can count down rather than surprising somebody on day
	// fifteen.
	TrialEndsAt time.Time `json:"trialEndsAt,omitempty"`
	// RenewsAt is when a live subscription next bills, or when a cancelled
	// one runs out.
	RenewsAt time.Time `json:"renewsAt,omitempty"`
	// CancelAtPeriodEnd says the subscription is live and will not renew.
	CancelAtPeriodEnd bool `json:"cancelAtPeriodEnd,omitempty"`
}

// liveStatuses are the Stripe subscription statuses that entitle somebody to
// use the thing.
//
// past_due is deliberately included. A payment that failed this morning is
// somebody's expired card, not somebody's theft, and Stripe will retry for
// days — locking them out on the first failure would punish the wrong person
// at the wrong moment. Stripe moves it to canceled or unpaid when the retries
// are exhausted, and those are refused.
var liveStatuses = map[string]bool{
	"trialing": true,
	"active":   true,
	"past_due": true,
}

// Decide answers whether a user may use the hosted service.
//
// hosted says whether this instance is the one being sold; createdAt is when
// the account was made, which is what the trial is counted from; now is
// passed in so this is testable and so two calls in one request cannot
// disagree.
func Decide(hosted bool, rec Record, createdAt, now time.Time) Access {
	if !hosted {
		return Access{Allowed: true, Reason: ReasonSelfHosted}
	}
	if rec.Comped {
		return Access{Allowed: true, Reason: ReasonComped}
	}

	// A subscription outranks the trial, because somebody who paid during
	// their trial should not be told about a trial.
	if liveStatuses[rec.Status] {
		return Access{
			Allowed:           true,
			Reason:            ReasonSubscribed,
			RenewsAt:          rec.CurrentPeriodEnd,
			CancelAtPeriodEnd: rec.CancelAtPeriodEnd,
		}
	}

	trialEnds := createdAt.AddDate(0, 0, TrialDays)
	if now.Before(trialEnds) {
		return Access{Allowed: true, Reason: ReasonTrial, TrialEndsAt: trialEnds}
	}

	// Out of road. Which of the two refusals it is matters: somebody who
	// never subscribed is being asked to start, and somebody whose payment
	// stopped is being asked to fix a card. Telling one of them the other's
	// story is how support tickets are made.
	if rec.Status != "" {
		return Access{Reason: ReasonLapsed, RenewsAt: rec.CurrentPeriodEnd}
	}
	return Access{Reason: ReasonTrialOver, TrialEndsAt: trialEnds}
}

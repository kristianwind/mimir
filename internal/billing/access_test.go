package billing

import (
	"testing"
	"time"
)

var (
	made = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	// Day 3 of the trial, and day 30, well past it.
	inTrial  = made.AddDate(0, 0, 3)
	outTrial = made.AddDate(0, 0, 30)
)

// The single most important property in this package: a self-hosted Mimir has
// no billing at all. If this ever returns anything but an unconditional yes,
// somebody running their own copy of free software has been locked out of it.
func TestSelfHostedIsNeverGated(t *testing.T) {
	for _, rec := range []Record{
		{},
		{Status: "canceled"},
		{Status: "unpaid"},
		{Comped: false},
	} {
		got := Decide(false, rec, made, outTrial.AddDate(10, 0, 0))
		if !got.Allowed {
			t.Fatalf("a self-hosted install was refused with %+v", rec)
		}
		if got.Reason != ReasonSelfHosted {
			t.Errorf("reason = %q, want %q", got.Reason, ReasonSelfHosted)
		}
	}
}

// A comped account is Kristian's testers. It must outrank everything,
// including an expired trial and a cancelled subscription, and must not
// require the account to be an administrator — which this package cannot even
// express, because it never sees a role.
func TestACompedAccountOutranksEverything(t *testing.T) {
	for _, rec := range []Record{
		{Comped: true},
		{Comped: true, Status: "canceled"},
		{Comped: true, Status: "unpaid"},
	} {
		got := Decide(true, rec, made, outTrial.AddDate(5, 0, 0))
		if !got.Allowed || got.Reason != ReasonComped {
			t.Errorf("comped %+v gave %+v", rec, got)
		}
	}
}

// The trial runs from the day the account was made and is fourteen days.
func TestTheTrialRunsFourteenDaysFromSignup(t *testing.T) {
	last := made.AddDate(0, 0, TrialDays).Add(-time.Second)
	if got := Decide(true, Record{}, made, last); !got.Allowed || got.Reason != ReasonTrial {
		t.Errorf("the last second of the trial gave %+v", got)
	}
	first := made.AddDate(0, 0, TrialDays)
	if got := Decide(true, Record{}, made, first); got.Allowed {
		t.Errorf("the moment the trial ended still allowed access: %+v", got)
	}
}

// While the trial is what is granting access, when it ends has to travel with
// the answer — otherwise day fifteen is a surprise.
func TestTheTrialSaysWhenItEnds(t *testing.T) {
	got := Decide(true, Record{}, made, inTrial)
	if want := made.AddDate(0, 0, TrialDays); !got.TrialEndsAt.Equal(want) {
		t.Errorf("TrialEndsAt = %v, want %v", got.TrialEndsAt, want)
	}
}

// Paying during the trial must stop the answer talking about a trial.
func TestASubscriptionOutranksTheTrial(t *testing.T) {
	renews := inTrial.AddDate(0, 1, 0)
	got := Decide(true, Record{Status: "active", CurrentPeriodEnd: renews}, made, inTrial)
	if got.Reason != ReasonSubscribed {
		t.Errorf("reason = %q, want %q", got.Reason, ReasonSubscribed)
	}
	if !got.TrialEndsAt.IsZero() {
		t.Error("a subscriber was told about a trial")
	}
	if !got.RenewsAt.Equal(renews) {
		t.Errorf("RenewsAt = %v, want %v", got.RenewsAt, renews)
	}
}

// A failed payment this morning is an expired card, not a theft, and Stripe
// retries for days. Locking somebody out on the first failure punishes the
// wrong person at the wrong moment.
func TestAFailedPaymentDoesNotLockSomebodyOutImmediately(t *testing.T) {
	got := Decide(true, Record{Status: "past_due"}, made, outTrial)
	if !got.Allowed {
		t.Error("a past_due subscription was locked out on the first failed payment")
	}
}

// But once Stripe gives up, it is over.
func TestAnExhaustedSubscriptionIsRefused(t *testing.T) {
	for _, status := range []string{"canceled", "unpaid", "incomplete_expired"} {
		got := Decide(true, Record{Status: status}, made, outTrial)
		if got.Allowed {
			t.Errorf("status %q was allowed", status)
		}
		if got.Reason != ReasonLapsed {
			t.Errorf("status %q gave reason %q, want %q", status, got.Reason, ReasonLapsed)
		}
	}
}

// The two refusals are different questions. Somebody who never subscribed is
// being asked to start; somebody whose card stopped working is being asked to
// fix it. Telling one of them the other's story is how support tickets are
// made.
func TestTheTwoRefusalsAreToldApart(t *testing.T) {
	never := Decide(true, Record{}, made, outTrial)
	if never.Reason != ReasonTrialOver {
		t.Errorf("someone who never subscribed got %q", never.Reason)
	}
	if never.TrialEndsAt.IsZero() {
		t.Error("the refusal does not say when the trial ended")
	}

	lapsed := Decide(true, Record{Status: "canceled"}, made, outTrial)
	if lapsed.Reason != ReasonLapsed {
		t.Errorf("a lapsed subscriber got %q", lapsed.Reason)
	}
}

// A subscription set to stop at the end of the period is still live until
// then, and the answer has to say both halves of that.
func TestACancelledSubscriptionRunsToTheEndOfWhatWasPaidFor(t *testing.T) {
	ends := outTrial.AddDate(0, 0, 10)
	got := Decide(true, Record{
		Status: "active", CurrentPeriodEnd: ends, CancelAtPeriodEnd: true,
	}, made, outTrial)
	if !got.Allowed {
		t.Error("a cancelled-but-not-yet-expired subscription was refused")
	}
	if !got.CancelAtPeriodEnd {
		t.Error("the answer does not say it will not renew")
	}
	if !got.RenewsAt.Equal(ends) {
		t.Errorf("RenewsAt = %v, want %v", got.RenewsAt, ends)
	}
}

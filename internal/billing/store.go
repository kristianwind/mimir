package billing

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Store reads and writes what the database knows about a user's billing.
//
// Absence of a row is the normal state and means "on trial", so every read
// returns the zero Record rather than an error when there is nothing there.
// A caller that had to distinguish "no row" from "no subscription" would be a
// caller about to get it wrong.
type Store struct {
	DB *sql.DB
}

// Get returns a user's billing record and when their account was made, which
// is what the trial is counted from.
func (s *Store) Get(ctx context.Context, userID int64) (Record, time.Time, error) {
	var createdAt string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT created_at FROM users WHERE id = ?`, userID).Scan(&createdAt); err != nil {
		return Record{}, time.Time{}, err
	}
	made := parseTime(createdAt)

	var (
		rec       Record
		comped    int
		cancel    int
		periodEnd string
	)
	err := s.DB.QueryRowContext(ctx,
		`SELECT comped, stripe_customer_id, stripe_subscription_id, status,
		        current_period_end, cancel_at_period_end
		   FROM subscriptions WHERE user_id = ?`, userID).
		Scan(&comped, &rec.StripeCustomerID, &rec.SubscriptionID, &rec.Status,
			&periodEnd, &cancel)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, made, nil
	}
	if err != nil {
		return Record{}, time.Time{}, err
	}
	rec.Comped = comped == 1
	rec.CancelAtPeriodEnd = cancel == 1
	rec.CurrentPeriodEnd = parseTime(periodEnd)
	return rec, made, nil
}

// SetComped grants or withdraws free access.
//
// Deliberately separate from every Stripe write. Comping somebody is an
// administrator's decision about a person, not a fact Stripe reported, and
// mixing the two would mean a webhook could take away access that was given
// by hand.
func (s *Store) SetComped(ctx context.Context, userID int64, comped bool, note string) error {
	v := 0
	if comped {
		v = 1
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO subscriptions (user_id, comped, comped_note, updated_at)
		 VALUES (?, ?, ?, datetime('now'))
		 ON CONFLICT(user_id) DO UPDATE SET
		   comped = excluded.comped,
		   comped_note = excluded.comped_note,
		   updated_at = datetime('now')`,
		userID, v, note)
	return err
}

// SetCustomer records the Stripe customer a user was given, before they have
// any subscription. Written at checkout so a second attempt reuses the same
// customer instead of making a duplicate.
// ForgetCustomer drops a stored Stripe customer that no longer exists.
//
// The subscription status is left alone on purpose. A customer disappearing
// says the link is dead, not that somebody stopped paying, and revoking
// access on the strength of one API reply is the wrong way round: entitlement
// is the webhook's to change. This only stops the interface offering a
// billing portal that cannot open.
func (s *Store) ForgetCustomer(ctx context.Context, userID int64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE subscriptions SET stripe_customer_id = '', updated_at = datetime('now')
		 WHERE user_id = ?`, userID)
	return err
}

func (s *Store) SetCustomer(ctx context.Context, userID int64, customerID string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO subscriptions (user_id, stripe_customer_id, updated_at)
		 VALUES (?, ?, datetime('now'))
		 ON CONFLICT(user_id) DO UPDATE SET
		   stripe_customer_id = excluded.stripe_customer_id,
		   updated_at = datetime('now')`,
		userID, customerID)
	return err
}

// SetSubscription records what Stripe says about a subscription.
//
// Only the Stripe-owned columns are touched. comped is never written here:
// an administrator gave that, and a webhook must not be able to take it away.
func (s *Store) SetSubscription(ctx context.Context, customerID, subscriptionID, status string,
	periodEnd time.Time, cancelAtPeriodEnd bool) error {
	cancel := 0
	if cancelAtPeriodEnd {
		cancel = 1
	}
	end := ""
	if !periodEnd.IsZero() {
		end = periodEnd.UTC().Format(time.RFC3339)
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE subscriptions
		    SET stripe_subscription_id = ?, status = ?, current_period_end = ?,
		        cancel_at_period_end = ?, updated_at = datetime('now')
		  WHERE stripe_customer_id = ?`,
		subscriptionID, status, end, cancel, customerID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// A subscription for a customer this instance has never seen. Not an
		// error worth failing a webhook over — Stripe would retry forever —
		// but it must not be silent either, so it is reported and the caller
		// logs it.
		return ErrUnknownCustomer
	}
	return nil
}

// UserByCustomer finds who a Stripe customer belongs to.
func (s *Store) UserByCustomer(ctx context.Context, customerID string) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT user_id FROM subscriptions WHERE stripe_customer_id = ?`, customerID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrUnknownCustomer
	}
	return id, err
}

// ErrUnknownCustomer is a Stripe customer with no user on this instance.
var ErrUnknownCustomer = errors.New("billing: no user for that Stripe customer")

// parseTime reads the two shapes SQLite hands back: what datetime('now')
// writes, and what this package writes.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

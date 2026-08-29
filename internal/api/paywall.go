package api

// Where the subscription actually bites.
//
// Until now it did not. `Decide` worked out that a trial had ended, the
// settings page said so convincingly, and then every endpoint answered anyway
// — the whole product was free to anyone who had once signed up. The logic
// was written and never connected to anything.
//
// What is behind it, and what is not, is the interesting part.
//
// Reading, editing and deleting your own account stays free, permanently.
// Somebody who stops paying has not stopped owning their inventory, and the
// pricing page promises their data is exportable at any time and that
// cancelling deletes nothing. A paywall in front of the export contradicts
// both, and would make the promise the sort a lawyer wrote rather than the
// sort the software keeps.
//
// What is behind it is work the server does: importing, ranking, comparing,
// computing a target, the plan, and the AI layer. That is the thing being
// sold, and it is also the thing that costs something to provide.
//
// Administration is not behind it either. The System and Users pages operate
// the machine rather than use the product, and locking an operator out of the
// controls because of a billing state is how somebody ends up unable to fix
// the billing state.

import (
	"net/http"

	"github.com/kristianwind/mimir/internal/auth"
	"github.com/kristianwind/mimir/internal/billing"
)

// requirePaid refuses the endpoints that are the product when the account is
// not entitled to it.
//
// A self-hosted instance never refuses anything: Decide answers allowed with
// ReasonSelfHosted before it looks at anything else, so this middleware is
// dead weight there rather than a switch somebody could flip.
func (s *Server) requirePaid(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Nothing to ask means nothing to refuse. A Server without a billing
		// store cannot tell whether somebody has paid, and a check that
		// cannot be made must not be resolved against the reader — the cost
		// of failing open here is revenue, and the cost of failing closed is
		// telling a paying customer they have not paid.
		//
		// Loud, because on the instance that sells this it would mean the
		// wiring is wrong rather than absent.
		if s.Billing == nil {
			if s.Config != nil && s.Config.Hosted && s.Log != nil {
				s.Log.Error("no billing store on a hosted instance; the subscription is not being enforced")
			}
			next.ServeHTTP(w, r)
			return
		}

		u, _ := auth.FromContext(r.Context())
		access, _, err := s.access(r.Context(), u.ID)
		if err != nil {
			// A database that cannot answer is not evidence of not paying.
			// Refusing here would turn a fault into a billing accusation, so
			// the error is reported as what it is.
			writeDomainError(w, err)
			return
		}
		if access.Allowed {
			next.ServeHTTP(w, r)
			return
		}

		// 402 rather than 403: this is not a permission the account will
		// never have, it is one that resumes the moment they subscribe, and
		// the two deserve different words.
		writeError(w, http.StatusPaymentRequired, paywallMessage(access.Reason),
			"Your account, your imported data and your goals are all still here, "+
				"and subscribing turns the rest back on.")
	})
}

func paywallMessage(reason billing.Reason) string {
	switch reason {
	case billing.ReasonTrialOver:
		return "the free trial has ended"
	case billing.ReasonLapsed:
		return "the subscription is not active"
	default:
		return "a subscription is needed for this"
	}
}

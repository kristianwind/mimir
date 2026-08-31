package api

// Reading the audit log.
//
// Twenty-odd call sites write to audit_log — sign-ins, failed sign-ins, 2FA
// changes, passkeys added and removed, accounts created and deleted, billing
// comped, game data replaced. Nothing anywhere read it back. No endpoint, no
// page, no query. A security record nobody can look at is a record that does
// not exist for any practical purpose: it cannot answer "who disabled that
// account", it cannot show a burst of failed sign-ins, and it grows forever
// while doing so.
//
// Administrator-only, because it is deliberately readable rather than
// anonymous — failedLogin stores the attempted username precisely so an
// investigation is possible, and the known hazard is somebody typing their
// password into the username box. That risk is bounded by who can read this,
// which is why there is no non-admin view of it and should not be one.

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"
)

type auditEntry struct {
	ID       int64  `json:"id"`
	When     string `json:"when"`
	Username string `json:"username"`
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Detail   string `json:"detail"`
}

// auditPageSize is how many entries one request returns.
const auditPageSize = 200

func (s *Server) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	limit := auditPageSize
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= auditPageSize {
			limit = n
		}
	}

	// Filtering by action is what makes this usable rather than merely
	// present: "show me every failed sign-in" is the question somebody
	// actually arrives with, and scrolling a mixed feed to find them is not
	// an answer. A prefix, so "user.login" also finds "user.login.failed".
	action := r.URL.Query().Get("action")

	// Keyset paging on the id. An OFFSET would drift as new entries land at
	// the top, which is exactly what an audit log does while you read it.
	before := int64(0)
	if v := r.URL.Query().Get("before"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			before = n
		}
	}

	// The username is resolved through the join rather than stored, and a
	// deleted user leaves NULL behind — ON DELETE SET NULL. That is not a
	// hole to paper over: the entry still says what happened and when, and
	// claiming to know who would be worse than admitting the account is gone.
	q := `SELECT a.id, a.ts, COALESCE(u.username, ''), a.action, a.resource, a.detail
	        FROM audit_log a
	        LEFT JOIN users u ON u.id = a.user_id
	       WHERE (? = 0 OR a.id < ?)
	         AND (? = '' OR a.action LIKE ? || '%')
	       ORDER BY a.id DESC
	       LIMIT ?`
	rows, err := s.DB.QueryContext(r.Context(), q, before, before, action, action, limit)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	defer rows.Close()

	out := []auditEntry{}
	for rows.Next() {
		var e auditEntry
		var detail sql.NullString
		if err := rows.Scan(&e.ID, &e.When, &e.Username, &e.Action, &e.Resource, &detail); err != nil {
			writeDomainError(w, err)
			return
		}
		e.Detail = detail.String
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		writeDomainError(w, err)
		return
	}

	// The caller needs to know whether asking again is worth it, and an
	// empty page is a clearer answer than a count that has to be kept true.
	var next int64
	if len(out) == limit {
		next = out[len(out)-1].ID
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entries": out,
		"next":    next,
		"now":     time.Now().UTC().Format(time.RFC3339),
	})
}

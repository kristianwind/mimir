package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type auditPage struct {
	Entries []auditEntry `json:"entries"`
	Next    int64        `json:"next"`
}

func getAudit(t *testing.T, do func(as, method, path, body string) *httptest.ResponseRecorder, as, path string) auditPage {
	t.Helper()
	w := do(as, "GET", path, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s as %s = %d, body %s", path, as, w.Code, w.Body)
	}
	var out auditPage
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The audit log names who did what. A plain user must not be able to read it —
// failedLogin deliberately stores the attempted username, and the hazard it
// accepts is bounded by exactly this check.
func TestOnlyAdminsReadTheAuditLog(t *testing.T) {
	_, do := newServer(t)
	if w := do("member", "GET", "/api/system/audit", ""); w.Code != http.StatusForbidden {
		t.Fatalf("a plain user got %d, want 403", w.Code)
	}
	if w := do("boss", "GET", "/api/system/audit", ""); w.Code != http.StatusOK {
		t.Fatalf("an admin got %d, want 200", w.Code)
	}
}

// Signing in is audited, so simply logging in as the admin puts entries there.
func TestAuditLogReturnsWhatWasWritten(t *testing.T) {
	s, do := newServer(t)
	if _, err := s.DB.Exec(
		`INSERT INTO audit_log (user_id, action, resource, detail) VALUES (1,'user.login.failed','ghost','{"reason":"credentials"}')`); err != nil {
		t.Fatal(err)
	}
	got := getAudit(t, do, "boss", "/api/system/audit")
	if len(got.Entries) == 0 {
		t.Fatal("the log read back empty")
	}
	var found bool
	for _, e := range got.Entries {
		if e.Action == "user.login.failed" && e.Resource == "ghost" {
			found = true
			if e.Username != "boss" {
				t.Errorf("username = %q, want boss", e.Username)
			}
			if e.Detail == "" {
				t.Error("detail was dropped")
			}
			if e.When == "" {
				t.Error("timestamp was dropped")
			}
		}
	}
	if !found {
		t.Fatalf("the entry did not come back: %+v", got.Entries)
	}
}

// Newest first: an audit log is read from the top.
func TestAuditLogIsNewestFirst(t *testing.T) {
	s, do := newServer(t)
	for _, a := range []string{"one", "two", "three"} {
		if _, err := s.DB.Exec(`INSERT INTO audit_log (user_id, action) VALUES (1, ?)`, a); err != nil {
			t.Fatal(err)
		}
	}
	got := getAudit(t, do, "boss", "/api/system/audit")
	for i := 1; i < len(got.Entries); i++ {
		if got.Entries[i-1].ID < got.Entries[i].ID {
			t.Fatalf("out of order at %d: %+v", i, got.Entries)
		}
	}
}

// "Show me every failed sign-in" is the question people arrive with, and a
// prefix means user.login also finds user.login.failed.
func TestAuditLogFiltersByActionPrefix(t *testing.T) {
	s, do := newServer(t)
	for _, a := range []string{"user.login.failed", "user.login.failed", "billing.checkout", "user.password"} {
		if _, err := s.DB.Exec(`INSERT INTO audit_log (user_id, action) VALUES (1, ?)`, a); err != nil {
			t.Fatal(err)
		}
	}
	got := getAudit(t, do, "boss", "/api/system/audit?action=user.login")
	if len(got.Entries) == 0 {
		t.Fatal("prefix filter returned nothing")
	}
	for _, e := range got.Entries {
		if len(e.Action) < 10 || e.Action[:10] != "user.login" {
			t.Fatalf("filter leaked %q", e.Action)
		}
	}
}

// Keyset paging, because an OFFSET drifts as new entries land at the top —
// which is what an audit log does while you are reading it.
func TestAuditLogPagesWithoutDrifting(t *testing.T) {
	s, do := newServer(t)
	for i := 0; i < 5; i++ {
		if _, err := s.DB.Exec(`INSERT INTO audit_log (user_id, action) VALUES (1,'x')`); err != nil {
			t.Fatal(err)
		}
	}
	first := getAudit(t, do, "boss", "/api/system/audit?limit=2")
	if len(first.Entries) != 2 || first.Next == 0 {
		t.Fatalf("first page = %+v", first)
	}
	// A brand-new entry arrives between the two requests.
	if _, err := s.DB.Exec(`INSERT INTO audit_log (user_id, action) VALUES (1,'newcomer')`); err != nil {
		t.Fatal(err)
	}
	second := getAudit(t, do, "boss", "/api/system/audit?limit=2&before="+itoa(first.Next))
	for _, e := range second.Entries {
		if e.ID >= first.Entries[len(first.Entries)-1].ID {
			t.Fatalf("page two repeated an entry from page one: %+v", e)
		}
		if e.Action == "newcomer" {
			t.Fatal("a row inserted mid-read appeared on a later page")
		}
	}
}

// A deleted user leaves NULL behind (ON DELETE SET NULL). The entry must still
// come back saying what happened, rather than vanishing or crashing the scan.
func TestAuditLogSurvivesADeletedUser(t *testing.T) {
	s, do := newServer(t)
	if _, err := s.DB.Exec(`INSERT INTO audit_log (user_id, action, resource) VALUES (NULL,'user.delete','42')`); err != nil {
		t.Fatal(err)
	}
	got := getAudit(t, do, "boss", "/api/system/audit?action=user.delete")
	if len(got.Entries) != 1 {
		t.Fatalf("entries = %+v", got.Entries)
	}
	if got.Entries[0].Username != "" {
		t.Errorf("username = %q, want empty for a deleted user", got.Entries[0].Username)
	}
}

// An absurd limit must not become a way to pull the whole table in one go.
func TestAuditLogCapsTheLimit(t *testing.T) {
	s, do := newServer(t)
	for i := 0; i < 5; i++ {
		if _, err := s.DB.Exec(`INSERT INTO audit_log (user_id, action) VALUES (1,'x')`); err != nil {
			t.Fatal(err)
		}
	}
	got := getAudit(t, do, "boss", "/api/system/audit?limit=100000")
	if len(got.Entries) > auditPageSize {
		t.Fatalf("returned %d entries, cap is %d", len(got.Entries), auditPageSize)
	}
}

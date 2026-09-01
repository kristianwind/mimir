package api

import (
	"net/http"
	"strings"
	"testing"
)

// Choosing who is in the grid is not optional, and the refusal has to explain
// itself: measuring a whole roster piece by piece is thousands of damage
// calculations, so guessing "everybody" would be a hang wearing the costume of
// a page load.
func TestTheGridInsistsOnAChoice(t *testing.T) {
	_, do := newServer(t)
	w := do("member", "GET", "/api/accounts/1/artifact-grid", "")
	if w.Code == http.StatusOK {
		t.Fatal("the grid measured something without being told who")
	}
	if w.Code == http.StatusBadRequest && !strings.Contains(w.Body.String(), "chosen") {
		t.Errorf("the refusal does not say what is missing: %s", w.Body)
	}
}

// A list long enough to hang the page is refused with a number in the message,
// not silently truncated — a grid quietly missing rows reads as "nothing to
// say about her".
func TestTheGridCapsTheList(t *testing.T) {
	_, do := newServer(t)
	many := strings.Repeat("Furina,", gridLimit+2)
	w := do("member", "GET", "/api/accounts/1/artifact-grid?characters="+many, "")
	if w.Code == http.StatusOK {
		t.Fatalf("accepted %d characters, cap is %d", gridLimit+2, gridLimit)
	}
	if w.Code == http.StatusUnprocessableEntity && !strings.Contains(w.Body.String(), "eight") {
		t.Errorf("the refusal does not say what the limit is: %s", w.Body)
	}
}

// The cap is a real number somebody has to be able to reason about, and a
// silent change to it would change how long a page takes without anybody
// noticing.
func TestTheGridLimitIsStated(t *testing.T) {
	if gridLimit < 1 || gridLimit > 20 {
		t.Fatalf("gridLimit = %d, which is either useless or a hang", gridLimit)
	}
}

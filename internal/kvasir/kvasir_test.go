package kvasir

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/llm"
)

// stub is an OpenAI-compatible endpoint that answers from a script.
//
// A recorded endpoint rather than a fake client: the wire format is half of
// what can break here — a model that wraps its JSON in a code fence, an
// endpoint that ignores response_format — and a fake at the Go interface would
// test none of it.
type stub struct {
	t        *testing.T
	replies  []string
	calls    int
	requests []map[string]any
}

func (s *stub) start() *llm.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		s.requests = append(s.requests, decoded)

		if s.calls >= len(s.replies) {
			s.t.Errorf("the model was asked %d times; the script has %d answers", s.calls+1, len(s.replies))
			http.Error(w, "no script left", http.StatusInternalServerError)
			return
		}
		reply := s.replies[s.calls]
		s.calls++

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":`+
			mustJSON(reply)+`},"finish_reason":"stop"}],"usage":{"total_tokens":42}}`)
	}))
	s.t.Cleanup(srv.Close)
	return llm.New(srv.URL, "test-model", "", 0, false)
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func testBrief() *Brief {
	b := NewBrief("plan", "", "The resin plan", "What should they do first?")
	sec := b.Add("The ranked plan")
	sec.Line("1. [RaidenShogun] Switch to 4pc EmblemOfSeveredFate · +34.53 % · free")
	sec.Line("2. [Xiangling] Elemental skill 9 → 10 · +1.09 % · 20 resin")
	return b
}

func TestAdviseReturnsAParsedOpinion(t *testing.T) {
	s := &stub{t: t, replies: []string{
		`{"verdict":"Do the Emblem swap.","points":[{"headline":"Move Emblem to Raiden","why":"It is +34.53 % and costs nothing."}],"questions":["Is Noblesse up?"]}`,
	}}
	a := &Advisor{Client: s.start()}

	got, err := a.Advise(context.Background(), testBrief())
	if err != nil {
		t.Fatal(err)
	}
	if got.Opinion.Verdict != "Do the Emblem swap." {
		t.Errorf("verdict = %q", got.Opinion.Verdict)
	}
	if len(got.Opinion.Points) != 1 || len(got.Opinion.Questions) != 1 {
		t.Errorf("opinion = %+v", got.Opinion)
	}
	if got.Attempts != 1 || len(got.Dropped) != 0 {
		t.Errorf("attempts = %d, dropped = %v", got.Attempts, got.Dropped)
	}
}

// Half the endpoints this has to work with treat response_format as advisory,
// and several models fence their JSON whatever they are told. Failing on that
// would make the layer look broken when it is one substring away from working.
func TestAdviseReadsFencedJSON(t *testing.T) {
	s := &stub{t: t, replies: []string{
		"Here you go:\n```json\n{\"verdict\":\"Swap the sands.\",\"points\":[]}\n```\n",
	}}
	a := &Advisor{Client: s.start()}

	got, err := a.Advise(context.Background(), testBrief())
	if err != nil {
		t.Fatal(err)
	}
	if got.Opinion.Verdict != "Swap the sands." {
		t.Fatalf("verdict = %q", got.Opinion.Verdict)
	}
}

// The point of the whole package: a fabricated figure is asked about once and
// then removed, and what was removed is reported rather than quietly dropped.
func TestAFabricatedNumberIsRetriedThenCut(t *testing.T) {
	bad := `{"verdict":"Do the Emblem swap.",
	         "points":[{"headline":"Level her talent","why":"Worth 12.7 % for 20 resin."},
	                   {"headline":"Move Emblem","why":"+34.53 % and free."}]}`
	s := &stub{t: t, replies: []string{bad, bad}}
	a := &Advisor{Client: s.start()}

	got, err := a.Advise(context.Background(), testBrief())
	if err != nil {
		t.Fatal(err)
	}
	if s.calls != 2 {
		t.Errorf("the model was asked %d times; it should get one chance to correct itself", s.calls)
	}
	if len(got.Opinion.Points) != 1 || got.Opinion.Points[0].Headline != "Move Emblem" {
		t.Errorf("the unsourced point survived: %+v", got.Opinion.Points)
	}
	if len(got.Dropped) != 1 || got.Dropped[0].Numbers[0] != "12.7" {
		t.Errorf("what was cut is not reported: %+v", got.Dropped)
	}

	// The correction has to name the figure, or the second attempt is a
	// coin toss rather than a fix.
	second := s.requests[1]["messages"].([]any)
	last := second[len(second)-1].(map[string]any)["content"].(string)
	if !strings.Contains(last, "12.7") {
		t.Errorf("the correction did not name the number: %q", last)
	}
}

// Nothing survived the check twice over. Showing an empty card would read as
// "Kvasir has no opinion"; the truth is that it had one and it was not sourced.
func TestAnAnswerThatIsAllInventionIsRefused(t *testing.T) {
	bad := `{"verdict":"You are 61.2 % off the mark.","points":[]}`
	s := &stub{t: t, replies: []string{bad, bad}}
	a := &Advisor{Client: s.start()}

	if _, err := a.Advise(context.Background(), testBrief()); !errors.Is(err, ErrUnsourced) {
		t.Fatalf("err = %v, want ErrUnsourced", err)
	}
}

func TestAdviseRefusesAnEmptyFactSheet(t *testing.T) {
	s := &stub{t: t}
	a := &Advisor{Client: s.start()}

	empty := NewBrief("plan", "", "Nothing", "?")
	if _, err := a.Advise(context.Background(), empty); !errors.Is(err, ErrNoFacts) {
		t.Fatalf("err = %v, want ErrNoFacts", err)
	}
	if s.calls != 0 {
		t.Error("the model was asked to comment on an empty page")
	}
}

func TestWithoutAnEndpointTheLayerIsSimplyOff(t *testing.T) {
	a := &Advisor{Client: llm.New("", "", "", 0, false)}
	if a.Available() {
		t.Fatal("an unconfigured advisor reports itself available")
	}
	if _, err := a.Advise(context.Background(), testBrief()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

// The product has one language, and the prompt has to say so: a model given a
// fact sheet full of proper nouns will otherwise answer in whatever language
// the question arrived in.
func TestThePromptAsksForEnglish(t *testing.T) {
	s := &stub{t: t, replies: []string{`{"verdict":"Swap the sands.","points":[]}`}}
	a := &Advisor{Client: s.start()}

	if _, err := a.Advise(context.Background(), testBrief()); err != nil {
		t.Fatal(err)
	}
	system := s.requests[0]["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, "in English") {
		t.Fatalf("the prompt does not ask for English: %q", system)
	}
}

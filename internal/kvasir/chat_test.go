package kvasir

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/llm"
)

// toolStub answers with whole message objects, so a scripted turn can be a
// tool call rather than only prose.
type toolStub struct {
	t        *testing.T
	messages []string
	calls    int
	requests []map[string]any
}

func (s *toolStub) start() *llm.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		s.requests = append(s.requests, decoded)

		if s.calls >= len(s.messages) {
			s.t.Errorf("the model was asked %d times; the script has %d answers", s.calls+1, len(s.messages))
			http.Error(w, "no script left", http.StatusInternalServerError)
			return
		}
		msg := s.messages[s.calls]
		s.calls++

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":`+msg+`,"finish_reason":"stop"}],"usage":{}}`)
	}))
	s.t.Cleanup(srv.Close)
	return llm.New(srv.URL, "test-model", "", 0, false)
}

// fakeRunner stands in for the engine.
type fakeRunner struct {
	result string
	called []string
	args   []map[string]any
}

func (f *fakeRunner) Run(_ context.Context, name string, args map[string]any) (string, error) {
	f.called = append(f.called, name)
	f.args = append(f.args, args)
	return f.result, nil
}

func TestChatFetchesFactsBeforeAnswering(t *testing.T) {
	s := &toolStub{t: t, messages: []string{
		`{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function",
		  "function":{"name":"build_sheet","arguments":"{\"character\":\"RaidenShogun\"}"}}]}`,
		`{"role":"assistant","content":"Her crit rate is 71.4 %, which is where the build is thin."}`,
	}}
	runner := &fakeRunner{result: "## Resolved stats\n- Crit Rate: 71.4 %\n"}
	a := &Advisor{Client: s.start()}

	got, err := a.Chat(context.Background(), ChatRequest{
		History: []Turn{{Role: "user", Content: "Where is Raiden weak?"}},
		Runner:  runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.called) != 1 || runner.called[0] != "build_sheet" {
		t.Fatalf("tools called: %v", runner.called)
	}
	if runner.args[0]["character"] != "RaidenShogun" {
		t.Errorf("arguments did not reach the engine: %v", runner.args[0])
	}
	if len(got.Unsourced) != 0 {
		t.Errorf("a figure the tool returned was flagged: %v", got.Unsourced)
	}
	if len(got.Used) != 1 {
		t.Errorf("the answer does not say what it looked up: %v", got.Used)
	}
}

// A figure nobody looked up is flagged rather than removed. A bullet can be
// cut and leave the rest true; a sentence cut out of a paragraph leaves an
// argument missing its middle, so the reader is told instead.
func TestChatFlagsFiguresNothingProduced(t *testing.T) {
	s := &toolStub{t: t, messages: []string{
		`{"role":"assistant","content":"Give her the Catch: it is worth about 16.9 %."}`,
	}}
	a := &Advisor{Client: s.start()}

	got, err := a.Chat(context.Background(), ChatRequest{
		History: []Turn{{Role: "user", Content: "What about the Catch?"}},
		Runner:  &fakeRunner{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Unsourced) != 1 || got.Unsourced[0] != "16.9" {
		t.Fatalf("unsourced = %v", got.Unsourced)
	}
	if got.Reply == "" {
		t.Error("the answer was withheld; it should be shown with the figure flagged")
	}
}

func TestChatStopsAskingForDataEventually(t *testing.T) {
	call := `{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function",
	          "function":{"name":"roster","arguments":"{}"}}]}`
	s := &toolStub{t: t, messages: []string{call, call, call, call}}
	a := &Advisor{Client: s.start()}

	_, err := a.Chat(context.Background(), ChatRequest{
		History: []Turn{{Role: "user", Content: "Well?"}},
		Runner:  &fakeRunner{result: "- nothing"},
	})
	if err == nil {
		t.Fatal("a model stuck in a fetch loop was allowed to run forever")
	}
}

// A tool that fails is a fact the model should read and work with. Aborting
// the conversation would make a typo in a character name look like an outage.
func TestAToolFailureBecomesAFact(t *testing.T) {
	s := &toolStub{t: t, messages: []string{
		`{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function",
		  "function":{"name":"build_sheet","arguments":"not json"}}]}`,
		`{"role":"assistant","content":"I could not read that character."}`,
	}}
	a := &Advisor{Client: s.start()}

	got, err := a.Chat(context.Background(), ChatRequest{
		History: []Turn{{Role: "user", Content: "And Xianglong?"}},
		Runner:  &fakeRunner{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Reply == "" {
		t.Fatal("no answer came back")
	}

	// The failure has to reach the model as a tool result, not vanish.
	messages := s.requests[1]["messages"].([]any)
	last := messages[len(messages)-1].(map[string]any)
	if last["role"] != "tool" || !strings.Contains(last["content"].(string), "JSON") {
		t.Fatalf("the failure did not reach the model: %v", last)
	}
}

func TestEveryToolIsReadOnly(t *testing.T) {
	// Kvasir advises; equipping a piece or changing a goal stays with the
	// player. A name here that implied otherwise would be a different
	// product with a different risk, so the list is pinned.
	want := map[string]bool{
		"account_plan": true, "goal_plan": true, "build_sheet": true,
		"talents": true, "roster": true, "goals": true,
		"inventory": true, "drop_model": true, "potential": true,
	}
	for _, tool := range Tools() {
		if !want[tool.Function.Name] {
			t.Errorf("unexpected tool %q — is it read-only?", tool.Function.Name)
		}
		delete(want, tool.Function.Name)
	}
	for name := range want {
		t.Errorf("tool %q disappeared", name)
	}
}

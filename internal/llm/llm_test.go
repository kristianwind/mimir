package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNoEndpointIsNotAClient(t *testing.T) {
	if c := New("  ", "m", "", 0, false); c != nil {
		t.Fatal("an empty base URL produced a client")
	}
	var c *Client
	if c.Configured() {
		t.Fatal("a nil client reports itself configured")
	}
	if _, err := c.Complete(context.Background(), Request{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestCompleteSendsTheModelAndTheKey(t *testing.T) {
	var got struct {
		Model      string    `json:"model"`
		Messages   []Message `json:"messages"`
		ToolChoice string    `json:"tool_choice"`
		Format     *struct {
			Type string `json:"type"`
		} `json:"response_format"`
	}
	var auth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"total_tokens":7}}`)
	}))
	defer srv.Close()

	c := New(srv.URL+"/", "qwen", "sk-test", 0, false)
	reply, err := c.Complete(context.Background(), Request{
		Messages:   []Message{{Role: RoleUser, Content: "hello"}},
		Tools:      []Tool{tool()},
		JSONObject: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Message.Content != "ok" || reply.Usage.TotalTokens != 7 || reply.FinishReason != "stop" {
		t.Errorf("reply = %+v", reply)
	}
	if auth != "Bearer sk-test" {
		t.Errorf("authorization = %q", auth)
	}
	if got.Model != "qwen" || len(got.Messages) != 1 {
		t.Errorf("request = %+v", got)
	}
	// Offering tools without saying they may be used gets them ignored by
	// several endpoints.
	if got.ToolChoice != "auto" {
		t.Errorf("tool_choice = %q", got.ToolChoice)
	}
	if got.Format == nil || got.Format.Type != "json_object" {
		t.Errorf("response_format = %+v", got.Format)
	}
	// A trailing slash on the configured URL must not produce //chat/…
	if c.BaseURL != srv.URL {
		t.Errorf("base URL = %q", c.BaseURL)
	}
}

// The three ways an endpoint disappoints, each with the address in the
// message: a wrong URL is the usual cause, and an error that does not name it
// sends the operator to the wrong place.
func TestFailuresNameTheEndpoint(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"a status": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "model not found", http.StatusNotFound)
		},
		"a page of HTML": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "<html>502 Bad Gateway</html>")
		},
		"an error object": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"error":{"message":"context length exceeded"}}`)
		},
		"no choices at all": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"choices":[]}`)
		},
	}

	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			c := New(srv.URL, "m", "", 0, false)
			_, err := c.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser}}})
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), srv.URL) {
				t.Errorf("error does not name the endpoint: %v", err)
			}
		})
	}
}

func TestModelsListsWhatIsServed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("asked for %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"qwen3"},{"id":"llama"}]}`)
	}))
	defer srv.Close()

	got, err := New(srv.URL, "", "", 0, false).Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "qwen3" {
		t.Fatalf("models = %v", got)
	}
}

func tool() Tool {
	return Tool{Type: "function", Function: Function{
		Name:       "roster",
		Parameters: map[string]any{"type": "object"},
	}}
}

// Thinking is off unless asked for, and off is a field in the body rather than
// a wish in the prompt.
func TestThinkingIsSwitchedOffByDefault(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()

	off := New(srv.URL, "m", "", 0, false)
	if _, err := off.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser}}}); err != nil {
		t.Fatal(err)
	}
	kwargs, ok := bodies[0]["chat_template_kwargs"].(map[string]any)
	if !ok || kwargs["enable_thinking"] != false {
		t.Fatalf("thinking was not switched off: %v", bodies[0]["chat_template_kwargs"])
	}

	on := New(srv.URL, "m", "", 0, true)
	if _, err := on.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser}}}); err != nil {
		t.Fatal(err)
	}
	if _, present := bodies[1]["chat_template_kwargs"]; present {
		t.Error("a client told to allow thinking still sent the switch")
	}
}

// chat_template_kwargs is not part of the OpenAI API. An endpoint that refuses
// the whole request over it must not leave the operator hunting for a setting.
func TestAnEndpointThatRefusesTheSwitchIsRetriedWithoutIt(t *testing.T) {
	var seen []bool // whether each request carried the field
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		_, has := body["chat_template_kwargs"]
		seen = append(seen, has)

		if has {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Unrecognized request argument: chat_template_kwargs"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "m", "", 0, false)
	reply, err := c.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser}}})
	if err != nil {
		t.Fatalf("the retry did not rescue the request: %v", err)
	}
	if reply.Message.Content != "ok" {
		t.Errorf("content = %q", reply.Message.Content)
	}
	if len(seen) != 2 || !seen[0] || seen[1] {
		t.Fatalf("expected one attempt with the field and one without, got %v", seen)
	}

	// And it is remembered: a second call does not spend a request finding out
	// the same thing again.
	if _, err := c.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser}}}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 || seen[2] {
		t.Errorf("the refusal was not remembered: %v", seen)
	}
}

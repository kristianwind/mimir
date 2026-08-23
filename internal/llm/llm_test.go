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
	if c := New("  ", "m", "", 0); c != nil {
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

	c := New(srv.URL+"/", "qwen", "sk-test", 0)
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

			c := New(srv.URL, "m", "", 0)
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

	got, err := New(srv.URL, "", "", 0).Models(context.Background())
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

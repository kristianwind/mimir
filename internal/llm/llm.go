// Package llm is a small client for OpenAI-compatible chat endpoints.
//
// Small on purpose. Mimir's AI layer explains numbers the engine produced; it
// never produces one. That makes the client's job narrow — send messages, get
// a message back, occasionally let the model ask for a tool — and a narrow job
// does not justify a vendor SDK and its transitive dependency tree in a binary
// whose whole selling point is that it is one static file.
//
// The endpoint is deliberately OpenAI-shaped rather than any single vendor's:
// LM Studio, Ollama, vLLM, llama.cpp's server and the hosted APIs all speak it,
// so the operator picks where their game account's data is allowed to go. With
// MIMIR_LLM_BASE_URL unset there is no client at all and every other part of
// Mimir works unchanged.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotConfigured is returned when no endpoint has been configured. It is a
// distinct error rather than a generic failure because every caller wants to
// treat it the same way: hide the AI layer, do not report a fault.
var ErrNotConfigured = errors.New("llm: no endpoint is configured")

// Roles. Strings rather than a type, because they cross the wire as strings
// and a conversion at every construction site buys nothing.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message is one turn. The shape is the wire shape: this struct is what gets
// serialised, so a field the endpoint does not know about must not appear
// unless it was asked for — hence omitempty on everything optional.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// ToolCalls is the assistant asking to run something. The content is
	// usually empty in that turn.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID ties a tool result back to the call that asked for it.
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
}

// ToolCall is a request from the model to run one of the tools it was offered.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall carries the arguments as a JSON string, which is how the API
// defines it — models emit text, and a malformed object has to be catchable as
// a parse error rather than corrupting the whole response.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool is one function offered to the model.
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Function describes a tool: its name, what it is for, and a JSON Schema for
// its arguments.
type Function struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Request is one completion.
type Request struct {
	Messages []Message
	Tools    []Tool
	// Temperature is low by default at the call sites: this is an advisor
	// reading a fact sheet, and creative rephrasing of a damage figure is
	// exactly the failure mode the whole layer is built to prevent.
	Temperature float64
	MaxTokens   int
	// JSONObject asks the endpoint to constrain the reply to a JSON object.
	// Support is uneven — Ollama honours it, some builds of llama.cpp ignore
	// it — so callers must still parse leniently rather than assume.
	JSONObject bool
}

// Reply is the model's answer plus what it cost.
type Reply struct {
	Message Message
	// Usage is reported when the endpoint bothers to; zero otherwise. It is
	// carried through so the System page can say what the layer is spending
	// rather than leaving that invisible.
	Usage Usage
	// FinishReason distinguishes a complete answer from one the token budget
	// cut off, which is the difference between an opinion and half of one.
	FinishReason string
}

// Usage is the token accounting the endpoint reports.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Client talks to one endpoint.
type Client struct {
	BaseURL string
	Model   string
	APIKey  string
	HTTP    *http.Client
}

// New returns a client, or nil when no endpoint is configured.
//
// Nil rather than an error: "the operator did not want an AI layer" is a
// configuration, not a fault, and returning nil lets every call site test for
// it with the same `if s.LLM == nil` it would need anyway.
func New(baseURL, model, apiKey string, timeout time.Duration) *Client {
	if strings.TrimSpace(baseURL) == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Model:   strings.TrimSpace(model),
		APIKey:  strings.TrimSpace(apiKey),
		HTTP:    &http.Client{Timeout: timeout},
	}
}

// Configured reports whether there is an endpoint to talk to.
func (c *Client) Configured() bool { return c != nil && c.BaseURL != "" }

type wireRequest struct {
	Model          string          `json:"model,omitempty"`
	Messages       []Message       `json:"messages"`
	Tools          []Tool          `json:"tools,omitempty"`
	ToolChoice     string          `json:"tool_choice,omitempty"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Stream         bool            `json:"stream"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type wireReply struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete sends one request and returns the model's message.
func (c *Client) Complete(ctx context.Context, req Request) (Reply, error) {
	if !c.Configured() {
		return Reply{}, ErrNotConfigured
	}

	body := wireRequest{
		Model:       c.Model,
		Messages:    req.Messages,
		Tools:       req.Tools,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	if req.JSONObject {
		body.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	if len(req.Tools) > 0 {
		body.ToolChoice = "auto"
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return Reply{}, fmt.Errorf("llm: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Reply{}, fmt.Errorf("llm: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	res, err := c.HTTP.Do(httpReq)
	if err != nil {
		return Reply{}, fmt.Errorf("llm: %s is not answering: %w", c.BaseURL, err)
	}
	defer res.Body.Close()

	// Capped: an endpoint that is actually an HTML error page should not be
	// read into memory in full before being rejected.
	payload, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return Reply{}, fmt.Errorf("llm: read response: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return Reply{}, fmt.Errorf("llm: %s answered %s: %s", c.BaseURL, res.Status, snippet(payload))
	}

	var out wireReply
	if err := json.Unmarshal(payload, &out); err != nil {
		return Reply{}, fmt.Errorf("llm: %s did not answer with JSON: %s", c.BaseURL, snippet(payload))
	}
	if out.Error != nil {
		return Reply{}, fmt.Errorf("llm: %s refused: %s", c.BaseURL, out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return Reply{}, fmt.Errorf("llm: %s returned no answer", c.BaseURL)
	}

	return Reply{
		Message:      out.Choices[0].Message,
		Usage:        out.Usage,
		FinishReason: out.Choices[0].FinishReason,
	}, nil
}

// Models lists what the endpoint serves.
//
// This exists so the System page can report the AI layer as reachable or not
// without spending a completion on the question. An endpoint that is
// configured but unreachable is the most common failure here — a container
// pointing at 127.0.0.1 for a model running on the host — and it should be
// visible as such rather than as an opinion that never arrives.
func (c *Client) Models(ctx context.Context) ([]string, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: %s is not answering: %w", c.BaseURL, err)
	}
	defer res.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm: %s answered %s", c.BaseURL, res.Status)
	}

	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("llm: %s did not answer with JSON", c.BaseURL)
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

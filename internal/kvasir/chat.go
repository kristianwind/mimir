package kvasir

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kristianwind/mimir/internal/llm"
)

// Asking follow-up questions.
//
// The opinion cards answer "how do I get better" for one page. A conversation
// answers the question after that — "why is Xiangling's Catch blocked?", "what
// do I lose if I give Raiden the Emblem sands?" — and those need facts the
// page never fetched.
//
// So chat is the one place the model chooses what to look at. It does not get
// a database; it gets a fixed menu of engine calls, each of which returns the
// same kind of fact sheet the opinion cards are built from. Every number that
// comes back is added to what the answer is allowed to say, which is why
// letting the model pick is safe: it can choose which calculation to run, and
// still cannot produce a number without running one.

// Runner executes a tool the model asked for.
//
// Implemented outside this package, where the account and the engine live.
// The result is a fact sheet as text — the same shape as a brief — because
// the numbers in it have to be collectable by the same check.
type Runner interface {
	Run(ctx context.Context, name string, args map[string]any) (string, error)
}

// Turn is one message in a conversation.
type Turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is one round of conversation.
type ChatRequest struct {
	// Brief is the page the player is looking at, so "and this one?" has a
	// referent without the model having to fetch anything.
	Brief   *Brief
	History []Turn
	Runner  Runner
}

// ChatResult is the answer plus what it was built from.
type ChatResult struct {
	Reply string `json:"reply"`
	// Used names the engine calls the model made, so the player can see the
	// answer was looked up rather than recalled.
	Used []string `json:"used,omitempty"`
	// Unsourced names figures in the answer that no calculation produced.
	//
	// Flagged rather than deleted, which is the opposite of what happens to
	// an opinion's bullet points — and deliberately. A bullet is a
	// self-contained claim and removing it leaves the rest true; a sentence
	// cut out of a paragraph leaves an argument missing its middle. So the
	// answer is shown with the offending figures named, and the reader is
	// told not to trust them.
	Unsourced []string  `json:"unsourced,omitempty"`
	Usage     llm.Usage `json:"usage,omitzero"`
}

// maxToolRounds bounds the loop. Four is enough for "look at the plan, then
// look at the build it argues about, then answer" with a round to spare, and
// small enough that a model stuck in a fetch loop stops costing tokens.
const maxToolRounds = 4

// Tools is the menu of engine calls offered to the model.
//
// Read-only, every one of them. Kvasir advises; changing a goal or equipping a
// piece stays with the player, and a model that could act on an account it
// reasons about imperfectly is a different product with a different risk.
func Tools() []llm.Tool {
	character := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"character": map[string]any{
				"type":        "string",
				"description": "The character key exactly as it appears in the fact sheet, e.g. RaidenShogun.",
			},
		},
		"required": []string{"character"},
	}
	none := map[string]any{"type": "object", "properties": map[string]any{}}

	return []llm.Tool{
		tool("account_plan", "Every upgrade on the account, ranked by damage gained per resin, with what is blocked and why.", none),
		tool("goal_plan", "The ranked upgrades for one character's goal, including what the optimizer would equip.", character),
		tool("build_sheet", "One character's resolved stats, which effects fired, the text each was checked against, and which conditions nobody has answered.", character),
		tool("talents", "One character's talent table: the real labels and their multipliers at the levels this account has.", character),
		tool("roster", "Every character on the account with level, constellation and talent levels.", none),
		tool("potential", "Every character measured with one ruler and no goals: what they score now, what the gear they own would give them, and the biggest single upgrade for each. Covers characters that have no goal and are therefore missing from the plan.", none),
		tool("goals", "The goals set up on this account: rotation, priority, declared conditions.", none),
		tool("inventory", "A summary of the artifact inventory: how many of each set, by slot, and the best unequipped pieces.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"set":  map[string]any{"type": "string", "description": "Optional: only this artifact set."},
				"slot": map[string]any{"type": "string", "description": "Optional: flower, plume, sands, goblet or circlet."},
			},
		}),
		tool("drop_model", "The drop distribution measured from this account's own inventory, and what is biased about it.", none),
	}
}

func tool(name, description string, params map[string]any) llm.Tool {
	return llm.Tool{Type: "function", Function: llm.Function{
		Name: name, Description: description, Parameters: params,
	}}
}

// Chat answers one question, fetching facts as it needs them.
func (a *Advisor) Chat(ctx context.Context, req ChatRequest) (ChatResult, error) {
	if !a.Available() {
		return ChatResult{}, ErrNotConfigured
	}

	sourced := Collect()
	messages := []llm.Message{{Role: llm.RoleSystem, Content: chatPrompt()}}
	if req.Brief != nil && req.Brief.Facts() {
		facts := req.Brief.Text()
		sourced.Add(facts)
		messages = append(messages, llm.Message{
			Role:    llm.RoleUser,
			Content: "This is the page I am looking at.\n\n" + facts,
		})
	}
	for _, turn := range req.History {
		role := llm.RoleUser
		if turn.Role == llm.RoleAssistant {
			role = llm.RoleAssistant
		}
		messages = append(messages, llm.Message{Role: role, Content: turn.Content})
	}

	var out ChatResult
	for round := 0; round < maxToolRounds; round++ {
		reply, err := a.Client.Complete(ctx, llm.Request{
			Messages:    messages,
			Tools:       Tools(),
			Temperature: 0.3,
			MaxTokens:   a.maxTokens(),
		})
		if err != nil {
			return out, err
		}
		out.Usage = reply.Usage

		if len(reply.Message.ToolCalls) == 0 {
			out.Reply = strings.TrimSpace(reply.Message.Content)
			if out.Reply == "" {
				if reply.FinishReason == "length" {
					return out, ErrBudget
				}
				return out, fmt.Errorf("kvasir: the model answered with nothing")
			}
			out.Unsourced = sourced.Unsourced(out.Reply)
			return out, nil
		}

		messages = append(messages, reply.Message)
		for _, call := range reply.Message.ToolCalls {
			result := a.runTool(ctx, req.Runner, call)
			sourced.Add(result)
			out.Used = append(out.Used, call.Function.Name)
			messages = append(messages, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: call.ID,
				Name:       call.Function.Name,
				Content:    result,
			})
		}
	}

	// Out of rounds with no answer. Say so rather than returning the last
	// half-formed thought as if it were one.
	return out, fmt.Errorf("kvasir: the model kept asking for data and never answered")
}

// runTool executes one call, turning a failure into a fact rather than an
// error: "that character is not on the account" is something the model should
// read and work with, and aborting the whole conversation over it would make
// a typo in a character name look like an outage.
func (a *Advisor) runTool(ctx context.Context, runner Runner, call llm.ToolCall) string {
	if runner == nil {
		return "This tool is not available in this context."
	}
	args := map[string]any{}
	if raw := strings.TrimSpace(call.Function.Arguments); raw != "" && raw != "null" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return "Those arguments could not be read as JSON. Try again with the shape in the tool definition."
		}
	}
	result, err := runner.Run(ctx, call.Function.Name, args)
	if err != nil {
		return "That could not be looked up: " + err.Error()
	}
	if strings.TrimSpace(result) == "" {
		return "There is nothing recorded for that."
	}
	return result
}

func chatPrompt() string {
	return `You are Kvasir, the advisor inside Mimir — a Genshin Impact account optimiser.

Mimir's damage engine does the arithmetic. You do not. When you need a number,
call a tool and read it; the tools run the engine against this player's real
account.

Hard rules:
1. Every number you write must have come from a fact sheet or a tool result in
   this conversation. Rounding is fine; estimating is not. This is checked, and
   an answer with an unsourced figure in it is shown to the player with the
   figure flagged as untrustworthy.
2. Never name a character, weapon, artifact set or domain you have not seen in
   this conversation. Look it up instead.
3. If the tools cannot settle the question, say plainly what is missing and
   what the player would have to do — import an inventory, declare a
   condition, set up a goal.
4. Answer in English, in a few short paragraphs. No preamble.`
}

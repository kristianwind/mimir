package kvasir

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/kristianwind/mimir/internal/llm"
)

// Against a real model, by hand.
//
// Everything else in this package tests the machinery against a scripted
// endpoint, which is right: those tests must not depend on a model's mood. But
// the machinery being correct says nothing about whether the prompt survives
// contact with an actual model, and that is a real question with real
// answers — the token budget was three times too small until this was run,
// and the failure looked like "the model said nothing".
//
// Skipped unless MIMIR_LIVE_LLM points somewhere:
//
//	MIMIR_LIVE_LLM=http://host:8080/v1 MIMIR_LIVE_MODEL=some-model \
//	  go test -v -run TestLive ./internal/kvasir/
func liveClient(t *testing.T) *llm.Client {
	t.Helper()
	base := os.Getenv("MIMIR_LIVE_LLM")
	if base == "" {
		t.Skip("set MIMIR_LIVE_LLM to run against a real endpoint")
	}
	return llm.New(base, os.Getenv("MIMIR_LIVE_MODEL"), os.Getenv("MIMIR_LIVE_KEY"), 5*time.Minute,
		os.Getenv("MIMIR_LIVE_THINKING") == "1")
}

func realPlanBrief() *Brief {
	b := NewBrief("plan", "", "The resin plan for account 700123456",
		"This is the ranked plan the player is looking at. What should they do first, what does the ranking not make obvious, and what is holding this account back?")

	m := b.Add("How these numbers were measured")
	m.Line("Every gain is the change in that goal's whole rotation damage, calculated on the gear this account actually owns.")
	m.Line("Free actions rank above paid ones. An action that cannot be carried out today ranks last, however large it looks.")
	m.Line("Efficiency is the gain per 100 resin. A day is 180 resin.")

	g := b.Add("The goals being optimised")
	g.Line("RaidenShogun: baseline 70206 damage per rotation, 5 upgrades found")
	g.Line("Xiangling: baseline 41883 damage per rotation, 4 upgrades found")

	r := b.Add("The ranked plan")
	r.Line("1. [RaidenShogun] Switch to 4pc EmblemOfSeveredFate on RaidenShogun · +34.53 % · free · Takes pieces from Xiangling")
	r.Line("2. [Xiangling] Give Xiangling the weapon TheCatch (R5) · +12.49 % · free · blocked: RaidenShogun is using it, and has at least as high a priority")
	r.Line("3. [Xiangling] Switch to 4pc EmblemOfSeveredFate on Xiangling · +12.42 % · free · blocked: RaidenShogun is using it, and has at least as high a priority")
	r.Line("4. [RaidenShogun] RaidenShogun: elemental burst 9 → 10 · +1.31 % · 20 resin · 6.55 % per 100 resin · blocked: requires a Crown of Insight")
	r.Line("5. [Xiangling] Xiangling: elemental skill 9 → 10 · +1.09 % · 20 resin · 5.45 % per 100 resin")

	c := b.Add("Gear two goals both want")
	c.Line("Xiangling wants EmblemOfSeveredFate from RaidenShogun — RaidenShogun has priority 3, Xiangling has 1")

	l := b.Add("What the engine refused to price")
	l.Line("Each goal is measured against the gear the character has now — not against what a higher-priority goal just claimed.")
	l.Line("RaidenShogun: artifact farming is not priced: the drop model is missing.")
	l.Line("Xiangling: CrimsonWitchOfFlames counts as switched off: set the condition \"CrimsonWitchOfFlames.stacks\" on the goal")

	a := b.Add("The account")
	a.Line("8 characters, 8 weapons and 40 artifacts have been imported.")
	a.Line("The inventory came from Enka, which only reports equipped pieces. Everything unequipped is invisible here — a .good file would change that.")
	return b
}

func liveBudget() int {
	if v := os.Getenv("MIMIR_LIVE_MAX"); v != "" {
		n := 0
		fmt.Sscanf(v, "%d", &n)
		return n
	}
	return 0
}

func TestLiveOpinion(t *testing.T) {
	a := &Advisor{Client: liveClient(t), MaxTokens: liveBudget()}
	brief := realPlanBrief()

	start := time.Now()
	got, err := a.Advise(context.Background(), brief)
	took := time.Since(start)
	if err != nil {
		t.Fatalf("after %s: %v", took.Round(time.Second), err)
	}

	out, _ := json.MarshalIndent(got.Opinion, "", "  ")
	fmt.Printf("\n=== %s in %s (attempts %d, %d tokens) ===\n%s\n",
		got.Model, took.Round(time.Second), got.Attempts, got.Usage.TotalTokens, out)
	if len(got.Dropped) > 0 {
		fmt.Printf("--- dropped ---\n")
		for _, d := range got.Dropped {
			fmt.Printf("  %q  numbers: %v\n", d.Text, d.Numbers)
		}
	}
}

func TestLiveChat(t *testing.T) {
	a := &Advisor{Client: liveClient(t), MaxTokens: liveBudget()}
	runner := &liveRunner{brief: realPlanBrief()}

	start := time.Now()
	got, err := a.Chat(context.Background(), ChatRequest{
		Brief:   realPlanBrief(),
		History: []Turn{{Role: "user", Content: "Why is The Catch blocked for Xiangling, and what would it cost me to give it to her anyway?"}},
		Runner:  runner,
	})
	took := time.Since(start)
	if err != nil {
		t.Fatalf("after %s: %v", took.Round(time.Second), err)
	}
	fmt.Printf("\n=== chat in %s (looked up: %v, unsourced: %v) ===\n%s\n",
		took.Round(time.Second), got.Used, got.Unsourced, got.Reply)
}

type liveRunner struct{ brief *Brief }

func (r *liveRunner) Run(_ context.Context, name string, args map[string]any) (string, error) {
	fmt.Printf("  [tool] %s %v\n", name, args)
	return r.brief.Text(), nil
}

// The biggest brief the product can actually build.
//
// realPlanBrief is a small account, and the plan brief is capped anyway
// (briefActions). The roster brief is not: it writes one line per owned
// character, so a complete account is roughly ten times the fact sheet every
// other measurement here was taken on. That is the input a "minimum model"
// has to survive, and judging one on the small brief measures the wrong thing.
func bigRosterBrief() *Brief {
	names := []string{
		"RaidenShogun", "Xiangling", "Bennett", "Xingqiu", "Kazuha", "Nahida",
		"Furina", "Neuvillette", "Zhongli", "Ganyu", "HuTao", "Yelan", "Ayaka",
		"Mona", "Diluc", "Klee", "Venti", "Albedo", "Eula", "Jean", "Qiqi",
		"Keqing", "Tighnari", "Cyno", "Nilou", "Wanderer", "Alhaitham", "Dehya",
		"Baizhu", "Kaveh", "Lyney", "Lynette", "Freminet", "Wriothesley",
		"Charlotte", "Navia", "Chevreuse", "Gaming", "Xianyun", "Chiori",
		"Arlecchino", "Clorinde", "Sigewinne", "Sethos", "Emilie", "Mualani",
		"Kinich", "Kachina", "Xilonen", "Chasca", "Ororon", "Mavuika", "Citlali",
		"Lanyan", "Mizuki", "Iansan", "Varesa", "Escoffier", "Ifa", "Skirk",
		"Dahlia", "Ineffa", "Flins", "Amber", "Kaeya", "Lisa", "Barbara",
		"Razor", "Fischl", "Ningguang", "Noelle", "Chongyun", "Sucrose",
		"Diona", "Beidou", "Xinyan", "Rosaria", "Yanfei", "Sayu", "Yoimiya",
		"Aloy", "Sara", "Thoma", "Gorou", "Itto", "Yunjin", "Shenhe", "YaeMiko",
		"Ayato", "Yaoyao", "Layla", "Faruzan", "Candace", "Collei", "Dori",
	}
	b := NewBrief("roster", "", "The roster on account 700123456",
		"Who is worth investing in next, and who is being carried by gear they should not have? Only judge what is listed here.")

	r := b.Add("Every character on the account")
	for i, n := range names {
		line := fmt.Sprintf("%s: level %d, C%d, talents %d/%d/%d, %d artifacts equipped",
			n, 80+(i%2)*10, i%7, 1+i%10, 1+(i*3)%10, 1+(i*5)%10, (i*3)%6)
		if i%5 == 0 {
			line += ", no weapon"
		} else {
			line += fmt.Sprintf(", holding Weapon%02d R%d", i%18, 1+i%5)
		}
		if i%4 == 0 {
			line += ", has a goal"
		} else {
			line += ", no goal set up"
		}
		r.Line(line)
	}

	a := b.Add("The account")
	a.Linef("%d characters, 88 weapons and 1240 artifacts have been imported.", len(names))
	a.Line("The inventory came from a .good export, so unequipped pieces are included.")

	m := b.Add("What Mimir can and cannot say here")
	m.Line("Nothing on this page has been through the damage engine: a character with no goal has no rotation, and without a rotation there is no number. Say what is worth setting up as a goal rather than claiming a gain.")
	return b
}

func TestLiveBigRoster(t *testing.T) {
	a := &Advisor{Client: liveClient(t), MaxTokens: liveBudget()}
	brief := bigRosterBrief()
	text := brief.Text()

	start := time.Now()
	got, err := a.Advise(context.Background(), brief)
	took := time.Since(start)
	if err != nil {
		t.Fatalf("after %s on a %d-byte brief: %v", took.Round(time.Second), len(text), err)
	}

	out, _ := json.MarshalIndent(got.Opinion, "", "  ")
	fmt.Printf("\n=== %s in %s on a %d-byte brief (attempts %d, %d tokens: %d in, %d out) ===\n%s\n",
		got.Model, took.Round(time.Second), len(text), got.Attempts,
		got.Usage.TotalTokens, got.Usage.PromptTokens, got.Usage.CompletionTokens, out)
	if len(got.Dropped) > 0 {
		fmt.Printf("--- dropped ---\n")
		for _, d := range got.Dropped {
			fmt.Printf("  %q  numbers: %v\n", d.Text, d.Numbers)
		}
	}
}

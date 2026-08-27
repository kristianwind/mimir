package advisor

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kristianwind/mimir/internal/calc"
	"github.com/kristianwind/mimir/internal/effect"
	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
	"github.com/kristianwind/mimir/internal/optimizer"
)

// Potential answers a different question from the plan.
//
// The plan asks "what should I spend tomorrow's resin on", and every number in
// it is measured against a rotation the player wrote down, ranked by damage per
// resin. That is the right shape for a daily decision and the wrong shape for
// "which of my characters is worth investing in at all" — because a character
// with no goal has no rotation, so the plan cannot see them, and because
// dividing by resin buries a large upgrade behind a cheap one.
//
// So this file measures the same builds against a **yardstick** instead, and
// ranks on the gain alone.
//
// The yardstick is stated rather than implied, because a damage number with no
// named conditions is not comparable to anything:
//
//	one cast of the elemental skill and one of the elemental burst,
//	at this character's talent levels, on this character's element,
//	against a level 90 enemy with 10% resistance.
//
// What that includes: base stats, artifacts and their set bonuses, weapon and
// its passive, constellations, talent levels, character level. Everything the
// effect layer resolves.
//
// What it deliberately leaves out, and the cost of leaving it out:
//
//   - **Normal attacks.** How many you land is the rotation question this
//     whole file exists to avoid. A character who lives on normals — Ayaka,
//     Itto — therefore scores below how they actually play.
//   - **Teams and reactions.** No Bennett, no vaporise. A character built to
//     react scores as their raw hit.
//   - **Attack-category bonuses.** A burst-only crit bonus does apply to the
//     burst cast, but nothing here weights a character by how much of their
//     damage is burst.
//
// It is a ruler, not a verdict. Two characters measured with it can be
// compared to each other; the number is not a claim about damage in play.
type Potential struct {
	Character string        `json:"character"`
	Element   model.Element `json:"element"`
	// Current is the yardstick score of what the character wears today.
	Current float64 `json:"current"`
	// Best is the score of the best arrangement of gear the account owns.
	Best float64 `json:"best"`
	// Headroom is Best/Current - 1: the damage already bought and not yet
	// equipped. It is the one number here that is free to collect.
	Headroom float64 `json:"headroom"`
	// Actions are everything that raises the score, ranked by gain alone.
	// Resin does not enter the ordering — see the file comment.
	Actions []Action `json:"actions"`
	// MeasuredOn names the casts the score is actually made of. It is not
	// always two: a support's burst deals no damage at all, and a score built
	// from one cast is not comparable to one built from two without saying so.
	MeasuredOn []string `json:"measuredOn,omitempty"`
	// Skipped names what could not be measured, and why.
	Skipped []string `json:"skipped,omitempty"`
}

// yardstickTarget is the enemy every score is measured against. Level 90
// rather than the plan's 100: this is a comparison between the player's own
// characters, and 90 is what the game's own damage numbers assume.
func yardstickTarget() calc.Target {
	res := map[model.Element]float64{}
	for _, e := range append([]model.Element(nil), model.Elements...) {
		res[e] = 0.10
	}
	res[model.Physical] = 0.10
	return calc.Target{Level: 90, Resistance: res}
}

// yardstickRotation builds the two casts the score is made of.
//
// The rows come from the mined talent tables at the character's real levels,
// so a talent upgrade moves the score — which is the point, and the reason the
// yardstick is not simply "one hit of 100% of your scaling stat". The
// highest-multiplier damage row of each talent stands for the cast: a skill
// with a press and a hold is represented by whichever hits harder.
func yardstickRotation(snap *gamedata.Snapshot, ch model.Character) (calc.Rotation, error) {
	def, err := snap.Char(ch.Key)
	if err != nil {
		return calc.Rotation{}, err
	}

	rot := calc.Rotation{Name: "yardstick"}
	for _, slot := range []string{"skill", "burst"} {
		talent, ok := def.Talents[slot]
		if !ok {
			continue
		}
		level := EffectiveTalentLevel(def, ch, slot)

		var best *calc.Instance
		for _, entry := range talent.Entries {
			if !entry.IsDamage() {
				continue
			}
			mult, err := entry.Multiplier(level)
			if err != nil || mult <= 0 {
				continue
			}
			element := entry.Element
			if element == "" {
				element = def.Element
			}
			inst := calc.Instance{
				Label:      slot + ": " + entry.Label,
				Element:    element,
				Category:   entry.Category,
				Scaling:    entry.Scaling,
				Multiplier: mult,
			}
			if best == nil || inst.Multiplier > best.Multiplier {
				b := inst
				best = &b
			}
		}
		if best != nil {
			rot.Instances = append(rot.Instances, *best)
		}
	}

	if len(rot.Instances) == 0 {
		return calc.Rotation{}, fmt.Errorf(
			"advisor: %s has no mined damage rows on skill or burst, so there is nothing to measure",
			ch.Key)
	}
	return rot, nil
}

// yardstickEvaluator scores a state without a goal.
//
// It mirrors EngineEvaluator — same stat resolution, same effect layer, same
// instance folding — and differs only in where the rotation comes from. Any
// divergence between the two would mean the potential view and the plan
// disagree about the same build, which is worse than either being wrong.
type yardstickEvaluator struct {
	Snapshot   *gamedata.Snapshot
	Conditions map[string]float64
}

func (y yardstickEvaluator) Score(ctx context.Context, _ Goal, s State) (float64, error) {
	bonuses := s.Fixed.Clone()
	for _, a := range s.Equipped {
		st, ok := s.ArtifactStats[a.ID]
		if !ok {
			return 0, fmt.Errorf("advisor: no stats for artifact %d", a.ID)
		}
		bonuses.AddInto(st)
	}
	counts := optimizer.SetCounts(s.Equipped)
	setBonus, err := optimizer.SetBonuses(counts, y.Snapshot)
	if err != nil {
		return 0, err
	}
	bonuses.AddInto(setBonus)

	ectx := effect.Context{
		Snapshot:   y.Snapshot,
		Character:  s.Character,
		SetCounts:  counts,
		WeaponKey:  s.WeaponKey,
		Refinement: s.WeaponRefinement,
		Conditions: y.Conditions,
	}
	totals, err := resolveWithEffects(y.Snapshot, s.Base, bonuses, ectx)
	if err != nil {
		return 0, err
	}

	rot, err := yardstickRotation(y.Snapshot, s.Character)
	if err != nil {
		return 0, err
	}
	rot, _, err = withEffectInstances(y.Snapshot, ectx, rot, totals, defElement(y.Snapshot, s.Character.Key))
	if err != nil {
		return 0, err
	}

	res, err := calc.Evaluate(s.Character.Level, totals, rot, yardstickTarget().WithDebuffs(totals))
	if err != nil {
		return 0, err
	}
	return res.Total, nil
}

// PotentialRequest is one character's assessment input.
type PotentialRequest struct {
	Snapshot *gamedata.Snapshot
	Loadout  Loadout
	// Inventory is the whole account's artifacts: a re-equip may pull from
	// anywhere, and that is the largest free gain on most accounts.
	Inventory []model.Artifact
	Weapons   []model.Weapon
	// Conditions are the player's declared answers, if the character happens
	// to have a goal carrying some. Absent is fine — an undeclared condition
	// is reported, not guessed.
	Conditions    map[string]float64
	MaxSetConfigs int
	// SearchBudget caps how many complete builds each set configuration's
	// search examines. Zero takes DefaultSearchBudget; negative means no cap.
	SearchBudget int
}

// Assess measures one character and lists everything that would raise the
// score, ranked by gain.
func Assess(ctx context.Context, req PotentialRequest) (Potential, error) {
	if req.Snapshot == nil {
		return Potential{}, fmt.Errorf("advisor: no game data snapshot")
	}
	base, err := Assemble(req.Snapshot, req.Loadout)
	if err != nil {
		return Potential{}, err
	}
	eval := yardstickEvaluator{Snapshot: req.Snapshot, Conditions: req.Conditions}

	current, err := eval.Score(ctx, Goal{}, base)
	if err != nil {
		return Potential{}, err
	}
	if current <= 0 {
		return Potential{}, fmt.Errorf("advisor: %s scores zero on the yardstick", req.Loadout.Character.Key)
	}

	out := Potential{
		Character: req.Loadout.Character.Key,
		Element:   defElement(req.Snapshot, req.Loadout.Character.Key),
		Current:   current,
		Best:      current,
	}
	if rot, err := yardstickRotation(req.Snapshot, base.Character); err == nil {
		for _, inst := range rot.Instances {
			out.MeasuredOn = append(out.MeasuredOn, inst.Label)
		}
	}
	add := func(a Action) {
		if a.GainPct > 0.0001 {
			out.Actions = append(out.Actions, a)
		}
	}
	skip := func(format string, args ...any) {
		out.Skipped = append(out.Skipped, fmt.Sprintf(format, args...))
	}

	// The goal-shaped generators want a Goal, and it has to carry the
	// yardstick as a rotation rather than be left empty.
	//
	// An empty spec builds an empty rotation, and the artifact objective sums
	// damage over its instances — so it returns zero for every build ever
	// offered to it. The search then "optimises" against a flat function and
	// keeps whichever arrangement it happened to look at first. The gain
	// reported afterwards is real, because it is measured with the yardstick;
	// the build it is measured on is arbitrary. That is the worst kind of
	// wrong number: a true measurement of a meaningless thing.
	spec, err := DeriveSpec(req.Snapshot, base.Character)
	if err != nil {
		return Potential{}, fmt.Errorf("advisor: %s has nothing to measure: %w",
			req.Loadout.Character.Key, err)
	}

	greq := Request{
		Snapshot:      req.Snapshot,
		Goal:          Goal{CharacterKey: req.Loadout.Character.Key, Spec: spec, Conditions: req.Conditions},
		Loadout:       req.Loadout,
		Inventory:     req.Inventory,
		Weapons:       req.Weapons,
		MaxSetConfigs: req.MaxSetConfigs,
		SearchBudget:  req.SearchBudget,
	}
	if greq.MaxSetConfigs <= 0 {
		greq.MaxSetConfigs = 8
	}

	if a, err := reequipCandidate(ctx, greq, eval, base, current); err != nil {
		skip("the best arrangement of your artifacts could not be searched: %v", err)
	} else if a != nil {
		add(*a)
		out.Best = current * (1 + a.GainPct)
	}

	levels, err := artifactLevelCandidates(ctx, req, eval, base, current)
	if err != nil {
		skip("artifact levelling could not be priced: %v", err)
	}
	for _, a := range levels {
		add(a)
	}

	talents, err := talentCandidates(ctx, greq, eval, base, current)
	if err != nil {
		skip("talent upgrades could not be priced: %v", err)
	}
	for _, a := range talents {
		add(a)
	}

	if a, err := levelCandidate(ctx, greq, eval, base, current); err != nil {
		skip("the level upgrade could not be priced: %v", err)
	} else if a != nil {
		add(*a)
	}

	weapons, err := weaponCandidates(ctx, greq, eval, base, current)
	if err != nil {
		skip("weapon swaps could not be priced: %v", err)
	}
	for _, a := range weapons {
		add(a)
	}

	for _, m := range effect.Undeclared(req.Snapshot.Effects, effect.Context{
		Snapshot:   req.Snapshot,
		Character:  base.Character,
		SetCounts:  optimizer.SetCounts(base.Equipped),
		WeaponKey:  base.WeaponKey,
		Refinement: base.WeaponRefinement,
		Conditions: req.Conditions,
	}) {
		skip("%s counts as switched off: nobody has answered %q", m.Source, m.Key)
	}

	// A support's burst deals no damage, so the score is one cast rather than
	// two. Saying so matters more than it looks: without it the character
	// simply appears weak, when what happened is that half the ruler found
	// nothing to measure.
	if len(out.MeasuredOn) < 2 {
		skip("measured on %s alone — the other ability has no damage rows in the game's own tables, which usually means it buffs or heals rather than hits",
			strings.Join(out.MeasuredOn, " and "))
	}

	out.Headroom = out.Best/out.Current - 1
	out.Actions = RankByGain(out.Actions)
	return out, nil
}

// RankByGain orders actions by how much they raise the score, and by nothing
// else.
//
// Rank() puts free actions first and divides by resin, which is right for a
// daily plan and wrong here: this view answers "where is the most damage",
// and a talent book costing resin does not make its 8% worth less than a
// re-equip's 3%. Blocked actions still trail, because an action that cannot be
// carried out is not an answer to "what should I do".
func RankByGain(actions []Action) []Action {
	out := append([]Action(nil), actions...)
	for i := range out {
		out[i].Free = !out[i].Unpriced && out[i].ResinCost <= 0 && out[i].GainPct > 0
		out[i].Efficiency = efficiency(out[i])
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.BlockedBy == "") != (b.BlockedBy == "") {
			return a.BlockedBy == ""
		}
		return a.GainPct > b.GainPct
	})
	return out
}

// ---------------------------------------------------------------- levelling

// KindArtifact is levelling a piece the player already owns.
//
// It is its own kind because it is the one upgrade that costs no resin and no
// domain run — artifact experience comes out of the pile of junk everyone
// already has — and because a plan that only ever says "farm a better sands"
// misses that the sands on the character is at +8.
const KindArtifact Kind = "artifact"

// maxArtifactLevel is read from the mined main-stat curve rather than assumed.
// Five-star pieces cap at +20 and four-stars at +16, but that is a fact about
// the game, and the table already carries it: the curve has one entry per
// level, so its length is the cap.
func maxArtifactLevel(snap *gamedata.Snapshot, a model.Artifact) (int, error) {
	byRarity, ok := snap.MainStatValues[a.Rarity]
	if !ok {
		return 0, fmt.Errorf("%w: main stat table for %d★ artifacts", gamedata.ErrMissing, a.Rarity)
	}
	curve, ok := byRarity[a.MainStat]
	if !ok {
		return 0, fmt.Errorf("%w: main stat %q on %d★", gamedata.ErrMissing, a.MainStat, a.Rarity)
	}
	return len(curve) - 1, nil
}

// artifactLevelCandidates prices taking each equipped piece to its cap.
//
// Only the main stat is projected. A piece gains a substat roll every four
// levels, and which stat it lands on is the whole reason artifact farming is a
// distribution rather than a purchase — so the gain reported here is the main
// stat's growth alone, and the action says so. Under-reporting is the right
// direction: it never talks anybody into levelling a piece on a promise the
// game did not make.
func artifactLevelCandidates(
	ctx context.Context, req PotentialRequest, eval Evaluator, base State, current float64,
) ([]Action, error) {
	var out []Action

	for _, piece := range base.Equipped {
		cap, err := maxArtifactLevel(req.Snapshot, piece)
		if err != nil {
			return nil, err
		}
		if piece.Level >= cap {
			continue
		}

		levelled := piece
		levelled.Level = cap
		block, err := optimizer.ArtifactStats(levelled, req.Snapshot)
		if err != nil {
			return nil, err
		}

		candidate := cloneState(base)
		candidate.ArtifactStats[piece.ID] = block
		score, err := eval.Score(ctx, Goal{}, candidate)
		if err != nil {
			return nil, err
		}

		gain := Gain(current, score)
		if gain <= 0 {
			continue
		}
		out = append(out, Action{
			Kind:    KindArtifact,
			Subject: req.Loadout.Character.Key,
			Headline: fmt.Sprintf("Level the %s %s from +%d to +%d",
				piece.SetKey, piece.SlotKey, piece.Level, cap),
			GainPct: gain,
			Note: fmt.Sprintf(
				"main stat only: the %d substat rolls it gains on the way are not projected, because which stats they land on is unknown",
				(cap-piece.Level)/4),
			Detail: map[string]any{
				"artifactId": piece.ID,
				"set":        piece.SetKey,
				"slot":       string(piece.SlotKey),
				"from":       piece.Level,
				"to":         cap,
				"mainStat":   string(piece.MainStat),
			},
		})
	}
	return out, nil
}

// ---------------------------------------------------------------- the roster

// Ranking is every owned character measured with the same ruler.
type Ranking struct {
	Characters []Ranked `json:"characters"`
	// Caveats state what the ruler does not measure. They are part of the
	// answer: a ranking with no stated limits invites being read as a verdict
	// on who is worth playing, which is not what it is.
	Caveats []string `json:"caveats"`
	// Skipped names characters that could not be measured at all.
	Skipped []string `json:"skipped,omitempty"`
}

// Ranked is one character's place in it.
type Ranked struct {
	Potential
	// TopGain is the damage the single best action would add, in yardstick
	// points. It is what the ranking sorts on: the question is "where does one
	// upgrade buy the most", and one upgrade is a thing a person can go and do.
	TopGain float64 `json:"topGain"`
	// TopAction is that upgrade.
	TopAction *Action `json:"topAction,omitempty"`
	// FreeGain is Best - Current: damage already owned and not equipped.
	FreeGain float64 `json:"freeGain"`
}

// AccountPotential measures every character and ranks them.
//
// The actions within a character are never summed. Re-equipping and levelling
// the piece that was replaced overlap, and adding two overlapping gains would
// invent damage that does not exist — the same reason the plan prices one
// arrangement rather than a shopping list. So the ranking uses the largest
// single action, and the free re-equip is reported alongside it because that
// one is combined properly by the optimizer.
func AccountPotential(ctx context.Context, reqs []PotentialRequest) (Ranking, error) {
	out := Ranking{
		Caveats: []string{
			"Every character is measured the same way: one cast of their elemental skill and one of their burst, at their own talent levels, against a level 90 enemy with 10% resistance.",
			"Normal attacks, teams and reactions are not in it. A character who lives on normal attacks, or who exists to enable somebody else, scores below how they actually play.",
			"Actions are not added together: re-equipping and levelling the piece it replaced overlap, so the ranking uses the largest single upgrade rather than a sum.",
			"Resin is not in the ordering. A talent book that costs resin is ranked above a free rearrangement when it buys more damage.",
			"Each character is measured as if it could take any piece on the account, so two characters' best builds may want the same artifact. The account plan is where that fight gets resolved; this ranking does not resolve it.",
			"The order is by damage added, not by how far behind a character is — so the same weapon upgrade ranks higher on an already-strong build than on a neglected one. That is what \"most value from the account\" means, but it is not what \"who needs attention\" means: headroom is the column for that.",
		},
	}

	// Each character is measured independently — the yardstick is the whole
	// point — so they are measured at the same time. The results are written
	// into a slice by index rather than appended, because a ranking that
	// came out in a different order on every request would be a different
	// answer to the same question.
	type outcome struct {
		ranked Ranked
		err    error
	}
	results := make([]outcome, len(reqs))

	// The whole roster shares one budget. Measuring forty characters must
	// not cost forty times what measuring one costs, or the feature stops
	// working on exactly the accounts it was built for.
	each := shareBudget(AccountSearchBudget, len(reqs))
	for i := range reqs {
		if reqs[i].SearchBudget == 0 {
			reqs[i].SearchBudget = each
		}
	}

	workers := runtime.NumCPU() - 1
	if workers < 1 {
		workers = 1
	}
	if workers > len(reqs) {
		workers = len(reqs)
	}

	var (
		wg   sync.WaitGroup
		next atomic.Int64
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(reqs) {
					return
				}
				p, err := Assess(ctx, reqs[i])
				if err != nil {
					results[i] = outcome{err: err}
					continue
				}
				r := Ranked{Potential: p, FreeGain: p.Best - p.Current}
				for j := range p.Actions {
					if p.Actions[j].BlockedBy != "" {
						continue
					}
					if gain := p.Current * p.Actions[j].GainPct; gain > r.TopGain {
						r.TopGain = gain
						r.TopAction = &p.Actions[j]
					}
				}
				results[i] = outcome{ranked: r}
			}
		}()
	}
	wg.Wait()

	for i, res := range results {
		if res.err != nil {
			out.Skipped = append(out.Skipped,
				fmt.Sprintf("%s: %v", reqs[i].Loadout.Character.Key, res.err))
			continue
		}
		out.Characters = append(out.Characters, res.ranked)
	}

	if len(out.Characters) == 0 {
		return out, fmt.Errorf("advisor: none of the characters could be measured")
	}

	sort.SliceStable(out.Characters, func(i, j int) bool {
		if out.Characters[i].TopGain != out.Characters[j].TopGain {
			return out.Characters[i].TopGain > out.Characters[j].TopGain
		}
		return out.Characters[i].Current > out.Characters[j].Current
	})
	return out, nil
}

// ---------------------------------------------------------------- derivation

// DeriveSpec writes the yardstick down as a rotation.
//
// It is the same two casts the score is made of, so a goal created from it
// ranks the character exactly as the potential view did. That is the only
// honest way to turn a ranking into goals: anything else would mean the
// numbers change the moment the goal exists.
//
// It is still a guess about how the character is played, and a guess is not
// what the plan is built on — every gain in the plan is measured against this
// rotation, so a wrong one is wrong all the way down. Goals made from it are
// therefore stored as derived rather than authored, and the difference is
// shown wherever their numbers appear. The player is expected to open it and
// say what they actually press.
func DeriveSpec(snap *gamedata.Snapshot, ch model.Character) (Spec, error) {
	def, err := snap.Char(ch.Key)
	if err != nil {
		return Spec{}, err
	}
	rot, err := yardstickRotation(snap, ch)
	if err != nil {
		return Spec{}, err
	}

	spec := Spec{Name: "derived", Duration: 20}
	for _, inst := range rot.Instances {
		slot, label, ok := splitYardstickLabel(inst.Label)
		if !ok {
			continue
		}
		// The label has to name a row that exists, or the goal will not save.
		if _, err := def.TalentEntry(slot, label); err != nil {
			return Spec{}, err
		}
		spec.Steps = append(spec.Steps, Step{Talent: slot, Entry: label, Hits: 1})
	}
	if len(spec.Steps) == 0 {
		return Spec{}, fmt.Errorf("advisor: no talent rows to derive a rotation for %s from", ch.Key)
	}
	return spec, nil
}

// splitYardstickLabel undoes the "slot: label" the yardstick builds.
func splitYardstickLabel(s string) (slot, label string, ok bool) {
	for _, prefix := range []string{"skill: ", "burst: "} {
		if len(s) > len(prefix) && s[:len(prefix)] == prefix {
			return prefix[:len(prefix)-2], s[len(prefix):], true
		}
	}
	return "", "", false
}

// The search budgets.
//
// These are wall-clock decisions, not quality ones, and they are stated in
// the only unit that predicts wall clock: how many complete builds the
// objective is asked to score. One evaluation runs the whole damage pipeline
// and costs a few microseconds, so a budget translates directly into seconds.
//
// They exist because the optimiser's upper bound barely prunes. A four-piece
// set on a full inventory offers something like 600 million arrangements, and
// an exhaustive search of a forty-character roster would run for hours —
// which reaches the player as a page that hangs and then dies in a proxy,
// with nothing to read. The pool is ordered best-piece-first, so a capped
// search spends what it has on the arrangements most likely to win; what it
// gives up is the proof that nothing better exists, and that is reported
// rather than absorbed.
const (
	// DefaultSearchBudget is for one character, where a few seconds of
	// searching is time well spent.
	DefaultSearchBudget = 400_000
	// AccountSearchBudget is shared across every character in a roster-wide
	// request. It is divided, not applied each time: a forty-character
	// account must not cost forty times as long as a one-character one.
	AccountSearchBudget = 3_000_000
	// MinSearchBudget is the floor each character keeps however large the
	// roster grows. Below this the search stops being a search.
	MinSearchBudget = 20_000
)

// shareBudget divides a roster-wide budget between the characters in it.
func shareBudget(total, characters int) int {
	if characters < 1 {
		return total
	}
	each := total / characters
	if each < MinSearchBudget {
		return MinSearchBudget
	}
	return each
}

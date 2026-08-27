package advisor

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/kristianwind/mimir/internal/calc"
	"github.com/kristianwind/mimir/internal/effect"
	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
	"github.com/kristianwind/mimir/internal/optimizer"
)

// Request is everything needed to rank one goal's upgrades.
type Request struct {
	Snapshot *gamedata.Snapshot
	Goal     Goal
	// Loadout is what the character wears today. The baseline is measured
	// from it, so every gain is relative to the build the player actually
	// has — not to a theoretical one.
	Loadout Loadout
	// Inventory is the whole account's artifacts, not just the equipped
	// ones. Free re-equips are found here.
	Inventory []model.Artifact
	// Weapons is the account's owned weapons, for swap candidates.
	Weapons []model.Weapon
	// Constraints are the build's hard requirements, typically an energy
	// recharge floor.
	Constraints []optimizer.Constraint
	// MaxSetConfigs caps how many artifact set arrangements to search.
	MaxSetConfigs int
	// SearchBudget caps how many complete builds each set configuration's
	// search will examine. Zero takes DefaultSearchBudget; a negative value
	// means no cap at all.
	//
	// It exists because the optimiser's upper bound is weak enough that a
	// large inventory turns the search into a brute force over millions of
	// combinations — a request that never returns. A cap turns that into an
	// answer that arrives and admits what it did not look at.
	SearchBudget int
	// FarmDays is the horizon for domain farming candidates.
	FarmDays int
	// ResinPerDay is the player's daily budget. 180 is the standard refresh.
	ResinPerDay float64
	// Sim estimates artifact farming. Nil disables those candidates.
	Sim *FarmSim
}

// BuildPlan generates and ranks every upgrade candidate for one goal.
//
// The generators are deliberately independent and each one either produces a
// number from the engine or produces nothing. There is no heuristic tier: an
// upgrade Mimir cannot price does not appear in the plan with a guessed
// value, it appears in Skipped with the reason.
func BuildPlan(ctx context.Context, req Request) (Plan, error) {
	if req.Snapshot == nil {
		return Plan{}, fmt.Errorf("advisor: no game data snapshot")
	}
	eval := EngineEvaluator{Snapshot: req.Snapshot}

	base, err := Assemble(req.Snapshot, req.Loadout)
	if err != nil {
		return Plan{}, err
	}
	baseline, err := eval.Score(ctx, req.Goal, base)
	if err != nil {
		return Plan{}, err
	}
	if baseline <= 0 {
		return Plan{}, fmt.Errorf("advisor: the rotation produces no damage; check the goal's steps")
	}

	plan := Plan{Goal: req.Goal.CharacterKey, Baseline: baseline}
	add := func(a Action) {
		if a.GainPct > 0.0001 {
			plan.Actions = append(plan.Actions, a)
		}
	}
	skip := func(format string, args ...any) {
		plan.Skipped = append(plan.Skipped, fmt.Sprintf(format, args...))
	}

	if a, err := reequipCandidate(ctx, req, eval, base, baseline); err != nil {
		skip("could not search for a better combination of your artifacts: %v", err)
	} else if a != nil {
		add(*a)
	}

	talents, err := talentCandidates(ctx, req, eval, base, baseline)
	if err != nil {
		skip("could not price talent upgrades: %v", err)
	}
	for _, a := range talents {
		add(a)
	}

	if a, err := levelCandidate(ctx, req, eval, base, baseline); err != nil {
		skip("could not price the level upgrade: %v", err)
	} else if a != nil {
		add(*a)
	}

	weapons, err := weaponCandidates(ctx, req, eval, base, baseline)
	if err != nil {
		skip("could not price weapon swaps: %v", err)
	}
	for _, a := range weapons {
		add(a)
	}

	farms, reason := farmCandidates(ctx, req, eval, base)
	if reason != "" {
		skip("%s", reason)
	}
	for _, a := range farms {
		add(a)
	}

	// A conditional set bonus that is off because nobody was asked reads, in
	// a ranking, exactly like a set bonus that does not exist.
	for _, m := range effect.Undeclared(req.Snapshot.Effects, effect.Context{
		Snapshot:   req.Snapshot,
		Character:  base.Character,
		SetCounts:  optimizer.SetCounts(base.Equipped),
		WeaponKey:  base.WeaponKey,
		Refinement: base.WeaponRefinement,
		Conditions: req.Goal.Conditions,
	}) {
		skip("%s counts as switched off: set the condition %q on the goal", m.Source, m.Key)
	}

	plan.Actions = Rank(plan.Actions)
	return plan, nil
}

// Plan is a ranked set of actions plus what could not be priced.
type Plan struct {
	Goal     string   `json:"goal"`
	Baseline float64  `json:"baseline"`
	Actions  []Action `json:"actions"`
	// Skipped names the candidates Mimir declined to price, and why. It is
	// part of the output rather than a log line: a plan that silently omits
	// artifact farming looks like a plan that says farming is worthless.
	Skipped []string `json:"skipped,omitempty"`
}

// ---------------------------------------------------------------- re-equip

// reequipCandidate searches the account's own artifacts for a better
// arrangement. It costs nothing, which is why it leads every plan it appears
// in — and on a real account it very often appears.
func reequipCandidate(
	ctx context.Context, req Request, eval Evaluator, base State, baseline float64,
) (*Action, error) {
	if len(req.Inventory) == 0 {
		return nil, nil
	}
	maxConfigs := req.MaxSetConfigs
	if maxConfigs <= 0 {
		maxConfigs = 12
	}
	// The cap is applied here rather than left to the caller. Forgetting it
	// does not produce a slightly slower answer, it produces a request that
	// never returns — and that failure reaches the player as a dead page
	// several minutes later, from a proxy, with nothing to read.
	budget := req.SearchBudget
	if budget == 0 {
		budget = DefaultSearchBudget
	}
	if budget < 0 {
		budget = 0 // an explicit "search everything, however long it takes"
	}
	configs := optimizer.EnumerateSetConfigs(req.Inventory, maxConfigs)
	if len(configs) == 0 {
		return nil, nil
	}

	scored := func(pieces []model.Artifact) (float64, error) {
		candidate := base
		candidate.Equipped = pieces
		candidate.ArtifactStats = base.ArtifactStats
		return eval.Score(ctx, req.Goal, candidate)
	}

	statsByID := make(map[int64]model.StatBlock, len(req.Inventory))
	for _, a := range req.Inventory {
		block, err := optimizer.ArtifactStats(a, req.Snapshot)
		if err != nil {
			return nil, err
		}
		statsByID[a.ID] = block
	}
	base.ArtifactStats = statsByID

	res, err := optimizer.BestBuild(ctx, req.Snapshot, req.Inventory, base.Fixed,
		req.Constraints, configs, func(cfg optimizer.SetConfig) optimizer.Objective {
			return statObjective(req, base, cfg)
		}, budget)
	if err != nil {
		return nil, err
	}

	improved, err := scored(res.Best.Pieces)
	if err != nil {
		return nil, err
	}
	gain := Gain(baseline, improved)
	if gain <= 0 {
		return nil, nil
	}

	action := Action{
		Kind:     KindReequip,
		Subject:  req.Goal.CharacterKey,
		Headline: fmt.Sprintf("Switch to %s on %s", res.BestConfig, req.Goal.CharacterKey),
		GainPct:  gain,
		Detail: map[string]any{
			"config": res.BestConfig.String(),
			"pieces": res.Best.Pieces,
		},
	}
	// A capped search found the best it could reach, not the best that
	// exists. Saying so is the difference between a result and a claim.
	if len(res.Capped) > 0 {
		action.Note = "the search was capped, so a better arrangement may exist"
	}
	if taken := takenFrom(res.Best.Pieces, req.Goal.CharacterKey); len(taken) > 0 {
		action.Note = fmt.Sprintf("Takes pieces from %s", joinNames(taken))
		action.Detail["takenFrom"] = taken
	}
	return &action, nil
}

// statObjective adapts the engine to the optimizer's monotone-objective
// contract by scoring a candidate's bonus block through the real rotation.
//
// Two things it must get right, and both were wrong in an earlier version:
//
// The optimizer hands the objective a *bonus* block, not resolved totals, so
// base stats have to be folded in here. Scoring the raw bonuses treats a
// character as if they had no base ATK, which systematically undervalues
// every ATK% roll against flat ATK.
//
// And a configuration's conditional effects belong in the score. 4pc Emblem
// without its Energy-Recharge-to-Burst conversion is just two pieces of
// Energy Recharge, and would lose to sets that are genuinely worse.
//
// The rotation's multipliers do not depend on the artifacts, and every step
// here is monotone non-decreasing in the stats it reads, which is what the
// branch-and-bound's upper bound requires.
func statObjective(req Request, base State, cfg optimizer.SetConfig) optimizer.Objective {
	rot, err := BuildRotation(req.Snapshot, base.Character, req.Goal.Spec)
	if err != nil {
		return func(model.StatBlock) float64 { return 0 }
	}
	ectx := effect.Context{
		Snapshot:   req.Snapshot,
		Character:  base.Character,
		SetCounts:  cfg.Counts(),
		WeaponKey:  weaponKey(req.Loadout.Weapon),
		Refinement: weaponRefinement(req.Loadout.Weapon),
		Conditions: req.Goal.Conditions,
	}

	element := defElement(req.Snapshot, base.Character.Key)

	return func(bonuses model.StatBlock) float64 {
		totals, err := resolveWithEffects(req.Snapshot, base.Base, bonuses, ectx)
		if err != nil {
			return 0
		}
		// Effect-added hits belong in the objective too: a set whose value
		// is an extra explosion would otherwise score as if it did nothing.
		full, _, err := withEffectInstances(req.Snapshot, ectx, rot, totals, element)
		if err != nil {
			return 0
		}

		tgt := req.Goal.Target.WithDebuffs(totals)
		var total float64
		for _, inst := range full.Instances {
			total += calc.Damage(base.Character.Level, totals, inst, tgt).Average
		}
		return total
	}
}

func weaponKey(w *model.Weapon) string {
	if w == nil {
		return ""
	}
	return w.Key
}

func weaponRefinement(w *model.Weapon) int {
	if w == nil || w.Refinement < 1 {
		return 1
	}
	return w.Refinement
}

// ---------------------------------------------------------------- talents

func talentCandidates(
	ctx context.Context, req Request, eval Evaluator, base State, baseline float64,
) ([]Action, error) {
	def, err := req.Snapshot.Char(req.Goal.CharacterKey)
	if err != nil {
		return nil, err
	}
	var out []Action

	for _, slot := range []string{gamedata.TalentAuto, gamedata.TalentSkill, gamedata.TalentBurst} {
		level, err := talentLevel(base.Character, slot)
		if err != nil {
			return nil, err
		}
		if level >= 10 {
			continue
		}

		candidate := base
		candidate.Character = withTalent(base.Character, slot, level+1)

		improved, err := eval.Score(ctx, req.Goal, candidate)
		if err != nil {
			// A rotation that does not touch this talent simply gains
			// nothing from it; that is an answer, not a failure.
			continue
		}
		gain := Gain(baseline, improved)
		if gain <= 0 {
			continue
		}

		action := Action{
			Kind:     KindTalent,
			Subject:  fmt.Sprintf("%s %s", req.Goal.CharacterKey, slot),
			Headline: fmt.Sprintf("%s: %s %d → %d", req.Goal.CharacterKey, slotLabel(slot), level, level+1),
			GainPct:  gain,
			Detail:   map[string]any{"slot": slot, "from": level, "to": level + 1},
		}
		bill, known := def.TalentBill(slot, level+1)
		applyBill(&action, req.Snapshot, def, bill, known)
		out = append(out, action)
	}
	return out, nil
}

func withTalent(c model.Character, slot string, level int) model.Character {
	switch slot {
	case gamedata.TalentAuto:
		c.TalentAuto = level
	case gamedata.TalentSkill:
		c.TalentSkill = level
	case gamedata.TalentBurst:
		c.TalentBurst = level
	}
	return c
}

func slotLabel(slot string) string {
	switch slot {
	case gamedata.TalentAuto:
		return "normal attack"
	case gamedata.TalentSkill:
		return "elemental skill"
	case gamedata.TalentBurst:
		return "elemental burst"
	default:
		return slot
	}
}

// ---------------------------------------------------------------- level

// levelCandidate prices taking the character to the next ascension cap.
//
// It is included precisely because the answer is so often "don't bother":
// levelling a support from 80 to 90 is a common way to spend a week of
// resources on a fraction of a percent, and the plan should say so.
func levelCandidate(
	ctx context.Context, req Request, eval Evaluator, base State, baseline float64,
) (*Action, error) {
	caps := []int{20, 40, 50, 60, 70, 80, 90}
	current := base.Character.Level
	if current >= 90 {
		return nil, nil
	}

	next := 90
	ascension := base.Character.Ascension
	for i, c := range caps {
		if current < c {
			next = c
			ascension = i + 1
			break
		}
	}
	if ascension > 6 {
		ascension = 6
	}

	candidate := base
	candidate.Character.Level = next
	candidate.Character.Ascension = ascension

	// Base stats change with level, so the loadout has to be re-assembled
	// rather than just re-scored.
	loadout := req.Loadout
	loadout.Character = candidate.Character
	reassembled, err := Assemble(req.Snapshot, loadout)
	if err != nil {
		return nil, err
	}

	improved, err := eval.Score(ctx, req.Goal, reassembled)
	if err != nil {
		return nil, err
	}
	gain := Gain(baseline, improved)
	if gain <= 0 {
		return nil, nil
	}

	def, err := req.Snapshot.Char(req.Goal.CharacterKey)
	if err != nil {
		return nil, err
	}
	action := Action{
		Kind:    KindAscend,
		Subject: req.Goal.CharacterKey,
		Headline: fmt.Sprintf("%s: level %d → %d (ascension %d)",
			req.Goal.CharacterKey, current, next, ascension),
		GainPct: gain,
		Detail:  map[string]any{"from": current, "to": next, "ascension": ascension},
	}
	bill, known := def.AscensionBill(ascension)
	applyBill(&action, req.Snapshot, def, bill, known)
	return &action, nil
}

// ---------------------------------------------------------------- weapons

// weaponCandidates checks every owned weapon of the right type. This is free
// damage sitting in the inventory, and it is the second most common untapped
// gain after re-equipping artifacts.
func weaponCandidates(
	ctx context.Context, req Request, eval Evaluator, base State, baseline float64,
) ([]Action, error) {
	if len(req.Weapons) == 0 {
		return nil, nil
	}
	def, err := req.Snapshot.Char(req.Goal.CharacterKey)
	if err != nil {
		return nil, err
	}

	var best *Action
	for i := range req.Weapons {
		w := req.Weapons[i]
		if req.Loadout.Weapon != nil && w.ID == req.Loadout.Weapon.ID {
			continue
		}
		wd, ok := req.Snapshot.Weapons[w.Key]
		if !ok || wd.Type != def.WeaponType {
			continue
		}

		loadout := req.Loadout
		loadout.Weapon = &w
		candidate, err := Assemble(req.Snapshot, loadout)
		if err != nil {
			continue
		}
		improved, err := eval.Score(ctx, req.Goal, candidate)
		if err != nil {
			continue
		}
		gain := Gain(baseline, improved)
		if gain <= 0 {
			continue
		}
		if best == nil || gain > best.GainPct {
			action := Action{
				Kind:     KindWeapon,
				Subject:  w.Key,
				Headline: fmt.Sprintf("Give %s the weapon %s (R%d)", req.Goal.CharacterKey, wd.Name, w.Refinement),
				GainPct:  gain,
				Detail: map[string]any{
					"weapon": w.Key, "refinement": w.Refinement,
					"level": w.Level, "takenFrom": w.Location,
				},
			}
			// A weapon sitting on somebody else is not free damage, it is a
			// transfer. Saying so is the difference between advice you can
			// act on and advice that quietly breaks another build.
			if w.Location != "" {
				action.Note = fmt.Sprintf("Currently on %s", w.Location)
			}
			best = &action
		}
	}
	if best == nil {
		return nil, nil
	}
	return []Action{*best}, nil
}

// ---------------------------------------------------------------- farming

// farmPieceHorizon is how many five-star drops a farming candidate simulates
// when the per-run yield is unknown. A hundred is roughly a week of a single
// domain for most players, which keeps the figure comparable to the seven-day
// horizon used when resin *is* known.
const farmPieceHorizon = 100

func farmCandidates(ctx context.Context, req Request, eval Evaluator, base State) ([]Action, string) {
	if req.Sim == nil {
		return nil, "artifact farming is not priced: the drop model is missing. " +
			"Upload a .good file with your whole inventory, and it is measured on your own artifacts."
	}
	if len(req.Snapshot.Domains) == 0 {
		return nil, "artifact farming is not priced: the domains are not synced"
	}
	days := req.FarmDays
	if days <= 0 {
		days = 7
	}
	resinPerDay := req.ResinPerDay
	if resinPerDay <= 0 {
		resinPerDay = 180
	}

	// Only domains that drop a set this character is actually building
	// toward. Farming a domain for a set the optimizer would never equip is
	// not an upgrade path, however good the artifacts are.
	wanted := map[string]bool{}
	for _, cfg := range optimizer.EnumerateSetConfigs(req.Inventory, req.MaxSetConfigs) {
		for _, key := range []string{cfg.Four, cfg.TwoA, cfg.TwoB} {
			if key != "" {
				wanted[key] = true
			}
		}
	}

	// Sorted, because the plan is meant to be reproducible: two domains that
	// score identically would otherwise swap places between runs on nothing
	// but map iteration order, and the player would be told to farm a
	// different place for the same reason as yesterday.
	keys := make([]string, 0, len(req.Snapshot.Domains))
	for key := range req.Snapshot.Domains {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var out []Action
	for _, key := range keys {
		domain := req.Snapshot.Domains[key]
		if domain.Kind != "artifact" {
			continue
		}
		var relevant bool
		for _, set := range domain.Sets {
			if wanted[set] {
				relevant = true
			}
		}
		if !relevant {
			continue
		}

		action := Action{
			Kind:    KindFarm,
			Subject: key,
		}

		var est FarmEstimate
		runs := 0
		if domain.ResinCost > 0 {
			runs = int(float64(days) * resinPerDay / domain.ResinCost)
		}

		e, err := req.Sim.Estimate(ctx, req.Goal, base, domain, runs, eval)
		switch {
		case err == nil:
			est = e
			action.Headline = fmt.Sprintf("Farm %s for %d days", domain.Name, days)
			action.ResinCost = est.ResinCost
		default:
			// No measured per-run yield, so the plan ranks this domain in
			// artifacts examined instead of resin. That still compares
			// domains against each other honestly; it just cannot compare
			// them against a talent upgrade.
			e, perr := req.Sim.EstimatePieces(ctx, req.Goal, base, domain, farmPieceHorizon, eval)
			if perr != nil {
				continue
			}
			est = e
			action.Headline = fmt.Sprintf("Farm %s (%d 5★ pieces)", domain.Name, farmPieceHorizon)
			action.Unpriced = true
			action.Note = "Priced in pieces, not resin: your drop rate is not measured."
		}

		action.GainPct = est.MeanGain
		action.Detail = map[string]any{
			"runs":                runs,
			"pieces":              est.Pieces,
			"medianGain":          est.MedianGain,
			"p10Gain":             est.P10Gain,
			"p90Gain":             est.P90Gain,
			"noImprovementChance": est.NoImprovementChance,
			"sets":                domain.Sets,
		}
		out = append(out, action)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].GainPct > out[j].GainPct })
	return out, ""
}

// takenFrom lists the other characters a build would strip artifacts from.
//
// Every plan in Mimir is per goal, so it optimises one character in isolation.
// Without this note, "+12% free" can quietly mean "and your Raiden loses her
// sands" — which is not free at all, just billed to somebody else.
func takenFrom(pieces []model.Artifact, owner string) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range pieces {
		if a.Location == "" || a.Location == owner || seen[a.Location] {
			continue
		}
		seen[a.Location] = true
		out = append(out, a.Location)
	}
	sort.Strings(out)
	return out
}

func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

package api

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kristianwind/mimir/internal/advisor"
	"github.com/kristianwind/mimir/internal/db"
	"github.com/kristianwind/mimir/internal/effect"
	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/kvasir"
	"github.com/kristianwind/mimir/internal/model"
	"github.com/kristianwind/mimir/internal/optimizer"
)

// The fact sheets.
//
// This file is the whole of what Kvasir knows. Every surface of the product
// has one function here that runs the engine and writes down what came back —
// the ranked plan, a resolved build with its citations, the measured drop
// model, the inventory — and the model is handed that document and nothing
// else.
//
// Two consequences worth stating, because they are the design rather than an
// implementation detail:
//
//   - The model cannot reach the database, the optimizer or the damage engine.
//     It reads a page of already-computed facts. There is no path from a
//     prompt to a number.
//   - The set of numbers the answer is allowed to contain is exactly the set
//     of numbers in here. That is what internal/kvasir/numbers.go checks, and
//     it is only meaningful because this file is the sole source.

// briefActions caps how much of a ranking goes into a brief. Past the top
// couple of dozen the actions are rounding errors, and the tail costs context
// that is better spent on the build sheet.
const briefActions = 25

// briefFor assembles the fact sheet for one surface.
func (s *Server) briefFor(ctx context.Context, a model.Account, surface, subject string) (*kvasir.Brief, error) {
	switch surface {
	case "plan":
		return s.planBrief(ctx, a)
	case "goal":
		return s.goalBrief(ctx, a, subject)
	case "character":
		return s.characterBrief(ctx, a, subject)
	case "roster":
		return s.rosterBrief(ctx, a)
	case "artifacts":
		return s.artifactsBrief(ctx, a)
	case "goals":
		return s.goalsBrief(ctx, a)
	case "potential":
		return s.potentialBrief(ctx, a)
	default:
		return nil, fmt.Errorf("%s", "Kvasir has no fact sheet for that page")
	}
}

// ---------------------------------------------------------------- the plan

func (s *Server) planBrief(ctx context.Context, a model.Account) (*kvasir.Brief, error) {
	snap, err := s.GameData.Current()
	if err != nil {
		return nil, err
	}
	keys, err := s.goalKeys(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%s", "no goals have been set up")
	}

	inventory, err := db.LoadArtifacts(s.DB, a.ID)
	if err != nil {
		return nil, err
	}
	weapons, err := s.loadWeapons(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	sim := farmSim(snap, inventory)

	var reqs []advisor.Request
	var unplannable []string
	for _, key := range keys {
		req, err := s.planRequest(ctx, a.ID, key, snap, inventory, weapons, sim)
		if err != nil {
			unplannable = append(unplannable, fmt.Sprintf("%s: %v", key, err))
			continue
		}
		reqs = append(reqs, req)
	}
	if len(reqs) == 0 {
		return nil, fmt.Errorf("%s", "none of the goals could be calculated")
	}

	plan, err := advisor.BuildAccountPlan(ctx, reqs)
	if err != nil {
		return nil, err
	}

	b := kvasir.NewBrief("plan", "",
		fmt.Sprintf("The resin plan for account %s", a.UID),
		"This is the ranked plan the player is looking at. What should they do first, what does the ranking not make obvious, and what is holding this account back?")

	method := b.Add("How these numbers were measured")
	method.Line("Every gain is the change in that goal's whole rotation damage, calculated on the gear this account actually owns.")
	method.Line("Free actions rank above paid ones. An action that cannot be carried out today ranks last, however large it looks.")
	method.Line("Efficiency is the gain per 100 resin. A day is 180 resin.")

	goals := b.Add("The goals being optimised")
	for _, p := range plan.Plans {
		goals.Linef("%s: baseline %s damage per rotation, %d upgrades found",
			p.Goal, num(p.Baseline), len(p.Actions))
	}

	ranked := b.Add("The ranked plan")
	for i, act := range plan.Ranked {
		if i >= briefActions {
			ranked.Linef("…and %d smaller actions below these.", len(plan.Ranked)-briefActions)
			break
		}
		ranked.Linef("%d. [%s] %s", i+1, act.Goal, actionFacts(act.Action))
	}
	if len(plan.Ranked) == 0 {
		ranked.Line("Nothing. Every goal is already the best this gear allows.")
	}

	if len(plan.Conflicts) > 0 {
		conflicts := b.Add("Gear two goals both want")
		for _, c := range plan.Conflicts {
			conflicts.Linef("%s wants %s from %s — %s", c.Wants, c.Item, c.Holds, c.Resolution)
		}
	}

	limits := b.Add("What the engine refused to price")
	for _, c := range plan.Caveats {
		limits.Line(c)
	}
	for _, p := range plan.Plans {
		for _, skipped := range p.Skipped {
			limits.Line(p.Goal + ": " + skipped)
		}
	}
	for _, u := range unplannable {
		limits.Line(u)
	}

	s.addAccountFacts(ctx, b, a, inventory)
	return b, nil
}

// actionFacts writes one ranked action down without editorialising: the
// headline the engine wrote, the gain, the price, and what stops it.
func actionFacts(act advisor.Action) string {
	parts := []string{act.Headline, "+" + pct(act.GainPct)}
	switch {
	case act.Free:
		parts = append(parts, "free")
	case act.Unpriced:
		parts = append(parts, "not priced in resin")
	default:
		parts = append(parts, fmt.Sprintf("%s resin", num(act.ResinCost)),
			fmt.Sprintf("%s per 100 resin", pct(act.Efficiency)))
	}
	if act.Note != "" {
		parts = append(parts, act.Note)
	}
	if act.BlockedBy != "" {
		parts = append(parts, fmt.Sprintf("blocked: %s", act.BlockedBy))
	}
	// The bill is a fact the model may repeat but must never compute: the
	// counts are exact, so an answer that changes one is checkably wrong.
	if cost, ok := act.Detail["cost"].(advisor.Cost); ok {
		if bill := cost.Summary(); bill != "" {
			parts = append(parts, "costs "+bill)
		}
		for _, run := range cost.Runs() {
			parts = append(parts, "from "+run)
		}
		if len(cost.Capped) > 0 {
			parts = append(parts, "weekly-capped: "+strings.Join(cost.Capped, ", "))
		}
	}
	if act.Kind == advisor.KindFarm && act.Detail != nil {
		if median, ok := act.Detail["medianGain"].(float64); ok {
			parts = append(parts, fmt.Sprintf(
				"median %s, p10 %s, p90 %s, and %s of simulated runs changed nothing",
				pct(median), pct(detailFloat(act.Detail, "p10Gain")), pct(detailFloat(act.Detail, "p90Gain")),
				pct(detailFloat(act.Detail, "noImprovementChance"))))
		}
	}
	return strings.Join(parts, " · ")
}

func detailFloat(detail map[string]any, key string) float64 {
	v, _ := detail[key].(float64)
	return v
}

// ---------------------------------------------------------------- one goal

func (s *Server) goalBrief(ctx context.Context, a model.Account, key string) (*kvasir.Brief, error) {
	if key == "" {
		return nil, fmt.Errorf("%s", "that needs a character")
	}
	snap, err := s.GameData.Current()
	if err != nil {
		return nil, err
	}
	inventory, err := db.LoadArtifacts(s.DB, a.ID)
	if err != nil {
		return nil, err
	}
	weapons, err := s.loadWeapons(ctx, a.ID)
	if err != nil {
		return nil, err
	}

	req, err := s.planRequest(ctx, a.ID, key, snap, inventory, weapons, farmSim(snap, inventory))
	if err != nil {
		return nil, err
	}
	plan, err := advisor.BuildPlan(ctx, req)
	if err != nil {
		return nil, err
	}

	b := kvasir.NewBrief("goal", key,
		fmt.Sprintf("%s as a goal", key),
		fmt.Sprintf("How does this player make %s hit harder? Weigh what the ranking costs elsewhere, and say what is missing from the goal itself.", key))

	setup := b.Add("The goal")
	setup.Linef("Priority %d among this account's goals.", req.Goal.Priority)
	setup.Linef("Baseline: %s damage per rotation.", num(plan.Baseline))
	setup.Linef("Measured against a level %d enemy.", req.Goal.Target.Level)
	if name := req.Goal.Spec.Name; name != "" {
		setup.Linef("Rotation: %s", name)
	}
	for i, step := range req.Goal.Spec.Steps {
		line := fmt.Sprintf("Step %d: %s %s ×%d", i+1, step.Talent, step.Entry, step.Hits)
		if step.Amplify != "" {
			line += fmt.Sprintf(", amplified by %s", string(step.Amplify))
		}
		setup.Line(line)
	}
	if len(req.Goal.Conditions) > 0 {
		for _, k := range sortedKeys(req.Goal.Conditions) {
			setup.Linef("Declared condition: %s = %s", k, num(req.Goal.Conditions[k]))
		}
	}

	s.addBuildFacts(b, snap, req.Loadout, req.Goal.Conditions)

	actions := b.Add("Ranked upgrades for this goal")
	for i, act := range plan.Actions {
		if i >= briefActions {
			break
		}
		actions.Line(fmt.Sprintf("%d. %s", i+1, actionFacts(act)))
	}
	if len(plan.Actions) == 0 {
		actions.Line("None. This build is the best the account's gear allows.")
	}

	if len(plan.Skipped) > 0 {
		limits := b.Add("What the engine refused to price")
		for _, line := range plan.Skipped {
			limits.Line(line)
		}
	}
	return b, nil
}

// ---------------------------------------------------------------- one build

func (s *Server) characterBrief(ctx context.Context, a model.Account, key string) (*kvasir.Brief, error) {
	if key == "" {
		return nil, fmt.Errorf("%s", "that needs a character")
	}
	snap, err := s.GameData.Current()
	if err != nil {
		return nil, err
	}
	character, err := s.loadCharacter(ctx, a.ID, key)
	if err != nil {
		return nil, fmt.Errorf("%s", fmt.Sprintf("%s is not on the account", key))
	}
	weapon, err := s.loadEquippedWeapon(ctx, a.ID, key)
	if err != nil {
		return nil, err
	}
	inventory, err := db.LoadArtifacts(s.DB, a.ID)
	if err != nil {
		return nil, err
	}
	var equipped []model.Artifact
	for _, art := range inventory {
		if art.Location == key {
			equipped = append(equipped, art)
		}
	}

	var conditions map[string]float64
	hasGoal := false
	if goal, _, _, err := s.loadGoal(ctx, a.ID, key); err == nil {
		conditions = goal.Conditions
		hasGoal = true
	}

	b := kvasir.NewBrief("character", key,
		fmt.Sprintf("%s's build", key),
		"What is wrong with this build, and what is the cheapest thing that would fix it? Say what the stats show, not what is usually recommended.")

	s.addBuildFacts(b, snap, advisor.Loadout{Character: character, Weapon: weapon, Artifacts: equipped}, conditions)

	if !hasGoal {
		note := b.Add("No goal")
		note.Line("This character has no goal, so nothing has been ranked for them: Mimir measures a gain against a rotation, and there is none to measure against.")
	}
	return b, nil
}

// addBuildFacts writes a resolved build down, citations and gaps included.
//
// The citations are in here on purpose. They are what let an answer say where
// a figure came from, and a model that can quote the game's own wording for a
// set bonus is a model that has no reason to paraphrase one from memory.
func (s *Server) addBuildFacts(
	b *kvasir.Brief, snap *gamedata.Snapshot, loadout advisor.Loadout,
	conditions map[string]float64,
) {
	c := loadout.Character
	who := b.Add("The character")
	who.Linef("%s, level %d, constellation %d.", c.Key, c.Level, c.Constellation)
	who.Linef("Talent levels: normal attack %d, elemental skill %d, elemental burst %d.",
		c.TalentAuto, c.TalentSkill, c.TalentBurst)
	if loadout.Weapon != nil {
		who.Linef("Weapon: %s, level %d, refinement %d.",
			loadout.Weapon.Key, loadout.Weapon.Level, loadout.Weapon.Refinement)
	} else {
		who.Line("No weapon is equipped.")
	}

	gear := b.Add("Equipped artifacts")
	for _, art := range loadout.Artifacts {
		gear.Linef("%s %s +%d, main stat %s%s",
			art.SetKey, string(art.SlotKey), art.Level, statName(art.MainStat), substatList(art))
	}
	if len(loadout.Artifacts) == 0 {
		gear.Line("Nothing is equipped, so there is no build to resolve.")
		return
	}

	state, err := advisor.Assemble(snap, loadout)
	if err != nil {
		gear.Linef("The build could not be resolved: %v", err)
		return
	}
	sheet, err := advisor.BuildSheet(snap, state, conditions)
	if err != nil {
		gear.Linef("The build could not be resolved: %v", err)
		return
	}

	for _, setKey := range sortedKeys(sheet.SetCounts) {
		gear.Linef("Set bonus in effect: %d pieces of %s.", sheet.SetCounts[setKey], setKey)
	}

	totals := b.Add("Resolved stats, everything included")
	for _, stat := range statOrder(sheet.Totals) {
		totals.Linef("%s: %s", statName(stat), statValue(stat, sheet.Totals[stat]))
	}

	if len(sheet.Effects) > 0 {
		effects := b.Add("What the conditional layer contributed, and the game text it was checked against")
		for _, g := range sheet.Effects {
			line := fmt.Sprintf("%s: %s %s", g.Source, statName(g.Stat), statValue(g.Stat, g.Value))
			if g.Cite != "" {
				line += " — " + trim(g.Cite, 220)
			}
			effects.Line(line)
		}
	}

	if len(sheet.Instances) > 0 {
		hits := b.Add("Damage the gear adds by itself")
		for _, in := range sheet.Instances {
			hits.Linef("%s adds its own hit at %s scaling.", in.Source, pct(in.Instance.Multiplier))
		}
	}

	if len(sheet.Undeclared) > 0 {
		gaps := b.Add("Conditions nobody has answered")
		gaps.Line("These are switched off in every number above. They are not absent bonuses; they are bonuses nobody has been asked about.")
		for _, m := range sheet.Undeclared {
			line := fmt.Sprintf("%s (%s)", m.Source, m.Key)
			if m.MaxStacks > 0 {
				line += fmt.Sprintf(", up to %s", num(m.MaxStacks))
			}
			if m.Note != "" {
				line += " — " + trim(m.Note, 200)
			}
			gaps.Line(line)
		}
	}
}

// ---------------------------------------------------------------- the roster

func (s *Server) rosterBrief(ctx context.Context, a model.Account) (*kvasir.Brief, error) {
	characters, err := s.loadCharacters(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	if len(characters) == 0 {
		return nil, fmt.Errorf("%s", "no characters have been imported yet")
	}
	weapons, err := s.loadWeapons(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	goals, err := s.goalKeys(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	hasGoal := map[string]bool{}
	for _, g := range goals {
		hasGoal[g] = true
	}
	weaponOf := map[string]model.Weapon{}
	for _, w := range weapons {
		if w.Location != "" {
			weaponOf[w.Location] = w
		}
	}

	inventory, err := db.LoadArtifacts(s.DB, a.ID)
	if err != nil {
		return nil, err
	}
	equippedCount := map[string]int{}
	for _, art := range inventory {
		if art.Location != "" {
			equippedCount[art.Location]++
		}
	}

	b := kvasir.NewBrief("roster", "",
		fmt.Sprintf("The roster on account %s", a.UID),
		"Who is worth investing in next, and who is being carried by gear they should not have? Only judge what is listed here.")

	roster := b.Add("Every character on the account")
	for _, c := range characters {
		line := fmt.Sprintf("%s: level %d, C%d, talents %d/%d/%d, %d artifacts equipped",
			c.Key, c.Level, c.Constellation, c.TalentAuto, c.TalentSkill, c.TalentBurst, equippedCount[c.Key])
		if w, ok := weaponOf[c.Key]; ok {
			line += fmt.Sprintf(", holding %s R%d", w.Key, w.Refinement)
		} else {
			line += ", no weapon"
		}
		if hasGoal[c.Key] {
			line += ", has a goal"
		} else {
			line += ", no goal set up"
		}
		roster.Line(line)
	}

	s.addAccountFacts(ctx, b, a, inventory)

	method := b.Add("What Mimir can and cannot say here")
	method.Line("Nothing on this page has been through the damage engine: a character with no goal has no rotation, and without a rotation there is no number. Say what is worth setting up as a goal rather than claiming a gain.")
	return b, nil
}

// ---------------------------------------------------------------- inventory

func (s *Server) artifactsBrief(ctx context.Context, a model.Account) (*kvasir.Brief, error) {
	inventory, err := db.LoadArtifacts(s.DB, a.ID)
	if err != nil {
		return nil, err
	}
	if len(inventory) == 0 {
		return nil, fmt.Errorf("%s", "no artifacts have been imported yet")
	}

	b := kvasir.NewBrief("artifacts", "",
		fmt.Sprintf("The artifact inventory on account %s", a.UID),
		"What should this player do with this inventory — what is worth levelling, what is dead weight, and which domain is worth a week? Do not claim a gain the engine has not measured.")

	s.addInventoryFacts(b, inventory, "", "")

	est, err := advisor.EstimateDropModel(inventory)
	drops := b.Add("The drop model measured from this inventory")
	if err != nil {
		drops.Linef("There is no measured drop model: %v", err)
		drops.Line("Without one, farming is ranked in artifacts examined rather than in resin.")
	} else {
		drops.Linef("Measured from %d five-star artifacts.", est.Sample)
		if est.HasYield {
			drops.Linef("Runs can be priced in resin: %s pieces per run.", num(est.Model.PiecesPerRun))
		} else {
			drops.Line("The per-run yield is unknown, so farming cannot be priced in resin. An inventory records what dropped, never how many runs it took.")
		}
		for _, c := range est.Caveats {
			drops.Line(c)
		}
	}
	return b, nil
}

// addInventoryFacts summarises an inventory, optionally filtered.
//
// A summary rather than a list: fourteen hundred artifacts is a fact sheet
// nobody's context window survives, and the individual pieces are the
// optimizer's business anyway. What a reader needs is the shape — which sets
// are deep enough to build around, which slots are thin, and what good gear is
// sitting unequipped.
func (s *Server) addInventoryFacts(
	b *kvasir.Brief, inventory []model.Artifact, setFilter, slotFilter string,
) {
	type setStat struct {
		total, fiveStar, maxed, equipped int
		bestCV                           float64
	}
	sets := map[string]*setStat{}
	bySlot := map[model.Slot]int{}
	equipped := 0

	shown := 0
	for _, art := range inventory {
		if setFilter != "" && !strings.EqualFold(art.SetKey, setFilter) {
			continue
		}
		if slotFilter != "" && !strings.EqualFold(string(art.SlotKey), slotFilter) {
			continue
		}
		shown++
		st := sets[art.SetKey]
		if st == nil {
			st = &setStat{}
			sets[art.SetKey] = st
		}
		st.total++
		if art.Rarity == 5 {
			st.fiveStar++
		}
		if art.Level >= 20 {
			st.maxed++
		}
		if art.Location != "" {
			st.equipped++
			equipped++
		}
		// Crit value is quoted as 2·CR% + CD% everywhere a player has seen
		// it, and the model holds stats as fractions, so it is scaled here
		// rather than printed as 0.4 next to a page that says 40.4.
		if cv := art.CritValue() * 100; cv > st.bestCV {
			st.bestCV = cv
		}
		bySlot[art.SlotKey]++
	}

	totals := b.Add("The inventory")
	totals.Linef("%d artifacts, %d of them equipped on somebody.", shown, equipped)
	for _, slot := range model.Slots {
		if bySlot[slot] > 0 {
			totals.Linef("%s: %d pieces", string(slot), bySlot[slot])
		}
	}

	keys := make([]string, 0, len(sets))
	for k := range sets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if sets[keys[i]].total != sets[keys[j]].total {
			return sets[keys[i]].total > sets[keys[j]].total
		}
		return keys[i] < keys[j]
	})

	bySet := b.Add("By set, deepest first")
	for i, k := range keys {
		if i >= 15 {
			bySet.Linef("…and %d further sets with less in them.", len(keys)-15)
			break
		}
		st := sets[k]
		bySet.Linef("%s: %d pieces, %d of them five-star, %d at +20, %d equipped, best crit value %s",
			k, st.total, st.fiveStar, st.maxed, st.equipped, num1(st.bestCV))
	}

	// The best unequipped pieces, which is where free damage hides. Crit
	// value is a triage number and says so: the optimizer is the verdict.
	spare := append([]model.Artifact(nil), inventory...)
	spare = filterArtifacts(spare, setFilter, slotFilter, func(art model.Artifact) bool {
		return art.Location == "" && art.Rarity == 5
	})
	sort.Slice(spare, func(i, j int) bool { return spare[i].CritValue() > spare[j].CritValue() })

	if len(spare) > 0 {
		best := b.Add("The best pieces nobody is wearing")
		best.Line("Crit value is 2×crit rate + crit damage. It is triage, not a verdict — the optimizer decides what is actually worn.")
		for i, art := range spare {
			if i >= 10 {
				break
			}
			best.Linef("%s %s +%d, main stat %s, crit value %s%s",
				art.SetKey, string(art.SlotKey), art.Level, statName(art.MainStat),
				num1(art.CritValue()*100), substatList(art))
		}
	}
}

func filterArtifacts(in []model.Artifact, setFilter, slotFilter string, keep func(model.Artifact) bool) []model.Artifact {
	out := in[:0]
	for _, art := range in {
		if setFilter != "" && !strings.EqualFold(art.SetKey, setFilter) {
			continue
		}
		if slotFilter != "" && !strings.EqualFold(string(art.SlotKey), slotFilter) {
			continue
		}
		if keep(art) {
			out = append(out, art)
		}
	}
	return out
}

// ------------------------------------------------------------- the potential

// potentialBrief is the roster measured with one ruler and no goals.
//
// It is the only fact sheet that covers characters the plan cannot see, so it
// is what an answer to "who should I build" has to be built from. The ruler's
// limits are in the sheet rather than in a footnote: a model handed a ranking
// with no stated method will explain it as if it were a verdict.
func (s *Server) potentialBrief(ctx context.Context, a model.Account) (*kvasir.Brief, error) {
	reqs, err := s.potentialRequests(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	if len(reqs) == 0 {
		return nil, fmt.Errorf("none of the characters have anything equipped")
	}
	ranking, err := advisor.AccountPotential(ctx, reqs)
	if err != nil {
		return nil, err
	}

	b := kvasir.NewBrief("potential", "",
		fmt.Sprintf("What every character on account %s is worth building", a.UID),
		"Which characters should this player invest in, and what should they do first for each? "+
			"Say who is being neglected as well as who buys the most damage — those are different questions and the ranking only answers the second.")

	method := b.Add("How every character here was measured")
	for _, c := range ranking.Caveats {
		method.Line(c)
	}

	list := b.Add("The roster, best single upgrade first")
	for i, c := range ranking.Characters {
		line := fmt.Sprintf("%d. %s (%s): scores %s today, %s with the best gear it owns — %s of headroom sitting unequipped",
			i+1, c.Character, c.Element, num(c.Current), num(c.Best), pct(c.Headroom))
		if c.TopAction != nil {
			line += fmt.Sprintf(". Biggest single upgrade: %s, +%s (%s points)",
				c.TopAction.Headline, pct(c.TopAction.GainPct), num(c.TopGain))
		}
		list.Line(line)
	}

	// Artifact levelling is called out separately because it is the one
	// upgrade that costs no resin and no domain run, and the one people
	// forget: a +8 piece on a finished build is free damage in a drawer.
	pieces := b.Add("Pieces that are not levelled")
	for _, c := range ranking.Characters {
		for _, act := range c.Actions {
			if act.Kind == advisor.KindArtifact {
				pieces.Linef("%s: %s (+%s)", c.Character, act.Headline, pct(act.GainPct))
			}
		}
	}
	if pieces.Empty() {
		pieces.Line("None: every equipped piece is at its cap.")
	}

	limits := b.Add("What could not be measured")
	for _, sk := range ranking.Skipped {
		limits.Line(sk)
	}
	for _, c := range ranking.Characters {
		for _, sk := range c.Skipped {
			limits.Linef("%s: %s", c.Character, sk)
		}
	}

	s.addAccountFacts(ctx, b, a, nil)
	return b, nil
}

// ---------------------------------------------------------------- the goals

func (s *Server) goalsBrief(ctx context.Context, a model.Account) (*kvasir.Brief, error) {
	keys, err := s.goalKeys(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	characters, err := s.loadCharacters(ctx, a.ID)
	if err != nil {
		return nil, err
	}

	b := kvasir.NewBrief("goals", "",
		fmt.Sprintf("The goals on account %s", a.UID),
		"Are these goals set up so the ranking can be trusted? Name what is missing — an unanswered condition, a rotation that does not match how the character is played, a priority order that fights itself.")

	snap, snapErr := s.GameData.Current()

	list := b.Add("Goals, highest priority first")
	for _, key := range keys {
		goal, _, _, err := s.loadGoal(ctx, a.ID, key)
		if err != nil {
			continue
		}
		list.Linef("%s: priority %d, rotation %q with %d steps, enemy level %d",
			key, goal.Priority, goal.Spec.Name, len(goal.Spec.Steps), goal.Target.Level)
		for i, step := range goal.Spec.Steps {
			list.Linef("    %s step %d: %s %s ×%d", key, i+1, step.Talent, step.Entry, step.Hits)
		}
		for _, k := range sortedKeys(goal.Conditions) {
			list.Linef("    %s declared condition: %s = %s", key, k, num(goal.Conditions[k]))
		}

		// Which conditions this goal's own gear would use, but nobody has
		// answered. This is the single most common reason a plan understates
		// a build, so it belongs on the goals page rather than only on the
		// build sheet.
		if snapErr == nil {
			for _, m := range s.undeclaredFor(ctx, a.ID, key, snap, goal.Conditions) {
				list.Linef("    %s has not been asked: %s (%s)", key, m.Source, m.Key)
			}
		}
	}
	if len(keys) == 0 {
		list.Line("No goals have been set up, so nothing on this account has been ranked.")
	}

	if len(characters) > len(keys) {
		without := b.Add("Characters with no goal")
		has := map[string]bool{}
		for _, k := range keys {
			has[k] = true
		}
		for _, c := range characters {
			if !has[c.Key] {
				without.Linef("%s, level %d, C%d", c.Key, c.Level, c.Constellation)
			}
		}
	}
	return b, nil
}

// undeclaredFor is the "nobody was asked" list for one character's current
// gear, or nothing if the build cannot be resolved.
func (s *Server) undeclaredFor(
	ctx context.Context, accountID int64, key string,
	snap *gamedata.Snapshot, conditions map[string]float64,
) []effect.Missing {
	character, err := s.loadCharacter(ctx, accountID, key)
	if err != nil {
		return nil
	}
	weapon, err := s.loadEquippedWeapon(ctx, accountID, key)
	if err != nil {
		return nil
	}
	inventory, err := db.LoadArtifacts(s.DB, accountID)
	if err != nil {
		return nil
	}
	var equipped []model.Artifact
	for _, art := range inventory {
		if art.Location == key {
			equipped = append(equipped, art)
		}
	}
	if len(equipped) == 0 {
		return nil
	}
	state, err := advisor.Assemble(snap, advisor.Loadout{Character: character, Weapon: weapon, Artifacts: equipped})
	if err != nil {
		return nil
	}
	return effect.Undeclared(snap.Effects, effect.Context{
		Snapshot:   snap,
		Character:  state.Character,
		SetCounts:  optimizer.SetCounts(state.Equipped),
		WeaponKey:  state.WeaponKey,
		Refinement: state.WeaponRefinement,
		Conditions: conditions,
	})
}

// ---------------------------------------------------------------- shared

// addAccountFacts states the size of what is being reasoned about. Without it
// an answer cannot tell an eight-character showcase from a full inventory, and
// the advice for those two accounts is not the same.
func (s *Server) addAccountFacts(
	ctx context.Context, b *kvasir.Brief, a model.Account, inventory []model.Artifact,
) {
	characters, _ := s.loadCharacters(ctx, a.ID)
	weapons, _ := s.loadWeapons(ctx, a.ID)

	sec := b.Add("The account")
	sec.Linef("%d characters, %d weapons and %d artifacts have been imported.",
		len(characters), len(weapons), len(inventory))
	if a.ARLevel > 0 {
		sec.Linef("Adventure rank %d, world level %d.", a.ARLevel, a.WLLevel)
	}
	source := map[string]int{}
	for _, art := range inventory {
		source[art.Source]++
	}
	if source["good"] == 0 && len(inventory) > 0 {
		sec.Line("The inventory came from Enka, which only reports equipped pieces. Everything unequipped is invisible here — a .good file would change that.")
	}
}

func (s *Server) loadCharacters(ctx context.Context, accountID int64) ([]model.Character, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT char_key, level, ascension, constellation, talent_auto, talent_skill, talent_burst
		FROM characters WHERE account_id = ? ORDER BY char_key`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Character
	for rows.Next() {
		c := model.Character{AccountID: accountID}
		if err := rows.Scan(&c.Key, &c.Level, &c.Ascension, &c.Constellation,
			&c.TalentAuto, &c.TalentSkill, &c.TalentBurst); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- formatting

// pct renders a fraction as a percentage with two decimals — the same
// precision the plan shows, so a figure in an answer can be found on the page
// it came from.
func pct(v float64) string { return strconv.FormatFloat(v*100, 'f', 2, 64) + " %" }

func num(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func num1(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }

// statName keeps the game's own vocabulary rather than inventing prose for it.
// The keys are GOOD's, which is what every other Genshin tool prints, so a
// player can match them against Genshin Optimizer without a glossary.
func statName(s model.Stat) string {
	switch s {
	case model.HP:
		return "HP"
	case model.HPPercent:
		return "HP%"
	case model.ATK:
		return "ATK"
	case model.ATKPercent:
		return "ATK%"
	case model.DEF:
		return "DEF"
	case model.DEFPercent:
		return "DEF%"
	case model.ElementalMastery:
		return "Elemental Mastery"
	case model.EnergyRecharge:
		return "Energy Recharge"
	case model.CritRate:
		return "Crit Rate"
	case model.CritDMG:
		return "Crit DMG"
	case model.HealingBonus:
		return "Healing Bonus"
	default:
		return string(s)
	}
}

// statValue formats a stat the way the game shows it: percentages as
// percentages, flat values as whole numbers.
func statValue(s model.Stat, v float64) string {
	if strings.HasSuffix(string(s), "_") {
		return strconv.FormatFloat(v*100, 'f', 1, 64) + " %"
	}
	return strconv.FormatFloat(v, 'f', 0, 64)
}

// statOrder puts the stats a player reads first at the top and drops the
// zeroes. A build sheet listing forty stats of which thirty are zero buries
// the six that decide the build.
func statOrder(block model.StatBlock) []model.Stat {
	lead := []model.Stat{
		model.HP, model.ATK, model.DEF, model.ElementalMastery,
		model.CritRate, model.CritDMG, model.EnergyRecharge,
	}
	seen := map[model.Stat]bool{}
	var out []model.Stat
	for _, s := range lead {
		if block[s] != 0 {
			out = append(out, s)
			seen[s] = true
		}
	}
	var rest []model.Stat
	for s, v := range block {
		if v != 0 && !seen[s] {
			rest = append(rest, s)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i] < rest[j] })
	return append(out, rest...)
}

func substatList(art model.Artifact) string {
	if len(art.Substats) == 0 {
		return ""
	}
	parts := make([]string, 0, len(art.Substats))
	for _, sub := range art.Substats {
		parts = append(parts, statName(sub.Key)+" "+statValue(sub.Key, sub.Value))
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func trim(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

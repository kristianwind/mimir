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
	"github.com/kristianwind/mimir/internal/i18n"
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
//
// The briefs are written in the reader's language for the same reason the
// plan's prose is: the player is shown the fact sheet behind any answer, and
// evidence they cannot read is not evidence.

// briefActions caps how much of a ranking goes into a brief. Past the top
// couple of dozen the actions are rounding errors, and the tail costs context
// that is better spent on the build sheet.
const briefActions = 25

// briefFor assembles the fact sheet for one surface.
func (s *Server) briefFor(ctx context.Context, a model.Account, surface, subject string, lang i18n.Lang) (*kvasir.Brief, error) {
	switch surface {
	case "plan":
		return s.planBrief(ctx, a, lang)
	case "goal":
		return s.goalBrief(ctx, a, subject, lang)
	case "character":
		return s.characterBrief(ctx, a, subject, lang)
	case "roster":
		return s.rosterBrief(ctx, a, lang)
	case "artifacts":
		return s.artifactsBrief(ctx, a, lang)
	case "goals":
		return s.goalsBrief(ctx, a, lang)
	default:
		return nil, fmt.Errorf("%s", i18n.T(lang, "Kvasir has no fact sheet for that page"))
	}
}

// ---------------------------------------------------------------- the plan

func (s *Server) planBrief(ctx context.Context, a model.Account, lang i18n.Lang) (*kvasir.Brief, error) {
	snap, err := s.GameData.Current()
	if err != nil {
		return nil, err
	}
	keys, err := s.goalKeys(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%s", i18n.T(lang, "no goals have been set up"))
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
		req, err := s.planRequest(ctx, a.ID, key, snap, inventory, weapons, sim, lang)
		if err != nil {
			unplannable = append(unplannable, fmt.Sprintf("%s: %v", key, err))
			continue
		}
		reqs = append(reqs, req)
	}
	if len(reqs) == 0 {
		return nil, fmt.Errorf("%s", i18n.T(lang, "none of the goals could be calculated"))
	}

	plan, err := advisor.BuildAccountPlan(ctx, reqs)
	if err != nil {
		return nil, err
	}

	b := kvasir.NewBrief("plan", "",
		i18n.T(lang, "The resin plan for account %s", a.UID),
		i18n.T(lang, "This is the ranked plan the player is looking at. What should they do first, what does the ranking not make obvious, and what is holding this account back?"))

	method := b.Add(i18n.T(lang, "How these numbers were measured"))
	method.T(lang, "Every gain is the change in that goal's whole rotation damage, calculated on the gear this account actually owns.")
	method.T(lang, "Free actions rank above paid ones. An action that cannot be carried out today ranks last, however large it looks.")
	method.T(lang, "Efficiency is the gain per 100 resin. A day is 180 resin.")

	goals := b.Add(i18n.T(lang, "The goals being optimised"))
	for _, p := range plan.Plans {
		goals.T(lang, "%s: baseline %s damage per rotation, %d upgrades found",
			p.Goal, num(p.Baseline), len(p.Actions))
	}

	ranked := b.Add(i18n.T(lang, "The ranked plan"))
	for i, act := range plan.Ranked {
		if i >= briefActions {
			ranked.T(lang, "…and %d smaller actions below these.", len(plan.Ranked)-briefActions)
			break
		}
		ranked.Line(fmt.Sprintf("%d. [%s] %s", i+1, act.Goal, actionFacts(lang, act.Action)))
	}
	if len(plan.Ranked) == 0 {
		ranked.T(lang, "Nothing. Every goal is already the best this gear allows.")
	}

	if len(plan.Conflicts) > 0 {
		conflicts := b.Add(i18n.T(lang, "Gear two goals both want"))
		for _, c := range plan.Conflicts {
			conflicts.T(lang, "%s wants %s from %s — %s", c.Wants, c.Item, c.Holds, c.Resolution)
		}
	}

	limits := b.Add(i18n.T(lang, "What the engine refused to price"))
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

	s.addAccountFacts(ctx, b, a, inventory, lang)
	return b, nil
}

// actionFacts writes one ranked action down without editorialising: the
// headline the engine wrote, the gain, the price, and what stops it.
func actionFacts(lang i18n.Lang, act advisor.Action) string {
	parts := []string{act.Headline, "+" + pct(act.GainPct)}
	switch {
	case act.Free:
		parts = append(parts, i18n.T(lang, "free"))
	case act.Unpriced:
		parts = append(parts, i18n.T(lang, "not priced in resin"))
	default:
		parts = append(parts, i18n.T(lang, "%s resin", num(act.ResinCost)),
			i18n.T(lang, "%s per 100 resin", pct(act.Efficiency)))
	}
	if act.Note != "" {
		parts = append(parts, act.Note)
	}
	if act.BlockedBy != "" {
		parts = append(parts, i18n.T(lang, "blocked: %s", act.BlockedBy))
	}
	if act.Kind == advisor.KindFarm && act.Detail != nil {
		if median, ok := act.Detail["medianGain"].(float64); ok {
			parts = append(parts, i18n.T(lang,
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

func (s *Server) goalBrief(ctx context.Context, a model.Account, key string, lang i18n.Lang) (*kvasir.Brief, error) {
	if key == "" {
		return nil, fmt.Errorf("%s", i18n.T(lang, "that needs a character"))
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

	req, err := s.planRequest(ctx, a.ID, key, snap, inventory, weapons, farmSim(snap, inventory), lang)
	if err != nil {
		return nil, err
	}
	plan, err := advisor.BuildPlan(ctx, req)
	if err != nil {
		return nil, err
	}

	b := kvasir.NewBrief("goal", key,
		i18n.T(lang, "%s as a goal", key),
		i18n.T(lang, "How does this player make %s hit harder? Weigh what the ranking costs elsewhere, and say what is missing from the goal itself.", key))

	setup := b.Add(i18n.T(lang, "The goal"))
	setup.T(lang, "Priority %d among this account's goals.", req.Goal.Priority)
	setup.T(lang, "Baseline: %s damage per rotation.", num(plan.Baseline))
	setup.T(lang, "Measured against a level %d enemy.", req.Goal.Target.Level)
	if name := req.Goal.Spec.Name; name != "" {
		setup.T(lang, "Rotation: %s", name)
	}
	for i, step := range req.Goal.Spec.Steps {
		line := i18n.T(lang, "Step %d: %s %s ×%d", i+1, step.Talent, step.Entry, step.Hits)
		if step.Amplify != "" {
			line += i18n.T(lang, ", amplified by %s", string(step.Amplify))
		}
		setup.Line(line)
	}
	if len(req.Goal.Conditions) > 0 {
		for _, k := range sortedKeys(req.Goal.Conditions) {
			setup.T(lang, "Declared condition: %s = %s", k, num(req.Goal.Conditions[k]))
		}
	}

	s.addBuildFacts(b, snap, req.Loadout, req.Goal.Conditions, lang)

	actions := b.Add(i18n.T(lang, "Ranked upgrades for this goal"))
	for i, act := range plan.Actions {
		if i >= briefActions {
			break
		}
		actions.Line(fmt.Sprintf("%d. %s", i+1, actionFacts(lang, act)))
	}
	if len(plan.Actions) == 0 {
		actions.T(lang, "None. This build is the best the account's gear allows.")
	}

	if len(plan.Skipped) > 0 {
		limits := b.Add(i18n.T(lang, "What the engine refused to price"))
		for _, line := range plan.Skipped {
			limits.Line(line)
		}
	}
	return b, nil
}

// ---------------------------------------------------------------- one build

func (s *Server) characterBrief(ctx context.Context, a model.Account, key string, lang i18n.Lang) (*kvasir.Brief, error) {
	if key == "" {
		return nil, fmt.Errorf("%s", i18n.T(lang, "that needs a character"))
	}
	snap, err := s.GameData.Current()
	if err != nil {
		return nil, err
	}
	character, err := s.loadCharacter(ctx, a.ID, key)
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T(lang, "%s is not on the account", key))
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
		i18n.T(lang, "%s's build", key),
		i18n.T(lang, "What is wrong with this build, and what is the cheapest thing that would fix it? Say what the stats show, not what is usually recommended."))

	s.addBuildFacts(b, snap, advisor.Loadout{Character: character, Weapon: weapon, Artifacts: equipped}, conditions, lang)

	if !hasGoal {
		note := b.Add(i18n.T(lang, "No goal"))
		note.T(lang, "This character has no goal, so nothing has been ranked for them: Mimir measures a gain against a rotation, and there is none to measure against.")
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
	conditions map[string]float64, lang i18n.Lang,
) {
	c := loadout.Character
	who := b.Add(i18n.T(lang, "The character"))
	who.T(lang, "%s, level %d, constellation %d.", c.Key, c.Level, c.Constellation)
	who.T(lang, "Talent levels: normal attack %d, elemental skill %d, elemental burst %d.",
		c.TalentAuto, c.TalentSkill, c.TalentBurst)
	if loadout.Weapon != nil {
		who.T(lang, "Weapon: %s, level %d, refinement %d.",
			loadout.Weapon.Key, loadout.Weapon.Level, loadout.Weapon.Refinement)
	} else {
		who.T(lang, "No weapon is equipped.")
	}

	gear := b.Add(i18n.T(lang, "Equipped artifacts"))
	for _, art := range loadout.Artifacts {
		gear.T(lang, "%s %s +%d, main stat %s%s",
			art.SetKey, string(art.SlotKey), art.Level, statName(art.MainStat), substatList(art))
	}
	if len(loadout.Artifacts) == 0 {
		gear.T(lang, "Nothing is equipped, so there is no build to resolve.")
		return
	}

	state, err := advisor.Assemble(snap, loadout)
	if err != nil {
		gear.T(lang, "The build could not be resolved: %v", err)
		return
	}
	sheet, err := advisor.BuildSheet(snap, state, conditions)
	if err != nil {
		gear.T(lang, "The build could not be resolved: %v", err)
		return
	}

	for _, setKey := range sortedKeys(sheet.SetCounts) {
		gear.T(lang, "Set bonus in effect: %d pieces of %s.", sheet.SetCounts[setKey], setKey)
	}

	totals := b.Add(i18n.T(lang, "Resolved stats, everything included"))
	for _, stat := range statOrder(sheet.Totals) {
		totals.Linef("%s: %s", statName(stat), statValue(stat, sheet.Totals[stat]))
	}

	if len(sheet.Effects) > 0 {
		effects := b.Add(i18n.T(lang, "What the conditional layer contributed, and the game text it was checked against"))
		for _, g := range sheet.Effects {
			line := fmt.Sprintf("%s: %s %s", g.Source, statName(g.Stat), statValue(g.Stat, g.Value))
			if g.Cite != "" {
				line += " — " + trim(g.Cite, 220)
			}
			effects.Line(line)
		}
	}

	if len(sheet.Instances) > 0 {
		hits := b.Add(i18n.T(lang, "Damage the gear adds by itself"))
		for _, in := range sheet.Instances {
			hits.T(lang, "%s adds its own hit at %s scaling.", in.Source, pct(in.Instance.Multiplier))
		}
	}

	if len(sheet.Undeclared) > 0 {
		gaps := b.Add(i18n.T(lang, "Conditions nobody has answered"))
		gaps.T(lang, "These are switched off in every number above. They are not absent bonuses; they are bonuses nobody has been asked about.")
		for _, m := range sheet.Undeclared {
			line := fmt.Sprintf("%s (%s)", m.Source, m.Key)
			if m.MaxStacks > 0 {
				line += i18n.T(lang, ", up to %s", num(m.MaxStacks))
			}
			if m.Note != "" {
				line += " — " + trim(m.Note, 200)
			}
			gaps.Line(line)
		}
	}
}

// ---------------------------------------------------------------- the roster

func (s *Server) rosterBrief(ctx context.Context, a model.Account, lang i18n.Lang) (*kvasir.Brief, error) {
	characters, err := s.loadCharacters(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	if len(characters) == 0 {
		return nil, fmt.Errorf("%s", i18n.T(lang, "no characters have been imported yet"))
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
		i18n.T(lang, "The roster on account %s", a.UID),
		i18n.T(lang, "Who is worth investing in next, and who is being carried by gear they should not have? Only judge what is listed here."))

	roster := b.Add(i18n.T(lang, "Every character on the account"))
	for _, c := range characters {
		line := i18n.T(lang, "%s: level %d, C%d, talents %d/%d/%d, %d artifacts equipped",
			c.Key, c.Level, c.Constellation, c.TalentAuto, c.TalentSkill, c.TalentBurst, equippedCount[c.Key])
		if w, ok := weaponOf[c.Key]; ok {
			line += i18n.T(lang, ", holding %s R%d", w.Key, w.Refinement)
		} else {
			line += i18n.T(lang, ", no weapon")
		}
		if hasGoal[c.Key] {
			line += i18n.T(lang, ", has a goal")
		} else {
			line += i18n.T(lang, ", no goal set up")
		}
		roster.Line(line)
	}

	s.addAccountFacts(ctx, b, a, inventory, lang)

	method := b.Add(i18n.T(lang, "What Mimir can and cannot say here"))
	method.T(lang, "Nothing on this page has been through the damage engine: a character with no goal has no rotation, and without a rotation there is no number. Say what is worth setting up as a goal rather than claiming a gain.")
	return b, nil
}

// ---------------------------------------------------------------- inventory

func (s *Server) artifactsBrief(ctx context.Context, a model.Account, lang i18n.Lang) (*kvasir.Brief, error) {
	inventory, err := db.LoadArtifacts(s.DB, a.ID)
	if err != nil {
		return nil, err
	}
	if len(inventory) == 0 {
		return nil, fmt.Errorf("%s", i18n.T(lang, "no artifacts have been imported yet"))
	}

	b := kvasir.NewBrief("artifacts", "",
		i18n.T(lang, "The artifact inventory on account %s", a.UID),
		i18n.T(lang, "What should this player do with this inventory — what is worth levelling, what is dead weight, and which domain is worth a week? Do not claim a gain the engine has not measured."))

	s.addInventoryFacts(b, inventory, "", "", lang)

	est, err := advisor.EstimateDropModel(inventory)
	drops := b.Add(i18n.T(lang, "The drop model measured from this inventory"))
	if err != nil {
		drops.T(lang, "There is no measured drop model: %v", err)
		drops.T(lang, "Without one, farming is ranked in artifacts examined rather than in resin.")
	} else {
		drops.T(lang, "Measured from %d five-star artifacts.", est.Sample)
		if est.HasYield {
			drops.T(lang, "Runs can be priced in resin: %s pieces per run.", num(est.Model.PiecesPerRun))
		} else {
			drops.T(lang, "The per-run yield is unknown, so farming cannot be priced in resin. An inventory records what dropped, never how many runs it took.")
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
	b *kvasir.Brief, inventory []model.Artifact, setFilter, slotFilter string, lang i18n.Lang,
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

	totals := b.Add(i18n.T(lang, "The inventory"))
	totals.T(lang, "%d artifacts, %d of them equipped on somebody.", shown, equipped)
	for _, slot := range model.Slots {
		if bySlot[slot] > 0 {
			totals.T(lang, "%s: %d pieces", string(slot), bySlot[slot])
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

	bySet := b.Add(i18n.T(lang, "By set, deepest first"))
	for i, k := range keys {
		if i >= 15 {
			bySet.T(lang, "…and %d further sets with less in them.", len(keys)-15)
			break
		}
		st := sets[k]
		bySet.T(lang, "%s: %d pieces, %d of them five-star, %d at +20, %d equipped, best crit value %s",
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
		best := b.Add(i18n.T(lang, "The best pieces nobody is wearing"))
		best.T(lang, "Crit value is 2×crit rate + crit damage. It is triage, not a verdict — the optimizer decides what is actually worn.")
		for i, art := range spare {
			if i >= 10 {
				break
			}
			best.T(lang, "%s %s +%d, main stat %s, crit value %s%s",
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

// ---------------------------------------------------------------- the goals

func (s *Server) goalsBrief(ctx context.Context, a model.Account, lang i18n.Lang) (*kvasir.Brief, error) {
	keys, err := s.goalKeys(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	characters, err := s.loadCharacters(ctx, a.ID)
	if err != nil {
		return nil, err
	}

	b := kvasir.NewBrief("goals", "",
		i18n.T(lang, "The goals on account %s", a.UID),
		i18n.T(lang, "Are these goals set up so the ranking can be trusted? Name what is missing — an unanswered condition, a rotation that does not match how the character is played, a priority order that fights itself."))

	snap, snapErr := s.GameData.Current()

	list := b.Add(i18n.T(lang, "Goals, highest priority first"))
	for _, key := range keys {
		goal, _, _, err := s.loadGoal(ctx, a.ID, key)
		if err != nil {
			continue
		}
		list.T(lang, "%s: priority %d, rotation %q with %d steps, enemy level %d",
			key, goal.Priority, goal.Spec.Name, len(goal.Spec.Steps), goal.Target.Level)
		for i, step := range goal.Spec.Steps {
			list.T(lang, "    %s step %d: %s %s ×%d", key, i+1, step.Talent, step.Entry, step.Hits)
		}
		for _, k := range sortedKeys(goal.Conditions) {
			list.T(lang, "    %s declared condition: %s = %s", key, k, num(goal.Conditions[k]))
		}

		// Which conditions this goal's own gear would use, but nobody has
		// answered. This is the single most common reason a plan understates
		// a build, so it belongs on the goals page rather than only on the
		// build sheet.
		if snapErr == nil {
			for _, m := range s.undeclaredFor(ctx, a.ID, key, snap, goal.Conditions) {
				list.T(lang, "    %s has not been asked: %s (%s)", key, m.Source, m.Key)
			}
		}
	}
	if len(keys) == 0 {
		list.T(lang, "No goals have been set up, so nothing on this account has been ranked.")
	}

	if len(characters) > len(keys) {
		without := b.Add(i18n.T(lang, "Characters with no goal"))
		has := map[string]bool{}
		for _, k := range keys {
			has[k] = true
		}
		for _, c := range characters {
			if !has[c.Key] {
				without.T(lang, "%s, level %d, C%d", c.Key, c.Level, c.Constellation)
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
	ctx context.Context, b *kvasir.Brief, a model.Account, inventory []model.Artifact, lang i18n.Lang,
) {
	characters, _ := s.loadCharacters(ctx, a.ID)
	weapons, _ := s.loadWeapons(ctx, a.ID)

	sec := b.Add(i18n.T(lang, "The account"))
	sec.T(lang, "%d characters, %d weapons and %d artifacts have been imported.",
		len(characters), len(weapons), len(inventory))
	if a.ARLevel > 0 {
		sec.T(lang, "Adventure rank %d, world level %d.", a.ARLevel, a.WLLevel)
	}
	source := map[string]int{}
	for _, art := range inventory {
		source[art.Source]++
	}
	if source["good"] == 0 && len(inventory) > 0 {
		sec.T(lang, "The inventory came from Enka, which only reports equipped pieces. Everything unequipped is invisible here — a .good file would change that.")
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

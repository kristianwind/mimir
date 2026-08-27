package advisor

import (
	"context"
	"fmt"
	"sort"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// What to aim for, as opposed to what you have.
//
// Every other view here answers a question about the artifacts in the bag:
// what to re-equip, what to level, who is worth building. This one ignores
// the bag entirely and asks what the character wants — which set, which main
// stats, which weapon — so that somebody who has none of it still knows what
// they are farming towards. It is the question people currently answer by
// looking their character up on a wiki.
//
// It is computed rather than looked up, and that is the point. A wiki gives
// one generic recommendation to every player; this is run against this
// character's own constellation, talent levels and rotation, with the numbers
// shown, so it can be checked and it can disagree with the wiki for a reason.
//
// The substats are the one part that is not a fact about the game. A target
// build has no real artifacts in it, so every candidate is given the same
// invented set of rolls — the mined roll values, in the count the game grants
// by +20, allocated to crit and to whatever the character scales on. That
// allocation is the recommendation, not a measurement, and the comparison
// between candidates is what it exists to make fair. It is stated wherever
// the result is shown.

// Target is the build a character should aim for.
type Target struct {
	Character string        `json:"character"`
	Element   model.Element `json:"element"`
	// Sets are the artifact set arrangements, best first.
	Sets []TargetSet `json:"sets"`
	// MainStats is the best main stat for each of the three slots that have
	// a choice. Flower and plume have none.
	MainStats map[model.Slot]model.Stat `json:"mainStats"`
	// Weapon is the weapon every candidate above was scored with. It is
	// held constant rather than chosen: see the caveats.
	Weapon string `json:"weapon,omitempty"`
	// Substats is the allocation every candidate was given, so the reader
	// can see what the ranking held constant.
	Substats map[model.Stat]int `json:"substats"`
	// MeasuredOn names the damage rows the scores are the sum of.
	MeasuredOn []string `json:"measuredOn,omitempty"`
	Caveats    []string `json:"caveats"`
}

// TargetSet is one artifact set arrangement, scored.
type TargetSet struct {
	Config string  `json:"config"`
	Score  float64 `json:"score"`
	// Behind is how far short of the best entry this one falls, as a
	// fraction. Zero on the winner.
	Behind float64 `json:"behind"`
	// Owned says whether the account can already assemble this.
	Owned bool `json:"owned"`
}

// TargetRequest is one character to describe a target for.
type TargetRequest struct {
	Snapshot  *gamedata.Snapshot
	Character model.Character
	// Owned lets the answer mark what the account already has. It is used
	// for labelling only: the search does not prefer what is owned, because
	// the whole question is what to farm towards.
	OwnedSets    map[string]bool
	OwnedWeapons map[string]int
	Conditions   map[string]float64
}

// targetSlots are the three slots whose main stat is a decision. A flower is
// always flat HP and a plume always flat ATK; offering a choice there would
// be offering a choice the game does not have.
var targetSlots = []model.Slot{model.Sands, model.Goblet, model.Circlet}

// BuildTarget works out what a character should aim for.
func BuildTarget(ctx context.Context, req TargetRequest) (Target, error) {
	snap := req.Snapshot
	if snap == nil {
		return Target{}, fmt.Errorf("advisor: no game data snapshot")
	}
	if len(snap.ArtifactRolls) == 0 {
		return Target{}, fmt.Errorf("%w: artifact roll counts, so a target build has no "+
			"consistent substat treatment to compare candidates under", gamedata.ErrMissing)
	}
	def, err := snap.Char(req.Character.Key)
	if err != nil {
		return Target{}, err
	}

	out := Target{
		Character: req.Character.Key,
		Element:   defElement(snap, req.Character.Key),
		MainStats: map[model.Slot]model.Stat{},
		Caveats: []string{
			"This is what the character wants, not what your bag holds. Nothing here is filtered by what you own; the entries you can already assemble are marked.",
			"Measured the same way as everything else in Mimir: one cast of the elemental skill and one of the burst, at this character's own talent levels, against a level 90 enemy with 10% resistance. No teams, no rotations, no reactions.",
			"The substats are the same invented allocation on every candidate, so the ranking is fair between them. They are not a claim about any artifact you will actually roll.",
			"Weapons are not ranked. Most of what makes a weapon good is its passive, and the passives are mined as wording rather than as numbers — four of the two hundred and forty-seven are modelled. A ranking on base attack and substat alone would put a four-star above a five-star and look like advice.",
		},
	}

	// The order matters and it is not arbitrary. Main stats are chosen
	// first, on a neutral set, because a goblet's element bonus is worth
	// more than any set bonus and would otherwise be decided by whichever
	// set happened to be searched with it. Then the set, on those main
	// stats. Then the weapon, on both.
	// Something has to be held in hand for the numbers to mean anything, and
	// what it is changes the answer: a weapon with energy recharge on it
	// moves what the sands should be. So one is chosen up front and kept
	// constant across every candidate.
	weapon := standInWeapon(req, def)
	if weapon != nil {
		out.Weapon = weapon.Key
	}

	mains, err := bestMainStats(ctx, req, weapon, def)
	if err != nil {
		return Target{}, err
	}
	out.MainStats = mains

	out.Sets, err = rankSets(ctx, req, weapon, def, mains)
	if err != nil {
		return Target{}, err
	}

	out.Substats = targetSubstats(snap, def)
	if spec, err := DeriveSpec(snap, req.Character); err == nil {
		for _, step := range spec.Steps {
			out.MeasuredOn = append(out.MeasuredOn, step.Talent+": "+step.Entry)
		}
	}
	return out, nil
}

// targetSubstats is the allocation every candidate is given.
//
// Crit rate and crit damage in a one-to-two ratio, because that is the ratio
// that maximises their product, and the rest into whatever the character's
// damage scales on. It is a recommendation stated as an allocation rather
// than a measurement, which is why it is reported alongside the answer.
func targetSubstats(snap *gamedata.Snapshot, def gamedata.Character) map[model.Stat]int {
	rolls := snap.ArtifactRolls[5] * len(model.Slots)
	if rolls <= 0 {
		return nil
	}
	scaling := scalingStat(def)

	out := map[model.Stat]int{}
	// Two rolls of crit damage for every one of crit rate, then a third of
	// the total into the scaling stat.
	toScaling := rolls / 3
	crit := rolls - toScaling
	out[model.CritRate] = crit / 3
	out[model.CritDMG] = crit - crit/3
	out[scaling] = toScaling
	return out
}

// scalingStat is what this character's damage is multiplied by.
func scalingStat(def gamedata.Character) model.Stat {
	counts := map[model.Stat]int{}
	for _, slot := range []string{gamedata.TalentSkill, gamedata.TalentBurst, gamedata.TalentAuto} {
		for _, entry := range def.Talents[slot].Entries {
			if entry.IsDamage() {
				counts[entry.Scaling]++
			}
		}
	}
	best, most := model.ATKPercent, 0
	for stat, n := range counts {
		if n > most {
			best, most = percentOf(stat), n
		}
	}
	return best
}

// percentOf maps a scaling stat onto the substat that raises it.
func percentOf(s model.Stat) model.Stat {
	switch s {
	case model.HP, model.HPPercent:
		return model.HPPercent
	case model.DEF, model.DEFPercent:
		return model.DEFPercent
	case model.ElementalMastery:
		return model.ElementalMastery
	default:
		return model.ATKPercent
	}
}

// substatBlock turns the allocation into stats, using the mined roll values.
func substatBlock(snap *gamedata.Snapshot, alloc map[model.Stat]int) (model.StatBlock, error) {
	out := model.StatBlock{}
	table, ok := snap.SubstatRolls[5]
	if !ok {
		return nil, fmt.Errorf("%w: five-star substat roll values", gamedata.ErrMissing)
	}
	for stat, n := range alloc {
		values, ok := table[stat]
		if !ok || len(values) == 0 {
			return nil, fmt.Errorf("%w: roll values for %s", gamedata.ErrMissing, stat)
		}
		// The average of the mined tiers. Using the highest would describe a
		// build nobody rolls; using the lowest would understate every
		// candidate equally, which is harmless but reads as pessimism.
		var sum float64
		for _, v := range values {
			sum += v
		}
		out[stat] += sum / float64(len(values)) * float64(n)
	}
	return out, nil
}

// targetLoadout builds a hypothetical five-piece set at +20.
func targetLoadout(
	snap *gamedata.Snapshot, ch model.Character, weapon *model.Weapon,
	set string, mains map[model.Slot]model.Stat,
) Loadout {
	pieces := make([]model.Artifact, 0, len(model.Slots))
	for i, slot := range model.Slots {
		main := fixedMain(slot)
		if main == "" {
			main = mains[slot]
		}
		pieces = append(pieces, model.Artifact{
			// Negative ids so a hypothetical piece can never be confused
			// with one out of the database.
			ID: int64(-(i + 1)), SetKey: set, SlotKey: slot,
			Rarity: 5, Level: maxLevel(snap), MainStat: main,
		})
	}
	return Loadout{Character: ch, Weapon: weapon, Artifacts: pieces}
}

func fixedMain(slot model.Slot) model.Stat {
	switch slot {
	case model.Flower:
		return model.HP
	case model.Plume:
		return model.ATK
	default:
		return ""
	}
}

// maxLevel is the level a five-star artifact caps at, read from the mined
// main-stat curve rather than assumed.
func maxLevel(snap *gamedata.Snapshot) int {
	for _, byStat := range snap.MainStatValues[5] {
		if n := len(byStat); n > 0 {
			return n - 1
		}
	}
	return 20
}

// scoreTarget measures one hypothetical build.
func scoreTarget(
	ctx context.Context, req TargetRequest, loadout Loadout, extra model.StatBlock,
) (float64, error) {
	state, err := Assemble(req.Snapshot, loadout)
	if err != nil {
		return 0, err
	}
	state.Fixed = state.Fixed.Add(extra)
	eval := yardstickEvaluator{Snapshot: req.Snapshot, Conditions: req.Conditions}
	return eval.Score(ctx, Goal{}, state)
}

// bestMainStats picks each slot's main stat on a neutral set.
func bestMainStats(
	ctx context.Context, req TargetRequest, weapon *model.Weapon, def gamedata.Character,
) (map[model.Slot]model.Stat, error) {
	subs, err := substatBlock(req.Snapshot, targetSubstats(req.Snapshot, def))
	if err != nil {
		return nil, err
	}

	chosen := map[model.Slot]model.Stat{}
	for _, slot := range targetSlots {
		var best model.Stat
		var bestScore float64
		for _, stat := range mainStatChoices(req.Snapshot, slot) {
			trial := map[model.Slot]model.Stat{}
			for k, v := range chosen {
				trial[k] = v
			}
			trial[slot] = stat
			for _, other := range targetSlots {
				if _, ok := trial[other]; !ok {
					trial[other] = mainStatChoices(req.Snapshot, other)[0]
				}
			}
			score, err := scoreTarget(ctx, req,
				targetLoadout(req.Snapshot, req.Character, weapon, "", trial), subs)
			if err != nil {
				continue
			}
			if score > bestScore {
				best, bestScore = stat, score
			}
		}
		if best == "" {
			return nil, fmt.Errorf("advisor: no main stat could be scored for %s", slot)
		}
		chosen[slot] = best
	}
	return chosen, nil
}

// mainStatChoices lists the main stats a slot can roll, from the mined table.
func mainStatChoices(snap *gamedata.Snapshot, slot model.Slot) []model.Stat {
	var out []model.Stat
	for stat := range snap.MainStatValues[5] {
		if !mainStatAllowed(slot, stat) {
			continue
		}
		out = append(out, stat)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// mainStatAllowed reports whether a slot can carry a main stat.
//
// The game does not publish this as a table, but it is not a balance number
// either: a sands never rolls an elemental bonus and a circlet never rolls
// energy recharge. Getting it wrong would offer a build that cannot exist.
func mainStatAllowed(slot model.Slot, stat model.Stat) bool {
	switch stat {
	case model.HP, model.ATK, model.DEF:
		return false // the flat versions are flower and plume only
	}
	switch slot {
	case model.Sands:
		switch stat {
		case model.HPPercent, model.ATKPercent, model.DEFPercent,
			model.ElementalMastery, model.EnergyRecharge:
			return true
		}
	case model.Goblet:
		switch stat {
		case model.HPPercent, model.ATKPercent, model.DEFPercent,
			model.ElementalMastery, model.PhysicalDMG:
			// Physical belongs here even though it is not an element: a
			// goblet carries it, and leaving it out would refuse to
			// describe a physical build at all.
			return true
		}
		return model.IsElementalDMG(stat)
	case model.Circlet:
		switch stat {
		case model.HPPercent, model.ATKPercent, model.DEFPercent,
			model.ElementalMastery, model.CritRate, model.CritDMG, model.HealingBonus:
			return true
		}
	}
	return false
}

// rankSets scores every four-piece set on the chosen main stats.
func rankSets(
	ctx context.Context, req TargetRequest, weapon *model.Weapon, def gamedata.Character,
	mains map[model.Slot]model.Stat,
) ([]TargetSet, error) {
	subs, err := substatBlock(req.Snapshot, targetSubstats(req.Snapshot, def))
	if err != nil {
		return nil, err
	}

	// Only sets a domain actually drops at five stars.
	//
	// The obvious filter — the rarities recorded on the set — does not work:
	// it lists every rarity the set has pieces for, so Berserker comes back
	// as a five-star set and its 12% crit rate two-piece then outranks half
	// the real ones. A set is farmable at five stars if and only if some
	// domain's five-star reward preview lists it, and that is mined.
	farmable := map[string]bool{}
	for _, d := range req.Snapshot.Domains {
		if d.Kind != "artifact" {
			continue
		}
		for _, set := range d.Sets {
			farmable[set] = true
		}
	}
	if len(farmable) == 0 {
		return nil, fmt.Errorf("%w: artifact domains, so Mimir cannot say which sets are farmable",
			gamedata.ErrMissing)
	}

	keys := make([]string, 0, len(farmable))
	for key := range farmable {
		if _, ok := req.Snapshot.ArtifactSets[key]; ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	var out []TargetSet
	for _, key := range keys {
		score, err := scoreTarget(ctx, req,
			targetLoadout(req.Snapshot, req.Character, weapon, key, mains), subs)
		if err != nil || score <= 0 {
			continue
		}
		out = append(out, TargetSet{
			Config: key, Score: score, Owned: req.OwnedSets[key],
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > 8 {
		out = out[:8]
	}
	fillBehind(len(out), func(i int) float64 { return out[i].Score },
		func(i int, g float64) { out[i].Behind = g })
	return out, nil
}

// standInWeapon picks what to hold while the artifacts are being decided.
//
// The account's own best of the type when it has one, so the intermediate
// builds are close to what the player actually holds; otherwise the highest
// base attack of the type, which is a stand-in and is named as one.
//
// Mimir does not rank weapons, and this is not a recommendation. See the
// caveat on the result: a weapon's passive is most of what makes it good, and
// the passives are not mined as numbers.
func standInWeapon(req TargetRequest, def gamedata.Character) *model.Weapon {
	var best string
	var bestRarity int
	var bestATK float64

	consider := func(key string, refinement int) {
		w, ok := req.Snapshot.Weapons[key]
		if !ok || w.Type != def.WeaponType {
			return
		}
		if w.Rarity > bestRarity || (w.Rarity == bestRarity && w.BaseATK > bestATK) {
			best, bestRarity, bestATK = key, w.Rarity, w.BaseATK
		}
	}
	for key, refinement := range req.OwnedWeapons {
		consider(key, refinement)
	}
	if best == "" {
		keys := make([]string, 0, len(req.Snapshot.Weapons))
		for key := range req.Snapshot.Weapons {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			consider(key, 1)
		}
	}
	if best == "" {
		return nil
	}
	refinement := req.OwnedWeapons[best]
	if refinement < 1 {
		refinement = 1
	}
	return &model.Weapon{Key: best, Level: 90, Ascension: 6, Refinement: refinement}
}

// fillBehind records how far short of the winner each entry falls.
//
// Against the best rather than against the next one down, because a list of
// consecutive gaps is unreadable: the second entry being 1% ahead of the third
// says nothing about whether either is worth farming.
func fillBehind(n int, score func(int) float64, set func(int, float64)) {
	if n == 0 {
		return
	}
	best := score(0)
	if best <= 0 {
		return
	}
	for i := 0; i < n; i++ {
		set(i, 1-score(i)/best)
	}
}

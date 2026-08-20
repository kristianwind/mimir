package calc

import (
	"fmt"

	"github.com/kristianwind/mimir/internal/model"
)

// Target describes the enemy side of the equation.
//
// Defaults matter for honesty: Mimir reports against a named, stated target
// (level 100 hilichurl at 10% RES, say) and shows it in the UI, because a DPS
// number without a target is meaningless and two tools that disagree usually
// just assumed different enemies.
type Target struct {
	Level int
	// Resistance is base elemental resistance, e.g. 0.10 for the standard
	// 10%. Missing entries are treated as zero.
	Resistance map[model.Element]float64
	// ResReduction is shred applied by the team (Viridescent, Zhongli, ...).
	ResReduction map[model.Element]float64
	// DefReduction is DEF shred (Raiden C2, Lyney, Candace burst).
	DefReduction float64
	// DefIgnore is DEF ignore, which is multiplicatively separate from shred.
	DefIgnore float64
}

// DefenseMultiplier is the enemy DEF term.
//
//	(lvl_char + 100) / ((lvl_char + 100) + (lvl_enemy + 100)(1 - shred)(1 - ignore))
func (t Target) DefenseMultiplier(attackerLevel int) float64 {
	a := float64(attackerLevel + 100)
	e := float64(t.Level+100) * (1 - t.DefReduction) * (1 - t.DefIgnore)
	if e < 0 {
		e = 0
	}
	return a / (a + e)
}

// ResistanceMultiplier is the piecewise RES term. Negative resistance is
// halved, resistance at or above 75% flips to the diminishing branch.
func (t Target) ResistanceMultiplier(e model.Element) float64 {
	res := t.Resistance[e] - t.ResReduction[e]
	switch {
	case res < 0:
		return 1 - res/2
	case res < 0.75:
		return 1 - res
	default:
		return 1 / (1 + 4*res)
	}
}

// WithDebuffs returns the target as the build's own debuffs leave it.
//
// Resistance shred and DEF reduction arrive through the same stat block as
// everything else — they come from set bonuses and constellations, so they
// have to. This is where they stop being stats and become properties of the
// enemy, which is the only place they make sense.
func (t Target) WithDebuffs(stats model.StatBlock) Target {
	out := Target{
		Level:        t.Level,
		DefReduction: t.DefReduction + stats[model.TargetDefReduction],
		DefIgnore:    t.DefIgnore + stats[model.TargetDefIgnore],
		Resistance:   t.Resistance,
		ResReduction: make(map[model.Element]float64, len(t.ResReduction)+len(model.Elements)),
	}
	for k, v := range t.ResReduction {
		out.ResReduction[k] = v
	}
	for _, e := range append(append([]model.Element{}, model.Elements...), model.Physical) {
		if v := stats[model.TargetResShred(e)]; v != 0 {
			out.ResReduction[e] += v
		}
	}
	// Shred beyond total immunity is not a thing, and letting it run makes
	// the negative-resistance branch pay out forever.
	if out.DefReduction > 1 {
		out.DefReduction = 1
	}
	if out.DefIgnore > 1 {
		out.DefIgnore = 1
	}
	return out
}

// Instance is a single damage hit to evaluate.
//
// It crosses the API boundary — a build sheet lists the hits an effect adds —
// so its fields carry JSON tags rather than shipping Go names to the client.
type Instance struct {
	Label string `json:"label"`
	// Element is the damage type. Physical is valid.
	Element model.Element `json:"element"`
	// Category selects the attack-category DMG bonus this instance picks
	// up. Empty means none — a reaction hit, say.
	Category model.Category `json:"category,omitempty"`
	// Scaling names the stat the talent multiplier applies to.
	Scaling model.Stat `json:"scaling"`
	// Multiplier is the talent scaling as a fraction: 368% is 3.68.
	Multiplier float64 `json:"multiplier"`
	// FlatBonus is additive damage applied after scaling but before the
	// DMG-bonus term (Ganyu's C4-style additions belong in Extra instead;
	// this is for true flat additions such as Xingqiu's C2 is not — see
	// docs/DATAMODEL.md for the classification table).
	FlatBonus float64 `json:"flatBonus,omitempty"`
	// Extra holds bonuses that apply only to this instance, e.g. a talent's
	// own DMG% or a passive's conditional crit rate. Merged over the
	// character's totals for this calculation only.
	Extra model.StatBlock `json:"extra,omitempty"`
	// Amplify names an amplifying reaction, or "" for none.
	Amplify Amplifying `json:"amplify,omitempty"`
}

// Result is a damage instance broken into its parts so the UI (and the AI
// layer) can explain *why* a number is what it is, not just report it.
type Result struct {
	Label string `json:"label"`

	NonCrit float64 `json:"nonCrit"`
	Crit    float64 `json:"crit"`
	Average float64 `json:"average"`

	// Terms are the intermediate multipliers, in formula order.
	ScalingStat    float64 `json:"scalingStat"`
	TalentMult     float64 `json:"talentMultiplier"`
	DMGBonus       float64 `json:"dmgBonus"`
	CritRate       float64 `json:"critRate"`
	CritDMG        float64 `json:"critDamage"`
	DefenseMult    float64 `json:"defenseMultiplier"`
	ResistanceMult float64 `json:"resistanceMultiplier"`
	AmplifyMult    float64 `json:"amplifyMultiplier"`
}

// Damage evaluates one instance.
//
//	outgoing = talent% × scaling stat + flat
//	         × (1 + DMG bonus)
//	         × crit
//	         × DEF multiplier
//	         × RES multiplier
//	         × amplifying reaction
func Damage(attackerLevel int, totals model.StatBlock, inst Instance, tgt Target) Result {
	stats := totals
	if len(inst.Extra) > 0 {
		stats = totals.Add(inst.Extra)
	}

	scaling := Scaling(stats, inst.Scaling)
	base := inst.Multiplier*scaling + inst.FlatBonus

	// DMG bonus is additive across three independent sources: the element,
	// the attack category, and any all-type bonus.
	bonus := stats[model.DMGBonusStat(inst.Element)] + stats[model.AllDMG]
	if key := model.CategoryDMGStat(inst.Category); key != "" {
		bonus += stats[key]
	}

	// Crit can be scoped to an attack category too — The Catch raises burst
	// crit rate and nothing else — so the category's share is added before
	// the multipliers are derived.
	cr := stats[model.CritRate]
	cd := stats[model.CritDMG]
	if inst.Category != "" {
		cr += stats[model.CategoryScoped(inst.Category, model.CritRate)]
		cd += stats[model.CategoryScoped(inst.Category, model.CritDMG)]
	}
	if cr > 1 {
		cr = 1
	}
	if cr < 0 {
		cr = 0
	}

	nonCritMul, critMul, avgMul := 1.0, 1+cd, 1+cr*cd
	def := tgt.DefenseMultiplier(attackerLevel)
	res := tgt.ResistanceMultiplier(inst.Element)
	amp := inst.Amplify.Multiplier(stats[model.ElementalMastery], stats)

	common := base * (1 + bonus) * def * res * amp

	return Result{
		Label:          inst.Label,
		NonCrit:        common * nonCritMul,
		Crit:           common * critMul,
		Average:        common * avgMul,
		ScalingStat:    scaling,
		TalentMult:     inst.Multiplier,
		DMGBonus:       bonus,
		CritRate:       cr,
		CritDMG:        cd,
		DefenseMult:    def,
		ResistanceMult: res,
		AmplifyMult:    amp,
	}
}

// Rotation is an ordered list of instances that together form one loop of a
// team's damage cycle. Total DPS divides by Duration.
type Rotation struct {
	Name      string     `json:"name"`
	Instances []Instance `json:"instances"`
	// Duration is the rotation length in seconds. Zero means "report total
	// damage only" — useful for burst-window comparisons.
	Duration float64 `json:"duration"`
}

// Evaluate runs every instance in a rotation.
func Evaluate(attackerLevel int, totals model.StatBlock, rot Rotation, tgt Target) (RotationResult, error) {
	if attackerLevel < 1 {
		return RotationResult{}, fmt.Errorf("calc: attacker level %d is invalid", attackerLevel)
	}
	out := RotationResult{Name: rot.Name, Duration: rot.Duration}
	for _, inst := range rot.Instances {
		r := Damage(attackerLevel, totals, inst, tgt)
		out.Hits = append(out.Hits, r)
		out.Total += r.Average
	}
	if rot.Duration > 0 {
		out.DPS = out.Total / rot.Duration
	}
	return out, nil
}

// RotationResult is the per-hit breakdown plus the totals.
type RotationResult struct {
	Name     string   `json:"name"`
	Hits     []Result `json:"hits"`
	Total    float64  `json:"total"`
	Duration float64  `json:"duration"`
	DPS      float64  `json:"dps"`
}

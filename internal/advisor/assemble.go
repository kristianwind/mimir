package advisor

import (
	"fmt"

	"github.com/kristianwind/mimir/internal/calc"
	"github.com/kristianwind/mimir/internal/effect"
	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
	"github.com/kristianwind/mimir/internal/optimizer"
)

// Loadout is what a character is actually wearing.
type Loadout struct {
	Character model.Character
	Weapon    *model.Weapon
	Artifacts []model.Artifact
	// Buffs are team-provided bonuses: Bennett's ATK, a Viridescent shred,
	// a Noblesse aura. They are an input rather than something Mimir infers,
	// because "what does your team actually do" is a modelling choice the
	// player owns.
	Buffs model.StatBlock
}

// Assemble resolves a loadout into the state the evaluator scores.
//
// This is the seam where the database, the game data and the engine meet, and
// it is deliberately the only place that knows how a character's numbers are
// put together: base stats from the level and ascension, the ascension stat,
// the weapon's base ATK and substat, then artifacts and team buffs.
func Assemble(snap *gamedata.Snapshot, l Loadout) (State, error) {
	def, err := snap.Char(l.Character.Key)
	if err != nil {
		return State{}, err
	}

	hp, atk, dfn, err := snap.BaseStats(def, l.Character.Level, l.Character.Ascension)
	if err != nil {
		return State{}, err
	}

	fixed := model.StatBlock{}
	if def.AscensionStat != "" {
		fixed[def.AscensionStat] += def.AscensionStatValue(l.Character.Ascension)
	}
	// Every character has 5% crit rate, 50% crit damage and 100% energy
	// recharge before anything is equipped. Leaving these out is the classic
	// way to under-report a build by exactly one crit multiplier.
	fixed[model.CritRate] += 0.05
	fixed[model.CritDMG] += 0.50
	fixed[model.EnergyRecharge] += 1.0

	if l.Weapon != nil {
		w, ok := snap.Weapons[l.Weapon.Key]
		if !ok {
			return State{}, fmt.Errorf("%w: weapon %q", gamedata.ErrMissing, l.Weapon.Key)
		}
		baseATK, err := snap.WeaponBaseATK(w, l.Weapon.Level, l.Weapon.Ascension)
		if err != nil {
			return State{}, err
		}
		atk += baseATK

		sub, err := snap.WeaponSubValue(w, l.Weapon.Level)
		if err != nil {
			return State{}, err
		}
		if w.SubStat != "" {
			fixed[w.SubStat] += sub
		}
		// Refinement-scaled passive stats, where the passive is
		// unconditional. Conditional passives belong to the effect layer.
		if idx := l.Weapon.Refinement - 1; idx >= 0 && idx < len(w.PassiveStats) {
			for k, v := range w.PassiveStats[idx] {
				fixed[k] += v
			}
		}
	}

	fixed.AddInto(l.Buffs)

	stats := make(map[int64]model.StatBlock, len(l.Artifacts))
	for _, a := range l.Artifacts {
		block, err := optimizer.ArtifactStats(a, snap)
		if err != nil {
			return State{}, err
		}
		stats[a.ID] = block
	}

	return State{
		Character:        l.Character,
		Base:             calc.Base{HP: hp, ATK: atk, DEF: dfn},
		Fixed:            fixed,
		Equipped:         l.Artifacts,
		ArtifactStats:    stats,
		WeaponKey:        weaponKey(l.Weapon),
		WeaponRefinement: weaponRefinement(l.Weapon),
	}, nil
}

// Totals resolves a state into the final stat block, set bonuses and effects
// included. It is what the character sheet in the UI shows.
func Totals(snap *gamedata.Snapshot, s State, conditions map[string]float64) (model.StatBlock, error) {
	bonuses := s.Fixed.Clone()
	for _, a := range s.Equipped {
		block, ok := s.ArtifactStats[a.ID]
		if !ok {
			return nil, fmt.Errorf("advisor: no stats for artifact %d", a.ID)
		}
		bonuses.AddInto(block)
	}
	counts := optimizer.SetCounts(s.Equipped)
	setBonus, err := optimizer.SetBonuses(counts, snap)
	if err != nil {
		return nil, err
	}
	bonuses.AddInto(setBonus)

	return resolveWithEffects(snap, s.Base, bonuses, effect.Context{
		Snapshot:   snap,
		Character:  s.Character,
		SetCounts:  counts,
		WeaponKey:  s.WeaponKey,
		Refinement: s.WeaponRefinement,
		Conditions: conditions,
	})
}

// resolveWithEffects turns a bonus block into final totals, running the
// effect layer on both sides of the resolve.
//
// The two phases exist because conversions cannot run before the stat they
// convert exists. A granted ATK% has to land before base ATK is multiplied
// out; Emblem's Burst DMG has to be computed from the *final* Energy
// Recharge, which is only known afterwards. Doing either in the wrong order
// silently changes the answer.
func resolveWithEffects(
	snap *gamedata.Snapshot,
	base calc.Base,
	bonuses model.StatBlock,
	ectx effect.Context,
) (model.StatBlock, error) {
	if len(snap.Effects) == 0 {
		return calc.Resolve(base, bonuses), nil
	}

	pre, err := effect.Apply(snap.Effects, ectx, gamedata.EffectPhasePre, bonuses)
	if err != nil {
		return nil, err
	}
	totals := calc.Resolve(base, bonuses.Add(pre))

	post, err := effect.Apply(snap.Effects, ectx, gamedata.EffectPhasePost, totals)
	if err != nil {
		return nil, err
	}
	totals.AddInto(post)
	return totals, nil
}

// Sheet is a fully resolved build with its provenance.
type Sheet struct {
	Totals model.StatBlock `json:"totals"`
	// Effects records what the conditional layer contributed and which
	// in-game text each grant was checked against.
	Effects []effect.Grant `json:"effects"`
	// Instances are the extra damage hits effects add to the rotation —
	// procs and constellation explosions that a stat-only model cannot see.
	Instances []effect.InstanceGrant `json:"instances"`
	// SetCounts is what the build is wearing.
	SetCounts map[string]int `json:"setCounts"`
	// Undeclared lists conditional effects that are active on this build but
	// switched off because nobody answered their question.
	//
	// No omitempty: "nothing to ask about" and "the field is missing" are
	// different answers, and a client that has to tell them apart should not
	// have to guess.
	Undeclared []effect.Missing `json:"undeclared"`
}

// BuildSheet resolves a state and reports where every effect-derived number
// came from.
func BuildSheet(snap *gamedata.Snapshot, s State, conditions map[string]float64) (Sheet, error) {
	bonuses := s.Fixed.Clone()
	for _, a := range s.Equipped {
		block, ok := s.ArtifactStats[a.ID]
		if !ok {
			return Sheet{}, fmt.Errorf("advisor: no stats for artifact %d", a.ID)
		}
		bonuses.AddInto(block)
	}
	counts := optimizer.SetCounts(s.Equipped)
	setBonus, err := optimizer.SetBonuses(counts, snap)
	if err != nil {
		return Sheet{}, err
	}
	bonuses.AddInto(setBonus)

	ectx := effect.Context{
		Snapshot:   snap,
		Character:  s.Character,
		SetCounts:  counts,
		WeaponKey:  s.WeaponKey,
		Refinement: s.WeaponRefinement,
		Conditions: conditions,
	}

	pre, preTrace, err := effect.ApplyTraced(snap.Effects, ectx, gamedata.EffectPhasePre, bonuses)
	if err != nil {
		return Sheet{}, err
	}
	totals := calc.Resolve(s.Base, bonuses.Add(pre))

	post, postTrace, err := effect.ApplyTraced(snap.Effects, ectx, gamedata.EffectPhasePost, totals)
	if err != nil {
		return Sheet{}, err
	}
	totals.AddInto(post)

	instances, err := effect.Instances(snap.Effects, ectx, totals, defElement(snap, s.Character.Key))
	if err != nil {
		return Sheet{}, err
	}
	if instances == nil {
		instances = []effect.InstanceGrant{}
	}

	// Non-nil slices: a JSON null and an empty list mean different things to
	// a caller, and "no effects fired" is a list of length zero.
	grants := append(preTrace, postTrace...)
	if grants == nil {
		grants = []effect.Grant{}
	}
	undeclared := effect.Undeclared(snap.Effects, ectx)
	if undeclared == nil {
		undeclared = []effect.Missing{}
	}

	return Sheet{
		Totals:     totals,
		Effects:    grants,
		Instances:  instances,
		SetCounts:  counts,
		Undeclared: undeclared,
	}, nil
}

// withEffectInstances appends the damage hits a build's effects add.
//
// They are appended rather than folded into the spec because they are not
// something the player rotates — Prototype Archaic's proc happens to them,
// it is not a button they press — and keeping them separate means the
// rotation editor stays a description of play rather than of gear.
func withEffectInstances(
	snap *gamedata.Snapshot,
	ectx effect.Context,
	rot calc.Rotation,
	totals model.StatBlock,
	element model.Element,
) (calc.Rotation, []effect.InstanceGrant, error) {
	if len(snap.Effects) == 0 {
		return rot, nil, nil
	}
	grants, err := effect.Instances(snap.Effects, ectx, totals, element)
	if err != nil {
		return calc.Rotation{}, nil, err
	}
	if len(grants) == 0 {
		return rot, nil, nil
	}
	// Copy before appending: the caller's rotation is reused across every
	// candidate build in a search, and appending into its backing array is
	// the kind of aliasing that produces a wrong number once in a hundred
	// runs and is never reproducible.
	merged := make([]calc.Instance, 0, len(rot.Instances)+len(grants))
	merged = append(merged, rot.Instances...)
	for _, g := range grants {
		merged = append(merged, g.Instance)
	}
	rot.Instances = merged
	return rot, grants, nil
}

// effectContext builds the evaluation context for a state.
func effectContext(snap *gamedata.Snapshot, s State, conditions map[string]float64) effect.Context {
	return effect.Context{
		Snapshot:   snap,
		Character:  s.Character,
		SetCounts:  optimizer.SetCounts(s.Equipped),
		WeaponKey:  s.WeaponKey,
		Refinement: s.WeaponRefinement,
		Conditions: conditions,
	}
}

// defElement returns a character's element, or empty when unknown.
func defElement(snap *gamedata.Snapshot, key string) model.Element {
	def, err := snap.Char(key)
	if err != nil {
		return ""
	}
	return def.Element
}

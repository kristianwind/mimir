package advisor

import (
	"context"
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/calc"
	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// planSnapshot is a miniature but complete game: one character with a real
// talent table, two weapons, two artifact sets and a domain.
func planSnapshot() *gamedata.Snapshot {
	snap := simSnapshot()
	snap.Curves = map[string][]float64{
		"FLAT": flatCurve(90),
	}
	snap.Characters = map[string]gamedata.Character{
		"Tester": {
			Key: "Tester", Name: "Tester", Element: model.Pyro,
			WeaponType: "sword", Rarity: 5,
			BaseHP: 1000, BaseATK: 100, BaseDEF: 500,
			CurveHP: "FLAT", CurveATK: "FLAT", CurveDEF: "FLAT",
			AscensionStat:  model.CritDMG,
			AscensionBonus: []float64{0, 0, 0.096, 0.192, 0.192, 0.288, 0.384},
			PromoteATK:     []float64{0, 10, 20, 30, 40, 50, 60},
			PromoteHP:      []float64{0, 100, 200, 300, 400, 500, 600},
			PromoteDEF:     []float64{0, 10, 20, 30, 40, 50, 60},
			SkillIDs:       gamedata.SkillIDs{Auto: 1, Skill: 2, Burst: 3},
			Talents: map[string]gamedata.Talent{
				gamedata.TalentSkill: {
					Name: "Test Skill",
					Entries: []gamedata.TalentEntry{{
						Label: "Skill DMG", Unit: "percent", Scaling: model.ATK, Element: model.Pyro,
						Multipliers: ladder(1.0, 0.15, 10),
					}},
				},
				gamedata.TalentBurst: {
					Name: "Test Burst",
					Entries: []gamedata.TalentEntry{{
						Label: "Burst DMG", Unit: "percent", Scaling: model.ATK, Element: model.Pyro,
						Multipliers: ladder(2.0, 0.2, 10),
					}},
				},
			},
		},
	}
	snap.Weapons = map[string]gamedata.Weapon{
		"Starter":   {Key: "Starter", Name: "Starter", Type: "sword", Rarity: 3, BaseATK: 200, CurveATK: "FLAT"},
		"Upgraded":  {Key: "Upgraded", Name: "Upgraded", Type: "sword", Rarity: 5, BaseATK: 600, CurveATK: "FLAT", SubStat: model.CritDMG, SubValue: 0.60, CurveSub: "FLAT"},
		"WrongType": {Key: "WrongType", Name: "Wrong Type", Type: "bow", Rarity: 5, BaseATK: 900, CurveATK: "FLAT"},
	}
	snap.ArtifactSets = map[string]gamedata.ArtifactSet{
		"A": {Key: "A", Name: "Set A", TwoPiece: model.StatBlock{model.ATKPercent: 0.18}},
		"B": {Key: "B", Name: "Set B", TwoPiece: model.StatBlock{model.PyroDMG: 0.15}},
	}
	snap.Domains = map[string]gamedata.Domain{
		"testdomain": {Key: "testdomain", Name: "Test Domain", Kind: "artifact", Sets: []string{"A", "B"}, ResinCost: 20},
	}
	snap.ResinCosts = map[string]float64{"talent_domain": 20, "artifact_domain": 20}
	return snap
}

func flatCurve(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = 1 + float64(i)*0.05
	}
	return out
}

func ladder(start, step float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = start + float64(i)*step
	}
	return out
}

func planSpec() Spec {
	return Spec{
		Name:     "test",
		Duration: 20,
		Steps: []Step{
			{Talent: gamedata.TalentBurst, Entry: "Burst DMG", Hits: 1},
			{Talent: gamedata.TalentSkill, Entry: "Skill DMG", Hits: 2},
		},
	}
}

func planInventory() []model.Artifact {
	// Two sets across all five slots, with one clearly superior piece per
	// slot sitting unequipped.
	var inv []model.Artifact
	var id int64
	mains := map[model.Slot]model.Stat{
		model.Flower: model.HP, model.Plume: model.ATK, model.Sands: model.ATKPercent,
		model.Goblet: model.PyroDMG, model.Circlet: model.CritDMG,
	}
	for _, set := range []string{"A", "B"} {
		for _, slot := range model.Slots {
			for tier := 0; tier < 2; tier++ {
				id++
				inv = append(inv, model.Artifact{
					ID: id, SetKey: set, SlotKey: slot, Rarity: 5, Level: 20,
					MainStat: mains[slot],
					Substats: []model.Substat{
						{Key: model.CritRate, Value: 0.05 + 0.05*float64(tier)},
						{Key: model.CritDMG, Value: 0.10 + 0.10*float64(tier)},
						{Key: model.ATKPercent, Value: 0.05 + 0.05*float64(tier)},
					},
				})
			}
		}
	}
	return inv
}

func planRequest(t *testing.T) Request {
	t.Helper()
	snap := planSnapshot()
	inv := planInventory()

	// Equip the worse piece of set A in every slot, so there is a free
	// upgrade to find.
	var equipped []model.Artifact
	for i := range inv {
		if inv[i].SetKey == "A" && i%2 == 0 {
			inv[i].Location = "Tester"
			equipped = append(equipped, inv[i])
		}
	}

	return Request{
		Snapshot: snap,
		Goal: Goal{
			CharacterKey: "Tester",
			Spec:         planSpec(),
			Target:       calc.Target{Level: 90, Resistance: map[model.Element]float64{model.Pyro: 0.10}},
		},
		Loadout: Loadout{
			Character: model.Character{
				Key: "Tester", Level: 80, Ascension: 5,
				TalentAuto: 6, TalentSkill: 6, TalentBurst: 6,
			},
			Weapon:    &model.Weapon{ID: 1, Key: "Starter", Level: 90, Ascension: 6, Refinement: 1, Location: "Tester"},
			Artifacts: equipped,
		},
		Inventory: inv,
		Weapons: []model.Weapon{
			{ID: 1, Key: "Starter", Level: 90, Ascension: 6, Refinement: 1, Location: "Tester"},
			{ID: 2, Key: "Upgraded", Level: 90, Ascension: 6, Refinement: 1},
			{ID: 3, Key: "WrongType", Level: 90, Ascension: 6, Refinement: 1},
		},
		MaxSetConfigs: 6,
		FarmDays:      7,
		ResinPerDay:   180,
	}
}

func kinds(plan Plan) map[Kind]Action {
	out := map[Kind]Action{}
	for _, a := range plan.Actions {
		if _, seen := out[a.Kind]; !seen {
			out[a.Kind] = a
		}
	}
	return out
}

func TestBuildPlanFindsEveryCandidateKind(t *testing.T) {
	plan, err := BuildPlan(context.Background(), planRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Baseline <= 0 {
		t.Fatalf("baseline = %v", plan.Baseline)
	}

	got := kinds(plan)
	for _, want := range []Kind{KindReequip, KindTalent, KindAscend, KindWeapon} {
		if _, ok := got[want]; !ok {
			t.Errorf("no %s candidate in the plan; got %v", want, plan.Actions)
		}
	}

	// Both the re-equip and the weapon swap use gear the player already
	// owns, so both are free and one of them must lead — a resin cost can
	// never outrank something that costs nothing.
	if !plan.Actions[0].Free {
		t.Errorf("plan leads with a paid action (%s); free gains come first", plan.Actions[0].Kind)
	}
	for i, a := range plan.Actions {
		if !a.Free && i < len(plan.Actions)-1 && plan.Actions[i+1].Free && plan.Actions[i+1].BlockedBy == "" {
			t.Errorf("a paid action at %d sorts above a free one", i)
		}
	}
	if !got[KindReequip].Free || !got[KindWeapon].Free {
		t.Error("re-equipping and swapping to an owned weapon must both be free")
	}
}

func TestBuildPlanTalentGainMatchesTheRotation(t *testing.T) {
	req := planRequest(t)
	plan, err := BuildPlan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	// The rotation uses skill and burst but never the normal attack, so a
	// normal-attack upgrade must not appear at all.
	for _, a := range plan.Actions {
		if a.Kind == KindTalent && strings.Contains(a.Subject, gamedata.TalentAuto) {
			t.Errorf("offered a normal-attack upgrade for a rotation that never uses it: %+v", a)
		}
	}

	var burst, skill *Action
	for i := range plan.Actions {
		a := &plan.Actions[i]
		if a.Kind != KindTalent {
			continue
		}
		switch {
		case strings.Contains(a.Subject, gamedata.TalentBurst):
			burst = a
		case strings.Contains(a.Subject, gamedata.TalentSkill):
			skill = a
		}
	}
	if burst == nil || skill == nil {
		t.Fatal("expected both a skill and a burst upgrade")
	}
	// The burst adds 0.2 to one hit per level; the skill adds 0.15 to each
	// of two, so 0.30. A ranking that does not account for hit count would
	// pick the burst, which is exactly the mistake this guards against.
	if skill.GainPct <= burst.GainPct {
		t.Errorf("skill gain %v should exceed burst gain %v for this rotation",
			skill.GainPct, burst.GainPct)
	}
}

func TestBuildPlanPricesTalentsInResin(t *testing.T) {
	plan, err := BuildPlan(context.Background(), planRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range plan.Actions {
		if a.Kind != KindTalent {
			continue
		}
		if a.ResinCost != 20 {
			t.Errorf("talent upgrade priced at %v resin, want 20", a.ResinCost)
		}
		if a.Efficiency <= 0 {
			t.Errorf("talent upgrade has no efficiency: %+v", a)
		}
	}
}

func TestBuildPlanPicksTheRightWeapon(t *testing.T) {
	plan, err := BuildPlan(context.Background(), planRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range plan.Actions {
		if a.Kind != KindWeapon {
			continue
		}
		if a.Subject == "WrongType" {
			t.Error("offered a bow to a sword user")
		}
		if a.Subject != "Upgraded" {
			t.Errorf("weapon candidate is %q, want Upgraded", a.Subject)
		}
	}
}

func TestBuildPlanSaysWhyFarmingIsMissing(t *testing.T) {
	plan, err := BuildPlan(context.Background(), planRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	var mentioned bool
	for _, s := range plan.Skipped {
		if strings.Contains(s, "farming") {
			mentioned = true
		}
	}
	if !mentioned {
		t.Error("farming was dropped without explanation; a silent omission reads as 'not worth it'")
	}
}

func TestBuildPlanPricesFarmingWhenTheModelIsKnown(t *testing.T) {
	req := planRequest(t)
	snap := req.Snapshot
	req.Sim = &FarmSim{Snapshot: snap, Trials: 40, Seed: 5}

	plan, err := BuildPlan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var farm *Action
	for i := range plan.Actions {
		if plan.Actions[i].Kind == KindFarm {
			farm = &plan.Actions[i]
		}
	}
	if farm == nil {
		t.Fatal("no farming candidate despite a complete drop model")
	}
	if farm.ResinCost <= 0 {
		t.Errorf("farming priced at %v resin", farm.ResinCost)
	}
	if farm.Free {
		t.Error("farming is not free")
	}
}

func TestBuildPlanRejectsAnEmptyRotation(t *testing.T) {
	req := planRequest(t)
	req.Goal.Spec.Steps = nil
	if _, err := BuildPlan(context.Background(), req); err == nil {
		t.Error("expected an error for a goal with no rotation")
	}
}

func TestBuildRotationRejectsANonDamageEntry(t *testing.T) {
	snap := planSnapshot()
	c := snap.Characters["Tester"]
	talent := c.Talents[gamedata.TalentSkill]
	talent.Entries = append(talent.Entries, gamedata.TalentEntry{
		Label: "Skill CD", Unit: "seconds", Multipliers: ladder(6, 0, 10),
	})
	c.Talents[gamedata.TalentSkill] = talent
	snap.Characters["Tester"] = c

	_, err := BuildRotation(snap, model.Character{Key: "Tester", TalentSkill: 6}, Spec{
		Name:  "bad",
		Steps: []Step{{Talent: gamedata.TalentSkill, Entry: "Skill CD"}},
	})
	if err == nil {
		t.Fatal("a cooldown was accepted as a damage step")
	}
	if !strings.Contains(err.Error(), "seconds") {
		t.Errorf("error should name the unit, got %q", err)
	}
}

func TestBuildRotationSuggestsValidLabels(t *testing.T) {
	snap := planSnapshot()
	_, err := BuildRotation(snap, model.Character{Key: "Tester", TalentSkill: 6}, Spec{
		Name:  "typo",
		Steps: []Step{{Talent: gamedata.TalentSkill, Entry: "Skil DMG"}},
	})
	if err == nil {
		t.Fatal("a misspelled label was accepted")
	}
	if !strings.Contains(err.Error(), "Skill DMG") {
		t.Errorf("error should list the labels that do exist, got %q", err)
	}
}

func TestPlanNamesWhoLosesTheGear(t *testing.T) {
	req := planRequest(t)
	// The best unequipped pieces belong to somebody else, and the best
	// weapon is on another character too.
	for i := range req.Inventory {
		if req.Inventory[i].Location == "" {
			req.Inventory[i].Location = "Somebody"
		}
	}
	req.Weapons[1].Location = "Somebody"

	plan, err := BuildPlan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	got := kinds(plan)
	reequip, ok := got[KindReequip]
	if !ok {
		t.Fatal("no re-equip candidate")
	}
	if !strings.Contains(reequip.Note, "Somebody") {
		t.Errorf("re-equip note = %q; it must say whose artifacts it takes", reequip.Note)
	}

	weapon, ok := got[KindWeapon]
	if !ok {
		t.Fatal("no weapon candidate")
	}
	if !strings.Contains(weapon.Note, "Somebody") {
		t.Errorf("weapon note = %q; it must say who currently holds it", weapon.Note)
	}
}

func TestPlanStaysSilentWhenNothingIsTaken(t *testing.T) {
	plan, err := BuildPlan(context.Background(), planRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range plan.Actions {
		if a.Kind == KindReequip && strings.Contains(a.Note, "Tager stykker") {
			t.Errorf("claimed to take gear from someone when the pieces are unowned: %q", a.Note)
		}
	}
}

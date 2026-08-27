package mine

import (
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/gamedata"
)

// The classification decides whether a material can be priced at all, so each
// case here is a real entry from the catalogue rather than an invented one.
func TestMaterialSourceIsReadFromTheStructuredFieldFirst(t *testing.T) {
	book := gdbMaterial{
		Name: "Philosophies of Contention", TypeText: "Character Talent Material",
		DropDomainID: 4437, DaysOfWeek: []string{"Monday", "Thursday", "Sunday"},
		// The prose upstream says something else entirely for these.
		Sources: []string{"Placeholder - Craftable Amount: {0}"},
	}
	if got := materialSource(book); got != gamedata.SourceDomain {
		t.Errorf("a book that names its domain was classified %q", got)
	}
}

func TestMaterialSourceTellsTheTwoKindsOfBossApart(t *testing.T) {
	cases := []struct {
		name string
		in   gdbMaterial
		want gamedata.MaterialSource
	}{
		{"world boss", gdbMaterial{
			Name: "Everflame Seed", TypeText: "Character Level-Up Material",
			Sources: []string{"Dropped by Lv. 30+ Pyro Regisvines"},
		}, gamedata.SourceBoss},
		{"trounce boss", gdbMaterial{
			Name: "Ashen Heart", TypeText: "Character Level-Up Material",
			Sources: []string{"Lv. 70+ Signora Challenge Reward",
				"Obtained from the Crafting Bench Conversion Tab"},
		}, gamedata.SourceWeekly},
		{"gem", gdbMaterial{
			Name: "Agnidus Agate Chunk", TypeText: "Character Ascension Material",
			Sources: []string{"Check the Enemies Tab in the Adventurer Handbook"},
		}, gamedata.SourceGem},
		{"local specialty", gdbMaterial{
			Name: "Cecilia", TypeText: "Local Specialty (Mondstadt)",
			Sources: []string{"Recommendation: Found on Starsnatch Cliff"},
		}, gamedata.SourceOverworld},
		{"mob drop", gdbMaterial{
			Name: "Slime Condensate", TypeText: "Character and Weapon Enhancement Material",
			Sources: []string{"Dropped by slimes"},
		}, gamedata.SourceOverworld},
		{"event only", gdbMaterial{
			Name: "Crown of Insight", TypeText: "Character Talent Material",
			Sources: []string{"Limited-Duration Event Reward"},
		}, gamedata.SourceEvent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := materialSource(c.in); got != c.want {
				t.Errorf("%s classified %q, want %q", c.in.Name, got, c.want)
			}
		})
	}
}

// A material nobody can place must stay unplaced. Falling back to a plausible
// source would put an unfarmable material in a farm plan.
func TestAnUnplaceableMaterialStaysUnplaced(t *testing.T) {
	got := materialSource(gdbMaterial{
		Name: "Something New", TypeText: "Material", Sources: []string{"Somewhere"},
	})
	if got != gamedata.SourceUnknown {
		t.Errorf("source = %q, want it left unknown", got)
	}
}

func TestWithoutTierMergesTheFourDifficulties(t *testing.T) {
	for in, want := range map[string]string{
		"Domain of Mastery: Altar of Flames IV": "Domain of Mastery: Altar of Flames",
		"Domain of Mastery: Altar of Flames":    "Domain of Mastery: Altar of Flames",
		"Domain of Forgery: Altar of Sands II":  "Domain of Forgery: Altar of Sands",
		// Not a tier: the numeral has to stand alone at the end.
		"Court of Flowing Sand": "Court of Flowing Sand",
	} {
		if got := withoutTier(in); got != want {
			t.Errorf("withoutTier(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWeekdaysAreNumberedFromSunday(t *testing.T) {
	got := weekdays([]string{"Wednesday", "Saturday", "Sunday"})
	want := []int{0, 3, 6}
	if len(got) != len(want) {
		t.Fatalf("weekdays = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("weekdays = %v, want %v", got, want)
		}
	}
}

// The two name sources are independent, so they can drift apart. When they
// do, a player is sent to a domain on a day it is shut — and only notices on
// the days they are not playing.
func TestLinkRefusesWhenTheDaySourcesDisagree(t *testing.T) {
	snap := &gamedata.Snapshot{
		Domains: map[string]gamedata.Domain{
			"altarofflames": {Key: "altarofflames", Name: "Altar of Flames", Kind: "talent",
				Rewards: []int{1}, Days: []int{1, 4, 0}},
		},
		Materials: map[int]gamedata.Material{
			1: {ID: 1, Name: "Philosophies of Prosperity", Days: []int{2, 5, 0}},
		},
	}
	err := linkMaterialDomains(snap)
	if err == nil {
		t.Fatal("two sources disagreed about the rotation and it was accepted")
	}
	if !strings.Contains(err.Error(), "Philosophies of Prosperity") {
		t.Errorf("the error does not name the material: %v", err)
	}
}

// A domain that drops something the catalogue has never heard of means the
// two sources are out of step, which would leave a bill unpriceable later
// with no explanation of why.
func TestLinkRefusesADomainDroppingAnUnknownItem(t *testing.T) {
	snap := &gamedata.Snapshot{
		Domains: map[string]gamedata.Domain{
			"d": {Key: "d", Kind: "talent", Rewards: []int{404}},
		},
		Materials: map[int]gamedata.Material{},
	}
	if err := linkMaterialDomains(snap); err == nil {
		t.Fatal("a domain dropping an unknown item was accepted")
	}
}

func TestLinkRecordsTheDomainOnTheMaterial(t *testing.T) {
	snap := &gamedata.Snapshot{
		Domains: map[string]gamedata.Domain{
			"altarofflames": {Key: "altarofflames", Kind: "talent",
				Rewards: []int{1}, Days: []int{1, 4, 0}},
			// Artifact domains are skipped: their rewards are sets, not
			// materials, and they are open every day.
			"testdomain": {Key: "testdomain", Kind: "artifact", Sets: []string{"A"}},
		},
		Materials: map[int]gamedata.Material{1: {ID: 1, Name: "Book"}},
	}
	if err := linkMaterialDomains(snap); err != nil {
		t.Fatal(err)
	}
	got := snap.Materials[1]
	if got.Domain != "altarofflames" {
		t.Errorf("domain = %q", got.Domain)
	}
	// A material with no days of its own inherits the domain's, because
	// that is the same fact stated once.
	if len(got.Days) != 3 {
		t.Errorf("days = %v", got.Days)
	}
}

func TestBillLiftsMoraOutOfTheMaterialList(t *testing.T) {
	got := bill(6, 120_000, []costItem{
		{ID: 104162, Count: 6},
		{}, // upstream pads the list with empty objects
		{ID: MoraID, Count: 20_000},
		{ID: 112045, Count: 12},
	})
	if got.Mora != 140_000 {
		t.Errorf("mora = %d, want the coin cost plus the mora line", got.Mora)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %+v", got.Items)
	}
	// Sorted by id, so two runs of the miner produce the same snapshot.
	if got.Items[0].ID != 104162 || got.Items[1].ID != 112045 {
		t.Errorf("items are not in id order: %+v", got.Items)
	}
}

func TestAscensionBillsSkipThePhaseNobodyPaysFor(t *testing.T) {
	var c gamedata.Character
	applyAscensionBills(&c, []promoteRow{
		{PromoteLevel: 0, ScoinCost: 0},
		{PromoteLevel: 2, ScoinCost: 40_000, CostItems: []costItem{{ID: 1, Count: 3}}},
		{PromoteLevel: 1, ScoinCost: 20_000, CostItems: []costItem{{ID: 1, Count: 1}}},
	})
	if len(c.AscensionBills) != 2 {
		t.Fatalf("bills = %+v", c.AscensionBills)
	}
	if c.AscensionBills[0].Level != 1 || c.AscensionBills[1].Level != 2 {
		t.Errorf("bills are not in phase order: %+v", c.AscensionBills)
	}
}

// The Geo Traveler pays for the normal attack out of a different book series
// and a different weekly boss than the skill and the burst. One table read
// once and reused would be right for the other 116 characters and quietly
// wrong there, so the three are kept apart.
func TestTalentBillsAreKeptPerTalent(t *testing.T) {
	var c gamedata.Character
	c.ProudSkillGroupIDs = gamedata.SkillIDs{Auto: 701, Skill: 702, Burst: 703}

	byGroup := map[int][]proudSkillRow{
		701: {{ProudSkillGroupID: 701, Level: 2, CoinCost: 12_500,
			CostItems: []costItem{{ID: 104301, Count: 3}}}},
		702: {{ProudSkillGroupID: 702, Level: 2, CoinCost: 12_500,
			CostItems: []costItem{{ID: 104304, Count: 3}}}},
		703: {{ProudSkillGroupID: 703, Level: 2, CoinCost: 12_500,
			CostItems: []costItem{{ID: 104307, Count: 3}}}},
	}

	bills := map[string][]gamedata.Bill{}
	for slot, group := range map[string]int{
		gamedata.TalentAuto:  c.ProudSkillGroupIDs.Auto,
		gamedata.TalentSkill: c.ProudSkillGroupIDs.Skill,
		gamedata.TalentBurst: c.ProudSkillGroupIDs.Burst,
	} {
		for _, r := range byGroup[group] {
			bills[slot] = append(bills[slot], bill(r.Level, r.CoinCost, r.CostItems))
		}
	}
	c.TalentBills = bills

	seen := map[int]string{}
	for _, slot := range []string{gamedata.TalentAuto, gamedata.TalentSkill, gamedata.TalentBurst} {
		b, ok := c.TalentBill(slot, 2)
		if !ok {
			t.Fatalf("no bill for %s", slot)
		}
		id := b.Items[0].ID
		if other, clash := seen[id]; clash {
			t.Fatalf("%s and %s were charged the same book %d", other, slot, id)
		}
		seen[id] = slot
	}
}

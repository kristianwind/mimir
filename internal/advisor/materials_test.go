package advisor

import (
	"strings"
	"testing"

	"github.com/kristianwind/mimir/internal/gamedata"
)

func materialSnapshot() *gamedata.Snapshot {
	return &gamedata.Snapshot{
		ResinCosts: map[string]float64{"world_boss": 40, "weekly_boss": 30},
		Domains: map[string]gamedata.Domain{
			"altarofflames": {
				Key: "altarofflames", Name: "Altar of Flames", Kind: "talent",
				Entrance: "Taishan Mansion", Days: []int{1, 4, 0}, ResinCost: 20,
			},
		},
		Materials: map[int]gamedata.Material{
			1: {ID: 1, Name: "Philosophies of Prosperity", Source: gamedata.SourceDomain,
				Domain: "altarofflames", Days: []int{1, 4, 0}},
			2: {ID: 2, Name: "Old Handguard", Source: gamedata.SourceOverworld},
			3: {ID: 3, Name: "Molten Moment", Source: gamedata.SourceWeekly},
			4: {ID: 4, Name: "Crown of Insight", Source: gamedata.SourceEvent},
			5: {ID: 5, Name: "Storm Beads", Source: gamedata.SourceBoss},
			6: {ID: 6, Name: "Vajrada Amethyst Chunk", Source: gamedata.SourceGem},
		},
	}
}

func TestABillNamesWhereEachMaterialComesFrom(t *testing.T) {
	cost := BillCost(materialSnapshot(), gamedata.Bill{
		Level: 9, Mora: 450_000,
		Items: []gamedata.ItemCost{{ID: 1, Count: 12}, {ID: 2, Count: 9}, {ID: 3, Count: 2}},
	})

	if cost.Mora != 450_000 {
		t.Errorf("mora = %d", cost.Mora)
	}
	book := cost.Lines[0]
	if book.Where != "Altar of Flames (Taishan Mansion)" {
		t.Errorf("where = %q; the name says which day, the entrance says which door", book.Where)
	}
	if book.ResinPerRun != 20 {
		t.Errorf("a domain run costs %v, want the domain's own price", book.ResinPerRun)
	}

	// A material you pick up is not a material you spend resin on, and
	// pricing it would put "go and gather" inside a resin budget.
	if hand := cost.Lines[1]; hand.ResinPerRun != 0 {
		t.Errorf("an overworld drop was priced at %v resin", hand.ResinPerRun)
	}
}

// The count is never multiplied by the price of a run. Twelve books do not
// cost twelve runs, and Mimir has no source for how many they do cost.
func TestABillNeverInventsATotal(t *testing.T) {
	cost := BillCost(materialSnapshot(), gamedata.Bill{
		Level: 9, Items: []gamedata.ItemCost{{ID: 1, Count: 12}},
	})
	if cost.Resin != 0 {
		t.Errorf("resin total = %v, but nobody published the drop counts", cost.Resin)
	}
	if cost.Unpriced == "" {
		t.Error("the bill has no total and does not say why")
	}
	if !strings.Contains(cost.Unpriced, "runs") {
		t.Errorf("the reason does not name what is missing: %q", cost.Unpriced)
	}
}

// A weekly cap changes what more resin can buy, so it is reported as its own
// fact rather than folded into a price.
func TestAWeeklyMaterialIsReportedAsCapped(t *testing.T) {
	cost := BillCost(materialSnapshot(), gamedata.Bill{
		Level: 9, Items: []gamedata.ItemCost{{ID: 3, Count: 2}, {ID: 3, Count: 2}},
	})
	if len(cost.Capped) != 1 || cost.Capped[0] != "Molten Moment" {
		t.Fatalf("capped = %v", cost.Capped)
	}
}

func TestAnUnfarmableMaterialBlocksTheUpgrade(t *testing.T) {
	cost := BillCost(materialSnapshot(), gamedata.Bill{
		Level: 10, Items: []gamedata.ItemCost{{ID: 4, Count: 1}, {ID: 1, Count: 16}},
	})
	if cost.Blocked != "Crown of Insight" {
		t.Errorf("blocked = %q; an event-only material cannot be farmed at any price", cost.Blocked)
	}
}

// A material the catalogue does not know must make the bill visibly
// incomplete. Silently dropping the line would understate the cost, which is
// the one direction an error here must never go.
func TestAnUnknownMaterialIsSaidOutLoud(t *testing.T) {
	cost := BillCost(materialSnapshot(), gamedata.Bill{
		Items: []gamedata.ItemCost{{ID: 999, Count: 3}},
	})
	if len(cost.Lines) != 1 || cost.Lines[0].Count != 3 {
		t.Fatalf("the line was dropped: %+v", cost.Lines)
	}
	if !strings.Contains(cost.Unpriced, "999") {
		t.Errorf("the unknown material is not named: %q", cost.Unpriced)
	}
}

// The gems come from any boss of the matching element, so there is a price
// but no place — and saying a place would be making one up.
func TestAGemIsPricedWithoutNamingABoss(t *testing.T) {
	cost := BillCost(materialSnapshot(), gamedata.Bill{
		Items: []gamedata.ItemCost{{ID: 6, Count: 6}},
	})
	line := cost.Lines[0]
	if line.ResinPerRun != 40 {
		t.Errorf("resin per run = %v, want the world boss price", line.ResinPerRun)
	}
	if line.Where != "" {
		t.Errorf("where = %q, but no single boss drops it", line.Where)
	}
	runs := cost.Runs()
	if len(runs) != 1 || !strings.Contains(runs[0], "matching element") {
		t.Errorf("runs = %v", runs)
	}
}

func TestRunsSaysWhichDaysTheDomainIsOpen(t *testing.T) {
	cost := BillCost(materialSnapshot(), gamedata.Bill{
		Items: []gamedata.ItemCost{{ID: 1, Count: 12}, {ID: 5, Count: 20}},
	})
	runs := cost.Runs()
	if len(runs) != 2 {
		t.Fatalf("runs = %v", runs)
	}
	var domain string
	for _, r := range runs {
		if strings.Contains(r, "Altar of Flames") {
			domain = r
		}
	}
	// The game's own order: the week runs Monday to Sunday, even though the
	// stored numbering starts on Sunday.
	if !strings.Contains(domain, "Monday, Thursday and Sunday") {
		t.Errorf("the domain line does not say when it is open: %q", domain)
	}
}

func TestThousandsSeparatesTheMoraFromTheNoise(t *testing.T) {
	for in, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 700000: "700,000"} {
		if got := thousands(in); got != want {
			t.Errorf("thousands(%d) = %q, want %q", in, got, want)
		}
	}
}

package advisor

import (
	"fmt"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// MinInventoryForEstimate is the smallest inventory the estimator will work
// from. Below it the distributions are noise dressed up as data.
const MinInventoryForEstimate = 200

// DropEstimate is an artifact drop model measured from a real inventory,
// together with an honest account of what it does and does not say.
type DropEstimate struct {
	Model gamedata.DropModel `json:"model"`
	// Sample is how many artifacts the estimate is based on.
	Sample int `json:"sample"`
	// Caveats are the known biases, surfaced to the user rather than buried.
	Caveats []string `json:"caveats"`
	// HasYield reports whether the estimate can be converted to resin. It
	// cannot be measured from an inventory — an inventory records what
	// dropped, never how many runs it took.
	HasYield bool `json:"hasYield"`
}

// EstimateDropModel derives the artifact drop distributions from an owned
// inventory.
//
// The distributions the farm simulator needs — which main stats appear in
// which slot, how often each substat rolls — are not in the datamine in any
// usable form. Rather than asserting community-measured numbers as if they
// were mined, Mimir measures them from the player's own artifacts. That has a
// real bias, stated in Caveats rather than glossed over: an inventory is what
// somebody chose to keep, so good main stats are over-represented.
//
// The bias is bounded in a useful way. It skews toward the main stats the
// player actually wants, which is also what the optimizer would equip, so the
// simulation errs toward the outcomes that matter to them.
func EstimateDropModel(inventory []model.Artifact) (DropEstimate, error) {
	var fiveStar []model.Artifact
	for _, a := range inventory {
		if a.Rarity == 5 {
			fiveStar = append(fiveStar, a)
		}
	}
	if len(fiveStar) < MinInventoryForEstimate {
		return DropEstimate{}, fmt.Errorf(
			"advisor: %d five-star artifacts is too small a sample to estimate a drop model (need %d); "+
				"import your full inventory with a .good file",
			len(fiveStar), MinInventoryForEstimate)
	}

	est := DropEstimate{
		Sample: len(fiveStar),
		Model: gamedata.DropModel{
			SlotWeights:     map[model.Slot]float64{},
			MainStatWeights: map[model.Slot]map[model.Stat]float64{},
			SubstatWeights:  map[model.Stat]float64{},
			RollsToMax:      9,
		},
		Caveats: []string{
			"Målt på dit eget inventar, ikke på spillets drop-tabeller.",
			"Inventaret er hvad du har valgt at beholde, så gode main stats er overrepræsenteret.",
		},
	}

	for _, a := range fiveStar {
		est.Model.SlotWeights[a.SlotKey]++
		if est.Model.MainStatWeights[a.SlotKey] == nil {
			est.Model.MainStatWeights[a.SlotKey] = map[model.Stat]float64{}
		}
		est.Model.MainStatWeights[a.SlotKey][a.MainStat]++
		for _, s := range a.Substats {
			est.Model.SubstatWeights[s.Key]++
		}
	}

	// The fourth substat only exists from the start on some pieces, and an
	// upgraded artifact always shows four — so this can only be measured on
	// unupgraded ones.
	var unupgraded, withFour int
	for _, a := range fiveStar {
		if a.Level != 0 {
			continue
		}
		unupgraded++
		if len(a.Substats) >= 4 {
			withFour++
		}
	}
	if unupgraded >= 30 {
		est.Model.FourSubstatChance = float64(withFour) / float64(unupgraded)
	} else {
		est.Caveats = append(est.Caveats,
			fmt.Sprintf("Kun %d uopgraderede stykker: chancen for fire substats er ikke målt.", unupgraded))
	}

	// Flower and plume have exactly one possible main stat, so a slot that
	// never appears would silently remove it from the simulation.
	for _, slot := range model.Slots {
		if est.Model.SlotWeights[slot] == 0 {
			return DropEstimate{}, fmt.Errorf(
				"advisor: the inventory holds no five-star %s, so the drop model would never produce one", slot)
		}
	}

	return est, nil
}

// WithYield attaches the per-run yield the estimator cannot measure.
//
// An inventory records what dropped, never how many runs it took. Supplying
// these two numbers — count your five-star drops over fifty runs — is what
// turns "worth +6.2% per hundred pieces" into "worth +6.2% for 1000 resin".
// Without them the simulator still ranks domains against each other, just in
// pieces rather than resin.
func (e DropEstimate) WithYield(piecesPerRun, fiveStarChance float64) (DropEstimate, error) {
	if piecesPerRun <= 0 || fiveStarChance <= 0 || fiveStarChance > 1 {
		return e, fmt.Errorf("advisor: yield must be positive and the five-star chance in (0,1]")
	}
	e.Model.PiecesPerRun = piecesPerRun
	e.Model.FiveStarChance = fiveStarChance
	e.HasYield = true
	return e, nil
}

package advisor

// One character, and how far along it is.
//
// Sabrina, testing: "Jeg vil gerne have en side til hver karakter, som viser
// mig hvilke stykker jeg får mest ud af at opgradere. Og på karakter siden der
// er status på hvor godt karakteren er bygget."
//
// Two questions, and Mimir could already answer both — in two different views,
// neither of which is about one character. Potential ranks the whole roster and
// prices levelling every worn piece on the way; the artifact ranking scores
// every piece in the bag against one character's target. What was missing was
// the page that puts one character's own answers in one place.
//
// So almost nothing here is a new measurement. It is the assembly, and the
// assembly is the feature: RankArtifacts for what each slot is worth,
// artifactLevelCandidates' own arithmetic for what levelling buys,
// SubstatValues for what the next roll should go to.
//
// The one genuinely new number is Built, and it is the one to be careful with.

import (
	"context"
	"fmt"
	"sort"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
	"github.com/kristianwind/mimir/internal/optimizer"
)

// SlotStatus is one of the five slots: what is in it, and what could be.
type SlotStatus struct {
	Slot model.Slot `json:"slot"`
	// Ideal is the main stat the target build wants here.
	Ideal model.Stat `json:"ideal,omitempty"`
	// Worn is the piece equipped now, absent when the slot is empty.
	Worn *PieceScore `json:"worn,omitempty"`
	// LevelGain is what taking the worn piece to its cap would add, as a
	// fraction of the character's current damage. Main stat only — see the
	// caveat, which is the same one Potential states for the same number.
	LevelGain float64 `json:"levelGain,omitempty"`
	LevelTo   int     `json:"levelTo,omitempty"`
	// SwapGain is what the best piece in the bag would add over what is worn.
	// SwapTo identifies it, and SwapWornBy names whoever has it on, because a
	// piece someone else is wearing is not free to take.
	SwapGain   float64 `json:"swapGain,omitempty"`
	SwapTo     int64   `json:"swapTo,omitempty"`
	SwapWornBy string  `json:"swapWornBy,omitempty"`
	// Best is the larger of the two gains, and Action names which it is. This
	// is what the slots are ranked on.
	Best   float64 `json:"best"`
	Action string  `json:"action,omitempty"`
}

// BuildStatus is the character page.
type BuildStatus struct {
	Character     string        `json:"character"`
	Element       model.Element `json:"element"`
	Level         int           `json:"level"`
	Constellation int           `json:"constellation"`
	Weapon        string        `json:"weapon,omitempty"`
	// Built is this build's damage as a fraction of the idealised build's.
	//
	// The denominator is five idealised pieces — right main stat, the target
	// view's substat allocation, at +20 — so it is a yardstick and not a
	// target anybody reaches. Read it as "how much of the slot's worth is
	// claimed", not as a grade out of a hundred. Deliberately no bands: the
	// artifact score refused to cut itself into good/fine/bad because
	// nothing in the game says where the lines are, and the same holds here.
	Built float64 `json:"built"`
	// Slots are the five, ranked by how much is available in each.
	Slots []SlotStatus `json:"slots"`
	// Substats is what one more roll would buy, best first.
	Substats []SubstatValue `json:"substats,omitempty"`
	Caveats  []string       `json:"caveats"`
	Skipped  []string       `json:"skipped,omitempty"`
}

// BuildStatusRequest is one character, measured against the whole bag.
type BuildStatusRequest struct {
	Snapshot   *gamedata.Snapshot
	Loadout    Loadout
	Inventory  []model.Artifact
	Conditions map[string]float64
}

// CharacterStatus answers both halves of the character page.
func CharacterStatus(ctx context.Context, req BuildStatusRequest) (BuildStatus, error) {
	if req.Snapshot == nil {
		return BuildStatus{}, fmt.Errorf("advisor: no game data snapshot")
	}
	snap := req.Snapshot
	key := req.Loadout.Character.Key

	def, err := snap.Char(key)
	if err != nil {
		return BuildStatus{}, err
	}
	base, err := Assemble(snap, req.Loadout)
	if err != nil {
		return BuildStatus{}, err
	}
	eval := yardstickEvaluator{Snapshot: snap, Conditions: req.Conditions}
	current, err := eval.Score(ctx, Goal{}, base)
	if err != nil {
		return BuildStatus{}, err
	}

	// The ranking does the expensive half: every piece in the bag scored in
	// this character's current build, per slot, with the verdicts already
	// attached. Recomputing any of it here would be a second opinion that
	// could disagree with the first.
	ranking, err := RankArtifacts(ctx, ArtifactRankingRequest{
		Snapshot:   snap,
		Loadout:    req.Loadout,
		Inventory:  req.Inventory,
		Conditions: req.Conditions,
	})
	if err != nil {
		return BuildStatus{}, err
	}

	out := BuildStatus{
		Character:     key,
		Element:       defElement(snap, key),
		Level:         req.Loadout.Character.Level,
		Constellation: req.Loadout.Character.Constellation,
		Skipped:       ranking.Skipped,
		Caveats: append([]string{
			"Built is this build's damage against an idealised one — five pieces with the right main stat, the target view's substat allocation, at +20. Nothing reaches a hundred, because that build has perfect substats on all five pieces and no real account does. It is a ruler, not a grade.",
			"What levelling buys is the main stat's growth alone. A piece gains a substat roll every four levels and which stat it lands on is unknown, so that part is left out rather than guessed — the real gain is this number or better, never worse.",
		}, ranking.Caveats...),
	}
	if req.Loadout.Weapon != nil {
		out.Weapon = req.Loadout.Weapon.Key
	}

	// The set the idealised build wears. It has to be the target build's set
	// rather than whatever is on today, for the same reason the artifact
	// score's ceiling does: a denominator that moved with the player's
	// current gear would make Built mean something different every time they
	// changed a piece. Resolved once, on the caller's context.
	var idealSet string
	if target, err := BuildTarget(ctx, TargetRequest{
		Snapshot: snap, Character: req.Loadout.Character, Conditions: req.Conditions,
	}); err == nil && len(target.Sets) > 0 {
		idealSet = target.Sets[0].Config
	}

	// Built. One extra evaluation: the same build with every slot replaced by
	// its idealised piece, so the denominator is the whole build rather than
	// five separate slot spans added up. Damage is not linear in stats and a
	// sum of per-slot fractions would not be a fraction of anything.
	ideal := base
	for _, slot := range model.Slots {
		main, ok := ranking.idealMain(slot)
		if !ok {
			continue
		}
		block, err := idealBlock(snap, def, main)
		if err != nil {
			out.Skipped = append(out.Skipped, fmt.Sprintf("%s: %v", slot, err))
			continue
		}
		piece := model.Artifact{ID: -3 - int64(len(ideal.Equipped)), SlotKey: slot,
			MainStat: main, Level: 20, Rarity: 5, SetKey: idealSet}
		ideal = withPiece(ideal, slot, &piece, block)
	}
	perfect, err := eval.Score(ctx, Goal{}, ideal)
	if err != nil {
		return BuildStatus{}, err
	}
	if perfect > 0 {
		out.Built = current / perfect
	}

	// What one more roll buys. Cheap next to the ranking above, and it is the
	// thing the piece verdicts cannot say: a green piece with its rolls in the
	// wrong stats is still the slot with the most left in it.
	if values, err := SubstatValues(ctx, snap, base, req.Conditions); err == nil {
		out.Substats = values
	} else {
		out.Skipped = append(out.Skipped, fmt.Sprintf("substat values: %v", err))
	}

	worn := map[model.Slot]model.Artifact{}
	for _, a := range base.Equipped {
		worn[a.SlotKey] = a
	}

	for _, sr := range ranking.Slots {
		st := SlotStatus{Slot: sr.Slot, Ideal: sr.Ideal}

		for i := range sr.Pieces {
			p := sr.Pieces[i]
			switch {
			case p.Worn:
				st.Worn = &p
			case p.Gain > st.SwapGain:
				st.SwapGain, st.SwapTo, st.SwapWornBy = p.Gain, p.ArtifactID, p.WornBy
			}
		}

		if a, ok := worn[sr.Slot]; ok {
			gain, to, err := levelGain(ctx, snap, eval, base, a, current)
			if err != nil {
				out.Skipped = append(out.Skipped, fmt.Sprintf("%s: %v", sr.Slot, err))
			} else {
				st.LevelGain, st.LevelTo = gain, to
			}
		}

		st.Best, st.Action = bestAction(st)
		out.Slots = append(out.Slots, st)
	}

	// Ranked by what is available, which is the question the page is for.
	// Stable, so two slots with nothing in them keep the game's own order
	// rather than shuffling between requests.
	sort.SliceStable(out.Slots, func(i, j int) bool { return out.Slots[i].Best > out.Slots[j].Best })
	return out, nil
}

// bestAction picks the larger of the two gains and names it.
//
// A swap and a level are not alternatives in the long run — a piece you level
// today can still be replaced tomorrow — but they are alternatives for the
// next thing you do, and that is what the page is ranking.
func bestAction(st SlotStatus) (float64, string) {
	switch {
	case st.Worn == nil && st.SwapGain > 0:
		return st.SwapGain, "equip"
	case st.SwapGain <= 0 && st.LevelGain <= 0:
		return 0, ""
	case st.LevelGain >= st.SwapGain:
		return st.LevelGain, "level"
	default:
		return st.SwapGain, "swap"
	}
}

// levelGain prices taking one worn piece to its cap.
//
// Deliberately the same arithmetic as artifactLevelCandidates, main stat only
// and for the same reason: a piece gains a substat roll every four levels and
// which stat it lands on is unknowable, so projecting it would be inventing
// the number that matters most. Under-reporting is the safe direction. It
// never talks anybody into levelling a piece on a promise the game did not
// make.
func levelGain(
	ctx context.Context, snap *gamedata.Snapshot, eval Evaluator,
	base State, piece model.Artifact, current float64,
) (float64, int, error) {
	cap, err := maxArtifactLevel(snap, piece)
	if err != nil {
		return 0, 0, err
	}
	if piece.Level >= cap {
		return 0, 0, nil
	}
	levelled := piece
	levelled.Level = cap
	block, err := optimizer.ArtifactStats(levelled, snap)
	if err != nil {
		return 0, 0, err
	}
	candidate := cloneState(base)
	candidate.ArtifactStats[piece.ID] = block
	score, err := eval.Score(ctx, Goal{}, candidate)
	if err != nil {
		return 0, 0, err
	}
	gain := Gain(current, score)
	if gain <= 0 {
		return 0, 0, nil
	}
	return gain, cap, nil
}

// idealMain is the main stat the ranking settled on for a slot. The ranking
// already resolved the target build's choice against the two slots that have
// none, so asking it again keeps the page and the grid agreeing.
func (r ArtifactRanking) idealMain(slot model.Slot) (model.Stat, bool) {
	for _, s := range r.Slots {
		if s.Slot == slot && s.Ideal != "" {
			return s.Ideal, true
		}
	}
	return "", false
}

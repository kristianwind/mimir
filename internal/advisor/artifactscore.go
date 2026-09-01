package advisor

// How good is this piece, on this character, out of a hundred.
//
// This is the question a tester brought that nothing here answered. The plan
// ranks upgrades by damage per resin, which is the right shape for "what do I
// spend today's resin on" and the wrong one for somebody who has plenty of
// resin and a bag full of artifacts: it buries a large gain behind a cheap
// one, and it needs a goal, so a character with no rotation written down is
// invisible to it. Potential fixed the second half — one ruler, no goals,
// ranked on gain alone — but it scores *characters*, and the question was
// about *pieces*.
//
// So: every artifact the account owns, measured on one character, on the same
// yardstick Potential uses. No goal, no rotation, nothing to fill in first.
//
// The scale is the part worth arguing about, because 0-100 implies an anchor
// and the obvious anchors are both wrong:
//
//   - Against the best piece you own, your best piece is always 100. Then
//     "get everything to 100" means "already done", which is not what anybody
//     asking the question wants to hear.
//   - Against the best piece in the game, nothing is comparable across
//     characters, because the best conceivable circlet for one character is
//     not the best for another.
//
// So the anchor is this character's own target build:
//
//	  0 = the slot empty. The piece contributes nothing.
//	100 = the idealised piece from the target view — correct main stat for
//	      this character, every substat roll allocated the way BuildTarget
//	      allocates them, at +20.
//
// Both ends are computed for this character, so a 60 means the same thing in
// every slot and on every character: sixty per cent of the damage that the
// slot could be worth. Above 100 is possible and is not an error — an
// artifact whose set bonus the target build does not use can beat the
// idealised piece, and clamping that to 100 would hide a real result.

import (
	"context"
	"fmt"
	"sort"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
	"github.com/kristianwind/mimir/internal/optimizer"
)

// PieceScore is one artifact measured on one character.
type PieceScore struct {
	ArtifactID int64      `json:"artifactId"`
	Slot       model.Slot `json:"slot"`
	Set        string     `json:"set"`
	Level      int        `json:"level"`
	MainStat   model.Stat `json:"mainStat"`
	// Score is 0-100 against this character's target build. Not clamped.
	Score float64 `json:"score"`
	// Worn says whether this character has it on right now.
	Worn bool `json:"worn"`
	// WornBy names the other character using it, where one is. A piece is
	// not free to take, and a ranking that does not say so recommends
	// undressing somebody.
	WornBy string `json:"wornBy,omitempty"`
	// Gain is the fraction this character's yardstick damage would change by
	// wearing this instead of what is in the slot now. Negative is a
	// downgrade. Zero on the piece already worn.
	Gain float64 `json:"gain"`
	// Substats are the piece's own rolls, carried so a reader can see what
	// the score is made of. They are already IN the score — ArtifactStats
	// resolves them into the stat block the damage engine reads — but a
	// number with its inputs hidden invites the question this field answers:
	// "is it only ranking main stats?" It never was.
	Substats []model.Substat `json:"substats,omitempty"`
	// Verdict is the traffic light, and Why is the fact behind it.
	Verdict Verdict `json:"verdict"`
	Why     string  `json:"why"`
}

// Verdict is green, yellow, red — the shape a player already reads their own
// spreadsheet in.
//
// It is deliberately NOT a set of thresholds on Score. Cutting a 0-100 at, say,
// 70 and 45 would be taste presented as advice: nothing in the game says a 68
// is worse than "fine", and the bands would have to be re-tuned every time the
// yardstick moved. Each verdict below is instead a fact about the piece that a
// player can check and act on, which is why each one carries the reason that
// produced it.
//
// Score stays as the sortable number underneath. The colour is the verdict;
// the number is the evidence.
type Verdict string

const (
	// VerdictGood: right main stat, fully levelled, and nothing in the bag
	// beats it. There is no action here.
	VerdictGood Verdict = "good"
	// VerdictOK: it works, and something specific would improve it — it is
	// not levelled, or a better piece is sitting unused.
	VerdictOK Verdict = "ok"
	// VerdictReplace: no amount of levelling fixes it. The main stat is one
	// the character does not want, or the piece is below five stars and so
	// caps under what a five-star reaches.
	VerdictReplace Verdict = "replace"
)

// SlotRanking is one slot's candidates, best first.
type SlotRanking struct {
	Slot model.Slot `json:"slot"`
	// Ideal is the main stat the target build wants here, so a reader can
	// see why a piece scores badly rather than only that it does.
	Ideal  model.Stat   `json:"ideal,omitempty"`
	Pieces []PieceScore `json:"pieces"`
}

// ArtifactRanking is every slot on one character.
type ArtifactRanking struct {
	Character string        `json:"character"`
	Slots     []SlotRanking `json:"slots"`
	Caveats   []string      `json:"caveats"`
	Skipped   []string      `json:"skipped,omitempty"`
}

// ArtifactRankingRequest is one character to rank the bag against.
type ArtifactRankingRequest struct {
	Snapshot *gamedata.Snapshot
	Loadout  Loadout
	// Inventory is the whole account's artifacts. Both halves of the
	// question need it: what to level is about what is worn, what to switch
	// to is about everything else.
	Inventory  []model.Artifact
	Conditions map[string]float64
}

// idealBlock is the stat block of the perfect piece for one slot.
//
// Main stat at a five-star's +20 value, and the target view's own substat
// allocation — the same invented allocation BuildTarget gives every candidate
// it ranks, so the ceiling here and the recommendation there agree about what
// "ideal" means.
func idealBlock(snap *gamedata.Snapshot, def gamedata.Character, main model.Stat) (model.StatBlock, error) {
	table, ok := snap.MainStatValues[5]
	if !ok {
		return nil, fmt.Errorf("%w: five-star main stat values", gamedata.ErrMissing)
	}
	values, ok := table[main]
	if !ok || len(values) < 21 {
		return nil, fmt.Errorf("%w: main stat values for %s at +20", gamedata.ErrMissing, main)
	}
	block, err := substatBlock(snap, targetSubstats(snap, def))
	if err != nil {
		return nil, err
	}
	out := block.Clone()
	out[main] += values[20]
	return out, nil
}

// withPiece returns s with one slot replaced.
//
// The State is copied rather than mutated: the caller scores dozens of
// candidates against the same base, and a shared slice would leave each one
// measuring the leftovers of the last.
func withPiece(s State, slot model.Slot, a *model.Artifact, stats model.StatBlock) State {
	out := s
	out.Equipped = make([]model.Artifact, 0, len(s.Equipped)+1)
	for _, e := range s.Equipped {
		if e.SlotKey != slot {
			out.Equipped = append(out.Equipped, e)
		}
	}
	out.ArtifactStats = make(map[int64]model.StatBlock, len(s.ArtifactStats)+1)
	for k, v := range s.ArtifactStats {
		out.ArtifactStats[k] = v
	}
	if a != nil {
		out.Equipped = append(out.Equipped, *a)
		out.ArtifactStats[a.ID] = stats
	}
	return out
}

// verdictFor turns facts about a piece into a colour.
//
// Order matters: a wrong main stat is reported even on a maxed piece, because
// levelling it further is the one thing that definitely will not help.
func verdictFor(a model.Artifact, score float64, ideal model.Stat, slotHasChoice bool, maxLevel int, better *PieceScore) (Verdict, string) {
	if a.Rarity < 5 {
		return VerdictReplace, fmt.Sprintf("%d★, so its main stat caps below what a five-star reaches", a.Rarity)
	}
	if slotHasChoice && a.MainStat != ideal {
		return VerdictReplace, fmt.Sprintf("main stat is %s; this character wants %s here, and levelling cannot change it", a.MainStat, ideal)
	}
	if maxLevel > 0 && a.Level < maxLevel {
		return VerdictOK, fmt.Sprintf("right main stat, but only +%d of +%d", a.Level, maxLevel)
	}
	if better != nil {
		why := fmt.Sprintf("a piece you already own scores %.0f against this one's %.0f", better.Score, score)
		if better.WornBy != "" {
			why += fmt.Sprintf(", though %s is wearing it", better.WornBy)
		}
		return VerdictOK, why
	}
	// Everything that can be acted on has been ruled out above: the rarity is
	// five, the main stat is the one wanted, it is at its cap, and nothing
	// owned beats it. Whatever separates the score from a hundred is
	// therefore the substats and the set — which is worth saying, because
	// green otherwise reads as "this piece is good" when it means "there is
	// nothing you can do about this piece".
	if score < 99 {
		return VerdictGood, fmt.Sprintf(
			"nothing to do here: right main stat, at its cap, and nothing in the bag beats it. It scores %.0f of 100 — the rest is substats and the set, which no amount of levelling chooses for you",
			score)
	}
	return VerdictGood, "right main stat, at its cap, and nothing in the bag beats it"
}

// RankArtifacts scores the account's artifacts on one character.
func RankArtifacts(ctx context.Context, req ArtifactRankingRequest) (ArtifactRanking, error) {
	if req.Snapshot == nil {
		return ArtifactRanking{}, fmt.Errorf("advisor: no game data snapshot")
	}
	snap := req.Snapshot
	def, err := snap.Char(req.Loadout.Character.Key)
	if err != nil {
		return ArtifactRanking{}, err
	}
	base, err := Assemble(snap, req.Loadout)
	if err != nil {
		return ArtifactRanking{}, err
	}
	eval := yardstickEvaluator{Snapshot: snap, Conditions: req.Conditions}

	target, err := BuildTarget(ctx, TargetRequest{
		Snapshot: snap, Character: req.Loadout.Character, Conditions: req.Conditions,
	})
	if err != nil {
		return ArtifactRanking{}, err
	}

	out := ArtifactRanking{
		Character: req.Loadout.Character.Key,
		Caveats: []string{
			"Zero is the slot left empty and a hundred is the idealised piece for this character — correct main stat, every substat roll allocated the way the target view allocates them, at +20. A piece can score above a hundred if its set bonus beats what the target build assumes.",
			"Measured the same way as Potential: one cast of the elemental skill and one of the burst, at this character's own talent levels, against a level 90 enemy with 10% resistance. No teams, no reactions, no normal attacks — a character who lives on normals is measured on the part of them this ruler can see.",
			"Every piece is scored in the build this character wears now, so a set bonus that a swap would break or complete is already in the number. Change what is equipped and the scores move.",
		},
	}

	// Flower and plume have no main-stat decision, so the target view does
	// not name one. The game does: flat HP and flat ATK, always.
	fixedMain := map[model.Slot]model.Stat{
		model.Flower: model.HP,
		model.Plume:  model.ATK,
	}

	bySlot := map[model.Slot][]model.Artifact{}
	for _, a := range req.Inventory {
		bySlot[a.SlotKey] = append(bySlot[a.SlotKey], a)
	}

	for _, slot := range []model.Slot{model.Flower, model.Plume, model.Sands, model.Goblet, model.Circlet} {
		main, ok := target.MainStats[slot]
		if !ok {
			main, ok = fixedMain[slot]
		}
		if !ok {
			out.Skipped = append(out.Skipped, fmt.Sprintf("%s: the target build names no main stat", slot))
			continue
		}

		empty, err := eval.Score(ctx, Goal{}, withPiece(base, slot, nil, nil))
		if err != nil {
			out.Skipped = append(out.Skipped, fmt.Sprintf("%s: %v", slot, err))
			continue
		}
		ideal, err := idealBlock(snap, def, main)
		if err != nil {
			out.Skipped = append(out.Skipped, fmt.Sprintf("%s: %v", slot, err))
			continue
		}
		// The ceiling wears the set the target build recommends, because the
		// idealised piece is that build's piece. Scoring it in whatever the
		// character happens to have on would make the denominator depend on
		// today's gear, and then a score would not mean the same thing
		// tomorrow.
		ceilingPiece := model.Artifact{ID: -1, SlotKey: slot, MainStat: main, Level: 20, Rarity: 5}
		if len(target.Sets) > 0 {
			ceilingPiece.SetKey = target.Sets[0].Config
		}
		top, err := eval.Score(ctx, Goal{}, withPiece(base, slot, &ceilingPiece, ideal))
		if err != nil {
			out.Skipped = append(out.Skipped, fmt.Sprintf("%s: %v", slot, err))
			continue
		}
		span := top - empty
		if span <= 0 {
			// The slot cannot be shown to matter on this ruler — a support
			// whose burst does no damage, most often. Saying so is better
			// than dividing by it.
			out.Skipped = append(out.Skipped,
				fmt.Sprintf("%s: this slot makes no measurable difference on the yardstick, so there is nothing to score against", slot))
			continue
		}

		wornScore := empty
		var wornID int64 = -1
		for _, e := range base.Equipped {
			if e.SlotKey == slot {
				wornID = e.ID
				if s, err := eval.Score(ctx, Goal{}, base); err == nil {
					wornScore = s
				}
				break
			}
		}

		rank := SlotRanking{Slot: slot, Ideal: main}
		for _, a := range bySlot[slot] {
			stats, err := optimizer.ArtifactStats(a, snap)
			if err != nil {
				out.Skipped = append(out.Skipped, fmt.Sprintf("artifact %d: %v", a.ID, err))
				continue
			}
			score, err := eval.Score(ctx, Goal{}, withPiece(base, slot, &a, stats))
			if err != nil {
				out.Skipped = append(out.Skipped, fmt.Sprintf("artifact %d: %v", a.ID, err))
				continue
			}
			p := PieceScore{
				ArtifactID: a.ID, Slot: slot, Set: a.SetKey, Level: a.Level,
				MainStat: a.MainStat, Substats: a.Substats,
				Score: 100 * (score - empty) / span,
				Worn:  a.ID == wornID,
			}
			if !p.Worn {
				p.WornBy = a.Location
			}
			if wornScore > 0 {
				p.Gain = score/wornScore - 1
			}
			rank.Pieces = append(rank.Pieces, p)
		}
		sort.SliceStable(rank.Pieces, func(i, j int) bool {
			return rank.Pieces[i].Score > rank.Pieces[j].Score
		})

		// Verdicts come after the sort, because "you own something better"
		// cannot be answered until every candidate in the slot has a number.
		//
		// betterMargin is a point of the 0-100 scale: one per cent of what
		// the slot is worth. Without it the top piece would flag every other
		// piece as improvable by a rounding error, and a grid where nothing
		// is ever green tells you nothing.
		const betterMargin = 1.0
		byID := map[int64]model.Artifact{}
		for _, a := range bySlot[slot] {
			byID[a.ID] = a
		}
		_, slotHasChoice := target.MainStats[slot]
		for i := range rank.Pieces {
			p := &rank.Pieces[i]
			a := byID[p.ArtifactID]
			maxLevel, err := maxArtifactLevel(snap, a)
			if err != nil {
				maxLevel = 0 // unknown cap: do not claim it is unlevelled
			}
			var better *PieceScore
			if len(rank.Pieces) > 0 && rank.Pieces[0].Score > p.Score+betterMargin {
				better = &rank.Pieces[0]
			}
			p.Verdict, p.Why = verdictFor(a, p.Score, main, slotHasChoice, maxLevel, better)
		}
		out.Slots = append(out.Slots, rank)
	}
	return out, nil
}

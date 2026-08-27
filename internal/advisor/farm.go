package advisor

import (
	"context"
	"fmt"
	"math/rand/v2"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/kristianwind/mimir/internal/gamedata"
	"github.com/kristianwind/mimir/internal/model"
)

// FarmSim estimates what farming a domain is actually worth.
//
// This is the hard half of "DPS per resin". A talent upgrade has a
// deterministic answer; artifact farming has a distribution. Existing tools
// dodge it with folklore ("farm Emblem until you get a good sands"), which is
// unusable for planning. Mimir samples the domain's real drop distribution,
// equips whatever beats the current build, and reports the expected gain and
// its spread — so "5 days in this domain" becomes a number you can compare
// against levelling a talent.
type FarmSim struct {
	Snapshot *gamedata.Snapshot
	// DropModel overrides the snapshot's. The usable distributions are
	// measured from one account's own inventory, so they are per-account
	// data and have no business living in a shared game data snapshot.
	DropModel *gamedata.DropModel
	// Trials is how many independent futures to sample. A few hundred is
	// enough for the ranking to be stable; the spread is reported so the UI
	// can say "+6.2% expected, +1.1% to +14% across trials" rather than
	// implying a precision the simulation does not have.
	Trials int
	// Seed makes a plan reproducible: the same account on the same day gets
	// the same recommendation, which matters when a user asks "why did this
	// change?".
	Seed uint64
}

// FarmEstimate is the outcome distribution of a farming plan.
type FarmEstimate struct {
	Domain string `json:"domain"`
	// Pieces is how many five-star drops the simulation examined. It is the
	// unit that needs no drop-rate assumption, and the one domains can be
	// compared in when the per-run yield is unknown.
	Pieces int `json:"pieces"`
	// Runs and ResinCost are filled in only when the drop model carries a
	// measured per-run yield.
	Runs       int     `json:"runs"`
	ResinCost  float64 `json:"resinCost"`
	MeanGain   float64 `json:"meanGain"`
	MedianGain float64 `json:"medianGain"`
	P10Gain    float64 `json:"p10Gain"`
	P90Gain    float64 `json:"p90Gain"`
	// NoImprovementChance is how often the whole farming run changed nothing.
	// Players deserve to see this: it is frequently above 30%.
	NoImprovementChance float64 `json:"noImprovementChance"`
}

// Estimate simulates `runs` runs of a domain against a goal.
//
// This requires a measured per-run yield, which an inventory cannot provide.
// When that is unknown, use EstimatePieces instead: it ranks domains in
// five-star pieces examined, which needs no drop-rate assumption at all.
func (f FarmSim) Estimate(
	ctx context.Context,
	g Goal,
	state State,
	domain gamedata.Domain,
	runs int,
	eval Evaluator,
) (FarmEstimate, error) {
	if runs <= 0 {
		return FarmEstimate{}, fmt.Errorf("advisor: runs must be positive")
	}
	dm := f.dropModel()
	if dm.PiecesPerRun <= 0 || dm.FiveStarChance <= 0 {
		return FarmEstimate{}, fmt.Errorf(
			"%w: per-run artifact yield. Count your five-star drops over fifty runs and supply it, "+
				"or rank domains by pieces examined instead", gamedata.ErrMissing)
	}

	pieces := int(float64(runs) * dm.PiecesPerRun * dm.FiveStarChance)
	if pieces < 1 {
		pieces = 1
	}
	est, err := f.EstimatePieces(ctx, g, state, domain, pieces, eval)
	if err != nil {
		return FarmEstimate{}, err
	}
	est.Runs = runs
	est.ResinCost = float64(runs) * domain.ResinCost
	return est, nil
}

// EstimatePieces simulates examining a fixed number of five-star drops.
func (f FarmSim) EstimatePieces(
	ctx context.Context,
	g Goal,
	state State,
	domain gamedata.Domain,
	pieces int,
	eval Evaluator,
) (FarmEstimate, error) {
	if pieces <= 0 {
		return FarmEstimate{}, fmt.Errorf("advisor: pieces must be positive")
	}
	if domain.Kind != "artifact" {
		return FarmEstimate{}, fmt.Errorf("advisor: domain %q is not an artifact domain", domain.Key)
	}
	trials := f.Trials
	if trials <= 0 {
		trials = defaultFarmTrials
	}
	dm := f.dropModel()
	if len(dm.SlotWeights) == 0 || len(dm.MainStatWeights) == 0 {
		return FarmEstimate{}, fmt.Errorf("%w: artifact drop distributions", gamedata.ErrMissing)
	}

	baseline, err := eval.Score(ctx, g, state)
	if err != nil {
		return FarmEstimate{}, err
	}
	if baseline <= 0 {
		return FarmEstimate{}, fmt.Errorf("advisor: baseline score is %v; the rotation produces no damage", baseline)
	}

	// The trials are independent and each carries its own seeded generator,
	// so running them at the same time cannot change the answer — trial 7
	// rolls the same artifacts whichever core it lands on. That is what makes
	// this safe to parallelise where the account plan, which hands gear from
	// one goal to the next, is not.
	gains := make([]float64, trials)
	errs := make([]error, trials)

	workers := runtime.NumCPU() - 1
	if workers < 1 {
		workers = 1
	}
	if workers > trials {
		workers = trials
	}

	var (
		wg   sync.WaitGroup
		next atomic.Int64
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				t := int(next.Add(1)) - 1
				if t >= trials {
					return
				}
				if err := ctx.Err(); err != nil {
					errs[t] = err
					continue
				}
				rng := rand.New(rand.NewPCG(f.Seed, uint64(t)))

				trialState := cloneState(state)
				best := baseline
				var synthetic int64 = -1

				for p := 0; p < pieces; p++ {
					art, stats, ok := f.roll(rng, domain, synthetic)
					if !ok {
						continue
					}
					synthetic--

					candidate := swap(trialState, art, stats)
					score, err := eval.Score(ctx, g, candidate)
					if err != nil {
						errs[t] = err
						break
					}
					if score > best {
						best = score
						trialState = candidate
					}
				}
				gains[t] = Gain(baseline, best)
			}
		}()
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return FarmEstimate{}, err
		}
	}

	var noImprovement int
	for _, g := range gains {
		if g <= 0 {
			noImprovement++
		}
	}

	sort.Float64s(gains)
	var sum float64
	for _, g := range gains {
		sum += g
	}

	return FarmEstimate{
		Domain:              domain.Key,
		Pieces:              pieces,
		MeanGain:            sum / float64(len(gains)),
		MedianGain:          percentile(gains, 0.50),
		P10Gain:             percentile(gains, 0.10),
		P90Gain:             percentile(gains, 0.90),
		NoImprovementChance: float64(noImprovement) / float64(trials),
	}, nil
}

// roll samples one artifact drop. The second return is its stat block; the
// third is false when the drop was not a 5-star worth considering.
func (f FarmSim) roll(rng *rand.Rand, domain gamedata.Domain, id int64) (model.Artifact, model.StatBlock, bool) {
	dm := f.dropModel()
	if len(domain.Sets) == 0 {
		return model.Artifact{}, nil, false
	}

	setKey := domain.Sets[rng.IntN(len(domain.Sets))]
	slot := pickWeighted(rng, dm.SlotWeights)
	mainWeights, ok := dm.MainStatWeights[slot]
	if !ok || len(mainWeights) == 0 {
		return model.Artifact{}, nil, false
	}
	main := pickWeighted(rng, mainWeights)

	// Substats: three or four to start, never duplicating the main stat.
	count := 3
	if rng.Float64() < dm.FourSubstatChance {
		count = 4
	}
	pool := make(map[model.Stat]float64, len(dm.SubstatWeights))
	for k, v := range dm.SubstatWeights {
		if k != main {
			pool[k] = v
		}
	}
	chosen := make([]model.Stat, 0, 4)
	for i := 0; i < count && len(pool) > 0; i++ {
		s := pickWeighted(rng, pool)
		delete(pool, s)
		chosen = append(chosen, s)
	}

	// Upgrade rolls. A piece that started with three substats spends its
	// first roll unlocking a fourth, which is why four-liners are worth so
	// much more than the extra line suggests.
	rolls := dm.RollsToMax - count
	values := make(map[model.Stat]float64, len(chosen))
	rollTable := f.Snapshot.SubstatRolls[5]
	for _, s := range chosen {
		v, ok := rollValue(rng, rollTable, s)
		if !ok {
			return model.Artifact{}, nil, false
		}
		values[s] += v
	}
	for i := 0; i < rolls; i++ {
		s := chosen[rng.IntN(len(chosen))]
		v, ok := rollValue(rng, rollTable, s)
		if !ok {
			return model.Artifact{}, nil, false
		}
		values[s] += v
	}

	subs := make([]model.Substat, 0, len(chosen))
	for _, s := range chosen {
		subs = append(subs, model.Substat{Key: s, Value: values[s]})
	}

	art := model.Artifact{
		ID:       id,
		SetKey:   setKey,
		SlotKey:  slot,
		Rarity:   5,
		Level:    20,
		MainStat: main,
		Substats: subs,
		Source:   "simulated",
	}

	stats := make(model.StatBlock, len(subs)+1)
	if curve, ok := f.Snapshot.MainStatValues[5][main]; ok && len(curve) > 20 {
		stats[main] = curve[20]
	}
	for _, s := range subs {
		stats[s.Key] += s.Value
	}
	return art, stats, true
}

func rollValue(rng *rand.Rand, table map[model.Stat][]float64, s model.Stat) (float64, bool) {
	vals, ok := table[s]
	if !ok || len(vals) == 0 {
		return 0, false
	}
	return vals[rng.IntN(len(vals))], true
}

// pickWeighted samples a key proportionally to its weight. Iteration order of
// a Go map is random, so the cumulative walk is sorted first to keep a seeded
// run reproducible.
func pickWeighted[K ~string](rng *rand.Rand, weights map[K]float64) K {
	keys := make([]K, 0, len(weights))
	var total float64
	for k, v := range weights {
		if v <= 0 {
			continue
		}
		keys = append(keys, k)
		total += v
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	target := rng.Float64() * total
	var acc float64
	for _, k := range keys {
		acc += weights[k]
		if target <= acc {
			return k
		}
	}
	var zero K
	if len(keys) > 0 {
		return keys[len(keys)-1]
	}
	return zero
}

func cloneState(s State) State {
	out := s
	out.Equipped = append([]model.Artifact(nil), s.Equipped...)
	out.ArtifactStats = make(map[int64]model.StatBlock, len(s.ArtifactStats))
	for k, v := range s.ArtifactStats {
		out.ArtifactStats[k] = v
	}
	return out
}

// swap returns a state with art occupying its slot.
func swap(s State, art model.Artifact, stats model.StatBlock) State {
	out := cloneState(s)
	out.ArtifactStats[art.ID] = stats
	replaced := false
	for i, a := range out.Equipped {
		if a.SlotKey == art.SlotKey {
			out.Equipped[i] = art
			replaced = true
			break
		}
	}
	if !replaced {
		out.Equipped = append(out.Equipped, art)
	}
	return out
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(p * float64(len(sorted)-1))
	if i < 0 {
		i = 0
	}
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

// dropModel returns the override when set, else the snapshot's.
func (f FarmSim) dropModel() gamedata.DropModel {
	if f.DropModel != nil {
		return *f.DropModel
	}
	return f.Snapshot.DropModel
}

// defaultFarmTrials is how many futures to sample when nobody says otherwise.
// A few hundred is enough for the ranking to be stable, and the spread is
// reported so the answer never implies more precision than it has.
const defaultFarmTrials = 300

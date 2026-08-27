package advisor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kristianwind/mimir/internal/gamedata"
)

// What an upgrade costs.
//
// The bill is exact. Every count in it is mined, keyed by item id, and it is
// the same number the game charges. What is not exact — and cannot be made
// exact from any published source — is how many domain runs or boss kills it
// takes to collect one, because the drop counts are not in the datamine and
// the in-game reward preview lists the materials without quantities.
//
// So this reports two different kinds of fact and keeps them apart: what you
// need, which is certain, and what one attempt at getting it costs, which is
// also certain. The number of attempts is the gap, and it is named rather
// than filled with a plausible average. The same discipline as the reaction
// coefficients: the formula is here and working, and the moment somebody
// supplies measured yields the resin total falls out of it.

// Line is one material in a bill, with what it takes to get it.
type Line struct {
	Material string `json:"material"`
	Count    int    `json:"count"`
	// Source is where it comes from: domain, boss, weekly, gem, overworld,
	// event. Empty means the catalogue could not place it, which is
	// reported rather than hidden.
	Source string `json:"source"`
	// Where names the domain, when there is one.
	Where string `json:"where,omitempty"`
	// Days are the weekdays that domain is open, 0 = Sunday. Empty means
	// every day.
	Days []int `json:"days,omitempty"`
	// ResinPerRun is what one run of the activity behind this material
	// costs. It is deliberately not the cost of the line: multiplying it by
	// Count would assume one material per run, which is wrong for every
	// material in the game.
	ResinPerRun float64 `json:"resinPerRun,omitempty"`
}

// Cost is a whole upgrade bill.
type Cost struct {
	Mora  int    `json:"mora"`
	Lines []Line `json:"lines"`
	// Resin is the total, and is only set when every line could be priced.
	Resin float64 `json:"resin,omitempty"`
	// Unpriced says why there is no total, when there is none.
	Unpriced string `json:"unpriced,omitempty"`
	// Blocked names a material that cannot be farmed at any price.
	Blocked string `json:"blocked,omitempty"`
	// Capped names the materials that are limited per week, which changes
	// what "spend more resin" can buy.
	Capped []string `json:"capped,omitempty"`
}

// resinActivity maps a material source onto the activity key its resin price
// is stored under. A source that is not here is not resin-gated: a local
// specialty and a mob drop cost time, not resin, and pretending otherwise
// would put "go pick 60 flowers" in a resin budget.
//
// A domain is absent because its price comes from the domain itself — a
// talent domain and a weapon domain are both "a domain" and need not cost the
// same.
var resinActivity = map[gamedata.MaterialSource]string{
	gamedata.SourceBoss:   "world_boss",
	gamedata.SourceWeekly: "weekly_boss",
	gamedata.SourceGem:    "world_boss",
}

// BillCost describes what a bill costs and where each part of it comes from.
func BillCost(snap *gamedata.Snapshot, b gamedata.Bill) Cost {
	cost := Cost{Mora: b.Mora}

	var unknown []string
	for _, item := range b.Items {
		mat, ok := snap.Material(item.ID)
		if !ok {
			unknown = append(unknown, fmt.Sprintf("item %d", item.ID))
			cost.Lines = append(cost.Lines, Line{
				Material: fmt.Sprintf("item %d", item.ID),
				Count:    item.Count,
			})
			continue
		}

		line := Line{
			Material: mat.Name,
			Count:    item.Count,
			Source:   string(mat.Source),
			Days:     mat.Days,
		}
		if d, ok := snap.Domains[mat.Domain]; ok {
			line.Where = d.Name
			if d.Entrance != "" {
				line.Where = fmt.Sprintf("%s (%s)", d.Name, d.Entrance)
			}
			line.ResinPerRun = d.ResinCost
			if len(line.Days) == 0 {
				line.Days = d.Days
			}
		}
		if activity, gated := resinActivity[mat.Source]; gated {
			line.ResinPerRun = snap.ResinCosts[activity]
		}

		switch mat.Source {
		case gamedata.SourceEvent:
			cost.Blocked = mat.Name
		case gamedata.SourceWeekly:
			cost.Capped = append(cost.Capped, mat.Name)
		case gamedata.SourceUnknown:
			unknown = append(unknown, mat.Name)
		}
		cost.Lines = append(cost.Lines, line)
	}

	sort.Strings(cost.Capped)
	cost.Capped = dedupe(cost.Capped)

	switch {
	case len(unknown) > 0:
		cost.Unpriced = fmt.Sprintf("Mimir cannot say where %s comes from",
			joinNames(unknown))
	case len(cost.Lines) > 0:
		// The one thing missing, stated as the one thing missing.
		cost.Unpriced = "how many runs each material takes is not published, " +
			"so the bill is exact and the resin total is not Mimir's to give"
	}
	return cost
}

// Summary is the bill in one line, for a headline that has no room for a table.
func (c Cost) Summary() string {
	if len(c.Lines) == 0 {
		if c.Mora > 0 {
			return fmt.Sprintf("%s mora", thousands(c.Mora))
		}
		return ""
	}
	parts := make([]string, 0, len(c.Lines))
	for _, l := range c.Lines {
		parts = append(parts, fmt.Sprintf("%d× %s", l.Count, l.Material))
	}
	if c.Mora > 0 {
		parts = append(parts, thousands(c.Mora)+" mora")
	}
	return strings.Join(parts, ", ")
}

// Runs describes the resin-gated half of a bill: which activity, at what
// price a run, open on which days. It is what a player can act on today.
func (c Cost) Runs() []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range c.Lines {
		if l.ResinPerRun <= 0 {
			continue
		}
		where := l.Where
		if where == "" {
			where = activityLabel(l.Source)
		}
		line := fmt.Sprintf("%s — %g resin a run", where, l.ResinPerRun)
		if len(l.Days) > 0 {
			line += ", open " + weekdayNames(l.Days)
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	sort.Strings(out)
	return out
}

func activityLabel(source string) string {
	switch gamedata.MaterialSource(source) {
	case gamedata.SourceBoss:
		return "a world boss"
	case gamedata.SourceWeekly:
		return "a weekly boss"
	case gamedata.SourceGem:
		return "any boss of the matching element"
	case gamedata.SourceQuest:
		return "a quest"
	default:
		return "a domain"
	}
}

var weekdayLabels = [...]string{"Sunday", "Monday", "Tuesday", "Wednesday",
	"Thursday", "Friday", "Saturday"}

// weekdayNames reads the days out in the order the game shows them: the week
// starts on Monday and Sunday closes it, which is also the order a player
// thinks about a rotation in. The stored numbering starts on Sunday, so the
// two are not the same sort.
func weekdayNames(days []int) string {
	ordered := append([]int(nil), days...)
	sort.Slice(ordered, func(i, j int) bool {
		return (ordered[i]+6)%7 < (ordered[j]+6)%7
	})
	names := make([]string, 0, len(ordered))
	for _, d := range ordered {
		if d >= 0 && d < len(weekdayLabels) {
			names = append(names, weekdayLabels[d])
		}
	}
	return joinNames(names)
}

func dedupe(in []string) []string {
	var out []string
	for i, s := range in {
		if i == 0 || in[i-1] != s {
			out = append(out, s)
		}
	}
	return out
}

func thousands(n int) string {
	s := fmt.Sprint(n)
	if n < 0 {
		return "-" + thousands(-n)
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// applyBill records what an upgrade costs on the action that proposes it.
//
// The resin field is left at zero and the action marked unpriced, which is
// not the same as free: Rank() reads Unpriced and keeps it out of the free
// tier, so an upgrade whose price is unknown cannot outrank one that is
// genuinely free. What the action does carry is the exact bill, the domains
// and bosses behind it, and the price of a single run at each — everything
// that is known, and nothing that is not.
func applyBill(a *Action, snap *gamedata.Snapshot, def gamedata.Character, b gamedata.Bill, ok bool) {
	if !ok {
		a.Unpriced = true
		a.Note = "Mimir has not mined a material bill for " + def.Name +
			", so it cannot say what this costs"
		return
	}

	cost := BillCost(snap, b)
	a.Unpriced = true
	a.ResinCost = 0
	a.Detail["cost"] = cost
	a.Detail["bill"] = cost.Summary()

	switch {
	case cost.Blocked != "":
		// Not farmable at any price, so it is blocked rather than merely
		// unpriced — the two lead to different decisions.
		a.BlockedBy = "requires " + cost.Blocked
	case len(cost.Capped) > 0:
		a.Note = "needs " + joinNames(cost.Capped) +
			", which a weekly boss caps at three discounted runs a week"
	default:
		a.Note = cost.Unpriced
	}
}

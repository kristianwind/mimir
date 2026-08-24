# Mimir — Architecture

## Stack

| Layer | Choice | Rationale |
|---|---|---|
| Backend | Go, single static binary | Ships as a Yggdrasil rune; the calculation core is CPU-bound and benefits from real concurrency |
| Database | SQLite via `modernc.org/sqlite` | Pure Go, no cgo, no separate service. A rune that needs Postgres is a rune nobody installs |
| Frontend | Svelte 5 + Tailwind, built by Vite, `go:embed`ed | ~35 KB gzipped of JS; the theme system is CSS custom properties, so seven element themes cost nothing at runtime |
| Vector search | float32 blobs + linear scan in Go | A few thousand guide chunks. pgvector would add a database server to save microseconds |
| Container | Alpine, `CGO_ENABLED=0` | One file, no runtime dependencies |

Deliberately not chosen: Postgres + pgvector (deployment burden out of
proportion to a personal instance), Redis (SQLite holds the Enka cache; a
restart should not re-spend the rate-limit budget, which an in-memory cache
would), SvelteKit (no SSR needed for an authenticated PWA).

## Repository layout

```
mimir/
├── cmd/mimir/              main, serve, useradd, gamedata subcommands
├── cmd/mimir-mine/         the game data miner, deliberately a separate binary
├── internal/
│   ├── model/              domain vocabulary: elements, stats, slots, records
│   ├── gamedata/           every version-dependent number + snapshot store
│   ├── mine/               builds a snapshot from the public datamines
│   ├── calc/               damage engine — formulas only, no constants
│   ├── optimizer/          branch-and-bound artifact search
│   ├── advisor/            the ranked plan, and the farm simulator
│   ├── enka/               Enka.Network client, TTL cache, mapping
│   ├── good/               GOOD format import/export
│   ├── hoyolab/            Battle Chronicle client (resin, notes)
│   ├── db/                 schema, migrations, artifact matching
│   ├── auth/               argon2id, sessions, middleware
│   ├── kvasir/             the AI layer: fact sheets, and the number check
│   ├── llm/                OpenAI-compatible client, ~300 lines, no SDK
│   ├── api/                HTTP routing and response shaping
│   └── config/             environment configuration
├── web/                    Svelte 5 frontend
├── deploy/                 Dockerfile + Yggdrasil rune
└── docs/
```

## The three rules

### 1. The engine holds formulas; gamedata holds numbers

`internal/calc` contains the damage formula, the resistance branches, the EM
curves and nothing else. Every talent multiplier, base stat, reaction
coefficient and level multiplier enters as an argument from
`internal/gamedata`.

This is what makes the engine testable against the KQM community spreadsheets:
a failing test means the formula is wrong, never that a patch moved a constant.
It also means Mimir refuses to guess. A missing level multiplier returns
`gamedata.ErrMissing` rather than a plausible number, because one fabricated
constant poisons every ranking downstream and nobody would ever notice.

### 2. A number Mimir cannot source does not exist

Missing reaction coefficients produce an error naming the gap, never a
plausible guess. Farming without a measured drop rate is ranked in artifacts
examined rather than in resin. Anything the plan cannot price appears under
"skipped" with the reason — because a silent omission reads as "not worth
doing", which is a claim Mimir has not earned.

The corollary is that the miner refuses to write a snapshot that would make
the engine quietly wrong, and the server refuses to activate one. See
[docs/GAMEDATA.md](docs/GAMEDATA.md).

### 3. The model explains; it never computes

The AI layer calls the engine as a tool and puts words around what comes back.
It has no path to producing a number itself. The moment a language model is
allowed to estimate a multiplier, every figure in the product becomes
unfalsifiable.

Consequence: the AI layer is entirely optional. With `MIMIR_LLM_BASE_URL`
unset, everything works except the explanations.

See [Kvasir](#kvasir) for how that rule is enforced rather than promised.

## Data flow

```
                        ┌── AnimeGameData (numbers, by id)
  mimir-mine ◄──────────┼── Enka store    (character names, by avatarId)
      │                 └── genshin-db    (weapon/set/domain names, talent labels)
      ▼
  snapshot ──────────────────────┐
                                 ▼
UID ──► Enka.Network ──┐    ┌─────────┐
.good file ────────────┼──► │inventory│──► optimizer ──► advisor ──► plan
HoYoLAB cookies ───────┘    └─────────┘        ▲            ▲
                                               │            │
                                          set configs   goals + rotations
```

Three sources write into one inventory. They disagree, so the merge rules are
explicit — see [docs/DATAMODEL.md](docs/DATAMODEL.md). Three more feed the
miner, and the join between them is by numeric id rather than by name for
reasons documented in [docs/GAMEDATA.md](docs/GAMEDATA.md).

## The effect layer

Most of a build is additive stats and needs no machinery. A minority are
conversions and conditionals — Emblem turning Energy Recharge into Burst DMG,
Raiden turning it into Electro DMG, Marechaussee stacking crit rate — and
those are attached to precisely the sets and characters people build around.
Leaving them out does not make a build sheet slightly wrong; it made Raiden
read 43,872 damage per rotation instead of 70,206.

They cannot be mined: they live in ability configs under `BinOutput`, not in a
table. So they are hand-written data in `deploy/effects.json`, under a rule
that makes hand-written safe:

**Every rule cites the in-game wording, and the loader checks the numbers
against it.** Emblem's rule claims 25% and a 75% cap; it only loads because
the mined four-piece text says "25% of Energy Recharge" and "A maximum of
75%". Change the 25 to a 35 and the miner refuses to write a snapshot. Values
taken from a mined talent row are exempt — those are already tracked by the
game data — and a `maxStacks` of 1 is exempt because it is a modelling choice,
not a claim about the game.

An effect is `clamp(flat + rate × (source − offset), min, max) × times ×
stacks`, evaluated in two phases: `pre` grants land before base stats are
resolved, `post` conversions read the final totals. Doing either in the wrong
order silently changes the answer.

Weapon passives add refinement: `byRefinement` carries the value at R1..R5,
and each of the five is checked against that refinement's own wording. The
per-refinement check is the point — a rule claiming 32% at R1 would pass
against the five sentences joined together, because 32 appears in one of them.

**Some effects debuff the enemy, not the character.** Viridescent Venerer's
resistance shred and Raiden's C2 DEF ignore arrive through the same stat block
as everything else — they come from the same sets and constellations, so they
have to — and `Target.WithDebuffs` is where they stop being stats and become
properties of the enemy. Without somewhere to put them, an anemo build's
numbers are unreachably wrong.

**Bonuses stay attached to the attacks they apply to.** DMG bonuses and crit
can both be scoped to an attack category, so The Catch raises Elemental Burst
crit rate and nothing else. This is not pedantry: Raiden's Musou Isshin sword
swings are normal attacks that occur during her burst, so on her The Catch is
worth a fraction of what it is worth on Xiangling, whose whole rotation is
burst damage. Measured on one real account: +16.9% for Xiangling against a
baseline that already includes it, versus a modest bump for Raiden.

**Anything that depends on how you play is asked, not assumed.** Whether your
Noblesse is up, how many Marechaussee stacks you hold, whether the enemy is
frozen — these are declared on the goal. And a conditional that is off because
nobody was asked is reported as such, in the build sheet and in the plan,
because in a ranking it otherwise looks exactly like a bonus that does not
exist.

Every effect-derived number on a build sheet carries its source and the text
it was checked against, so a figure can be verified against the game by
anyone who doubts it.

### Effects that deal their own damage

Some of the game's most-used effects are not bonuses at all. Prototype
Archaic's proc, Noelle's C4 shield explosion and Xiangling's C2 Implode each
land their own hit, and a model that only knows how to add stats simply does
not see them.

An effect can therefore carry an `instance`, and the value it computes becomes
that hit's total scaling multiplier. The occurrence count folds into the
multiplier rather than emitting repeated hits: every proc is identical — same
scaling, same crit, same target — so two hits of 240% and one of 480% are the
same expected damage, and collapsing them keeps a rotation readable.

How often a proc fires is a fact about the rotation, not about the effect, so
it is declared like any other condition. Undeclared means zero: assuming an
occurrence count would put damage into a build the player never claimed.

Hits are post-phase by definition, checked at load and again at evaluation —
one may scale off a final stat, and running it before the totals exist would
read zeros and silently report no damage.

The `maxStacks` on an instance effect is the one place the citation check
steps back, and deliberately: on a stat effect it is the cap the game states
("Max 3 stacks") and is verified, but on a proc it bounds how many times the
*player* says something happened. The game states no such number. It caps
their own input, so it cannot invent damage — it only stops a typed 9999 from
reaching the engine. A test pins the exemption to instance effects so it
cannot leak back into stat caps.

### Constellation talent levels

Two constellations each add three levels to one talent, and *which* talent is
not a rule: Xiangling's C3 raises her burst, Diluc's raises his skill. The
mapping is derived from the constellation's own wording — "Increases the Level
of Pyronado by 3" against the character's mined talent names — for 113 of 117
characters. The four without are Aloy, whose constellations upgrade nothing,
and three trial entries.

The bonus is applied at calculation time rather than stored, because storing
it compounds: a C5 character re-imported three times would climb from level 9
to 18.

The derivation is a heuristic over text, so it is checked against the game.
Enka reports the levels the game actually applied, in
`proudSkillExtraLevelMap`; every import compares Mimir's answer to it and
warns per character when they disagree. On a real eight-character account they
agree exactly.

## The plan

`internal/advisor` generates one candidate per kind — re-equip, weapon swap,
talent, ascension, farming — prices each through the engine, and ranks them by
damage gained per resin. Free actions lead; blocked ones trail.

Two properties are worth calling out because they are what separate a ranking
from a list:

**Contention is named.** A re-equip that pulls a sands off another character
is not a free win, it is a transfer, and the action says whose build pays for
it. Across an account, `BuildAccountPlan` runs goals in priority order, commits
each winner's claim to a shared working inventory, and blocks the lower-
priority side of a tug-of-war instead of offering both directions as wins.

**Farming is simulated, not asserted.** A talent upgrade has a deterministic
answer; artifact farming has a distribution. The simulator samples the drop
model, equips whatever beats the current build, and reports mean, median, p10,
p90 and the probability the whole run changed nothing.

## The optimizer

Five slots over a few hundred owned pieces each is a fifteen-digit combination
count. The search is depth-first over slots with an admissible upper bound at
every node: a partial build plus the best conceivable completion, computed from
per-slot suffix maxima. If that bound cannot beat the worst build currently
kept, the whole subtree is dropped.

The bound is only valid if the objective is monotone non-decreasing in every
stat it reads. Damage objectives are. An objective that *punishes* a stat must
be expressed as a `Constraint` instead — this is enforced by documentation and
by a test that compares the search against brute force.

Set bonuses are discrete and would break the bound, so the search runs once per
set configuration over a pool already restricted to it.

## The farm simulator

A talent upgrade has a deterministic answer. Artifact farming has a
distribution, and every existing tool dodges it with folklore. Mimir samples
the domain's real drop model — 5-star chance, slot and main-stat weights,
substat weights, roll values — equips whatever beats the current build, and
reports mean, median, p10, p90 and the probability that the whole run changed
nothing. That last number is frequently above 30% and players deserve to see it.

Runs are seeded, so the same account on the same day gets the same plan and
"why did this change?" has an answer.

## Updating

A container cannot replace its own image, and Mimir ships as a rune. So the
updater detects how it is deployed and behaves differently rather than
half-working:

- **container** — reports the new version and its notes, and names the rune
  action that applies it. It does not pretend it can do more.
- **dev** — an unstamped build is never "older" than a tag. Replacing a binary
  somebody built on purpose is not an upgrade.
- **binary** — checks the executable is writable *before* downloading
  anything, then installs.

Installing is four steps, and each one has to pass before the next runs:

1. Download the platform's asset from the release.
2. Verify SHA-256 against the release's `checksums.txt`. Refusing a release
   that ships no checksums is the point — an executable about to be launched
   is the wrong thing to take on trust from the network.
3. **Run it.** The candidate is started with its own empty data directory on
   an ephemeral port, and must answer `/api/healthz`. A checksum proves the
   bytes arrived; only running it proves they execute on this kernel and libc.
4. Back up the current binary, start a watchdog, swap, exit.

The watchdog runs from the **backup** — the binary already known to work here
— so a broken update cannot also break the thing that would undo it. It waits
for the health endpoint and, if the new version never answers, copies itself
back. `mimir rollback` does the same by hand.

That is the honest limit of what a process can promise about its own
replacement: the swap is committed only after the candidate has been observed
serving, and if it fails afterwards, something that definitely works is
already waiting.

## Kvasir

The AI layer is named after the wisest of the gods' creations, because the
division of labour is the same as in the myth: Mimir's head at the well knows
the numbers, and Kvasir is who you ask what they mean.

It appears on every page — the plan, a goal, a build, the roster, the
inventory — and answers one question: how do you get better. There is also a
conversation for the question after that.

The design problem is not making a model produce advice. It is making advice a
player can trust next to figures that were calculated. So rule 3 is enforced at
two points rather than asked for in a prompt.

**The model is handed a fact sheet, not a database.** Every surface has one
function that runs the engine and writes down what came back — the ranked
actions, the resolved stats with the game text each effect was checked
against, the measured drop model, what the engine refused to price. That
document is the model's entire knowledge of the account. There is no tool that
computes, no inventory table to reason over, no path from a prompt to a
number.

**Every number in the answer is checked back against that fact sheet.** This is
the same rule that makes the hand-written effect library safe — a claim only
loads if its numbers are in the text it cites — pointed at a sentence instead
of a set bonus. The check reads a decimal point and a decimal comma alike —
models trained on European text write 34,53 whatever they are asked for —
allows a figure to be rounded but not to gain precision it never had, and
exempts integers up to ten, because counting is not calculating.

What happens on a violation differs by shape, and deliberately:

- **In an opinion**, the offending bullet is deleted, and the deletion is
  reported to the reader with the figure named. A bullet is a self-contained
  claim; cutting one leaves the rest true. First the model is told exactly
  which figures were not sourced and asked once more — most of the time that
  is enough. If nothing survives twice, nothing is shown.
- **In a conversation**, the answer stands and the figures are flagged as
  untrustworthy. A sentence cut out of a paragraph leaves an argument missing
  its middle, so naming the problem beats silently editing it.

The conversation is the one place the model chooses what to look at. It gets a
menu of eight engine calls — the plan, a build, a talent table, the roster, the
goals, the inventory, the drop model — every one of them read-only, and each
returning the same kind of fact sheet the opinion cards are built from.
Whatever comes back is added to the set of numbers the answer may contain. So
the model can pick which calculation to run and still cannot produce a figure
without running one. Kvasir advises; equipping a piece or changing a goal stays
with the player.

**An answer is kept next to the fact sheet that produced it**, keyed on a hash
of that sheet. That is a cache — an account that has not changed is not
re-asked, which matters when the endpoint is a local model on the household's
own machine — but the reason it is stored rather than memoised is the evidence.
Every page offers "what was Kvasir told?", and an opinion whose input has been
thrown away cannot be checked. Change a goal or equip a piece and the hash
moves, so a stale opinion can never sit next to numbers it was not talking
about. It is the same instinct as seeding the farm simulator: "why did this
change?" deserves an answer.

The endpoint is any OpenAI-compatible one — LM Studio, Ollama, vLLM, or a
hosted API — so the operator decides where a household's game account is
allowed to go. Unset, and none of this exists: no card, no page, no request.

## The beacon

One ping a day carrying exactly two fields: a random instance id and the
version. No UIDs, no account data, no inventory, no request metadata. The
settings page renders that payload literally rather than describing it,
because a promise about telemetry is worth what the operator can verify.

Off until switched on, and off stays off — only an explicit `"1"` enables it,
so no upgrade or schema change can flip it back. Yggdrasil ships the same
beacon default-on behind a first-login disclosure; Mimir holds a household's
game account and starts from the other end.

**There is no default collector**, and that is a correction rather than an
omission. Reusing Yggdrasil's address seemed harmless and was not: the first
test ping landed in Yggdrasil's live install count as a phantom panel running
a version that does not exist. Enabling the beacon now requires an address,
and a ping that fails is recorded and shown — failing silently and retrying
every thirty minutes is how an install goes missing from a count with nobody
able to say why.

## Security

- Passwords: argon2id, 64 MB / 3 iterations / 4 lanes, same as Yggdrasil
- Sessions: opaque 256-bit random tokens, stored only as SHA-256 hashes,
  HttpOnly + SameSite=Strict
- HoYoLAB cookies: AES-256-GCM at rest, key derived from a machine secret
  generated on first boot. Losing the key makes them undecryptable, which is
  the intended failure mode
- Account ownership is enforced in one middleware, not per handler, so a new
  endpoint cannot forget it
- Login failures are indistinguishable between a wrong password and an unknown
  user, in both message and timing

## Rate limits and external APIs

Enka returns a `ttl` on every response: requests before it expires return the
identical payload and still consume rate limit. Mimir caches on that TTL, and
degrades to labelled stale data on a rate limit or outage rather than showing
an empty page — the player's build did not change in the meantime.

HoYoLAB is an unofficial API. It requires the user to enable Real-Time Notes
themselves, and its cookies are full account credentials. It is opt-in, it is
never required, and nothing else depends on it.

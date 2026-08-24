# Mimir

A Genshin Impact adviser: import your account, have it optimised, and be told
what to spend tomorrow's resin on.

The name is the adviser at the well, who knows.

## What it does that other tools do not

The existing tools answer *what your best build would be*. That is a static
answer to a dynamic question. You have 180 resin a day, a domain rotation that
changes with the weekday, a half-finished roster and a banner closing on
Thursday.

Mimir instead ranks **every possible upgrade on the whole account by expected
damage gained per resin**. Real output from a real account with two goals set
up:

```
1. [RaidenShogun] Switch to 4pc EmblemOfSeveredFate    +34.53 %   free
                  takes pieces from Xiangling
2. [Xiangling]    Give Xiangling the weapon The Catch  +12.49 %   free
                  blocked: RaidenShogun is using it, and has at least as high a priority
3. [Xiangling]    Switch to 4pc EmblemOfSeveredFate    +12.42 %   free
                  blocked: RaidenShogun is using it, and has at least as high a priority
4. [Xiangling]    elemental skill 9 → 10                +1.09 %   20 resin
                  blocked: requires a Crown of Insight
```

Free rearrangements first, blocked actions last, and artifact farming answered
with a simulation of the domain's drop distribution rather than a rule of
thumb.

Note what it does *not* do: it does not claim that numbers 2 and 3 are free
wins. They cost Raiden her set, and it says so.

## Data sources

Nobody types in 1,400 artifacts. All three established sources are supported:

| Source | What it gives | What it needs |
|---|---|---|
| **Enka.Network** | Showcase: up to eight characters with level, constellation, talents, weapon and equipped artifacts | A UID, nothing else. *Show Character Details* has to be switched on |
| **GOOD format** | The whole inventory — every artifact, weapon and material | A `.good` file from Inventory Kamera or Genshin Optimizer |
| **HoYoLAB** | Resin, the day's commissions, expeditions, Abyss | `ltoken`/`ltuid` cookies, encrypted in the database |

Static game data is mined from three sources, all keyed on numeric ids: the
numbers from `DimbreathBot/AnimeGameData`, the names from Enka's own store and
from genshin-db. The obvious route — TextMap — does not work: the hashes in the
current mirror resolve zero out of 165 character names.
[docs/GAMEDATA.md](docs/GAMEDATA.md) explains why, and what is done instead.

## Kvasir's opinion

The plan is ranked, but it is silent. It says Emblem is +34.53 % and that it
costs Xiangling her set; it does not say whether you should do it, what is
actually holding the account back, or what you forgot to tell Mimir.

Kvasir does. He sits on every page — the plan, the goals, the characters, the
artifacts — and answers one question: how do you get better. And there is a
conversation for the question after that.

The hard part is not getting a model to write advice. It is making the advice
worth trusting when it stands next to figures that were calculated. So the rule
— **the model never calculates** — is not a plea in a prompt. It is enforced in
two places:

1. **Kvasir is handed a fact sheet, not a database.** Each page has one
   function that runs the calculation core and writes down what came back. That
   sheet is everything the model knows about the account. There is no path from
   a prompt to a number.
2. **Every figure in the answer is checked against the sheet.** Exactly as an
   effect rule only loads if its numbers appear in the game text it cites. A
   point containing a figure nobody calculated is deleted before you see it —
   and you are told that it was deleted, and which figure it was.

Every page has a *What was Kvasir told?* — the whole fact sheet, verbatim. An
answer whose evidence has been thrown away cannot be checked.

The conversation is the one place the model picks what to look at: it can call
the calculation core — the plan, a build, a talent table, the inventory — and
the answer says what it looked up. All eight calls are read-only. Kvasir
advises; equipping a piece or changing a goal is yours.

All of it is optional. `MIMIR_LLM_BASE_URL` points at an OpenAI-compatible
endpoint — LM Studio, Ollama, vLLM or a hosted API — so you decide where the
household's game account is allowed to go. Leave it blank and the layer does
not exist: no card, no page, no request.

## Getting started

```bash
npm --prefix web install && npm --prefix web run build
go build -o mimir ./cmd/mimir && go build -o mimir-mine ./cmd/mimir-mine

./mimir-mine -version 7.0.0 \
  -supplements deploy/supplements.json \
  -effects deploy/effects.json \
  -o snapshot.json
./mimir gamedata import snapshot.json
./mimir useradd -u sabrina
./mimir serve
```

The miner fetches game data from the public datamines. It takes ten to twenty
seconds cold, under two warm, and refuses to write a snapshot that would make
the calculations quietly wrong. See [docs/GAMEDATA.md](docs/GAMEDATA.md) — in
particular the part about why the names do not come from TextMap.

The server listens on `:8080`. In development the frontend runs separately with
hot reload and proxies `/api` through:

```bash
npm --prefix web run dev
```

## As a Yggdrasil rune

`deploy/mimir.yaml` is the rune definition and `deploy/Dockerfile` builds the
image. The frontend is `go:embed`ed into the binary, so the container is one
static file and a data directory — no node, no CGO, no external database.

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md). The three rules that govern everything
else:

1. **The calculation core holds formulas, never game constants.** Everything
   that changes with a patch lives in `internal/gamedata` and is mined. A new
   version is a data sync, not a code change.
2. **The language model never calculates.** It calls the core as a tool and
   explains the result. Otherwise it hallucinates multipliers, and the whole
   product's credibility is gone. The rule is enforced, not promised: every
   figure Kvasir writes is checked against the fact sheet the core gave him.
3. **A number Mimir cannot source does not exist.** Missing reaction
   coefficients produce an error naming the gap, not a plausible guess. Farming
   without a measured drop rate is ranked in pieces rather than in resin.
   Anything that cannot be priced appears in the plan under "caveats" — because
   a silent omission reads as "not worth doing".

   That covers the conditional bonuses too — sets, character passives and
   weapon passives — which are the one thing Mimir *cannot* mine. They live in
   `deploy/effects.json` as hand-written rules, but every rule cites the game's
   own wording, and the loader checks that the numbers are in it. A rule
   claiming 25 % against a text that says 20 % does not load. For weapons, each
   refinement is checked against its own sentence.

## Status

See [PROGRESS.md](PROGRESS.md).

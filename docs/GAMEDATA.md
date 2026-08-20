# Game data

## What a snapshot contains

One `gamedata.Snapshot` is a complete, self-consistent picture of one game
version:

- **characters**: base stats, growth curve names, ascension bumps and the
  ascension stat, skill ids, and the full per-level talent tables
- **weapons**: base ATK, secondary stat, curves, ascension bumps
- **artifact sets**: two- and four-piece stat bonuses, plus the names of the
  conditional effects the effect layer handles
- **curves**: the shared growth curves, stored once and referenced by name
- **levelMultipliers**: transformative reaction level multipliers
- **substatRolls** and **mainStatValues**: the artifact stat tables
- **avatarIds**, **weaponIds**, **setIds**, **travelerDepots**: the bridge from
  the ids Enka reports to GOOD keys
- **domains**: the artifact domains and which sets each drops

## Sources, and why there are three

No single upstream provides all of it, so the miner joins three — every one of
them keyed by **numeric id**, never by name. That is the design decision the
rest of this document explains.

| What | Source | Why this one |
|---|---|---|
| Numeric tables | `DimbreathBot/AnimeGameData` (`ExcelBinOutput`) | The canonical datamine, current within days of a patch |
| Character names, elements, skill order | `EnkaNetwork/API-docs` (`store/`) | Keyed by the same `avatarId` a showcase reports, so a name can never disagree with the data being imported |
| Weapon, artifact-set and domain names; talent labels; passive and constellation text | `theBowja/genshin-db` | Curated, current, keyed by the same numeric ids as the datamine |

### The trap: TextMap does not resolve

The obvious approach — take `nameTextMapHash` from `ExcelBinOutput` and look it
up in `TextMap/TextMapEN.json` — **does not work** against the current
Dimbreath mirror. Measured on version 7.0:

```
AvatarExcelConfigData.nameTextMapHash      0 of 165 resolve
WeaponExcelConfigData.nameTextMapHash      0 of 281 resolve
EquipAffixExcelConfigData.nameTextMapHash  0 of 1382 resolve
```

Not a few misses — none. The hashes in that repository's `ExcelBinOutput` are
not the keys of its own `TextMap`.

The mirror `vonsilke/AnimeGameData` *does* resolve cleanly (126 of 126
characters, and its hashes match Enka's store exactly), but its last commit is
version 5.3 — well over a year stale. Neither repository is usable on its own:
one has current numbers with unusable names, the other usable names with stale
numbers.

Hence the split. Numbers come from the current mirror keyed by id; names come
from sources that are keyed by the same ids. No TextMap is read at all.

### Talent tables

Talent scaling in the raw datamine is an unlabelled `paramList` per level. The
labels that say which parameter is "Press DMG" and which is "Press CD" live in
TextMap — the part that does not resolve. genshin-db has already paired the
two, so the miner reads a solved problem rather than re-solving it against a
broken hash table.

A label carrying several placeholders is one attack with several components
(Xiangling's third normal hits twice), and each becomes its own row, numbered
`· del 1`, `· del 2`. Scaling is inferred per placeholder from the text that
follows it, which is why Nahida's skill correctly reports one ATK-scaling and
one EM-scaling component from a single label.

### Ability text

Passive, constellation, artifact-set and weapon descriptions are mined
alongside the numbers, because the effect layer cites them and those
citations are checked (see [../ARCHITECTURE.md](../ARCHITECTURE.md), "The
effect layer"). Without the text there would be nothing to check against.

Weapon passives are mined **once per refinement** — all five wordings, not
one. A weapon passive's numbers change with refinement, so a rule claiming
32% at R5 has to be checked against the R5 sentence specifically. Checking
against the five joined together would happily accept an R5 figure presented
as an R1 claim, which is exactly the mistake that matters. Coverage is 227 of
237 weapons; the rest have no passive.

### Constellation talent levels

Two constellations add three levels to one talent each, and which talent it is
varies per character. The miner reads it out of the constellation's own
wording and matches the named ability against that character's talent names —
three passes, most specific first: the name as written; the name with a
"Elemental Skill"/"Elemental Burst" label stripped off; and finally the label
alone, which still identifies the slot if upstream renames the ability.
Emphasis markup (`**Eternal Tides**`) is stripped, because newer characters
use it and older ones do not.

Coverage is 113 of 117. The rest are Aloy, whose constellations upgrade
nothing, and three trial entries.

Being a heuristic over text, it is verified rather than trusted: Enka reports
the bonuses the game actually applied, and every import compares them against
Mimir's derivation, warning per character on any disagreement.

## What is deliberately not mined

**Reaction coefficients** (overloaded, hyperbloom, …) live in the ability
configs under `BinOutput`, not in a table. Mimir does not guess a damage
multiplier, so transformative reaction damage returns an error naming the gap
until they are mined properly.

**Resin costs** are not in `ExcelBinOutput` — `enterCostItems` is empty for
every dungeon. They are seeded in `deploy/supplements.json` with their origin
stated, and they are the operator's to verify.

**Artifact drop distributions** are not in the datamine in any usable form.
Rather than asserting community-measured figures as if they were mined, Mimir
measures them from the player's own inventory (`GET /accounts/{id}/dropmodel`)
and reports the bias that carries: an inventory is what somebody chose to keep,
so good main stats are over-represented. The per-run *yield* cannot be measured
from an inventory at all — an inventory records what dropped, never how many
runs it took — so without it farming is ranked in five-star pieces examined
rather than in resin. That still compares domains honestly; it just cannot
compare them against a talent upgrade.

**Weekday domain rotation** is in `DailyDungeonConfigData`, but its weekday
field names are obfuscated hashes that rotate between versions. Artifact
domains are open every day and are mined; talent and weapon domains wait.

## Running the miner

```bash
go build -o mimir-mine ./cmd/mimir-mine
./mimir-mine -version 7.0.0 -supplements deploy/supplements.json -o snapshot.json
./mimir gamedata import snapshot.json
```

A full mine is about 350 small requests and takes ten to twenty seconds cold,
under two warm — downloads are cached on disk by URL, so iterating on the
mapping costs no network at all.

Useful flags: `-gamedata-repo` / `-gamedata-ref` and `-names-repo` /
`-names-ref` to point at a different mirror or pin a commit, `-cache-ttl 0` to
freeze a cache permanently.

## Validation

The miner refuses to write a snapshot that would make Mimir silently wrong,
and distinguishes the two failure modes:

- **Errors** stop the write: too few characters resolved (the classic symptom
  of a desynced name source), an id mapping pointing at a definition that does
  not exist, a character referencing a missing growth curve, an artifact stat
  table that came back short.
- **Warnings** let it through and name the feature that will refuse to run.

```
error: only 3 characters resolved; the name source has desynced from the datamine
warning: no reaction coefficients; transformative reaction damage is unavailable
```

The server validates again on import, so a snapshot missing an Enka bridge or
an artifact stat table never becomes active.

## Storage and rollback

Snapshots are stored gzipped in SQLite and activated by flipping one flag:

```bash
mimir gamedata list                  # what is stored
mimir gamedata activate 7.0.0        # roll back or forward
```

Activation swaps an atomic pointer, so a request in flight keeps reading a
consistent snapshot. A failed mine leaves the previous version serving.

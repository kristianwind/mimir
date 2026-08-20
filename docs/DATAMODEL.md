# Data model

## Where the numbers live

Every value in Mimir is in exactly one of three categories, and the category
decides where it lives.

| Category | Example | Lives in | Changes when |
|---|---|---|---|
| Formula | `(1 - res)` below 75% RES | `internal/calc`, in code | Never; HoYoverse has not changed a damage formula |
| Game constant | Nahida's burst multiplier at talent 9 | `internal/gamedata`, synced | Every patch |
| Player state | Your Nahida is C2 with a +16 circlet | SQLite | Whenever you play |

The middle row is the one that gets tools wrong. A hardcoded multiplier that
silently drifts a patch out of date produces numbers that look right and rank
builds wrongly. Mimir's engine takes every game constant as an argument and
returns `gamedata.ErrMissing` when one has not been synced, so an out-of-date
install is loud rather than subtly wrong.

Identifier *names* from the datamine (`FIGHT_PROP_CRITICAL_HURT`,
`EQUIP_BRACER`) are treated as formula, not constant: they have been stable
since release, and a rename breaks parsing loudly instead of shifting a number
quietly.

## Three sources, one inventory

| | Enka.Network | GOOD file | HoYoLAB |
|---|---|---|---|
| Characters | 8 showcased | all owned | all owned |
| Artifacts | equipped only | entire inventory | none |
| Weapons | equipped only | entire inventory | none |
| Live state | none | none | resin, notes, Abyss |
| Cost to user | a UID | install a scanner | hand over cookies |

They overlap and they disagree. The merge rules:

- **Characters** are keyed by `(account, character)`. Last write wins on level,
  ascension, constellation and talents. A GOOD import from a scanner and an
  Enka import minutes apart will agree; where they do not, the more recent one
  is the more recent truth.
- **Weapons** are replaced wholesale on import. Two R1 Favonius Swords at
  level 90 are genuinely indistinguishable, so incremental matching would
  invent distinctions that do not exist.
- **Artifacts** are matched, not replaced. See below.

## Artifact identity

An artifact has two kinds of sameness, so it carries two hashes.

**`fingerprint`** covers the full current state: set, slot, rarity, main stat,
level and every substat value. Re-importing an unchanged `.good` file matches
on this and updates 1,400 rows instead of duplicating them.

**`identity`** covers only what upgrading cannot change: set, slot, rarity,
main stat, and *which* substat lines the piece rolled. This is how Mimir
recognises "the same flower, now +20" rather than filing it as a second flower.

Import is a three-way decision per piece:

1. Exact `fingerprint` match → unchanged, refresh location and lock.
2. Same `identity`, level not lower, and **every** substat value greater than or
   equal to the stored one → the same physical artifact, levelled. Rolls only
   ever add, so a substat that shrank proves it is a different piece.
3. Otherwise → new.

Where several stored rows could be the upgrade, the closest by total substat
distance wins. An account can hold two Deepwood flowers with the same substat
lines, and picking the nearer one keeps their histories from swapping places
between imports.

The result is reported back as `{inserted, upgraded, unchanged}` rather than a
silent success, because "1,412 unchanged, 6 upgraded, 3 new" is how you know
the import did what you meant.

## Units

The engine works in fractions throughout: 46.6% is `0.466`.

Both GOOD and Enka report percentages in display units, and both mark them the
same way — a trailing underscore in the stat key (`atk_` is a percentage,
`atk` is flat). Normalisation happens once, at the import boundary, using that
rule. `eleMas` has no underscore and is never divided; this is the single most
likely place for a silent factor-of-100 error, and it has a test.

## Talent levels and constellations

Stored talent levels are **base** levels, 1–10. The +3 from C3 and C5 is
applied by the engine from the constellation count, using
`gamedata.Character.ConstellationTalentBonus`.

Storing the displayed level instead would compound on every re-import: a C5
character's burst would climb from 9 to 12 to 15 across three scans.

## Conditional effects

Artifact set bonuses split in two.

Unconditional bonuses (Gladiator's +18% ATK) are stat blocks and live directly
on `gamedata.ArtifactSet`.

Conditional ones (Marechaussee's HP-change stacks, Emblem's ER-to-burst-damage
conversion) are not stat blocks — they depend on game state that the optimizer
has to be told about. They are listed by name on the set definition and
evaluated by the effect layer, which receives the assembled build and the
rotation context. A set whose conditional is not yet modelled reports its
two-piece bonus and says so, rather than pretending the four-piece is inert.

## Goals

`goals` is the table that makes ranking possible. "Should I level Bennett?" has
no answer without knowing which team he serves and which rotation is being
measured — +8% on a burst nobody casts is +0%.

A goal names a character, a team, a rotation and a target (enemy level and
resistances). Every gain figure Mimir reports is relative to a stated goal
against a stated target, and both are shown in the UI. Two tools that disagree
about a DPS number have usually just assumed different enemies.

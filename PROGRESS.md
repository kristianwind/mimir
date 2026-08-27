# Progress

The plan — every upgrade ranked by gain per resin — is the part no other tool
builds, and it works on real data now. Everything below is what makes it more
accurate, not what makes it work.

## Done

### Foundation
- [x] Go module, package layout and dependencies match the Yggdrasil conventions
- [x] SQLite schema: users, sessions, accounts, inventory, goals, plan,
      game data snapshots, guide corpus, audit
- [x] argon2id login, opaque session tokens (stored only as SHA-256), middleware
- [x] Configuration from environment variables, machine secret on first boot
- [x] HTTP server on chi, embedded frontend, graceful shutdown

### The game data miner
- [x] **Numeric tables** from `DimbreathBot/AnimeGameData`, keyed on id
- [x] **Names** from the Enka store (characters) and genshin-db (weapons, sets,
      domains) — keyed on the same ids. TextMap is not used: the hashes in the
      current mirror resolve 0 out of 165. See [docs/GAMEDATA.md](docs/GAMEDATA.md)
- [x] **Talent tables** with labels, units and the scaling stat per parameter
- [x] **Artifact domains** with the sets they drop
- [x] Disk cache, validation that refuses an incomplete snapshot by name,
      gzipped storage in SQLite, atomic activation and rollback
- [x] Verified against known figures: 4780 HP flower, 62.2 % CD circlet,
      1446.8535 level multiplier, Emblem 2pc = 20 % ER

### Calculation
- [x] **Damage engine** — formulas with no game constants in them: the DEF, RES
      and crit terms, the EM curves, rotation evaluation
- [x] **Artifact optimizer** — branch-and-bound with an admissible upper bound
      and a validity predicate for set requirements. Tested against brute force
- [x] **Set configurations** — 4pc and 2+2 enumerated from what you own, each
      searched separately so the bonuses do not break the bound
- [x] **Farm simulator** — Monte Carlo over the drop distribution; mean,
      median, p10, p90 and the chance the trip changed nothing
- [x] **A drop model measured on your own inventory** rather than asserted drop
      rates, with the bias written out
- [x] **Rotations** built from mined talent rows, validated against the labels

### The plan
- [x] Candidates: free rearrangement, weapon swap, talent +1, level/ascension,
      artifact farming
- [x] Ranked on gain per resin — free first, blocked last
- [x] **Gear contention named**: "takes pieces from Xiangling"
- [x] **Account plan** across goals with priority resolution
- [x] Anything that cannot be priced appears in `skipped` with the reason

### Potential: which characters are worth building
- [x] **A yardstick that needs no rotation** — one cast of the elemental skill
      and one of the burst, at the character's own talent levels, against a
      level 90 enemy. Stated on the page, not implied: a damage number with no
      named conditions is not comparable to anything
- [x] **Every owned character measured**, including the ones the plan cannot
      see because they have no goal
- [x] **Ranked on gain alone** — resin is not in the ordering, so a talent book
      that buys more damage outranks a free rearrangement that buys less
- [x] **Two numbers, because they answer different questions**: damage added
      (what one upgrade buys, which favours strong builds) and headroom (how
      much owned gear is unequipped, which is where the neglected ones show up)
- [x] **Unlevelled artifacts called out** — the one upgrade that costs no resin
      and no domain run. Only the main stat is projected; the substat rolls a
      piece gains on the way are the one thing nobody can predict
- [x] **Actions are never summed** — re-equipping and levelling the piece it
      replaced overlap, so the ranking uses the largest single upgrade
- [x] **Goals can be written from it**, marked `derived` and saying so wherever
      their numbers appear. A goal you wrote is never touched; saving a derived
      one makes it yours
- [x] **A hit and a buff are no longer the same thing** — `IsDamage` treated any
      percentage row containing "DMG" as damage, so Raiden's *Elemental Burst
      DMG Bonus* was a valid rotation step the engine multiplied by her attack.
      Checked against all 117 mined characters: 44 rows are modifiers, 1,300
      are hits, and none of the real ones are excluded

### Material accounting: what an upgrade actually costs
- [x] **Every ascension phase and every talent level carries its exact bill** —
      mined from the datamine's own cost tables, keyed by item id, with mora
      kept separate because mora is the one cost resin never buys
- [x] **A catalogue of 919 materials** joined to those bills by id: what each
      one is, and where it comes from. Names cannot come from the datamine —
      its TextMap does not resolve — so they come from genshin-db, keyed by
      the same ids the quantities are keyed by. A stale name source can
      mislabel a material; it cannot change how many of them an ascension costs
- [x] **Every material in every bill is placed**: domain, world boss, weekly
      boss, elemental gem, overworld, quest or event. Checked across all 117
      characters — no material in any bill is left unclassified. The one
      family the prose could not place, the 32 elemental gems, is identified
      by a type text verified to cover exactly those 32 and nothing else
- [x] **Talent and weapon domains, with their weekdays** — the datamine's own
      rotation table has obfuscated keys, so the days come from genshin-db and
      are *checked* against the datamine's grouping of which materials share a
      rotation slot. The labels cannot be read there; the partition can, and a
      disagreement fails the sync rather than sending somebody to a shut door
- [x] **Bills are per talent, not per character** — on the strength of one
      character in 117. The Geo Traveler's normal attack takes Resistance
      books and Dvalin's Sigh where the skill and burst take Diligence and
      Tail of Boreas
- [x] **A talent level is no longer priced at one domain run** — it was, flat,
      for every level. That is roughly right at level 2 and wrong by more than
      an order of magnitude at level 9, which needs twelve four-star books and
      a weekly boss drop. The plan sorted on that number
- [x] **What is known is separated from what is not**: the bill is exact and
      so is the price of one run at each place it comes from. How many runs it
      takes is not published anywhere — the in-game preview lists a domain's
      materials without quantities — so the resin total is reported as missing,
      with the reason, rather than filled in with a plausible average

### Who counts as a character
- [x] **The datamine is the roster** — it used to be Enka's store, on the
      reasoning that a character you cannot showcase is not one you can build.
      Sound reasoning, wrong consequence: that store is community-maintained
      and runs months behind the game, so a character released in April was
      still absent in August. Not absent from a dropdown — absent from the
      engine, so an account that had her showed her level and then could say
      nothing at all about her
- [x] **Two name sources, asked in turn** — Enka first, because it alone
      orders the three talents; then genshin-db, keyed by the same avatar id.
      Both are consulted only for what the datamine cannot be read for
- [x] **Trial copies are not characters** — the old roster let in
      "PyroArchonTest" and "HuTaoTrial" while dropping real ones. A trial
      reuses the character's portrait and is added to the game later, so one
      icon is one character and the lowest id is the real one. No list of
      names to exclude, no id range treated as magic
- [x] **Talent groups come from the datamine too**, so a character the name
      sources are behind on still gets talent tables and a material bill
- [x] Net effect on a 7.0.0 sync: 117 characters became 124 — ten real
      characters recovered, three test rows removed, and every one of them has
      an element, a portrait and a bill

### What to aim for
- [x] **A target build per character, computed rather than looked up** — which
      set, which main stat in each slot, which substats to chase. It is the
      question people take to a wiki, and a wiki gives one answer to every
      player; this one runs against this character's own constellation, talent
      levels and rotation, shows its numbers, and can therefore be checked
- [x] **Only sets a domain actually drops at five stars.** The obvious filter
      — the rarities recorded on the set — lists every rarity the set has
      pieces for, so Berserker came back as a five-star set and its 12% crit
      two-piece outranked half the real ones. Which domain drops what is mined
- [x] **Main stats only where the slot can roll them**, and no choice offered
      for the flower and plume, which do not have one
- [x] **The substat allocation travels with the answer.** A target build has
      no real artifacts in it, so every candidate is given the same invented
      rolls — the mined roll values, in the count the game grants by +20. That
      allocation is the recommendation, not a measurement, and saying so is
      what makes the ranking between candidates honest
- [x] **Weapons are deliberately not ranked** — and the answer says so. Most
      of what makes a weapon good is its passive, and the passives are mined
      as wording rather than as numbers: four of two hundred and forty-seven
      are modelled. A ranking on base attack and substat alone puts a
      four-star above a five-star and looks like advice

### Comparing against another account
- [x] **Any published showcase, on the same yardstick as yours** — weakest
      first, because the point is what to work on. Nothing about this account
      is sent anywhere and nothing about theirs is kept: fetched, measured,
      dropped, with a test counting the rows before and after
- [x] **No leaderboard, and the page says why.** The public ones are built
      from accounts whose owners chose to submit them, so a percentile
      measures who bothers to publish. The largest also answers 403 to
      anything that is not a browser
- [x] **Constellation and refinement sit beside the score, never inside it** —
      a build that wins on a constellation is not one anybody can copy

### Data in
- [x] **Enka.Network** — client, TTL cache with labelled stale fallback,
      setId as the primary bridge, Traveler variants via the skill depot
- [x] **GOOD format** — parser, version validation, unit normalisation.
      Versions 2 and 3 both import: 3 only adds optional fields to an artifact,
      and Genshin Optimizer's own schema reads all of them through one
      definition. A version above what has been checked is still refused, with
      a message that says the file is fine and Mimir is behind
- [x] **Artifact matching** — fingerprint + identity, so a re-import becomes
      `{new, upgraded, unchanged}`
- [x] **HoYoLAB** — DS signature, Real-Time Notes, retcodes translated
- [x] **Secrets** — AES-256-GCM for the HoYoLAB cookies

### The effect layer
- [x] **A declarative DSL** for conversions and conditional bonuses — data, not
      code. Two phases, so a conversion reads the finished totals
- [x] **Citation requirement**: every rule points at the game text it comes
      from, and the loader checks that the numbers are actually in it. 25 %
      against a text that says 20 % does not load
- [x] **Attack categories** — Raiden's Musou Isshin sword swings are normal
      attacks and do not get the burst bonus. Both DMG bonus and crit can be
      bound to a category, so The Catch only lifts burst crit
- [x] **Weapon passives with refinement** — all five wordings are mined, and
      each value is checked against its own refinement's sentence. 227 of 237
      weapons
- [x] **Constellations** — both stat effects and the +3 talent levels, where
      which talent is hit is derived from the text (113 of 117 characters) and
      cross-checked against the game's own numbers on every Enka import
- [x] **Enemy debuffs** — Viridescent's RES shred and Raiden's C2 DEF ignore
      can be expressed, which they could not before
- [x] **Effects with their own damage instance** — procs and explosions that
      land their own hit (Prototype Archaic, Noelle C4, Xiangling C2), where
      the occurrence count is declared and folded into the multiplier
- [x] 21 rules in total: sets, character passives, constellations, weapons,
      enemy debuffs and procs
- [x] **Conditions are asked, not guessed** — and a condition nobody has
      answered appears in the plan rather than being quietly switched off
- [x] **Traceability**: every effect-derived figure on the build sheet carries
      its source and its citation
- [x] Verified against the game's own numbers: Raiden's HP, ATK, DEF, crit
      rate, crit damage, ER and Electro DMG match Enka's fightPropMap

### Nothing needs a shell any more
- [x] **Game data from the System page** — fetches, verifies the effect rules
      and activates the result, as a background job with progress. One at a
      time: two at once would fight over the same cache
- [x] Together with the first-run flow that means a fresh installation can be
      set up entirely in the browser. `docker exec` was the last thing that
      needed SSH to the host

### The AI layer: Kvasir
- [x] **Kvasir on every page** — the plan, the goals, a single goal, a single
      build, the roster and the inventory. One question: how do you get better.
      Plus a conversation for the question after that
- [x] **A fact sheet rather than a database** — each page has one function that
      runs the calculation core and writes down the result. That sheet is
      everything the model knows about the account. There is no path from a
      prompt to a number
- [x] **The number check** — every figure in the answer is checked against the
      sheet. The same rule as the effect layer: a claim only loads if the
      numbers are in what it cites. It reads a decimal point and a decimal
      comma alike, allows rounding but not invented precision, and exempts
      integers up to ten, because counting is not calculating
- [x] **A point with an unsourced figure is deleted** — but first the model is
      told exactly which figures could not be sourced and given one more try.
      And what was deleted is shown on the page. A silently edited answer is
      not an answer the reader can weigh. If nothing survives twice in a row,
      nothing is shown
- [x] **In the conversation the figure is flagged instead** — a sentence cut
      out of a paragraph leaves an argument missing its middle, so the answer
      stands and the figures carry a warning
- [x] **Eight read-only calls into the calculation core** — the plan, a build,
      a talent table, the roster, the goals, the inventory, the drop model. The
      model chooses what to look at, and the answer says what it looked up. It
      can pick which calculation to run and still cannot produce a figure
      without running one
- [x] **"What was Kvasir told?"** — the whole fact sheet, verbatim, on every
      page. The answer is stored next to the sheet it came from, keyed on that
      sheet's hash: an unchanged account is not asked again, and an old answer
      cannot sit next to numbers it was not about
- [x] **Thinking is off** — Kvasir asks for a short answer built from a fact
      sheet, and a reasoning model spends the whole token budget and most of
      the wait on a chain of thought nothing reads. Measured on the model this
      instance is configured with: 16,000 tokens and three and a half minutes
      without answering, against five seconds with it off. `MIMIR_LLM_THINKING`
      turns it back on for a model that needs it
- [x] **Optional all the way down** — without `MIMIR_LLM_BASE_URL` the layer
      does not exist: no card, no page, no request. The endpoint is any
      OpenAI-compatible one, so the operator decides where the household's game
      account is allowed to go. And the System page probes the endpoint rather
      than trusting it

### User management
- [x] **First-run flow** — the login page creates the first administrator when
      the instance is empty, and the window closes itself the moment the first
      account exists. The guard is in the insert, not around it
- [x] **Roles** — administrators can update Mimir, manage the beacon and manage
      users; ordinary users only their own accounts
- [x] **The last administrator cannot be removed** — not demoted, not disabled,
      not deleted. Three roads to the same irreversible state
- [x] Disabling and resetting a password clear sessions; changing your own
      requires the current one

### Frontend
- [x] Svelte 5 + Tailwind, 35 KB gzipped
- [x] **Theme picker** — the seven elements × light/dark/system, without a flash
- [x] Login, accounts, UID input, Enka fetch, .good upload
- [x] Character and artifact views
- [x] **Goal editor** with real talent rows and hit counters
- [x] **Plan view** with conflicts and caveats
- [x] **User and system pages** — version, update, beacon, roles
- [x] **Kvasir cards** on the plan, the goals, the characters and the
      artifacts, and a Kvasir page for the conversation. All of it invisible
      when the layer is switched off
- [x] **Artifact pictures on the inventory cards**, through the same cache as
      the character art and for the same reason: a page pulling two hundred
      icons from somebody else's server hands them the household's whole
      inventory, one request at a time
- [x] **An outage is no longer remembered as a missing picture.** The negative
      marker is permanent and was written on any failure, so one bad minute
      while a page was loading left the roster grey for good. Only a source
      that answered "no such image" is remembered as one
- [x] **Character art behind the roster cards** — the game's own namecard
      banner, fetched from Enka once and then served locally. The browser
      never talks to a third party: a page that loads eight pictures from
      somebody else tells them which characters this household plays
- [x] PWA manifest

### Deployment
- [x] Dockerfile (two stages, static binary, non-root)
- [x] Yggdrasil rune with a variable form

### Verified on a real account
8 characters, 8 weapons and 40 artifacts imported without a single warning. The
plan finds +23.8 % free on Raiden by rearranging Emblem — and says it costs
Xiangling her set. With the effect layer on, Raiden's baseline goes from 43,872
to 70,206 damage per rotation, and her stat sheet matches the game's own
numbers on all eight stats.

## Next

### 1. More effect rules
The DSL now covers sets, character passives, constellations, weapons, enemy
debuffs and effects with their own damage instance — 21 rules, all verified
against their own game text. The library still only covers the roster it has
been tested against; the rest are additions to `deploy/effects.json`, not code.

### 2. Reaction coefficients
They live in ability configs under `BinOutput`, not in a table. Until they are
mined, transformative reactions return an error naming what is missing. So
overload, hyperbloom and swirl cannot be calculated yet.

### 3. A per-run yield, so bills become resin
The bills are exact and each material's source is known, but turning "twelve
Philosophies of Light" into a number of resin needs the expected drops per
run, and nothing publishes it. The datamine has no drop table for domains and
the in-game reward preview lists the materials without quantities. Everything
downstream of that number is built and waiting: supply a measured yield and
the ascension and talent rows price themselves.

### 4. The optimiser's upper bound
The branch-and-bound's bound is admissible but weak — it adds the best value
of each stat across the remaining slots, describing a piece nobody owns — so
on a large inventory almost nothing is pruned. Restricting a four-piece search
by which slot is free cut the work enormously and is exact, but the bound
itself is untouched. Two things would fix it properly: a tighter bound, and a
stat block that is an array rather than a string-keyed map. A profile of a
roster-wide request spends about 45% of its time in map hashing alone.

### 5. The account plan needs to be a background job
It is sequential by construction: each goal claims gear and the next has to
see the claim. On an account with dozens of goals that is minutes, so it plans
the top few and says which ones it left out. The right answer is the pattern
the game data sync already uses — a job with progress — not a request that
has to finish before a proxy gives up.

### 6. ER calculator
Given a rotation and particle generation: how much Energy Recharge your Raiden
actually needs. The talent tables already carry the particle counts.

### 7. The proactive layer
A resin budget over 14 days with the domain rotation and weekly bosses. Push
via PWA + ntfy. A weekly report. Banner awareness. The HoYoLAB client and
`resin_snapshots` are there; the planner is not.

### 8. RAG over character guides
The `guides` table and its chunk table are empty: there is no ingestion yet, so
Kvasir answers purely from the calculation core's own numbers. That is the
honest order — a guide corpus is one more source to cite, not a prerequisite
for having an opinion about your own figures.

### 9. The training module
Quizzes on reaction formulas, rotation timing, ER requirements.

### 10. Joint optimisation across goals
The account plan runs goals by priority and lets the highest win the gear. That
beats showing both sides of a tug-of-war, but it is not a joint optimisation: a
goal is still measured against the gear the character has now, not against what
a higher-priority goal just took.

## Built last, as agreed

- [x] **Auto-updater** — version check, changelog and one click. Downloads,
      verifies the checksum, **starts the new binary and waits for a health
      check** before anything is replaced, and leaves behind a watchdog built
      from the known-good binary that rolls back if the new one does not come
      up after all. `mimir rollback` does the same by hand
- [x] **Deployment detection** — in a container it says honestly that an image
      cannot replace itself, and names the rune update. A locally built binary
      is never offered a release in its place
- [x] **Beacon** — one daily ping with an anonymous instance id and the
      version, nothing else, and the page shows the literal payload. Off until
      it is switched on, and off stays off: only an explicit "1" enables it
- [x] **No default collector** — borrowing Yggdrasil's address turned Mimir's
      first test ping into a phantom installation in Yggdrasil's count. The
      beacon now requires an address, and a failed ping is shown
- [x] **The collector side** — the same binary can receive. Off by default, and
      the endpoint answers 404 when it is. Stores only the instance id and the
      version, with a test that fails if the table gains another column. The
      cap on instances refuses only *new* ids, so a known installation never
      stops being counted

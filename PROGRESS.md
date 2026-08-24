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

### Data in
- [x] **Enka.Network** — client, TTL cache with labelled stale fallback,
      setId as the primary bridge, Traveler variants via the skill depot
- [x] **GOOD format** — parser, version validation, unit normalisation
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

### 3. Material accounting
Talent and ascension materials are mined per character, but not connected to
domains, weekdays and bosses. That is what makes `KindAscend` priced in resin
instead of blocked, and it is the prerequisite for the farming plan.

### 4. Talent and weapon domains
`DailyDungeonConfigData` has the weekdays, but the field names are obfuscated
and rotate between versions. Artifact domains are mined (they are open daily).

### 5. ER calculator
Given a rotation and particle generation: how much Energy Recharge your Raiden
actually needs. The talent tables already carry the particle counts.

### 6. The proactive layer
A resin budget over 14 days with the domain rotation and weekly bosses. Push
via PWA + ntfy. A weekly report. Banner awareness. The HoYoLAB client and
`resin_snapshots` are there; the planner is not.

### 7. RAG over character guides
The `guides` table and its chunk table are empty: there is no ingestion yet, so
Kvasir answers purely from the calculation core's own numbers. That is the
honest order — a guide corpus is one more source to cite, not a prerequisite
for having an opinion about your own figures.

### 8. The training module
Quizzes on reaction formulas, rotation timing, ER requirements.

### 9. Joint optimisation across goals
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

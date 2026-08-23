package i18n

// Danish.
//
// The keys are the English sentences as they appear in the source, so a change
// to a source string shows up here as a missing translation rather than as a
// silently stale one. Verbs and their order match the source; where Danish
// needs a different order, the translation uses explicit argument indexes.
var da = map[string]string{
	// ── Requests and authentication ─────────────────────────────────────
	"malformed request":             "ugyldig forespørgsel",
	"your session has expired":      "din session er udløbet",
	"Log in again.":                 "Log ind igen.",
	"requires administrator rights": "kræver administratorrettigheder",
	"could not send the response":   "kunne ikke sende svaret",

	// ── Preferences ─────────────────────────────────────────────────────
	"unknown theme":                   "ukendt tema",
	"Pick one of the seven elements.": "Vælg et af de syv elementer.",

	// ── Users ───────────────────────────────────────────────────────────
	"that would leave the instance with no administrators":                                         "det ville efterlade instansen uden administratorer",
	"Make somebody else an administrator first.":                                                   "Gør en anden til administrator først.",
	"the current password is wrong":                                                                "den nuværende adgangskode er forkert",
	"the password must be at least %d characters":                                                  "adgangskoden skal være mindst %d tegn",
	"the instance already has a user":                                                              "instansen har allerede en bruger",
	"Log in instead. If you have lost access, create a new user with `mimir useradd` on the host.": "Log ind i stedet. Har du mistet adgangen, skal en ny bruger oprettes med `mimir useradd` på værten.",

	// ── Accounts and import ─────────────────────────────────────────────
	"invalid UID": "ugyldigt UID",
	"The UID is at the bottom right in the game and is nine digits.":                                                      "UID'et står nederst til højre i spillet og er ni cifre.",
	"Enka has no characters for that UID":                                                                                 "Enka har ingen karakterer for det UID",
	"Switch on Show Character Details in the game under Profile → Edit Profile, wait a couple of minutes, and try again.": "Slå Vis karakterdetaljer til i spillet under Profil → Rediger profil, vent et par minutter, og prøv igen.",
	"that UID does not exist":                          "det UID findes ikke",
	"Check the nine digits in the game's Paimon menu.": "Tjek de ni cifre i spillets Paimon-menu.",
	"Enka has rate-limited us":                         "Enka har rate-limitet os",
	"Try again in a couple of minutes.":                "Prøv igen om et par minutter.",

	// ── Game data ───────────────────────────────────────────────────────
	"the game data has not been loaded yet":                                    "spildataene er ikke indlæst endnu",
	"Run a sync on the System page. Without it nothing can be calculated.":     "Kør en synkronisering på System-siden. Uden den kan der ikke regnes på noget.",
	"the game data is out of date":                                             "spildataene er forældede",
	"Something is missing from the active snapshot: %s. Sync a newer version.": "Der mangler noget i det aktive snapshot: %s. Synkronisér en nyere version.",
	"specify a game version":                                                   "angiv en spilversion",
	"For example 7.0.0. It labels the snapshot so you can roll back to it.":    "Fx 7.0.0. Den mærker snapshottet, så du kan rulle tilbage til det.",
	"a sync is already running":                                                "der kører allerede en synkronisering",

	// ── Goals and the plan ──────────────────────────────────────────────
	"the goal is missing a character": "målet mangler en karakter",
	"the goal is missing a rotation":  "målet mangler en rotation",
	"A ranking without a rotation is meaningless: a gain on a burst you never press is no gain.": "En rangering uden rotation er meningsløs: en gevinst på et burst du aldrig trykker, er ingen gevinst.",
	"Use one of the labels from the character's talent table.":                                   "Brug et af de labels der står i karakterens talenttabel.",
	"no goal has been set up for %s":                                                             "der er ikke sat et mål op for %s",
	"Create a goal with a rotation, and the plan can be calculated.":                             "Opret et mål med en rotation, så kan planen regnes ud.",
	"no goals have been set up":                                                                  "der er ikke sat nogen mål op",
	"Create at least one goal with a rotation, and the plan can be calculated.":                  "Opret mindst ét mål med en rotation, så kan planen regnes ud.",
	"none of the goals could be calculated":                                                      "ingen af målene kunne regnes ud",
	"the character is not on the account":                                                        "karakteren findes ikke på kontoen",
	"%s is not on the account":                                                                   "%s findes ikke på kontoen",
	"has no artifacts equipped":                                                                  "har ingen artifacts på",

	// ── Plan prose ──────────────────────────────────────────────────────
	"Switch to %s on %s":          "Skift til %s på %s",
	"Takes pieces from %s":        "Tager stykker fra %s",
	"Give %s the weapon %s (R%d)": "Giv %s våbnet %s (R%d)",
	"Currently on %s":             "Sidder på %s i dag",
	"Farm %s for %d days":         "Farm %s i %d dage",
	"Farm %s (%d 5★ pieces)":      "Farm %s (%d 5★-stykker)",
	"Priced in pieces, not resin: your drop rate is not measured.": "Prissat i stykker, ikke resin: din drop-rate er ikke målt.",
	"the resin cost of talent domains is not synced":               "resinprisen for talentdomæner er ikke synkroniseret",
	"requires a Crown of Insight":                                  "kræver en Crown of Insight",
	"normal attack":                                                "normalt angreb",
	"elemental skill":                                              "elemental skill",
	"elemental burst":                                              "elemental burst",
	"%s has priority %d, %s has %d":                                "%s har prioritet %d, %s har %d",
	"%s is using it, and has at least as high a priority":          "%s bruger det, og har mindst lige så høj prioritet",
	"Each goal is measured against the gear the character has now — not against what a higher-priority goal just claimed. Run the plan again once you have moved things around in the game.": "Hvert mål måles mod det udstyr karakteren har nu — ikke mod det, et højere prioriteret mål lige har taget. Kør planen igen, når du har flyttet tingene i spillet.",

	// ── Skipped reasons ─────────────────────────────────────────────────
	"could not search for a better combination of your artifacts: %v": "kunne ikke søge efter en bedre kombination af dine artifacts: %v",
	"could not price talent upgrades: %v":                             "kunne ikke prissætte talentopgraderinger: %v",
	"could not price the level upgrade: %v":                           "kunne ikke prissætte levelopgradering: %v",
	"could not be calculated: %v":                                     "kunne ikke regnes ud: %v",
	"the resin cost of ascension materials is not synced":             "resinprisen for ascension-materialer er ikke synkroniseret",
	"could not price weapon swaps: %v":                                "kunne ikke prissætte våbenskift: %v",
	"%s counts as switched off: set the condition %q on the goal":     "%s regnes som slukket: sæt betingelsen %q på målet",
	"artifact farming is not priced: the drop model is missing. Upload a .good file with your whole inventory, and it is measured on your own artifacts.": "artifact-farming er ikke prissat: drop-modellen mangler. Upload en .good-fil med hele dit inventar, så måles den på dine egne artifacts.",
	"artifact farming is not priced: the domains are not synced":                                                                                          "artifact-farming er ikke prissat: domænerne er ikke synkroniseret",
	"Upload a .good file from Inventory Kamera, and the model is measured on your whole inventory.":                                                       "Upload en .good-fil fra Inventory Kamera, så måles modellen på hele dit inventar.",

	// ── Drop model ──────────────────────────────────────────────────────
	"Measured on your own inventory, not on the game's drop tables.":                    "Målt på dit eget inventar, ikke på spillets drop-tabeller.",
	"The inventory is what you chose to keep, so good main stats are over-represented.": "Inventaret er hvad du har valgt at beholde, så gode main stats er overrepræsenteret.",
	"Only %d unupgraded pieces: the chance of four substats is not measured.":           "Kun %d uopgraderede stykker: chancen for fire substats er ikke målt.",

	// ── Authentication and accounts ─────────────────────────────────────
	"wrong username or password": "forkert brugernavn eller adgangskode",
	"the account does not exist": "kontoen findes ikke",
	"Import from Enka or upload a .good file, and equip the character in the game.": "Importér fra Enka eller upload en .good-fil, og udstyr karakteren i spillet.",

	// ── Updates and beacon ──────────────────────────────────────────────
	"updates are not available":                                   "opdateringer er ikke tilgængelige",
	"Restart Mimir to run the restored version.":                  "Genstart Mimir for at køre den gendannede version.",
	"the beacon is not available":                                 "beacon er ikke tilgængelig",
	"Set a collector address, and the beacon can be switched on.": "Sæt en collector-adresse, så kan beaconen slås til.",
	"the collector has reached its instance limit":                "collectoren har nået sin grænse for antal instanser",

	// ── Self-update ─────────────────────────────────────────────────────
	"Mimir runs in a container, and a container cannot replace its own image. Update the rune in Yggdrasil: that pulls the new image and recreates the container.": "Mimir kører i en container, og en container kan ikke udskifte sit eget image. Opdatér runen i Yggdrasil: så hentes det nye image og containeren genskabes.",
	"This binary was built locally (%s), not from a release. Updating would throw away a build somebody made on purpose.":                                          "Denne binær er bygget lokalt (%s), ikke fra en release. En opdatering ville smide en build væk, som nogen har lavet med vilje.",
	"The release has no binary for %s.":      "Udgivelsen har ingen binær til %s.",
	"selfupdate: could not reach GitHub: %s": "selfupdate: kunne ikke nå GitHub: %s",
	"selfupdate: GitHub returned %s":         "selfupdate: GitHub svarede %s",
	"selfupdate: found no releases in %s. Either there are none yet, or the repository is private — Mimir fetches without credentials, so a private repository's releases are invisible to it": "selfupdate: fandt ingen udgivelser i %s. Enten er der ingen endnu, eller også er repoet privat — Mimir henter uden login, så et privat repos udgivelser er usynlige for den",

	// ── Validation ──────────────────────────────────────────────────────
	"malformed payload":   "ugyldig payload",
	"username is missing": "brugernavn mangler",
	"login failed":        "login mislykkedes",
	"invalid account id":  "ugyldigt account-id",
	"Export a .good file from Inventory Kamera or Genshin Optimizer.": "Eksportér en .good-fil fra Inventory Kamera eller Genshin Optimizer.",
	"that username is taken": "brugernavnet er taget",
	"invalid user id":        "ugyldigt bruger-id",
	"unknown role":           "ukendt rolle",

	// ── Kvasir, the AI layer ────────────────────────────────────────────
	//
	// The fact sheets are here too. They are not only the model's input: the
	// player is shown the sheet behind any answer, and evidence they cannot
	// read is not evidence.
	"no language model is configured": "der er ikke sat en sprogmodel op",
	"Set MIMIR_LLM_BASE_URL to an OpenAI-compatible endpoint. Everything else in Mimir works without one.": "Sæt MIMIR_LLM_BASE_URL til et OpenAI-kompatibelt endepunkt. Alt andet i Mimir virker uden.",
	"there is nothing calculated to have an opinion about":                                                 "der er ikke regnet noget ud at have en mening om",
	"Import an account and set up a goal, and the engine has something to hand over.":                      "Importér en konto og sæt et mål op, så har beregningskernen noget at udlevere.",
	"there is no question to answer":                                                                       "der er ikke stillet noget spørgsmål",
	"Kvasir used numbers that are not in the calculation, twice in a row":                                  "Kvasir brugte tal, der ikke står i beregningen, to gange i træk",
	"The answer was discarded rather than shown. A smaller or better-instructed model usually fixes this.": "Svaret blev kasseret i stedet for vist. En mindre eller bedre instrueret model plejer at løse det.",
	"the model did not answer in time":                                                                     "modellen svarede ikke i tide",
	"A local model on a small machine can take longer than Mimir waits.":                                   "En lokal model på en lille maskine kan tage længere tid, end Mimir venter.",
	"there is no such tool":                  "det værktøj findes ikke",
	"that needs a character":                 "det kræver en karakter",
	"%s's talent table":                      "%ss talenttabel",
	"%s — %s, at level %d":                   "%s — %s, på niveau %d",
	"no artifacts have been imported yet":    "der er ikke importeret nogen artifacts endnu",
	"The artifact inventory on account %s":   "Artifact-inventaret på konto %s",
	"nothing in the inventory matches that":  "intet i inventaret matcher det",
	"Kvasir has no fact sheet for that page": "Kvasir har ikke et faktaark til den side",
	"no characters have been imported yet":   "der er ikke importeret nogen karakterer endnu",

	// The plan's fact sheet
	"The resin plan for account %s": "Resin-planen for konto %s",
	"This is the ranked plan the player is looking at. What should they do first, what does the ranking not make obvious, and what is holding this account back?": "Det her er den rangerede plan, spilleren kigger på. Hvad skal de gøre først, hvad gør rangeringen ikke tydeligt, og hvad holder kontoen tilbage?",
	"How these numbers were measured": "Sådan er tallene målt",
	"Every gain is the change in that goal's whole rotation damage, calculated on the gear this account actually owns.": "Hver gevinst er ændringen i målets samlede rotationsskade, regnet på det udstyr kontoen rent faktisk ejer.",
	"Free actions rank above paid ones. An action that cannot be carried out today ranks last, however large it looks.": "Gratis handlinger rangerer over dem, der koster. En handling, der ikke kan udføres i dag, rangerer sidst, uanset hvor stor den ser ud.",
	"Efficiency is the gain per 100 resin. A day is 180 resin.":                                                         "Effektivitet er gevinsten pr. 100 resin. En dag er 180 resin.",
	"The goals being optimised":                                 "Målene der optimeres",
	"%s: baseline %s damage per rotation, %d upgrades found":    "%s: baseline %s skade pr. rotation, %d opgraderinger fundet",
	"The ranked plan":                                           "Den rangerede plan",
	"…and %d smaller actions below these.":                      "…og %d mindre handlinger under de her.",
	"Nothing. Every goal is already the best this gear allows.": "Ingenting. Hvert mål er allerede det bedste, udstyret tillader.",
	"Gear two goals both want":                                  "Udstyr som to mål begge vil have",
	"%s wants %s from %s — %s":                                  "%s vil have %s fra %s — %s",
	"What the engine refused to price":                          "Hvad beregningskernen nægtede at prissætte",
	"free":                                                      "gratis",
	"not priced in resin":                                       "ikke prissat i resin",
	"%s resin":                                                  "%s resin",
	"%s per 100 resin":                                          "%s pr. 100 resin",
	"blocked: %s":                                               "blokeret: %s",
	"median %s, p10 %s, p90 %s, and %s of simulated runs changed nothing": "median %s, p10 %s, p90 %s, og %s af de simulerede ture ændrede ingenting",

	// One goal
	"%s as a goal": "%s som mål",
	"How does this player make %s hit harder? Weigh what the ranking costs elsewhere, and say what is missing from the goal itself.": "Hvordan får spilleren %s til at ramme hårdere? Vej hvad rangeringen koster andre steder, og sig hvad der mangler i selve målet.",
	"The goal": "Målet",
	"Priority %d among this account's goals.":                 "Prioritet %d blandt kontoens mål.",
	"Baseline: %s damage per rotation.":                       "Baseline: %s skade pr. rotation.",
	"Measured against a level %d enemy.":                      "Målt mod en fjende på niveau %d.",
	"Rotation: %s":                                            "Rotation: %s",
	"Step %d: %s %s ×%d":                                      "Trin %d: %s %s ×%d",
	", amplified by %s":                                       ", forstærket af %s",
	"Declared condition: %s = %s":                             "Angivet betingelse: %s = %s",
	"Ranked upgrades for this goal":                           "Rangerede opgraderinger for det her mål",
	"None. This build is the best the account's gear allows.": "Ingen. Den her build er det bedste, kontoens udstyr tillader.",

	// One build
	"%s's build": "%ss build",
	"What is wrong with this build, and what is the cheapest thing that would fix it? Say what the stats show, not what is usually recommended.": "Hvad er der galt med den her build, og hvad er det billigste, der ville rette det? Sig hvad statterne viser, ikke hvad man plejer at anbefale.",
	"No goal": "Intet mål",
	"This character has no goal, so nothing has been ranked for them: Mimir measures a gain against a rotation, and there is none to measure against.": "Karakteren har intet mål, så der er ikke rangeret noget for den: Mimir måler en gevinst mod en rotation, og der er ingen at måle mod.",
	"The character":                   "Karakteren",
	"%s, level %d, constellation %d.": "%s, niveau %d, constellation %d.",
	"Talent levels: normal attack %d, elemental skill %d, elemental burst %d.":         "Talentniveauer: normalt angreb %d, elemental skill %d, elemental burst %d.",
	"Weapon: %s, level %d, refinement %d.":                                             "Våben: %s, niveau %d, refinement %d.",
	"No weapon is equipped.":                                                           "Der er ikke udstyret et våben.",
	"Equipped artifacts":                                                               "Udstyrede artifacts",
	"%s %s +%d, main stat %s%s":                                                        "%s %s +%d, main stat %s%s",
	"Nothing is equipped, so there is no build to resolve.":                            "Der er ikke udstyret noget, så der er ingen build at opløse.",
	"The build could not be resolved: %v":                                              "Builden kunne ikke opløses: %v",
	"Set bonus in effect: %d pieces of %s.":                                            "Sætbonus i kraft: %d stykker %s.",
	"Resolved stats, everything included":                                              "Opløste statter, alt indregnet",
	"What the conditional layer contributed, and the game text it was checked against": "Hvad effekt-laget bidrog med, og den spiltekst det blev tjekket imod",
	"Damage the gear adds by itself":                                                   "Skade udstyret tilføjer af sig selv",
	"%s adds its own hit at %s scaling.":                                               "%s lander sit eget hit med %s scaling.",
	"Conditions nobody has answered":                                                   "Betingelser ingen har svaret på",
	"These are switched off in every number above. They are not absent bonuses; they are bonuses nobody has been asked about.": "De er slået fra i alle tallene ovenfor. Det er ikke bonusser, der ikke findes; det er bonusser, ingen er blevet spurgt om.",
	", up to %s": ", op til %s",

	// The roster
	"The roster on account %s": "Rosteren på konto %s",
	"Who is worth investing in next, and who is being carried by gear they should not have? Only judge what is listed here.": "Hvem er værd at investere i som den næste, og hvem bliver båret af udstyr, de ikke burde have? Døm kun på det, der står her.",
	"Every character on the account":                             "Alle karakterer på kontoen",
	"%s: level %d, C%d, talents %d/%d/%d, %d artifacts equipped": "%s: niveau %d, C%d, talenter %d/%d/%d, %d artifacts udstyret",
	", holding %s R%d":                   ", har %s R%d",
	", no weapon":                        ", intet våben",
	", has a goal":                       ", har et mål",
	", no goal set up":                   ", intet mål sat op",
	"What Mimir can and cannot say here": "Hvad Mimir kan og ikke kan sige her",
	"Nothing on this page has been through the damage engine: a character with no goal has no rotation, and without a rotation there is no number. Say what is worth setting up as a goal rather than claiming a gain.": "Intet på den her side har været igennem beregningskernen: en karakter uden mål har ingen rotation, og uden en rotation er der ingen tal. Sig hvad der er værd at sætte op som mål frem for at påstå en gevinst.",

	// The inventory
	"What should this player do with this inventory — what is worth levelling, what is dead weight, and which domain is worth a week? Do not claim a gain the engine has not measured.": "Hvad skal spilleren gøre med det her inventar — hvad er værd at levele, hvad er dødvægt, og hvilket domæne er en uge værd? Påstå ikke en gevinst, beregningskernen ikke har målt.",
	"The drop model measured from this inventory":                                "Drop-modellen målt på det her inventar",
	"There is no measured drop model: %v":                                        "Der er ingen målt drop-model: %v",
	"Without one, farming is ranked in artifacts examined rather than in resin.": "Uden en bliver farming rangeret i undersøgte artifacts frem for i resin.",
	"Measured from %d five-star artifacts.":                                      "Målt på %d femstjernede artifacts.",
	"Runs can be priced in resin: %s pieces per run.":                            "Ture kan prissættes i resin: %s stykker pr. tur.",
	"The per-run yield is unknown, so farming cannot be priced in resin. An inventory records what dropped, never how many runs it took.": "Udbyttet pr. tur er ukendt, så farming kan ikke prissættes i resin. Et inventar viser hvad der droppede, aldrig hvor mange ture det tog.",
	"The inventory": "Inventaret",
	"%d artifacts, %d of them equipped on somebody.": "%d artifacts, %d af dem udstyret på nogen.",
	"%s: %d pieces":                           "%s: %d stykker",
	"By set, deepest first":                   "Efter sæt, dybest først",
	"…and %d further sets with less in them.": "…og %d andre sæt med færre i.",
	"%s: %d pieces, %d of them five-star, %d at +20, %d equipped, best crit value %s":                                     "%s: %d stykker, %d af dem femstjernede, %d på +20, %d udstyret, bedste crit value %s",
	"The best pieces nobody is wearing":                                                                                   "De bedste stykker, ingen har på",
	"Crit value is 2×crit rate + crit damage. It is triage, not a verdict — the optimizer decides what is actually worn.": "Crit value er 2×crit rate + crit damage. Det er triage, ikke en dom — optimizeren afgør, hvad der faktisk bæres.",
	"%s %s +%d, main stat %s, crit value %s%s":                                                                            "%s %s +%d, main stat %s, crit value %s%s",

	// The goals page
	"The goals on account %s": "Målene på konto %s",
	"Are these goals set up so the ranking can be trusted? Name what is missing — an unanswered condition, a rotation that does not match how the character is played, a priority order that fights itself.": "Er målene sat op, så rangeringen kan stoles på? Nævn hvad der mangler — en ubesvaret betingelse, en rotation der ikke passer til, hvordan karakteren spilles, en prioritetsrækkefølge der slås med sig selv.",
	"Goals, highest priority first":                                          "Mål, højeste prioritet først",
	"%s: priority %d, rotation %q with %d steps, enemy level %d":             "%s: prioritet %d, rotation %q med %d trin, fjendeniveau %d",
	"    %s step %d: %s %s ×%d":                                              "    %s trin %d: %s %s ×%d",
	"    %s declared condition: %s = %s":                                     "    %s angivet betingelse: %s = %s",
	"    %s has not been asked: %s (%s)":                                     "    %s er ikke blevet spurgt: %s (%s)",
	"No goals have been set up, so nothing on this account has been ranked.": "Der er ikke sat mål op, så intet på kontoen er rangeret.",
	"Characters with no goal":                                                "Karakterer uden mål",
	"%s, level %d, C%d":                                                      "%s, niveau %d, C%d",

	// The account
	"The account": "Kontoen",
	"%d characters, %d weapons and %d artifacts have been imported.": "%d karakterer, %d våben og %d artifacts er importeret.",
	"Adventure rank %d, world level %d.":                             "Adventure rank %d, world level %d.",
	"The inventory came from Enka, which only reports equipped pieces. Everything unequipped is invisible here — a .good file would change that.": "Inventaret kom fra Enka, som kun rapporterer udstyrede stykker. Alt uudstyret er usynligt her — en .good-fil ville ændre det.",
}

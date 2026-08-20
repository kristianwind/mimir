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
}

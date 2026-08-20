/**
 * Translation.
 *
 * English is the source language and doubles as the lookup key. That choice is
 * deliberate: a missing translation then degrades to a readable English
 * sentence rather than to a bare identifier like `plan.baseline`, and a string
 * that is never translated is still a string the user can act on.
 *
 * Danish was historically the only language here, so the Danish table below is
 * a translation *of* the source rather than the other way round.
 */

export const LANGUAGES = [
  { key: 'da', label: 'Dansk' },
  { key: 'en', label: 'English' },
]

/** Danish. A key absent here falls through to its English source. */
const da = {
  // ── Theme picker ────────────────────────────────────────────────────────
  Light: 'Lys',
  Dark: 'Mørk',
  System: 'System',
  'Element theme': 'Elementtema',
  'Light mode': 'Lystilstand',
  Language: 'Sprog',

  // ── Shell ───────────────────────────────────────────────────────────────
  Plan: 'Plan',
  'What should you spend resin on?': 'Hvad skal du bruge resin på?',
  Goals: 'Mål',
  'Who are you building, and how do you play them?':
    'Hvem bygger du på, og hvordan spiller du dem?',
  Characters: 'Karakterer',
  'Your roster': 'Din roster',
  Artifacts: 'Artifacts',
  'The whole inventory': 'Hele inventaret',
  Accounts: 'Konti',
  'UID and import': 'UID og import',
  'Version, updates and beacon': 'Version, opdatering og beacon',
  Users: 'Brugere',
  'Accounts, roles and passwords': 'Konti, roller og adgangskoder',
  'Game data is missing. Run a sync, or nothing can be calculated.':
    'Spildata mangler. Kør en sync, ellers kan der ikke regnes.',
  'Log out': 'Log ud',
  'Add your UID under Accounts to get started.':
    'Tilføj dit UID under Konti for at komme i gang.',
  'Add an account': 'Tilføj konto',

  // ── Login ───────────────────────────────────────────────────────────────
  'The adviser at the well': 'Rådgiveren ved brønden',
  'The two passwords do not match.': 'De to adgangskoder er ikke ens.',
  'The password must be at least 12 characters.':
    'Adgangskoden skal være mindst 12 tegn.',
  'Create the first administrator': 'Opret den første administrator',
  'This instance is empty. Mimir has no default account — this form only appears until the first one exists, and disappears by itself afterwards.':
    'Instansen er tom. Mimir har ingen standardbruger — den her form vises kun indtil den første konto findes, og forsvinder derefter af sig selv.',
  Username: 'Brugernavn',
  Password: 'Adgangskode',
  'At least 12 characters.': 'Mindst 12 tegn.',
  'Repeat the password': 'Gentag adgangskoden',
  'Creating…': 'Opretter…',
  'Logging in…': 'Logger ind…',
  'Create and log in': 'Opret og log ind',
  'Log in': 'Log ind',

  // ── Plan ────────────────────────────────────────────────────────────────
  Rearrange: 'Omrokering',
  Weapon: 'Våben',
  Talent: 'Talent',
  Level: 'Level',
  Farm: 'Farm',
  'Calculating…': 'Regner…',
  'The plan ranks every possible upgrade across your whole account by expected damage gained per resin — the free rearrangements first, then talents, levels and artifact domains. It needs at least one goal: which character, and which rotation the gain is measured on.':
    'Planen rangerer hver mulig opgradering på tværs af hele din konto efter forventet skadesgevinst pr. resin — de gratis omrokeringer først, derefter talenter, niveauer og artifact-domæner. Den kræver mindst ét mål: hvilken karakter, og hvilken rotation gevinsten skal måles på.',
  'Create a goal': 'Opret et mål',
  'All goals': 'Alle mål',
  'No upgrades found — your builds are already the best your gear allows.':
    'Ingen opgraderinger fundet — dine builds er allerede det bedste, dine ting kan give.',
  '{n} things you can do now': '{n} ting du kan gøre nu',
  ', {n} blocked': ', {n} blokeret',
  'Blocked: {what}': 'Blokeret: {what}',
  'median {median} · spread {low}–{high} · gives nothing {none} of the time':
    'median {median} · spænd {low}–{high} · giver intet {none} af gangene',
  'The fight over the gear': 'Kampen om udstyret',
  '{wants} wants {item} from {holds} — {resolution}':
    '{wants} vil have {item} fra {holds} — {resolution}',
  Caveats: 'Forbehold',
  free: 'gratis',
  'not priced': 'ikke prissat',
  '{n} resin': '{n} resin',

  // ── Goals ───────────────────────────────────────────────────────────────
  'Normal attack': 'Normalt angreb',
  'Elemental skill': 'Elemental skill',
  'Elemental burst': 'Elemental burst',
  'Pick at least one attack, otherwise there is nothing to measure the gain on.':
    'Vælg mindst ét angreb, ellers er der ikke noget at måle gevinsten på.',
  'Rotation for {name}': 'Rotation for {name}',
  Cancel: 'Annullér',
  'Pick which attacks make up one rotation, and how many times each of them hits. The numbers are your actual talent levels. The rotation is what gains are measured on — a burst you never use is worth no gain at all.':
    'Vælg hvilke angreb der indgår i én rotation, og hvor mange gange hvert af dem rammer. Tallene er dine faktiske talentniveauer. Rotationen er det gevinsterne måles på — et burst du aldrig bruger, er ingen gevinst værd.',
  'Fetching talent table…': 'Henter talenttabel…',
  '· level {n}': '· niveau {n}',
  '({base} + {extra} from constellation)': '({base} + {extra} fra constellation)',
  Fewer: 'Færre',
  More: 'Flere',
  Conditions: 'Betingelser',
  'Some bonuses — from sets, constellations and weapons — depend on how you play, and some weapons and constellations land their own extra hit. Mimir does not guess: a bonus switched off because nobody asked looks exactly like a bonus that does not exist. Set 0 if you never have it up.':
    'Nogle bonusser — fra sæt, constellations og våben — afhænger af hvordan du spiller, og nogle våben og constellations lander deres eget ekstra hit. Mimir gætter ikke: en bonus der er slukket fordi ingen spurgte, ser i en rangering præcis ud som en bonus der ikke findes. Sæt 0, hvis du aldrig har den oppe.',
  'of {n}': 'af {n}',
  'Rotation length (sec.)': 'Rotationens længde (sek.)',
  Priority: 'Prioritet',
  'Saving…': 'Gemmer…',
  'Save goal': 'Gem mål',
  '{hits} attacks over {duration} sec. · priority {priority}':
    '{hits} angreb over {duration} sek. · prioritet {priority}',
  Edit: 'Redigér',
  Delete: 'Slet',
  'No goals yet. Pick a character below.': 'Ingen mål endnu. Vælg en karakter nedenfor.',
  'Add a goal': 'Tilføj et mål',

  // ── Accounts ────────────────────────────────────────────────────────────
  'Genshin UID': 'Genshin UID',
  Add: 'Tilføj',
  "The UID is at the bottom right in the game. Enka fetches your showcase characters without a login — remember to switch on <em>Show Character Details</em> in your profile.":
    "UID'et står nederst til højre i spillet. Enka henter dine showcase-karakterer uden login — husk at slå <em>Vis karakterdetaljer</em> til i din profil.",
  'Import from {source} for {account}': 'Import fra {source} for {account}',
  '{characters} characters · {inserted} new artifacts, {upgraded} upgraded, {unchanged} unchanged':
    '{characters} karakterer · {inserted} nye artifacts, {upgraded} opgraderede, {unchanged} uændrede',
  '· data is from the cache': '· data er fra cachen',
  Unnamed: 'Uden navn',
  'Fetching…': 'Henter…',
  'Fetch from Enka': 'Hent fra Enka',
  'Importing…': 'Importerer…',
  'Upload .good': 'Upload .good',
  'No accounts yet.': 'Ingen konti endnu.',

  // ── Artifact slots ──────────────────────────────────────────────────────
  Flower: 'Blomst',
  Plume: 'Fjer',
  Sands: 'Sand',
  Goblet: 'Bæger',
  Circlet: 'Krone',

  // ── System ──────────────────────────────────────────────────────────────
  Version: 'Version',
  'Could not check for updates: {error}':
    'Kunne ikke tjekke for opdateringer: {error}',
  '{version} has been released.': '{version} er udkommet.',
  'Mimir downloads the binary, verifies its checksum, <em>starts it</em> and waits for it to answer a health check — only then is anything replaced. If the new version still fails to come up afterwards, a watchdog rolls back to {version}.':
    'Mimir henter binæren, tjekker dens checksum, <em>starter den</em> og venter på at den svarer på et helbredstjek — først derefter udskiftes noget. Kommer den nye version alligevel ikke op bagefter, ruller en vagthund tilbage til {version}.',
  'Updated to {to}. {note}': 'Opdateret til {to}. {note}',
  'Updating…': 'Opdaterer…',
  'Update to {version}': 'Opdatér til {version}',
  'You are running the newest version ({version}).':
    'Du kører den nyeste version ({version}).',
  'No newer version found.': 'Ingen nyere version fundet.',
  'Checking…': 'Tjekker…',
  'Check now': 'Tjek nu',
  'Restored {restored}. {note}': 'Gendannede {restored}. {note}',
  'Roll back to {version}': 'Rul tilbage til {version}',
  'Last updated to {version} on {date}.':
    'Sidst opdateret til {version} den {date}.',
  'Game data': 'Spildata',
  'Fetches from the public datamines, verifies the effect rules against the game’s own wording and activates the result. Takes about half a minute. If anything fails, nothing is swapped — the current snapshot stays.':
    'Henter fra de offentlige datamines, verificerer effekt-reglerne mod spillets egen ordlyd og aktiverer resultatet. Tager omkring et halvt minut. Fejler noget, skiftes der ikke — det nuværende snapshot bliver stående.',
  'Game version': 'Spilversion',
  'Syncing… {n}s': 'Synkroniserer… {n}s',
  'Sync game data': 'Synkronisér spildata',
  Beacon: 'Beacon',
  on: 'til',
  off: 'fra',
  'One ping a day, so the project can see how many installations exist and which version they run. It sends exactly this and nothing else — no UIDs, no accounts, no inventory:':
    'Én ping om dagen, så projektet kan se hvor mange installationer der findes og hvilken version de kører. Den sender præcis dette og intet andet — ingen UID’er, ingen konti, intet inventar:',
  'Collector address': 'Collector-adresse',
  'There is deliberately no default address. A beacon has to know where it reports — otherwise the ping either goes nowhere or somewhere it does not belong.':
    'Der er ingen standardadresse med vilje. En beacon skal vide hvor den rapporterer hen — ellers ender pingen enten ingen steder eller et sted den ikke hører til.',
  'It is switched off until you say otherwise.': 'Den er slået fra, indtil du siger andet.',
  'Turn off': 'Slå fra',
  'Turn on': 'Slå til',
  'The last attempt failed: {error}': 'Sidste forsøg mislykkedes: {error}',
  'Last sent {day} as {version}.': 'Sidst sendt {day} som {version}.',
  'This instance as collector': 'Denne instans som collector',
  'One instance can receive the others’ beacons. Switch it on here, and point the other installations at the address below. Only the anonymous instance id and the version are stored — no IP, no user agent, no request data. The sender promises its operator that nothing else leaves the machine, and that promise has to hold at this end too.':
    'Én instans kan tage imod de andres beacons. Slå det til her, og peg de øvrige installationer på adressen nedenfor. Der gemmes kun det anonyme instans-id og versionen — ingen IP, ingen brugeragent, ingen forespørgselsdata. Afsenderen lover sin operatør at intet andet forlader maskinen, og det løfte skal også holde i denne ende.',
  'Address for the other instances': 'Adresse til de andre instanser',
  'The endpoint answers 404 until you switch it on — an instance that is not a collector should not advertise something it rejects anyway.':
    'Endepunktet svarer 404 indtil du slår det til — en instans der ikke er collector, skal ikke reklamere for noget den alligevel afviser.',
  installations: 'installationer',
  'active 7 days': 'aktive 7 dage',
  'active 30 days': 'aktive 30 dage',
  'Turn collector off': 'Slå collector fra',
  'Turn collector on': 'Slå collector til',

  // ── Users ───────────────────────────────────────────────────────────────
  'Created {name}.': 'Oprettede {name}.',
  '· you': '· dig',
  administrator: 'administrator',
  user: 'bruger',
  disabled: 'deaktiveret',
  '· {accounts} accounts · {sessions} active logins':
    '· {accounts} konti · {sessions} aktive logins',
  'last administrator': 'sidste administrator',
  'Make user': 'Gør til bruger',
  'Make admin': 'Gør til admin',
  Enable: 'Aktivér',
  Disable: 'Deaktivér',
  'Delete {name}? That takes {accounts} game accounts and all their inventory with it.':
    'Slet {name}? Det tager {accounts} spilkonti med alt inventar med sig.',
  'Delete {name}?': 'Slet {name}?',
  'No users.': 'Ingen brugere.',
  'Add a user': 'Tilføj en bruger',
  'Every user has their own game accounts and goals. An administrator can also update Mimir and manage users.':
    'Hver bruger har sine egne spilkonti og mål. En administrator kan desuden opdatere Mimir og styre brugere.',
  Role: 'Rolle',
  Create: 'Opret',
  'Change your own password': 'Skift din egen adgangskode',
  'The current password is required — otherwise a borrowed session could be made permanent. Your other logins are signed out.':
    'Den nuværende kode skal med — ellers kunne en lånt session gøres permanent. Dine øvrige logins bliver logget ud.',
  Current: 'Nuværende',
  New: 'Ny',
  'The password has been changed.': 'Adgangskoden er skiftet.',
  'Changing…': 'Skifter…',
  Change: 'Skift',

  // ── Characters ──────────────────────────────────────────────────────────
  'Fetching characters…': 'Henter karakterer…',
  'No characters yet. Fetch from Enka or upload a .good file.':
    'Ingen karakterer endnu. Hent fra Enka eller upload en .good-fil.',
  'Level {n}': 'Niveau {n}',
  'lvl {n}': 'lvl {n}',

  // ── Artifacts ───────────────────────────────────────────────────────────
  'All ({n})': 'Alle ({n})',
  'Fetching artifacts…': 'Henter artifacts…',
  'No inventory yet.': 'Intet inventar endnu.',
  'Enka only gives the equipped pieces. Upload a .good file from Inventory Kamera for the whole inventory.':
    'Enka giver kun de udstyrede stykker. Upload en .good-fil fra Inventory Kamera for hele inventaret.',
  'Showing 120 of {n}. Filtering and sorting arrive with the optimizer view.':
    'Viser 120 af {n}. Filtrering og sortering kommer med optimizer-visningen.',

  // ── Client-side failures ────────────────────────────────────────────────
  'unexpected response from the server': 'uventet svar fra serveren',

  // ── Loading ─────────────────────────────────────────────────────────────
  'Loading…': 'Henter…',
}

export const dictionaries = { da, en: {} }

/**
 * translate renders `source` in `lang`, substituting `{name}` placeholders.
 *
 * Placeholders rather than string concatenation, because word order is part of
 * what differs between languages — "{n} resin" and "Blocked: {what}" have to be
 * free to move their pieces around.
 */
export function translate(lang, source, vars) {
  let s = dictionaries[lang]?.[source] ?? source
  if (vars) {
    for (const key of Object.keys(vars)) {
      s = s.split('{' + key + '}').join(String(vars[key]))
    }
  }
  return s
}

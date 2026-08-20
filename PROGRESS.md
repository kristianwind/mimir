# Progress

Planen — rangeringen af hver opgradering efter gevinst pr. resin — er den
del ingen andre værktøjer laver, og den virker nu på rigtige data. Resten
herunder er det der gør den mere præcis, ikke det der får den til at virke.

## Færdigt

### Fundament
- [x] Go-modul, pakkestruktur og afhængigheder matcher Yggdrasil-konventionerne
- [x] SQLite-skema: brugere, sessioner, konti, inventar, mål, plan,
      spildata-snapshots, guide-korpus, audit
- [x] argon2id-login, opake sessionstokens (kun gemt som SHA-256), middleware
- [x] Konfiguration via miljøvariabler, maskinehemmelighed ved første boot
- [x] HTTP-server med chi, embedded frontend, graceful shutdown

### Spildata-mineren
- [x] **Numeriske tabeller** fra `DimbreathBot/AnimeGameData`, nøglet på id
- [x] **Navne** fra Enka-store (karakterer) og genshin-db (våben, sæt,
      domæner) — nøglet på de samme id'er. TextMap bruges ikke: hashene i
      det aktuelle mirror resolver 0 ud af 165. Se [docs/GAMEDATA.md](docs/GAMEDATA.md)
- [x] **Talenttabeller** med labels, enheder og scaling-stat pr. parameter
- [x] **Artifact-domæner** med hvilke sæt de dropper
- [x] Diskcache, validering der afviser ufuldstændige snapshots ved navn,
      gzippet lagring i SQLite, atomisk aktivering og rollback
- [x] Verificeret mod kendte tal: 4780 HP-blomst, 62,2 % CD-krone,
      1446,8535 level-multiplier, Emblem 2pc = 20 % ER

### Beregning
- [x] **Damage engine** — formler uden spilkonstanter: DEF-, RES- og
      crit-led, EM-kurver, rotationsevaluering
- [x] **Artifact-optimizer** — branch-and-bound med admissible upper bound
      og et validitetsprædikat for sæt-krav. Testet mod brute force
- [x] **Sæt-konfigurationer** — 4pc og 2+2 opregnet fra hvad du ejer,
      søgt hver for sig så bonusserne ikke bryder grænsen
- [x] **Farm-simulator** — Monte Carlo over drop-fordelingen; middel,
      median, p10, p90 og chancen for at turen gav nul
- [x] **Drop-model målt på dit eget inventar** frem for påståede drop-rates,
      med biasen skrevet ud
- [x] **Rotationer** bygget af mined talent-rækker, valideret mod labels

### Planen
- [x] Kandidater: gratis omrokering, våbenskift, talent +1, level/ascension,
      artifact-farming
- [x] Rangering på gevinst pr. resin — gratis først, blokerede sidst
- [x] **Udstyrskonflikter** navngivet: "tager stykker fra Xiangling"
- [x] **Kontoplan** på tværs af mål med prioritetsopløsning
- [x] Alt der ikke kan prissættes står i `skipped` med en begrundelse

### Data ind
- [x] **Enka.Network** — klient, TTL-cache med mærket stale-fallback,
      setId som primær bro, Traveler-varianter via skill-depot
- [x] **GOOD-format** — parser, versionsvalidering, enhedsnormalisering
- [x] **Artifact-matching** — fingerprint + identity, så gen-import bliver
      `{nye, opgraderede, uændrede}`
- [x] **HoYoLAB** — DS-signatur, Real-Time Notes, retcodes oversat
- [x] **Hemmeligheder** — AES-256-GCM for HoYoLAB-cookies

### Effekt-laget
- [x] **Deklarativ DSL** for konverteringer og betingede bonusser — data,
      ikke kode. To faser, så en konvertering læser de færdige totaler
- [x] **Citatkrav**: hver regel peger på den spiltekst den kommer fra, og
      loaderen tjekker at tallene faktisk står der. 25 % mod en tekst der
      siger 20 % loader ikke
- [x] **Angrebskategorier** — Raidens Musou Isshin-sværdslag er normale
      angreb og får ikke burst-bonus. Både DMG-bonus og crit kan bindes til
      en kategori, så The Catch kun løfter burst-crit
- [x] **Våbenpassiver med refinement** — alle fem ordlyde mines, og hver
      værdi tjekkes mod netop sin refinements tekst. 227 af 237 våben
- [x] **Constellations** — både stat-effekter og de +3 talentniveauer, hvor
      hvilken talent der rammes udledes af teksten (113 af 117 karakterer)
      og krydstjekkes mod spillets egne tal ved hver Enka-import
- [x] **Fjende-debuffs** — Viridescents RES-shred og Raidens C2 DEF-ignore
      kan udtrykkes, hvilket de ikke kunne før
- [x] **Egne skadesinstanser** — procs og eksplosioner der lander deres eget
      hit (Prototype Archaic, Noelle C4, Xiangling C2), hvor antallet
      deklareres og foldes ind i multiplikatoren
- [x] 21 regler i alt: sæt, karakterpassiver, constellations, våben,
      fjende-debuffs og procs
- [x] **Betingelser spørges, ikke gættes** — og en betingelse ingen har
      svaret på, står i planen frem for at være stille slukket
- [x] **Sporbarhed**: hvert effekt-tal på build-arket bærer sin kilde og
      sit citat
- [x] Verificeret mod spillets egne tal: Raidens HP, ATK, DEF, crit rate,
      crit damage, ER og Electro DMG matcher Enkas fightPropMap

### Brugerstyring
- [x] **Førstegangs-flow** — loginsiden opretter den første administrator når
      instansen er tom, og vinduet lukker sig selv i samme øjeblik den
      første konto findes. Vagten ligger i indsætningen, ikke omkring den
- [x] **Roller** — administratorer kan opdatere Mimir, styre beaconen og
      styre brugere; almindelige brugere kan kun deres egne konti
- [x] **Den sidste administrator kan ikke fjernes** — hverken degraderes,
      deaktiveres eller slettes. Tre veje til samme uoprettelige tilstand
- [x] Deaktivering og nulstilling af adgangskode rydder sessioner; at skifte
      sin egen kræver den nuværende

### Frontend
- [x] Svelte 5 + Tailwind, 25 KB gzipped
- [x] **Temavælger** — de syv elementer × lys/mørk/system, uden blink
- [x] Login, konti, UID-input, Enka-hentning, .good-upload
- [x] Karakter- og artifact-visning
- [x] **Mål-editor** med rigtige talent-rækker og hit-tællere
- [x] **Planvisning** med konflikter og forbehold
- [x] **Bruger- og systemsider** — version, opdatering, beacon, roller
- [x] PWA-manifest

### Udrulning
- [x] Dockerfile (to stages, statisk binær, ikke-root)
- [x] Yggdrasil-rune med variabelformular

### Verificeret på en rigtig konto
8 karakterer, 8 våben og 40 artifacts importeret uden en eneste advarsel.
Planen finder +23,8 % gratis på Raiden ved at omrokere Emblem — og siger at
det koster Xiangling hendes sæt. Med effekt-laget slået til går Raidens
baseline fra 43.872 til 70.206 skade pr. rotation, og hendes stat-ark matcher
spillets egne tal på alle otte statter.

## Næste

### 1. Flere effekt-regler
DSL'en dækker nu sæt, karakterpassiver, constellations, våben,
fjende-debuffs og effekter med deres egen skadesinstans — 21 regler, alle
verificeret mod deres egen spiltekst. Biblioteket dækker stadig kun den
roster, der er testet imod; resten er tilføjelser til `deploy/effects.json`,
ikke kode.

### 2. Reaktions-koefficienter
Ligger i ability-configs under `BinOutput`, ikke i en tabel. Indtil de er
mined, returnerer transformative reaktioner en fejl der siger hvad der
mangler. Overload, hyperbloom og swirl kan altså ikke regnes endnu.

### 3. Materialeregnskab
Talent- og ascension-materialer er mined pr. karakter, men ikke koblet til
domæner, ugedage og bosser. Det er dét der gør `KindAscend` prissat i resin
i stedet for blokeret, og det er forudsætningen for farmplanen.

### 4. Talent- og våbendomæner
`DailyDungeonConfigData` har ugedagene, men feltnavnene er obfuskerede og
roterer mellem versioner. Artifact-domæner er mined (de er åbne hver dag).

### 5. ER-beregner
Givet rotation og partikelgenerering: hvor meget Energy Recharge kræver din
Raiden reelt. Talenttabellerne har allerede partikeltallene.

### 6. Det proaktive lag
Resin-budget over 14 dage med domænerotation og ugentlige bosser. Push via
PWA + ntfy. Ugentlig rapport. Banner-bevidsthed. HoYoLAB-klienten og
`resin_snapshots` er der; planlæggeren mangler.

### 7. AI-laget
Værktøjskald mod beregningskernen, RAG over karakterguides i `guides`-
tabellen, naturligt sprog ind ("byg mit bedste Hyperbloom-hold").
Sprogmodellen regner aldrig selv.

### 8. Træningsmodulet
Quiz på reaktionsformler, rotationstiming, ER-krav.

### 9. Fælles optimering på tværs af mål
Kontoplanen kører målene efter prioritet og lader det højeste vinde
udstyret. Det er bedre end at vise begge sider af en tovtrækning, men det
er ikke en fælles optimering: et mål måles stadig mod det udstyr
karakteren har nu, ikke mod det et højere mål lige har taget.

## Bygget til sidst, som aftalt

- [x] **Auto-updater** — versionstjek, changelog og ét klik. Henter,
      verificerer checksum, **starter den nye binær og venter på et
      helbredstjek** før noget udskiftes, og efterlader en vagthund bygget af
      den kendte-gode binær der ruller tilbage hvis den nye alligevel ikke
      kommer op. `mimir rollback` gør det samme i hånden
- [x] **Deployment-detektion** — i en container siger den ærligt at et image
      ikke kan udskifte sig selv, og peger på rune-opdateringen. En
      lokalt bygget binær tilbydes aldrig erstattet af en release
- [x] **Beacon** — én daglig ping med anonymt instans-id og version, intet
      andet, og siden viser den bogstavelige payload. Slået fra indtil den
      slås til, og fra bliver fra: kun et eksplicit "1" tænder den
- [x] **Ingen standard-collector** — at låne Yggdrasils adresse gjorde
      Mimirs første testping til en spøgelses-installation i Yggdrasils
      tælling. Beaconen kræver nu en adresse, og en mislykket ping vises
- [x] **Collector-siden** — samme binær kan tage imod. Slået fra som
      standard, og endepunktet svarer 404 når den er det. Gemmer kun
      instans-id og version, med en test der fejler hvis tabellen får en
      kolonne mere. Loftet på antal instanser afviser kun *nye* id'er, så
      en kendt installation aldrig holder op med at blive talt

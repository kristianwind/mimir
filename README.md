# Mimir

En Genshin Impact-rådgiver: importér din konto, få den optimeret, og få at vide
hvad du skal bruge morgendagens resin på.

Navnet er rådgiveren ved brønden, der ved besked.

## Hvad den kan, som andre værktøjer ikke kan

De eksisterende værktøjer svarer på *hvad din bedste build ville være*. Det er
et statisk svar på et dynamisk spørgsmål. Du har 180 resin om dagen, en
domæne-rotation der skifter med ugedagen, en halvfærdig roster og et banner der
lukker på torsdag.

Mimir rangerer i stedet **hver mulig opgradering på hele kontoen efter forventet
skadesgevinst pr. resin**. Rigtigt output fra en rigtig konto med to mål sat op:

```
1. [RaidenShogun] Skift til 4pc EmblemOfSeveredFate     +34,53 %   gratis
                  tager stykker fra Xiangling
2. [Xiangling]    Giv Xiangling "The Catch" (R5)        +12,49 %   gratis
                  blokeret: RaidenShogun bruger det, og har højere prioritet
3. [Xiangling]    Skift til 4pc EmblemOfSeveredFate     +12,42 %   gratis
                  blokeret: RaidenShogun bruger det, og har højere prioritet
4. [Xiangling]    Elemental skill 9 → 10                 +1,09 %   20 resin
                  blokeret: kræver en Crown of Insight
```

Gratis omrokeringer først, blokerede handlinger sidst, og artifact-farming
besvaret med en simulation af domænets drop-fordeling frem for en
tommelfingerregel.

Bemærk hvad den *ikke* gør: den påstår ikke at nummer 2 og 3 er gratis
gevinster. De koster Raiden hendes sæt, og det står der.

## Datakilder

Ingen taster 1.400 artifacts ind. Alle tre etablerede kilder understøttes:

| Kilde | Hvad den giver | Hvad den kræver |
|---|---|---|
| **Enka.Network** | Showcase: op til otte karakterer med level, constellation, talenter, våben og udstyrede artifacts | Kun et UID. *Vis karakterdetaljer* skal være slået til |
| **GOOD-format** | Hele inventaret — hver eneste artifact, våben og materiale | En `.good`-fil fra Inventory Kamera eller Genshin Optimizer |
| **HoYoLAB** | Resin, dagens commissions, ekspeditioner, Abyss | `ltoken`/`ltuid`-cookies, krypteret i databasen |

Statiske spildata mines fra tre kilder, alle nøglet på numeriske id'er:
tallene fra `DimbreathBot/AnimeGameData`, navnene fra Enkas eget store og
genshin-db. Den oplagte vej — TextMap — virker ikke: hashene i det aktuelle
mirror resolver nul ud af 165 karakternavne. [docs/GAMEDATA.md](docs/GAMEDATA.md)
forklarer hvorfor, og hvad der gøres i stedet.

## Kvasirs mening

Planen er rangeret, men den er tavs. Den siger at Emblem er +34,53 % og at
det koster Xiangling hendes sæt; den siger ikke om du skal gøre det, hvad der
egentlig holder kontoen tilbage, eller hvad du har glemt at fortælle Mimir.

Det gør Kvasir. Han sidder på hver side — planen, målene, karaktererne,
artifacts — og svarer på ét spørgsmål: hvordan bliver du bedre. Og der er en
samtale til spørgsmålet efter det.

Det svære er ikke at få en model til at skrive råd. Det er at gøre rådet
værd at stole på, når det står ved siden af tal, der er regnet ud. Så reglen
— **sprogmodellen regner aldrig selv** — er ikke en bøn i en prompt. Den er
håndhævet to steder:

1. **Kvasir får et faktaark, ikke en database.** Hver side har én funktion der
   kører beregningskernen og skriver ned hvad der kom tilbage. Det ark er alt,
   hvad modellen ved om kontoen. Der er ingen vej fra en prompt til et tal.
2. **Hvert tal i svaret tjekkes mod arket.** Præcis som en effekt-regel kun
   loader, hvis dens tal står i den spiltekst den citerer. Et punkt med et tal,
   ingen har regnet ud, bliver slettet før du ser det — og du får at vide, at
   det blev slettet, og hvilket tal der var tale om.

Hver side har et *Hvad fik Kvasir at vide?* — hele faktaarket, ordret. Et svar
hvis grundlag er smidt væk, kan ikke efterprøves.

Samtalen er det ene sted, modellen selv vælger hvad den kigger på: den kan
kalde beregningskernen — planen, en build, en talenttabel, inventaret — og
svaret siger hvad den slog op. Alle otte kald er læse-kald. Kvasir rådgiver;
at udstyre et stykke eller ændre et mål er dit.

Alt det her er valgfrit. `MIMIR_LLM_BASE_URL` peger på et OpenAI-kompatibelt
endepunkt — LM Studio, Ollama, vLLM eller en hostet API — så du bestemmer,
hvor husstandens spilkonto må havne. Står den tom, findes laget ikke: intet
kort, ingen side, ingen forespørgsel, og alt andet virker som før.

## Kom i gang

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

Mineren henter spildata fra de offentlige datamines. Den tager ti-tyve
sekunder koldt, under to varmt, og afviser at skrive et snapshot der ville
gøre beregningerne stille forkerte. Se [docs/GAMEDATA.md](docs/GAMEDATA.md) —
især afsnittet om hvorfor navnene ikke kommer fra TextMap.

Serveren lytter på `:8080`. I udvikling kører frontend'en separat med hot
reload og proxyer `/api` videre:

```bash
npm --prefix web run dev
```

## Som Yggdrasil-rune

`deploy/mimir.yaml` er rune-definitionen og `deploy/Dockerfile` bygger imaget.
Frontend'en er `go:embed`'et ind i binæren, så containeren er én statisk fil og
et datakatalog — ingen node, ingen CGO, ingen ekstern database.

## Arkitektur

Se [ARCHITECTURE.md](ARCHITECTURE.md). De tre regler der styrer alt andet:

1. **Beregningskernen indeholder formler, aldrig spilkonstanter.** Alt der
   ændrer sig med en patch ligger i `internal/gamedata` og mines. En ny
   version er en datasynkronisering, ikke en kodeændring.
2. **Sprogmodellen regner aldrig selv.** Den kalder beregningskernen som
   værktøj og forklarer resultatet. Ellers hallucinerer den multipliers, og så
   er hele produktets troværdighed væk. Reglen er håndhævet, ikke lovet: hvert
   tal Kvasir skriver tjekkes mod det faktaark, beregningskernen gav ham.
3. **Et tal Mimir ikke kan kilde, findes ikke.** Manglende reaktions-
   koefficienter giver en fejl der siger hvad der mangler, ikke et
   sandsynligt gæt. Farming uden en målt drop-rate rangeres i stykker frem
   for resin. Alt der ikke kan prissættes, står i planen under "forbehold" —
   for en stille udeladelse læses som "det er ikke værd at gøre".

   Det gælder også de betingede bonusser — sæt, karakterpassiver og
   våbenpassiver — som er den ene ting Mimir *ikke* kan mine. De står i
   `deploy/effects.json` som håndskrevne regler, men hver regel citerer
   spillets egen ordlyd, og loaderen tjekker at tallene står der. En regel der
   påstår 25 % mod en tekst der siger 20 %, loader ikke. For våben tjekkes
   hver refinement mod netop sin egen sætning.

## Status

Se [PROGRESS.md](PROGRESS.md).

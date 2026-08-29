# Configuration

Everything is read from the environment at startup. There is no config file —
the deployment is a container in a panel that has a variables form, and one
source of truth beats two.

Nothing here needs setting to run Mimir. Every variable has a default that
produces a working single-user instance; the ones below exist to point it at a
domain, a model, or a payment processor.

## The basics

| Variable | Default | What it does |
|---|---|---|
| `MIMIR_ADDR` | `:8080` | Listen address. |
| `MIMIR_DATA_DIR` | `./data` | The SQLite database, the game data snapshots, the machine secret and the cached art. **Back this up; losing it loses everything.** |
| `MIMIR_BASE_URL` | `http://localhost:8080` | The public address. Load-bearing — see below. |
| `MIMIR_SECURE_COOKIES` | `false` | Set the session cookie's Secure flag. On behind TLS. |
| `MIMIR_USER_AGENT` | `mimir/0.1 …` | How Mimir identifies itself to Enka.Network, who ask integrators for a contactable string. |
| `MIMIR_REPO` | `kristianwind/mimir` | Which repository releases are checked against. |

### `MIMIR_BASE_URL` decides more than it looks like

Four things read it, and three of them fail quietly if it is wrong.

It is where Stripe sends a customer back to after checkout. It is the origin
Mimir accepts passkeys from, and the host part becomes the WebAuthn RP ID —
**a passkey enrolled under one hostname will not work under another**, and the
failure is a browser silently declining rather than an error anybody sees. It
is the address in the PWA manifest. And it is the origin every canonical
URL, `og:image` and sitemap entry is built from, so a wrong one tells search
engines the real pages live somewhere else.

Change the domain and every existing passkey is orphaned. Get it right before
anybody enrols one.

## Being found

Only on the hosted instance, and this is not a knob — it follows
`MIMIR_HOSTED`.

The frontend is a single page application, so every address is served the
same document and the page is chosen in the browser. Search engines will run
the script eventually; the crawlers that decide what a pasted link looks like
— Discord, Reddit, Slack, iMessage, Bluesky — will not. They read the head
and give up. So the server writes the title, description, Open Graph and
JSON-LD into the document per path before it goes out (`internal/api/seo.go`),
and `/robots.txt` and `/sitemap.xml` are served from the same table.

A self-hosted instance is the opposite: `robots.txt` refuses everything and
no page describes itself. Being reachable is not the same as asking to be
listed, and somebody running this on a box at home did not ask to appear in
Google.

The card in a link preview is `web/public/og.jpg`, 1200×630. It is a real
file rather than something rendered on demand, because the one thing a link
preview must not do is depend on the service being quick.

## The AI layer

Entirely optional. With no endpoint configured Kvasir does not exist — no
card, no page, no request — and every number in the product is unchanged,
because no number comes from a model.

| Variable | Default | What it does |
|---|---|---|
| `MIMIR_LLM_BASE_URL` | *(empty)* | An OpenAI-compatible endpoint: LM Studio, Ollama, vLLM. Empty disables the whole layer. |
| `MIMIR_LLM_MODEL` | *(empty)* | Model name at that endpoint. |
| `MIMIR_LLM_API_KEY` | *(empty)* | Sent as a bearer token where the endpoint wants one. |
| `MIMIR_LLM_THINKING` | `false` | Let a reasoning model reason first. Off because Kvasir wants a JSON object built from a fact sheet, and the chain of thought spends the budget and the wait on something nothing reads. |
| `MIMIR_LLM_MAX_TOKENS` | `4000` | Bounds one answer. A reasoning model spends this on thinking before writing. |
| `MIMIR_LLM_TIMEOUT` | `90` | Seconds. Sits inside the server's own write timeout on purpose: an answer arriving after the browser has gone costs the same tokens and tells nobody anything. |

## Data files

| Variable | Default | What it does |
|---|---|---|
| `MIMIR_SUPPLEMENTS` | `/etc/mimir/supplements.json` | Hand-maintained numbers the datamine does not publish. |
| `MIMIR_EFFECTS` | `/etc/mimir/effects.json` | Effect rules, with the in-game wording each number is checked against. |

Both ship inside the image; the defaults point there. A local checkout
overrides them.

## Selling it

**Only the one instance that is sold sets any of this.** A self-hosted Mimir
leaves it all empty and then has no public pages, no signup, no billing and no
expiry — the software is free and running it yourself holds nothing back.

| Variable | Default | What it does |
|---|---|---|
| `MIMIR_HOSTED` | `false` | Marks this as the instance offered as a service. Turns on the public pages and, by default, signups. |
| `MIMIR_ALLOW_REGISTRATION` | follows `MIMIR_HOSTED` | Whether strangers may create accounts. |
| `MIMIR_STRIPE_SECRET_KEY` | *(empty)* | 🔴 A credential. `sk_live_…` in production. |
| `MIMIR_STRIPE_WEBHOOK_SECRET` | *(empty)* | 🔴 A credential. `whsec_…`, from the webhook endpoint you create. |
| `MIMIR_STRIPE_PUBLISHABLE_KEY` | *(empty)* | `pk_…`. Not secret; it is embedded in checkout pages by design. |
| `MIMIR_STRIPE_PRICE_MONTHLY` | *(empty)* | `price_…` for the monthly plan. |
| `MIMIR_STRIPE_PRICE_YEARLY` | *(empty)* | `price_…` for the yearly plan. |

### Setting up the Stripe side

Four things have to be true or checkout fails, and three of them fail with an
error that does not obviously name the cause. A fifth does not fail at all —
it quietly charges the wrong amount, and cannot be corrected later.

1. **The product needs a tax code.** Managed Payments makes Stripe the seller,
   and a seller has to know what it is selling to charge the right VAT.
   Without one, every checkout is rejected. Mimir's is `txcd_10103000` —
   Software as a service, personal use.

2. **The destination must send a snapshot payload, not a thin one.** Stripe
   can deliver either the whole object or just an id for you to fetch back.
   Mimir reads the object, so a thin destination arrives with no customer on
   it — and says exactly that rather than failing vaguely.

   The API version does **not** have to match. It used to be required to, and
   that was wrong: an account's version is whatever Stripe has moved it to,
   the dashboard offers only that and a preview, and no release of the Go
   library has ever matched it exactly. What is checked instead is whether
   the fields entitlement is computed from actually arrived — a live
   subscription with no period end is refused, because recording it would set
   an expiry of the zero time and lock out somebody who is paying.

3. **The endpoint URL is `<MIMIR_BASE_URL>/api/stripe/webhook`**, subscribed to
   `customer.subscription.created`, `.updated` and `.deleted` — those three
   and nothing else. Anything else is acknowledged and ignored so Stripe stops
   retrying it, but subscribing to everything just spends both ends' time.

4. **Products and prices are per mode.** Ones created in live mode are
   invisible to test keys and the other way round. Mismatched ids give "no
   such price".

5. **Every price is created with `tax_behavior=inclusive`, and that cannot be
   changed afterwards.** The advertised figure is what the customer is
   charged: Stripe accounts for VAT out of it rather than adding it on top.
   Exclusive pricing would show $4 to a Danish reader and take $5, which is
   both the worst moment to surprise somebody and not how a consumer price
   may be shown in the EU — the inclusive figure is the one that has to
   appear. The catch is that Stripe freezes `tax_behavior` once it is set to
   `inclusive` or `exclusive`; only `unspecified` can still be changed, and
   `unspecified` behaves as exclusive in the meantime. So a price created
   wrongly cannot be repaired — it has to be replaced and the environment
   variable pointed at the new id.

### What the trial is, and is not

Fourteen days from the day the account was made, and it is not a Stripe
object. It asks for no card, so there is nothing to charge and nothing for
Stripe to hold; creating a customer and a subscription for everybody who signs
up would fill the account with objects for people who never pay, and would
stop the trial working whenever Stripe was unreachable.

Entitlement is therefore one function of three inputs — comped, trial window,
Stripe subscription status — and Stripe is the source of truth for exactly one
of them. See `internal/billing/access.go`; the rules are readable in one place
on purpose.

### Free access

Administrators can give an account full access without payment, from the Users
page. It is a column and not a role, deliberately: somebody given the product
for nothing is a user of it and not an operator of the machine, and making
them an administrator to do it would hand them the controls for the server.

## Command line

    mimir serve                  start the server (default)
    mimir useradd -u NAME        create a user, password read from stdin
    mimir reset-2fa -u NAME      remove a second factor after a lost phone
    mimir gamedata import FILE   load a snapshot and make it active
    mimir gamedata list          list stored snapshots
    mimir gamedata activate VER  roll back or forward to a stored snapshot
    mimir rollback               restore the binary the last update replaced
    mimir version

`reset-2fa` is the break-glass path. It runs on the machine, as whoever can
read the data directory, which is the point: it is an administrator acting at
the box rather than anything reachable over the network. It removes the factor
so the user enrols again — recovery leads back to a protected account, not to
an unprotected one.

/**
 * The public pages: what Mimir is, what it costs, and the terms of selling it.
 *
 * Content as data for the same reason the manual is — a parser is a
 * dependency and this frontend has none beyond Svelte. Keeping it here rather
 * than in the components also means the legal text sits in one file that can
 * be read end to end by somebody checking it, instead of being spread across
 * markup.
 *
 * Two rules govern what may be written here.
 *
 * Nothing may claim more than the product does. Mimir's whole argument is
 * that it shows its gaps rather than filling them with guesses, and a sales
 * page that oversells it would be the first thing to break that. Where the
 * engine is honest about a limit, this page is too.
 *
 * And the seller's details are facts about a business, not copy. They are
 * gathered in SELLER below so there is exactly one place to get them right.
 */

// The one instance that is sold. Everything below assumes it.
export const DOMAIN = 'mimir.guide'

// Required of a service provider to consumers in the EU: who you are, where
// you are, and how to reach a person.
//
// vat is empty because there is no registered company behind this yet. Empty
// rather than a placeholder on purpose — every sentence below omits the
// clause when it is blank, so the page says nothing about a registration
// that does not exist instead of saying something false about one that does.
export const SELLER = {
  name: 'Kristian Wind',
  address: 'Denmark',
  vat: '',
  email: `support@${DOMAIN}`,
}

// vatClause renders ", CVR 12345678" or nothing at all.
const vatClause = SELLER.vat ? `, ${SELLER.vat}` : ''

// Dated when the text was last changed, because "last updated" on a legal
// page is a promise that somebody looked.
const UPDATED = '28 August 2026'

export const PRICE = {
  monthly: '$4',
  yearly: '$40',
  trialDays: 14,
}

export const LANDING = {
  title: 'Mimir',
  tagline: 'What should you do next with this account?',
  intro:
    'Every other Genshin tool tells you what your best possible build would be. Mimir answers the question before that: out of everything available to you right now, which single change buys the most — and what does it cost.',
  points: [
    [
      'It starts with what you already own',
      'Import your inventory and the first thing Mimir looks for is damage sitting in your bag unequipped. That is the one upgrade no amount of farming buys back, and it is almost always the biggest.',
    ],
    [
      'It ranks the whole account, not one character',
      'Every upgrade for every character in one list, ordered by what it buys. Where two characters want the same artifact, it says so and resolves it rather than pretending the conflict does not exist.',
    ],
    [
      'It shows its working',
      'Every number comes from the game’s own data, synced from the datamines. Nothing is a constant somebody typed in, so a patch is a sync rather than a wait for an update.',
    ],
    [
      'It tells you what it does not know',
      'Where something cannot be computed you get a stated gap, never an estimate dressed as a fact. Weapons are not ranked because their passives are not modelled. Most artifact set bonuses are not modelled either, and every recommendation that involves one says so. That is the part most tools leave out.',
    ],
  ],
  // The honest version of "why pay". Not a feature list — the free version
  // has every feature.
  why: {
    title: 'Mimir is free, and this is the hosted version of it',
    body: 'The software is open and costs nothing. Run it on your own machine and you get all of it — every calculation, the whole inventory, the AI layer if you point it at a model. Nothing is held back and nothing ever will be. What a subscription pays for is not features: it is a machine that is already running, kept updated, backed up, and reachable from your phone without you administering anything.',
  },
}

export const PRICING = {
  title: 'Pricing',
  intro: `One plan. ${PRICE.trialDays} days free to start, and no card until you decide.`,
  plans: [
    {
      name: 'Monthly',
      price: PRICE.monthly,
      per: 'per month',
      note: 'Cancel whenever. You keep it until the period you paid for runs out.',
    },
    {
      name: 'Yearly',
      price: PRICE.yearly,
      per: 'per year',
      note: 'Two months cheaper than paying monthly.',
      best: true,
    },
  ],
  included: [
    'Everything the software does. There is no smaller tier.',
    'Your whole inventory, not just your showcase.',
    'Kept on the current game version without you doing anything.',
    'Your data exportable at any time, in the same format you imported.',
  ],
  trial: `The trial is ${PRICE.trialDays} days and asks for no card. When it ends the account simply stops until you subscribe — nothing is deleted and nothing is charged.`,
  selfHost:
    'If you would rather not pay, run it yourself. The source is public and the same. That is not a lesser option offered grudgingly; it is the point of the project.',
}

export const LEGAL = {
  terms: {
    title: 'Terms of service',
    updated: UPDATED,
    sections: [
      [
        'Who you are contracting with',
        `Hosted Mimir at ${DOMAIN} is operated by ${SELLER.name}, ${SELLER.address}${vatClause}. Questions about the service, your account or your data go to ${SELLER.email}.`,
      ],
      [
        'Who sells it to you',
        'Payment is sold and processed by Stripe, who act as the merchant of record for this subscription. That means Stripe is the seller for the purposes of the transaction: they take the payment, they are responsible for charging and remitting any sales tax or VAT that applies where you live, and their name may appear on your bank statement and on your receipt. The service itself is provided to you by the operator named above, under these terms.',
      ],
      [
        'What the service is',
        'An account on a hosted instance of Mimir, a build advisor for the game Genshin Impact. It is not affiliated with, endorsed by, or connected to HoYoverse or Cognosphere. All game names, data and imagery belong to their owners.',
      ],
      [
        'What it does not promise',
        'Mimir produces calculations from public game data. It does not promise that its recommendations are optimal, that the game data is current, or that following its advice produces any particular result in the game. Where a figure cannot be sourced, the product says so rather than estimating; that behaviour is deliberate and is not a defect.',
      ],
      [
        'Your account',
        'You are responsible for keeping your sign-in credentials secure. One account is for one person. Accounts sharing credentials may be suspended.',
      ],
      [
        'Your data',
        'The account data you import stays yours. You can export it at any time in the format you imported it, and deleting your account deletes it. See the privacy policy for what is stored and for how long.',
      ],
      [
        'Payment and renewal',
        `Subscriptions renew automatically at ${PRICE.monthly} monthly or ${PRICE.yearly} yearly until cancelled. Card details are never seen by or stored on this service; Stripe holds them. Prices are shown exclusive of any sales tax or VAT that applies where you live, which Stripe calculates at checkout and adds to the total shown before you pay.`,
      ],
      [
        'Cancellation',
        'Cancel at any time from your account. The subscription then runs to the end of the period already paid for and does not renew. See the refund policy for what happens to money already taken.',
      ],
      [
        'Ending the service',
        'If the hosted service is discontinued, you will be given notice, a refund of the unused part of any period paid for, and an export of your data. The software itself is public and can be run by you, which is what makes that promise keepable.',
      ],
      [
        'Governing law',
        'Danish law, and the courts of Denmark, without prejudice to any mandatory consumer protection in your country of residence.',
      ],
    ],
  },

  privacy: {
    title: 'Privacy',
    updated: UPDATED,
    sections: [
      [
        'The short version',
        'There is no analytics on this site or in the product. No tracking pixels, no third-party scripts watching you, no advertising identifiers. This is not a stance taken for a policy page — the software has never contained any.',
      ],
      [
        'What is stored',
        'Your sign-in details, with the password stored only as a hash. Your game account UID and the inventory you import. The goals and settings you create. Nothing else.',
      ],
      [
        'What is sent elsewhere',
        'Your UID is sent to Enka.Network when you ask for a showcase import, because that is the service that holds it. If the AI layer is enabled on your account, the question you type and a fact sheet of your own numbers are sent to the configured model to be answered. Nothing else about your account leaves the service.',
      ],
      [
        'Payments',
        'Stripe sells and processes the subscription as merchant of record, and holds the card details; this service never receives them. For payment data Stripe is a controller in their own right rather than merely a processor acting on instructions, and their own privacy terms govern what they hold. What this service receives back from them is your subscription status and nothing else — no card number, no billing address.',
      ],
      [
        'Logs',
        'The server keeps operational logs of requests for a short period to diagnose faults. They are not used to build a profile of you and are not shared.',
      ],
      [
        'Your rights',
        `You can export or delete everything from your account page at any time. If you would rather ask a person, write to ${SELLER.email}. Under the GDPR you may also access, correct, restrict or object to the processing of your data, and complain to your national supervisory authority.`,
      ],
    ],
  },

  refunds: {
    title: 'Refunds and cancellation',
    updated: UPDATED,
    sections: [
      [
        'The trial comes first',
        `There is a ${PRICE.trialDays} day trial and it does not ask for a card, so nobody pays before they have used the thing. That is the intended way to find out whether it is for you.`,
      ],
      [
        'Your right to withdraw',
        'As a consumer in the EU you have 14 days to withdraw from a purchase. Because this is a digital service delivered immediately, you will be asked to agree at checkout that delivery starts at once and that you thereby lose that right. If you would rather keep it, do not tick the box — the service will begin after 14 days instead.',
      ],
      [
        'Who to ask for a refund',
        `Write to ${SELLER.email} first. Because Stripe is the merchant of record they are the party who actually issues a refund, but the operator arranges it — you do not have to work out who to chase, and you should not have to raise a dispute with your bank to be heard.`,
      ],
      [
        'Refunds beyond the withdrawal period',
        'If something is broken, or you were charged in error, write and it will be put right. This is a small service and disputes are handled by a person reading the message, not by a policy.',
      ],
      [
        'Cancelling',
        'Cancel from your account at any time. The subscription runs to the end of the period you have paid for and then stops. Nothing is deleted when it does; your data waits for you and can be exported.',
      ],
    ],
  },

  contact: {
    title: 'Contact',
    updated: '',
    sections: [
      ['Email', SELLER.email],
      ['Who is behind it', `${SELLER.name}, ${SELLER.address}${vatClause}.`],
      [
        'The software',
        'Mimir is open and can be run by anyone. The source, the issue tracker and the release notes are at github.com/kristianwind/mimir.',
      ],
    ],
  },
}

// The routes, in the order they appear in the footer.
export const PAGES = [
  { path: '/', label: 'Home' },
  { path: '/pricing', label: 'Pricing' },
  { path: '/terms', label: 'Terms' },
  { path: '/privacy', label: 'Privacy' },
  { path: '/refunds', label: 'Refunds' },
  { path: '/contact', label: 'Contact' },
]

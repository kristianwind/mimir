/**
 * The manual.
 *
 * Kept as data rather than as Markdown for two reasons. A parser is a
 * dependency, and this frontend has none beyond Svelte — forty kilobytes
 * gzipped is a feature, not an accident. And the sections are keyed to the
 * views they explain, which a document has no way to express.
 *
 * What belongs here is what a page cannot say about itself: the order to do
 * things in, what the words mean, and why a number is missing. What does not
 * belong here is anything the page already states — every view carries its own
 * caveats, and a manual that repeats them is a second place to keep them true.
 */

export const MANUAL = [
  {
    id: 'about',
    title: 'What Mimir is',
    blocks: [
      'Mimir answers one question: what should you do next with this account? Not what your best possible build would be — every other tool answers that — but which single upgrade, out of everything available to you, buys the most.',
      {
        list: [
          'The engine holds formulas, not constants. Every number in the game comes from a synced snapshot, so a patch is a sync rather than a release.',
          'A number Mimir cannot source does not exist. Where something cannot be computed you get a stated gap, never an estimate dressed as a fact.',
          'The AI layer explains numbers; it never produces them. Kvasir reads what the engine calculated and can only quote it.',
        ],
      },
      {
        note: 'That third rule is why Kvasir sometimes says it cannot answer. It is not being unhelpful — it has nothing sourced to say, and inventing something would be worse.',
      },
    ],
  },
  {
    id: 'start',
    title: 'Getting started, in order',
    blocks: [
      'Four steps, and each one unlocks the next. Skipping one leaves the pages after it looking broken when they are only empty.',
      {
        steps: [
          ['Sync the game data', 'System → Sync game data. Nothing can be calculated without it: no character definitions, no talent tables, no set bonuses.'],
          ['Add an account', 'Accounts → your UID. Enka gives the eight characters in your showcase. A .good file from Genshin Optimizer or Inventory Kamera gives the whole inventory, which is what the plan actually needs.'],
          ['Set a goal or two', 'Goals → which character, and which rotation you actually press. A goal is how Mimir knows what "better" means for that character.'],
          ['Read the plan', 'Plan → every upgrade across the account, ranked. Free rearrangements first, because damage you already own but have not equipped is the one gain no farming buys back.'],
        ],
      },
      {
        note: 'You can skip step three. Potential measures every character without a goal, using one fixed yardstick — but the plan cannot, because "how much better" has no meaning without a rotation to measure it on.',
      },
    ],
  },
  {
    id: 'words',
    title: 'The words on the pages',
    blocks: [
      {
        terms: [
          ['The yardstick', 'One cast of the elemental skill and one of the burst, at that character\'s own talent levels, against a level 90 enemy with 10% resistance. It is the same ruler for everybody, which is what makes the ranking a ranking. It is not how you play.'],
          ['Headroom', 'Damage you have already collected and not equipped — the gain from rearranging what is in the bag. The one upgrade that costs nothing.'],
          ['Unpriced', 'Mimir knows the upgrade is worth something and cannot say what it costs in resin. Not the same as free: an unpriced action never sorts above one that genuinely costs nothing.'],
          ['Blocked', 'You cannot do this today at any price — a Crown of Insight, a domain that is shut until Thursday.'],
          ['Derived', 'A goal Mimir wrote for you, using its own guess at your rotation. Every number measured against it inherits that guess, so it stays marked until you open it and say what you actually press.'],
          ['Conditions', 'Questions only you can answer: is Noblesse up, how many stacks, is the enemy frozen. An unanswered condition is reported rather than quietly switched off, because switching it off understates the build.'],
        ],
      },
    ],
  },
  {
    id: 'plan',
    title: 'Plan',
    blocks: [
      'Every upgrade for every goal on the account, in one list, ranked by damage gained per resin. Free actions lead regardless of size.',
      'Where two characters want the same artifact it says so — "takes pieces from Xiangling" — and resolves it by goal priority rather than pretending there is no conflict.',
      {
        note: 'An account with many goals is planned in priority order down to the first few, and the page names the ones it left out. The work grows with each goal and it is sequential by nature: each goal claims gear the next one then cannot have.',
      },
    ],
  },
  {
    id: 'potential',
    title: 'Potential',
    blocks: [
      'Who is worth building at all. Every character measured on the same yardstick, including everyone with no goal, who is invisible to the plan.',
      'It is ordered by damage added, not by how far behind a character is. The same upgrade buys more absolute damage on an already-strong build, so a settled character can lead while a neglected one has all the room. That is what "most value from the account" means, and it is not the same question as "who needs attention".',
      {
        note: 'If the same artifact set is recommended to everybody, there are two reasons and the page names both. The search only offers sets you can actually field, so one winner can mean one candidate. And most sets have no four-piece bonus the engine can score, so the arrangement was picked on its stats and the set name is a label — rows where that applies say so.',
      },
    ],
  },
  {
    id: 'characters',
    title: 'Characters',
    blocks: [
      'Your roster, and per character the thing people usually look up on a wiki: what to aim for. Which main stat in each slot, which sets are worth farming, which substats to chase.',
      'It is computed rather than repeated. A wiki gives one answer to every player; this runs against your constellation, your talent levels and your rotation, and shows its numbers so you can disagree with it for a reason.',
      {
        note: 'Weapons are deliberately not ranked. Most of what makes a weapon good is its passive, and the passives are mined as wording rather than as numbers — four of two hundred and forty-seven are modelled. A ranking on base attack alone would put a four-star above a five-star and look like advice.',
      },
      {
        note: 'Artifact sets have the same problem and are handled the same way. A four-piece bonus is nearly always conditional wording, so only seven of sixty-three are modelled; the rest are ranked on their stats alone and marked "stats only". They are still worth farming — the entry just is not a claim about the set bonus.',
      },
    ],
  },
  {
    id: 'compare',
    title: 'Compare',
    blocks: [
      'Your builds against somebody who has published a showcase, measured with the same ruler on both sides. Weakest first, because the point is what to work on.',
      'There is no ranking against everybody. The public leaderboards are built from accounts whose owners chose to submit them, so a position in one measures who bothers to publish rather than whether a build is good.',
      {
        note: 'Nothing about your account is sent anywhere, and nothing about theirs is kept — it is fetched, measured and dropped.',
      },
    ],
  },
  {
    id: 'goals',
    title: 'Goals',
    blocks: [
      'A goal is a character and the rotation you actually press, built from the game\'s own talent rows. It is what every gain in the plan is measured against, so a rotation nobody casts produces numbers nobody can use.',
      'Priority breaks ties when two goals want the same piece.',
    ],
  },
  {
    id: 'artifacts',
    title: 'Artifacts',
    blocks: [
      'The whole inventory, with each piece\'s crit value. Enka only ever gives the equipped pieces; the rest comes from a .good file.',
      {
        note: 'A partial export is the commonest reason the plan seems thin. Mimir can only rearrange what it can see, so an inventory of a couple of hundred pieces gives advice about a couple of hundred pieces.',
      },
    ],
  },
  {
    id: 'kvasir',
    title: 'Kvasir',
    blocks: [
      'The AI layer, and optional — without a model configured it does not exist: no card, no page, no request.',
      'It never produces a number. Every figure in an answer is checked against the fact sheet the engine wrote, and anything unsourced is deleted from an opinion or flagged in a conversation. "What was Kvasir told?" shows the whole sheet, verbatim.',
      'In conversation it chooses which calculation to run from a fixed menu of read-only calls. It can decide what to look at and still cannot produce a figure without looking.',
      {
        note: 'An answer that looked nothing up is marked as such. "I do not have that information" from something that never asked is a different claim from the same words after it did.',
      },
    ],
  },
  {
    id: 'accounts',
    title: 'Accounts',
    blocks: [
      'Your UID, and the two ways data gets in.',
      {
        terms: [
          ['Enka', 'The eight characters in your showcase, live. Needs Show Character Details switched on in the game under Profile → Edit Profile.'],
          ['.good file', 'The whole inventory, from Genshin Optimizer or Inventory Kamera. This is the one the plan wants: it is the only source that includes artifacts you are not wearing.'],
          ['Not the same as Settings', 'This page is your Genshin accounts. Settings is your Mimir login — password and two-factor.'],
        ],
      },
    ],
  },
  {
    id: 'account',
    title: 'Settings',
    blocks: [
      'Your own account: how you sign in, what you pay, and which colours the app uses. Not to be confused with Accounts, which is your Genshin UIDs.',
      {
        terms: [
          ['Passkeys', 'Your fingerprint, face or device PIN instead of typing anything. A passkey signs in on its own — no password and no code — because the device already checked it was you before it would sign. It will only answer the real address of this site, so a convincing copy of the sign-in page cannot collect it, which is the one thing care while typing cannot promise.'],
          ['Two-factor authentication', 'A code from an app on your phone as well as your password, so a stolen password is not enough on its own. Nothing is protected until you have typed one code back and proved it works — stopping halfway leaves the account exactly as it was.'],
          ['Recovery codes', 'Ten single-use codes, shown once when you switch two-factor on. They are stored hashed, so not even the server can print them again. Keep them somewhere that is not the phone they exist to replace.'],
        ],
      },
      {
        note: 'Turning two-factor off, and printing a new set of recovery codes, both ask for your password again. A session only proves somebody signed in once, and surviving a stolen session is the whole point.',
      },
      {
        note: 'A passkey and an authenticator code are not a ladder to climb — either is enough on its own. Having both simply means losing one device does not lock you out.',
      },
      {
        note: 'Locked out with no codes left? Whoever runs the server can clear the second factor from the machine itself. That removes it so you can enrol again — it is a way back to a protected account, not to an unprotected one.',
      },
    ],
  },
  {
    id: 'system',
    title: 'System',
    blocks: [
      'Version, updates, the game data sync and the beacon. Administrators only.',
      'Syncing fetches from the public datamines, verifies the effect rules against the game\'s own wording, and activates the result. If anything fails nothing is swapped — the snapshot in use stays. Old snapshots stay in the list and you can go back to one.',
      {
        note: 'The beacon is the one thing Mimir sends anywhere: once a day, an anonymous instance id and a version number, and the page shows the literal payload. It is off until switched on, and off stays off.',
      },
    ],
  },
  {
    id: 'missing',
    title: 'When a number is missing',
    blocks: [
      'Mimir would rather show a gap than a guess, so blanks are usually deliberate and always explained where they appear. The common ones:',
      {
        terms: [
          ['No resin cost on an ascension or talent', 'The material bill is exact and mined. How many domain runs it takes to collect is published nowhere, so the total is not Mimir\'s to give.'],
          ['A set recommendation marked "stats only"', 'That set\'s four-piece bonus is conditional wording rather than numbers, so it is not in the score. Seven of the sixty-three sets are modelled. It is also why farming domains can tie: with no set contribution there is nothing left to tell them apart, and the plan folds them into one row rather than printing the same number six times.'],
          ['Transformative reactions unavailable', 'Their coefficients live in ability configs rather than in a table, and are not mined yet. Overload, hyperbloom and swirl therefore return an error naming what is missing.'],
          ['Farming priced in pieces, not resin', 'Your drop rate has not been measured. It is estimated from your own inventory and needs at least two hundred five-star pieces to say anything.'],
          ['A condition nobody has answered', 'The effect is real and switched off, and it appears in the plan rather than being silently ignored.'],
        ],
      },
    ],
  },
]

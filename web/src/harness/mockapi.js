// Stands in for the real api.js so the grid can be rendered without a server.
// The shape must match what handleArtifactGrid returns, or this proves nothing.
const row = (character, cells) => ({
  character,
  slots: ['flower', 'plume', 'sands', 'goblet', 'circlet'].map((slot, i) => ({
    slot,
    ideal: cells[i].ideal,
    pieces: cells[i].worn
      ? [{ artifactId: i + 1, slot, set: cells[i].set, level: cells[i].level,
           mainStat: cells[i].main, score: cells[i].score, worn: true,
           substats: cells[i].subs ?? [
             { key: 'critRate_', value: 0.066 }, { key: 'critDMG_', value: 0.148 },
             { key: 'atk_', value: 0.099 }, { key: 'eleMas', value: 40 },
           ],
           verdict: cells[i].verdict, why: cells[i].why, gain: 0 }]
      : [],
  })),
  caveats: [],
})
export const api = {
  // The chat asks whether the AI layer is configured at all. Both branches are
  // worth being able to look at: `?c=chat` renders the enabled view, and
  // flipping this to false renders the "not switched on" copy.
  kvasirStatus: async () => ({ enabled: true, model: 'gemma-4-26b-qat' }),

  // Enough of the System page to look at the audit log, which is the part
  // that had never been rendered.
  system: async () => ({ version: 'v0.5.3', hosted: true, dataDir: '/data' }),
  gamedata: async () => ({ synced: true, version: '7.0.0', characters: 124 }),
  receiver: async () => ({ enabled: false }),
  mineStatus: async () => ({ running: false, log: [] }),
  audit: async ({ action = '' } = {}) => ({
    entries: [
      { id: 41, when: '2026-09-01 12:08:17', username: 'kristian', action: 'gamedata.mine', resource: '7.0.0', detail: '' },
      { id: 40, when: '2026-09-01 11:59:02', username: 'kristian', action: 'user.login', resource: 'kristian', detail: '' },
      { id: 39, when: '2026-09-01 11:41:55', username: '', action: 'user.login.failed', resource: 'sabrina', detail: '{"reason":"credentials"}' },
      { id: 38, when: '2026-09-01 10:12:40', username: 'kristian', action: 'billing.comp', resource: 'sabrina', detail: '{"months":12,"note":"tester"}' },
      { id: 37, when: '2026-08-31 22:03:11', username: 'sabrina', action: 'user.2fa.enabled', resource: 'sabrina', detail: '' },
    ].filter((e) => !action || e.action.startsWith(action)),
    next: 0,
    now: '2026-09-01T12:30:00Z',
  }),
  kvasirOpinion: async () => ({
    cached: true,
    opinion: {
      verdict: 'Prioritize setting goals for Arlecchino and Columbina to move them beyond baseline equipment.',
      points: [
        { headline: 'Establish goals for Arlecchino and Columbina',
          why: 'They are level 90 with 9/9/9 talents but have no goals set up.',
          do: 'Define specific combat rotations and damage targets for these two.' },
      ],
      questions: [],
    },
    dropped: [],
    brief: '# The roster on account 700123456',
  }),
  kvasirChat: async () => ({
    reply: 'Arlecchino has the most to gain. Her goblet is a DEF% piece and she wants Pyro DMG%, which levelling cannot change.',
    used: ['potential', 'target'],
    unsourced: [],
  }),
  // The Users page with exactly one administrator, which is the shape the
  // hosted instance actually has. kristian is that administrator and is not
  // comped; sabrina is a comped tester, so both states of the button render.
  users: async () => [
    { id: 1, username: 'kristian', role: 'admin', disabled: false, accounts: 2, sessions: 1, comped: false, compedNote: '' },
    { id: 2, username: 'sabrina', role: 'user', disabled: false, accounts: 1, sessions: 0, comped: true, compedNote: 'tester' },
  ],
  comp: async () => ({ status: 'ok' }),
  updateUser: async () => ({ status: 'ok' }),
  deleteUser: async () => ({ status: 'ok' }),
  createUser: async () => ({ status: 'ok' }),
  // The character page. Shape copied from advisor.BuildStatus's JSON tags —
  // a mock that has drifted from the endpoint proves nothing about the page.
  character: async () => ({
    character: 'RaidenShogun',
    element: 'electro',
    level: 90,
    constellation: 2,
    weapon: 'TheCatch',
    built: 0.384,
    slots: [
      {
        slot: 'goblet', ideal: 'electro_dmg_',
        worn: { artifactId: 41, slot: 'goblet', set: 'GladiatorsFinale', level: 20,
                mainStat: 'def_', score: 21, worn: true, gain: 0, verdict: 'replace',
                why: 'main stat is defPercent; this character wants electroDamageBonus here, and levelling cannot change it' },
        levelGain: 0, swapGain: 0.1842, swapTo: 77, best: 0.1842, action: 'swap',
      },
      {
        slot: 'circlet', ideal: 'critDMG_',
        worn: { artifactId: 52, slot: 'circlet', set: 'EmblemOfSeveredFate', level: 8,
                mainStat: 'critDMG_', score: 44, worn: true, gain: 0, verdict: 'ok',
                why: 'right main stat, but only +8 of +20' },
        levelGain: 0.0913, levelTo: 20, swapGain: 0.0410, swapTo: 91, swapWornBy: 'Xiangling',
        best: 0.0913, action: 'level',
      },
      {
        slot: 'sands', ideal: 'atk_',
        levelGain: 0, swapGain: 0.0617, swapTo: 63, best: 0.0617, action: 'equip',
      },
      {
        slot: 'plume', ideal: 'atk',
        worn: { artifactId: 22, slot: 'plume', set: 'EmblemOfSeveredFate', level: 20,
                mainStat: 'atk', score: 71, worn: true, gain: 0, verdict: 'good',
                why: 'nothing to do here: right main stat, at its cap, and nothing in the bag beats it. It scores 71 of 100 — the rest is substats and the set, which no amount of levelling chooses for you' },
        levelGain: 0, swapGain: 0, best: 0, action: '',
      },
      {
        slot: 'flower', ideal: 'hp',
        worn: { artifactId: 11, slot: 'flower', set: 'EmblemOfSeveredFate', level: 20,
                mainStat: 'hp', score: 74, worn: true, gain: 0, verdict: 'good',
                why: 'right main stat, at its cap, and nothing in the bag beats it' },
        levelGain: 0, swapGain: 0, best: 0, action: '',
      },
    ],
    substats: [
      { stat: 'critDMG_', perRoll: 0.0351, relative: 1 },
      { stat: 'atk_', perRoll: 0.0204, relative: 0.581 },
      { stat: 'eleMas', perRoll: 0.0041, relative: 0.117 },
      { stat: 'atk', perRoll: 0.0038, relative: 0.108 },
      { stat: 'critRate_', perRoll: 0, relative: 0,
        note: 'crit rate is already at the ceiling for this rotation, so a further roll buys nothing' },
      { stat: 'enerRech_', perRoll: 0, relative: 0, unmeasured: true,
        note: 'not measured rather than worthless: the yardstick is one skill and one burst, a rotation that never waits for energy, so recharge has nothing to change here. What it actually buys is how often you get the burst off at all, which this does not model' },
      { stat: 'hp', perRoll: 0, relative: 0, note: 'the rotation being measured does not scale on this stat, so a roll buys nothing' },
      { stat: 'hp_', perRoll: 0, relative: 0, note: 'the rotation being measured does not scale on this stat, so a roll buys nothing' },
      { stat: 'def', perRoll: 0, relative: 0, note: 'the rotation being measured does not scale on this stat, so a roll buys nothing' },
      { stat: 'def_', perRoll: 0, relative: 0, note: 'the rotation being measured does not scale on this stat, so a roll buys nothing' },
    ],
    aim: {
      character: 'RaidenShogun',
      element: 'electro',
      mainStats: { sands: 'enerRech_', goblet: 'electro_dmg_', circlet: 'critDMG_' },
      sets: [
        { config: 'EmblemOfSeveredFate', score: 41822, behind: 0, owned: true, modelled: true },
        { config: 'GildedDreams', score: 39901, behind: 0.046, owned: true, modelled: false },
        { config: 'ThunderingFury', score: 39104, behind: 0.065, owned: false, modelled: false },
        { config: 'NoblesseOblige', score: 38755, behind: 0.073, owned: true, modelled: true },
        { config: 'GladiatorsFinale', score: 38111, behind: 0.089, owned: true, modelled: false },
      ],
      substats: { critDMG_: 6, critRate_: 3 },
      caveats: [
        'Only 7 of the 61 artifact sets have a four-piece bonus with numbers behind it. The rest are scored on their stats alone, so they are ranked on less than the whole truth \u2014 each entry says which it is.',
        'The weapon is held constant across every candidate, so the ranking is between sets and not between builds.',
      ],
    },
    caveats: [
      'Built is this build\u2019s damage against an idealised one \u2014 five pieces with the right main stat, the target view\u2019s substat allocation, at +20. Nothing reaches a hundred, because that build has perfect substats on all five pieces and no real account does. It is a ruler, not a grade.',
      'What levelling buys is the main stat\u2019s growth alone. A piece gains a substat roll every four levels and which stat it lands on is unknown, so that part is left out rather than guessed \u2014 the real gain is this number or better, never worse.',
    ],
  }),
  artifactGrid: async () => ({
    rows: [
      row('Arlecchino', [
        { worn: true, ideal: 'hp', main: 'hp', set: 'FragmentOfHarmonicWhimsy', level: 20, score: 74, verdict: 'good', why: 'right main stat, fully levelled, and nothing in the bag beats it' },
        { worn: true, ideal: 'atk', main: 'atk', set: 'FragmentOfHarmonicWhimsy', level: 20, score: 71, verdict: 'good', why: 'right main stat, fully levelled, and nothing in the bag beats it' },
        { worn: true, ideal: 'atk_', main: 'atk_', set: 'FragmentOfHarmonicWhimsy', level: 12, score: 48, verdict: 'ok', why: 'right main stat, but only +12 of +20' },
        { worn: true, ideal: 'pyro_dmg_', main: 'def_', set: 'GladiatorsFinale', level: 20, score: 21, verdict: 'replace', why: 'main stat is defPercent; this character wants pyroDamageBonus here, and levelling cannot change it' },
        { worn: false, ideal: 'critDMG_' },
      ]),
      row('Sandrone', [
        { worn: true, ideal: 'hp', main: 'hp', set: 'EmblemOfSeveredFate', level: 20, score: 69, verdict: 'good', why: 'right main stat, fully levelled, and nothing in the bag beats it' },
        { worn: true, ideal: 'atk', main: 'atk', set: 'EmblemOfSeveredFate', level: 8, score: 33, verdict: 'ok', why: 'right main stat, but only +8 of +20' },
        { worn: true, ideal: 'atk_', main: 'enerRech_', set: 'EmblemOfSeveredFate', level: 20, score: 44, verdict: 'replace', why: 'main stat is energyRecharge; this character wants atkPercent here, and levelling cannot change it' },
        { worn: true, ideal: 'cryo_dmg_', main: 'cryo_dmg_', set: 'BlizzardStrayer', level: 16, score: 58, verdict: 'ok', why: 'a piece you already own scores 77 against this one’s 58, though Ayaka is wearing it' },
        { worn: true, ideal: 'critDMG_', main: 'critDMG_', set: 'EmblemOfSeveredFate', level: 20, score: 81, verdict: 'good', why: 'right main stat, fully levelled, and nothing in the bag beats it' },
      ]),
      row('Linnea', [
        { worn: true, ideal: 'hp', main: 'hp', set: 'AshenSeal', level: 4, score: 18, verdict: 'ok', why: 'right main stat, but only +4 of +20' },
        { worn: true, ideal: 'atk', main: 'atk', set: 'AshenSeal', level: 0, score: 9, verdict: 'ok', why: 'right main stat, but only +0 of +20' },
        { worn: true, ideal: 'atk_', main: 'hp_', set: 'NoblesseOblige', level: 20, score: 26, verdict: 'replace', why: 'main stat is hpPercent; this character wants atkPercent here, and levelling cannot change it' },
        { worn: true, ideal: 'atk_', main: 'atk_', set: 'AshenSeal', level: 16, score: 3, verdict: 'replace', why: '4★, so its main stat caps below what a five-star reaches' },
        { worn: true, ideal: 'critRate_', main: 'critRate_', set: 'AshenSeal', level: 20, score: 103, verdict: 'good', why: 'right main stat, fully levelled, and nothing in the bag beats it' },
      ]),
    ],
    missing: ['Furina: not on this account'],
  }),
}

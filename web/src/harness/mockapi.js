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
  kvasirChat: async () => ({
    reply: 'Arlecchino has the most to gain. Her goblet is a DEF% piece and she wants Pyro DMG%, which levelling cannot change.',
    used: ['potential', 'target'],
    unsourced: [],
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

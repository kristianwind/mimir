/**
 * What a stat is called to a player.
 *
 * The keys are the GOOD format's own — `atk_`, `critDMG_`, `pyro_dmg_` —
 * chosen so importing an inventory needs no translation table. That is the
 * right call for the file format and the wrong string to put in front of
 * somebody: a player reads "ATK%" and "CRIT DMG", and a grid cell labelled
 * `pyro_dmg_` looks like a bug even when the number above it is correct.
 *
 * Anything unmapped falls through unchanged rather than being hidden. A stat
 * nobody has named yet should look odd on the page, not disappear from it.
 */
const LABELS = {
  hp: 'HP',
  hp_: 'HP%',
  atk: 'ATK',
  atk_: 'ATK%',
  def: 'DEF',
  def_: 'DEF%',
  eleMas: 'EM',
  enerRech_: 'Energy Recharge',
  critRate_: 'CRIT Rate',
  critDMG_: 'CRIT DMG',
  heal_: 'Healing Bonus',
  pyro_dmg_: 'Pyro DMG%',
  hydro_dmg_: 'Hydro DMG%',
  anemo_dmg_: 'Anemo DMG%',
  electro_dmg_: 'Electro DMG%',
  dendro_dmg_: 'Dendro DMG%',
  cryo_dmg_: 'Cryo DMG%',
  geo_dmg_: 'Geo DMG%',
  physical_dmg_: 'Physical DMG%',
  dmg_: 'DMG Bonus',
  normal_dmg_: 'Normal Attack DMG%',
  charged_dmg_: 'Charged Attack DMG%',
}

/** @param {string} key @returns {string} the player-facing name. */
export function statLabel(key) {
  if (!key) return ''
  return LABELS[key] ?? key
}

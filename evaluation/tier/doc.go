// Package tier holds a quality tier's seeded sample list and the bars a tier is judged against.
//
// A tier is an ordered list of sample IDs drawn from one dataset with a fixed seed, so every run
// of the tier sends the same samples in the same order. Its early stop runs in the suites
// project (evaluation/suites), on Inspect AI's early-stopping hook: pass_stop.py's PassStop for a
// pass-rate tier, asr_quality.py's WERStop for an ASR set. The quality stage (package
// evaluation/gate) reads the account each writes.
//
// The API:
//   - List is a tier's seeded sample list; Generate draws one from a dataset's IDs with a seed,
//     the same list for the same IDs and seed in any order. ReadList, ReadListFile and List.Write
//     read and write its text format: three header lines, "# tier <name>", "# dataset <name>" and
//     "# seed <n>", then one sample ID a line. The committed lists are evaluation/tiers/<tier>.txt.
//   - Bar is a tier's or task's bar in [0, 1] and its direction: a floor a pass rate or score
//     must reach, or, AtMost, a ceiling a lower-is-better measure such as a WER must not exceed.
//     Bars are them by name; LoadBars reads them from a TOML file of name = bar keys, floors under
//     [bars] and ceilings under [bars_at_most], evaluation/bars.toml, which the baseline fills: a
//     bar is set A's score less (or, for a ceiling, plus) its two-run spread.
package tier

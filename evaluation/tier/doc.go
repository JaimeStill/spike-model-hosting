// Package tier runs a quality tier's samples with an early stop, and holds the seeded sample
// lists and the bars a tier is judged against.
//
// A tier is an ordered list of sample IDs drawn from one dataset with a fixed seed, so every run
// of the tier sends the same samples in the same order. Run sends the list's screening fifth
// (its first ceil(n/5) samples) whole, then judges after each sample: once the one-sided 95%
// upper confidence bound on the pass rate falls below the tier's bar, no sample still to come
// could lift the rate to the bar with that confidence, and the tier stops, failed, naming the
// bound that stopped it. A tier that runs to its end passes when its pass rate is at or above
// its bar. A tier without a bar runs to its end and records its rate; it never fails.
//
// The bound is Wilson's score interval, one-sided at 95% (z = 1.6449), chosen over
// Clopper-Pearson because it stays close to its nominal coverage at the small counts a
// screening fifth holds, where Clopper-Pearson is conservative and would stop later; it is also
// closed-form.
//
// The API:
//   - UpperBound is the Wilson bound on passes of n; Z95 is its z.
//   - List is a tier's seeded sample list; Generate draws one from a dataset's IDs with a seed,
//     the same list for the same IDs and seed in any order. ReadList, ReadListFile and List.Write
//     read and write its text format: three header lines, "# tier <name>", "# dataset <name>" and
//     "# seed <n>", then one sample ID a line. The committed lists are evaluation/tiers/<tier>.txt.
//   - Bars are each tier's bar, a pass rate in [0, 1]; LoadBars reads them from a TOML file of
//     tier = bar keys under [bars], evaluation/bars.toml, which the baseline fills: a bar is set
//     A's score less its two-run spread.
//   - Run runs a List against a bar, sending each sample to a Judge, and returns its Result.
package tier

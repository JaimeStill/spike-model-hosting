"""The quality tiers' early stops, offline, on either side: pass_stop.py's PassStop, which the vision
and BFCL tiers stop on, and asr_quality.py's WERStop, which the ASR sets stop on, each driven
through Inspect's early-stopping hooks as Inspect calls them for samples sent one at a time. The Go test in this
directory (suites_test.go) runs it through the pinned uv and Python, offline.
"""

import asyncio
import math
import unittest

from inspect_ai.scorer import Score, SampleScore

from asr_quality import WERStop, wer_bounds
from pass_stop import FAIL, PASS, PassStop, lower_bound, upper_bound


def run_pass(stop: PassStop, outcomes: list) -> int:
    """Schedules outcomes' samples through stop in order, as Inspect does one at a time, running
    each it doesn't stop, and returns how many ran. An outcome is a pass or a fail, or a partial
    score, an ANLS."""

    async def go() -> int:
        await stop.start_task(None, outcomes, 1)
        ran = 0
        for i, outcome in enumerate(outcomes):
            value = outcome if isinstance(outcome, float) else (1 if outcome else 0)
            if await stop.schedule_sample(f"s{i}", 1) is None:
                await stop.complete_sample(f"s{i}", 1, {"score": SampleScore(score=Score(value=value))})
                ran += 1
        return ran

    return asyncio.run(go())


def run_wer(stop: WERStop, utterances: list[tuple[int, int]]) -> int:
    """Schedules utterances, each (word errors, reference words), through stop in order, running
    each it doesn't stop, and returns how many ran."""

    async def go() -> int:
        await stop.start_task(None, utterances, 1)
        ran = 0
        for i, (errors, words) in enumerate(utterances):
            if await stop.schedule_sample(f"u{i}", 1) is None:
                score = Score(value=errors / words, metadata={"errors": errors, "words": words})
                await stop.complete_sample(f"u{i}", 1, {"wer": SampleScore(score=score)})
                ran += 1
        return ran

    return asyncio.run(go())


# Alternating passes and fails, a 50% rate: s0 passes, s1 fails, and so on.
ALTERNATE = [i % 2 == 0 for i in range(20)]


class PassStopTest(unittest.TestCase):
    def test_upper_bound(self):
        """The one-sided 95% Wilson upper bound at known points, computed independently."""
        for passes, n, want in [(0, 4, 0.4034786), (5, 10, 0.7307282), (8, 10, 0.9314420), (0, 1, 0.7301341), (0, 0, 1.0)]:
            self.assertAlmostEqual(upper_bound(passes, n), want, places=6, msg=f"upper_bound({passes}, {n})")

    def test_lower_bound(self):
        """The one-sided 95% Wilson lower bound at known points, computed independently: the upper
        bound's mirror, 1 less the upper bound on the fail rate."""
        for score, n, want in [(4, 4, 0.5965214), (5, 10, 0.2692718), (10, 10, 0.7870580), (0, 3, 0.0), (0, 0, 0.0)]:
            self.assertAlmostEqual(lower_bound(score, n), want, places=6, msg=f"lower_bound({score}, {n})")
        for passes, n in [(3, 7), (9, 10), (0, 5)]:
            self.assertAlmostEqual(lower_bound(passes, n), 1 - upper_bound(n - passes, n), places=9)

    def test_stops_at_known_point(self):
        """A 50% rate against bars it can't reach: the bound falls below 0.8 after the 6th sample
        and below 0.7 after the 16th, and the tier stops there, failed, naming the bound."""
        for bar, stop_at in [(0.8, 6), (0.7, 16)]:
            stop = PassStop(bar, len(ALTERNATE))
            self.assertEqual(run_pass(stop, ALTERNATE), stop_at, msg=f"bar {bar}")
            summary = stop.summary()
            self.assertEqual((summary["run"], summary["passes"]), (stop_at, stop_at // 2))
            self.assertEqual(summary["stopped"], [f"s{i}" for i in range(stop_at, len(ALTERNATE))])
            bound = upper_bound(stop_at // 2, stop_at)
            self.assertLess(bound, bar)
            self.assertGreaterEqual(upper_bound((stop_at - 1) // 2 + 1, stop_at - 1), bar)
            self.assertIn(f"upper bound {bound:.4f} on the pass rate ({stop_at // 2}/{stop_at})", summary["reason"])
            self.assertEqual(summary["side"], FAIL)

    def test_stops_passed(self):
        """Every sample passing against a bar of 0.7: the Wilson lower bound is under 0.7 after the
        screening fifth's 4 (0.5965), and after 6 (0.6892), and clears it after 7 (0.7212), and the
        tier stops there, passed, naming the bound; the rest are stopped."""
        stop = PassStop(0.7, 20)
        self.assertEqual(run_pass(stop, [True] * 20), 7)
        summary = stop.summary()
        self.assertEqual(summary["side"], PASS)
        self.assertEqual(summary["stopped"], [f"s{i}" for i in range(7, 20)])
        self.assertLess(lower_bound(6, 6), 0.7)
        self.assertGreaterEqual(lower_bound(7, 7), 0.7)
        self.assertIn("lower bound 0.7212 on the mean score (7/7) is at or over the bar 0.7", summary["reason"])

    def test_screens_first_on_a_pass(self):
        """The screening fifth runs whole on the pass side too: every sample passing clears a bar of
        0.5 after 3 (0.5258), but the tier runs its screening fifth, 4 of 20, first."""
        stop = PassStop(0.5, 20)
        self.assertGreaterEqual(lower_bound(3, 3), 0.5)
        self.assertEqual(run_pass(stop, [True] * 20), 4)
        self.assertEqual(stop.summary()["side"], PASS)

    def test_straddles_on(self):
        """A tier whose bounds straddle its bar after the screening fifth runs on until one side
        decides it: a 50% rate against 0.65 straddles from its screening fifth, 8 of 40, on, and
        stops failed once the upper bound falls below the bar, after 28 (0.6484)."""
        stop = PassStop(0.65, 40)
        outcomes = [i % 2 == 0 for i in range(40)]
        ran = run_pass(stop, outcomes)
        self.assertEqual((ran, stop.summary()["side"]), (28, FAIL))
        for n in range(stop.screening, ran):
            self.assertGreaterEqual(upper_bound((n + 1) // 2, n), 0.65, msg=f"decided at {n}")
            self.assertLess(lower_bound((n + 1) // 2, n), 0.65, msg=f"decided at {n}")
        self.assertLess(upper_bound(14, 28), 0.65)

    def test_partial_scores_pass_on_the_mean(self):
        """The pass side bounds the mean score, not the pass rate: ANLS 0.6 on every sample passes
        every sample, a pass rate of 1, but its mean 0.6 never clears a bar of 0.7, so the tier
        runs to its end, undecided."""
        stop = PassStop(0.7, 10)
        self.assertEqual(run_pass(stop, [0.6] * 10), 10)
        self.assertEqual((stop.summary()["passes"], stop.summary()["side"]), (10, ""))

    def test_screens_first(self):
        """The screening fifth runs whole: every sample failing, the bound is under the bar after
        one, but the tier stops only after its screening fifth, 4 of 20."""
        stop = PassStop(0.8, 20)
        self.assertEqual(run_pass(stop, [False] * 20), 4)
        self.assertEqual(stop.summary()["screening"], 4)

    def test_runs_to_end(self):
        """A tier whose bounds straddle its bar to the end runs its whole list, stopping nothing."""
        stop = PassStop(0.5, 10)
        self.assertEqual(run_pass(stop, ALTERNATE[:10]), 10)
        self.assertEqual(stop.summary()["stopped"], [])
        self.assertEqual((stop.summary()["reason"], stop.summary()["side"]), ("", ""))

    def test_partial_scores(self):
        """A sample passes at a score of 0.5 or more, an ANLS among them, and a choice's C."""
        stop = PassStop(0.0, 3)

        async def go():
            await stop.start_task(None, [1, 2, 3], 1)
            for i, value in enumerate([0.5, 0.49, "C"]):
                await stop.complete_sample(f"s{i}", 1, {"score": SampleScore(score=Score(value=value))})

        asyncio.run(go())
        self.assertEqual(stop.summary()["passes"], 2)


class WERStopTest(unittest.TestCase):
    def test_lower_bound(self):
        """The utterance-clustered lower bound at a point worked by hand: errors 1 and 3 over 10
        words each, a corpus WER of 0.2 with a standard error of 0.1."""
        self.assertAlmostEqual(wer_bounds([1, 3], [10, 10])[0], 0.2 - 1.6448536269514722 * 0.1, places=9)
        self.assertEqual(wer_bounds([4], [10])[0], 0.0)
        self.assertEqual(wer_bounds([0, 0], [10, 10])[0], 0.0)

    def test_upper_bound(self):
        """The upper bound mirrors the lower at the same point, R + z SE; one utterance bounds
        nothing."""
        self.assertAlmostEqual(wer_bounds([1, 3], [10, 10])[1], 0.2 + 1.6448536269514722 * 0.1, places=9)
        self.assertEqual(wer_bounds([4], [10])[1], math.inf)
        self.assertEqual(wer_bounds([0, 0], [10, 10]), (0.0, 0.0))

    def test_stops_after_screening(self):
        """A set whose every utterance has half its words wrong stops once its screening fifth,
        2 of 10, has run, its lower bound 0.5 above a 0.1 ceiling; under a 0.6 ceiling it runs
        whole."""
        half = [(5, 10)] * 10
        stop = WERStop(0.1)
        self.assertEqual(run_wer(stop, half), 2)
        self.assertEqual(stop.stopped_at, 2)
        self.assertAlmostEqual(stop.bound, 0.5)
        self.assertEqual(stop.side, FAIL)

    def test_stops_passed(self):
        """A set whose utterances alternate one and two errors in ten words, a WER of 0.15 with a
        small spread, stops passed once its screening fifth, 2 of 10, has run under a 0.5 ceiling,
        its upper bound 0.2322 at or under it."""
        mixed = [(1, 10), (2, 10)] * 5
        stop = WERStop(0.5)
        self.assertEqual(run_wer(stop, mixed), 2)
        self.assertEqual((stop.stopped_at, stop.side), (2, PASS))
        self.assertAlmostEqual(stop.bound, 0.15 + 1.6448536269514722 * 0.05, places=9)
        self.assertIn("upper bound 0.2322 on the WER is at or under the bar 0.5", stop.reason)

    def test_straddles_on(self):
        """A set whose bounds straddle its ceiling runs on until one decides it, or to its end: half
        its words wrong in every utterance but one is held to a 0.5 ceiling, straddled throughout,
        and runs whole, undecided."""
        uneven = [(9, 10), (1, 10)] + [(5, 10)] * 8
        whole = WERStop(0.5)
        self.assertEqual(run_wer(whole, uneven), 10)
        self.assertIsNone(whole.stopped_at)
        self.assertEqual(whole.side, "")


if __name__ == "__main__":
    unittest.main()

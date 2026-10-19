"""The early stop the pass-rate tiers share, vision_quality.py's and agent_quality.py's, and the tier
list format every suite reads (package evaluation/tier's, which writes it).

A tier's screening fifth runs whole, then, before each further sample, the tier stops failed once
the one-sided 95% Wilson upper bound on its pass rate is below the bar, and stops passed once the
one-sided 95% Wilson lower bound on its mean score is at or over it (PassStop); a tier whose bounds
still straddle the bar runs on until one decides it or its list ends. Wilson's interval
is chosen over Clopper-Pearson because it stays close to its nominal coverage at the small counts
a screening fifth holds, where Clopper-Pearson is conservative and would stop later; it is also
closed-form. The tier's account of its early stop, the samples it stopped and why, is written
beside its Inspect logs as EARLY_STOP_FILE, which evaluate reads. Its test is test_early_stop.py.
"""

import math
from pathlib import Path

from inspect_ai.scorer import CORRECT, INCORRECT
from inspect_ai.util import EarlyStop

# The standard normal quantile of a one-sided 95% bound.
Z95 = 1.6448536269514722

# A sample passes at a score of this or more: a correct choice or call (1), or an ANLS of 0.5 or
# more.
PASS_AT = 0.5

# The tier's early stop account, written beside its logs when it runs with a bar.
EARLY_STOP_FILE = "early_stop.json"

# The sides a tier stops on: failed, its bar out of reach, or passed, its bar cleared.
FAIL, PASS = "fail", "pass"


def upper_bound(passes: int, n: int) -> float:
    """The one-sided 95% Wilson score upper bound on a pass rate, and 1 for no samples."""
    if n <= 0:
        return 1.0
    p, z2 = passes / n, Z95 * Z95
    centre = p + z2 / (2 * n)
    margin = Z95 * math.sqrt(p * (1 - p) / n + z2 / (4 * n * n))
    return min(1.0, (centre + margin) / (1 + z2 / n))


def lower_bound(score: float, n: int) -> float:
    """The one-sided 95% Wilson score lower bound on a mean score in [0, 1], score being the n
    samples' scores summed, and 0 for no samples. For a 0-or-1 score it is the pass rate's bound;
    for a partial one, an ANLS, it is conservative, since a score in [0, 1] with mean p varies by at
    most the p(1 - p) Wilson's interval takes, so the bound holds for the mean ANLS the bar
    judges."""
    if n <= 0:
        return 0.0
    p, z2 = score / n, Z95 * Z95
    centre = p + z2 / (2 * n)
    margin = Z95 * math.sqrt(max(0.0, p * (1 - p)) / n + z2 / (4 * n * n))
    return max(0.0, (centre - margin) / (1 + z2 / n))


def score_value(value) -> float:
    """A sample's score as a number: choice()'s C and I as 1 and 0, a number (ANLS, BFCL's 1 or
    0) as it is."""
    if value == CORRECT:
        return 1.0
    if value == INCORRECT:
        return 0.0
    return float(value)


class PassStop:
    """Inspect early stopping on a pass-rate bar, shared by every task of a tier: the screening
    fifth of the tier's size runs whole, then, before each further sample, the tier stops failed
    once the Wilson upper bound on its pass rate is below the bar, and stops passed once the Wilson
    lower bound on its mean score (lower_bound) is at or over it. The pass side bounds the mean
    score, not the pass rate, because the bar judges the mean: for a 0-or-1 score they are the
    same, and a mean ANLS is never above its pass rate, so the fail side's pass-rate bound holds
    for it as well. With requests in flight, a sample is scheduled before the ones ahead of it
    complete, so the stop can come up to the slots less one samples after a bound first decides
    the tier.

    Inspect records a task's early stops in its log's results, but a task whose every sample
    stopped (V*Bench's second task, when the first stopped the tier) is logged with neither
    samples nor results; so the tier's account, summary(), is written beside the logs as
    EARLY_STOP_FILE, which evaluate reads instead."""

    def __init__(self, bar: float, size: int):
        self.bar, self.size = bar, size
        self.screening = math.ceil(size / 5)
        self.passes = self.run = 0
        self.score = 0.0
        self.stopped: list[str] = []
        self.reason = self.side = ""

    async def start_task(self, task, samples, epochs) -> str:
        return "pass_rate_bounds"

    async def schedule_sample(self, id, epoch) -> EarlyStop | None:
        if not self.side and self.screening <= self.run < self.size:
            upper, lower = upper_bound(self.passes, self.run), lower_bound(self.score, self.run)
            if upper < self.bar:
                self.side = FAIL
                self.reason = f"the 95% upper bound {upper:.4f} on the pass rate ({self.passes}/{self.run}) is below the bar {self.bar}"
            elif lower >= self.bar:
                self.side = PASS
                self.reason = f"the 95% lower bound {lower:.4f} on the mean score ({self.score:g}/{self.run}) is at or over the bar {self.bar}"
        if not self.side:
            return None
        self.stopped.append(str(id))
        return EarlyStop(id=id, epoch=epoch, reason=self.reason)

    async def complete_sample(self, id, epoch, scores) -> None:
        [score] = scores.values()
        value = score_value(score.score.value)
        self.run += 1
        self.score += value
        if value >= PASS_AT:
            self.passes += 1

    async def complete_task(self) -> dict:
        return self.summary()

    def summary(self) -> dict:
        return {
            "bar": self.bar, "size": self.size, "screening": self.screening, "run": self.run,
            "passes": self.passes, "score": self.score, "upper_bound": upper_bound(self.passes, self.run),
            "lower_bound": lower_bound(self.score, self.run), "stopped": self.stopped, "side": self.side,
            "reason": self.reason,
        }


def read_list(path: str) -> tuple[str, list[str]]:
    """A tier list's dataset header and IDs (package evaluation/tier's format)."""
    dataset, ids = "", []
    for line in Path(path).read_text().splitlines():
        line = line.strip()
        if line.startswith("# dataset "):
            dataset = line.removeprefix("# dataset ")
        elif line and not line.startswith("#"):
            ids.append(line)
    return dataset, ids

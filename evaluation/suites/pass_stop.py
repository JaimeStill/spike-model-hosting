"""The early stop the pass-rate tiers share, vision_quality.py's and agent_quality.py's, and the tier
list format they read: package evaluation/tier's rule and format, in Python.

A tier's screening fifth runs whole, then, before each further sample, the tier stops once the
one-sided 95% Wilson upper bound on its pass rate is below the bar (PassStop). The tier's account
of its early stop, the samples it stopped and why, is written beside its Inspect logs as
EARLY_STOP_FILE, which evaluate reads.
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


def upper_bound(passes: int, n: int) -> float:
    """The one-sided 95% Wilson score upper bound on a pass rate, package evaluation/tier's
    UpperBound, and 1 for no samples."""
    if n <= 0:
        return 1.0
    p, z2 = passes / n, Z95 * Z95
    centre = p + z2 / (2 * n)
    margin = Z95 * math.sqrt(p * (1 - p) / n + z2 / (4 * n * n))
    return min(1.0, (centre + margin) / (1 + z2 / n))


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
    fifth of the tier's size runs whole, then, before each further sample, the tier stops once
    the Wilson upper bound on its pass rate is below the bar. With requests in flight, a sample
    is scheduled before the ones ahead of it complete, so the stop can come up to the slots less
    one samples after the bound first falls under the bar.

    Inspect records a task's early stops in its log's results, but a task whose every sample
    stopped (V*Bench's second task, when the first stopped the tier) is logged with neither
    samples nor results; so the tier's account, summary(), is written beside the logs as
    EARLY_STOP_FILE, which evaluate reads instead."""

    def __init__(self, bar: float, size: int):
        self.bar, self.size = bar, size
        self.screening = math.ceil(size / 5)
        self.passes = self.run = 0
        self.stopped: list[str] = []
        self.reason = ""

    async def start_task(self, task, samples, epochs) -> str:
        return "pass_rate_upper_bound"

    async def schedule_sample(self, id, epoch) -> EarlyStop | None:
        if not self.stopped and self.screening <= self.run < self.size:
            bound = upper_bound(self.passes, self.run)
            if bound < self.bar:
                self.reason = f"the 95% upper bound {bound:.4f} on the pass rate ({self.passes}/{self.run}) is below the bar {self.bar}"
        if not self.reason:
            return None
        self.stopped.append(str(id))
        return EarlyStop(id=id, epoch=epoch, reason=self.reason)

    async def complete_sample(self, id, epoch, scores) -> None:
        [score] = scores.values()
        self.run += 1
        if score_value(score.score.value) >= PASS_AT:
            self.passes += 1

    async def complete_task(self) -> dict:
        return self.summary()

    def summary(self) -> dict:
        return {
            "bar": self.bar, "size": self.size, "screening": self.screening, "run": self.run,
            "passes": self.passes, "upper_bound": upper_bound(self.passes, self.run),
            "stopped": self.stopped, "reason": self.reason,
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

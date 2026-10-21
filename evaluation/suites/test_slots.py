"""The suites' --slots, offline: every suite that keeps requests in flight requires it, a whole
number of at least one, and keeps that many in flight on the model, the slots evaluate gives it
from the model's profile entry (slots.py). The Go test in this directory (suites_test.go) runs it
through the pinned uv and Python, offline.
"""

import argparse
import contextlib
import io
import unittest
from concurrent.futures import ThreadPoolExecutor
from unittest import mock

import agent_quality
import mteb_quality
import tau2_quality
import vision_quality
from slots import positive

# Each suite's eval arguments but --slots.
ARGS = {
    agent_quality: ["eval", "--router", "r", "--model", "m", "--tier", "bfcl-single", "--list", "l", "--output", "o"],
    vision_quality: ["eval", "--router", "r", "--model", "m", "--tier", "vstar", "--list", "l", "--output", "o"],
    tau2_quality: ["eval", "--router", "r", "--model", "m", "--domain", "airline", "--list", "l", "--output", "o",
                   "--trials", "2", "--claude", "c", "--claude-config", "d"],
    mteb_quality: ["--router", "r", "--model", "m", "--task", "STSBenchmark", "--prompts", "p", "--output", "o"],
}


def refused(parser: argparse.ArgumentParser, args: list) -> bool:
    """Whether parser refuses args, its usage error kept off the test's output."""
    with contextlib.redirect_stderr(io.StringIO()):
        try:
            parser.parse_args(args)
        except SystemExit:
            return True
    return False


class TestSlots(unittest.TestCase):
    def test_each_suite_takes_slots(self):
        for suite, args in ARGS.items():
            with self.subTest(suite=suite.__name__):
                self.assertEqual(suite.parser().parse_args(args + ["--slots", "2"]).slots, 2)
                self.assertEqual(suite.parser().parse_args(args + ["--slots", "8"]).slots, 8)
                self.assertTrue(refused(suite.parser(), args), "ran without --slots")
                for bad in ("0", "-1", "four"):
                    self.assertTrue(refused(suite.parser(), args + ["--slots", bad]), f"took --slots {bad}")

    def test_positive(self):
        self.assertEqual(positive("3"), 3)
        for bad in ("0", "x", "1.5"):
            with self.assertRaises(argparse.ArgumentTypeError):
                positive(bad)

    def test_encoder_keeps_slots_in_flight(self):
        # The encoder's pool of requests in flight is its --slots: a batch of 3 * 64 inputs goes
        # as three requests on a pool of slots workers.
        workers = []
        real = ThreadPoolExecutor

        def pool(n):
            workers.append(n)
            return real(n)

        enc = mteb_quality.RouterEncoder("http://router", "m", {}, 6)
        sent = []
        with mock.patch.object(mteb_quality, "ThreadPoolExecutor", pool), \
                mock.patch.object(enc, "_embed", lambda texts: sent.append(len(texts)) or [[0.0]] * len(texts)), \
                mock.patch.object(mteb_quality, "get_prompt", lambda *a: ""):
            vectors = enc.encode([{"text": ["t"] * 192}], task_metadata=None, hf_split="test", hf_subset="default")
        self.assertEqual(workers, [6])
        self.assertEqual(sorted(sent), [64, 64, 64])
        self.assertEqual(len(vectors), 192)


if __name__ == "__main__":
    unittest.main()

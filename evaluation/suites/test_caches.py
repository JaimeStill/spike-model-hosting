"""The suites' own result caches, offline: mteb_quality.py evaluates every task without MTEB's
result cache, so a unit evaluate runs is a real rerun and evaluate's results index (package
evaluation/gate) is the only reuse. The Go test in this directory (suites_test.go) runs it through
the pinned uv and Python, offline.
"""

import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import mteb_quality


class TestMTEBCache(unittest.TestCase):
    def test_evaluates_without_result_cache(self):
        with tempfile.TemporaryDirectory() as d:
            prompts = Path(d) / "prompts.json"
            prompts.write_text("{}")
            task = mock.Mock()
            task.metadata.main_score = "cosine_spearman"
            argv = ["mteb_quality.py", "--router", "http://router", "--model", "m", "--task", "STSBenchmark",
                    "--prompts", str(prompts), "--slots", "2", "--output", str(Path(d) / "out")]
            with mock.patch.object(sys, "argv", argv), \
                    mock.patch.object(mteb_quality.mteb, "get_task", return_value=task), \
                    mock.patch.object(mteb_quality.mteb, "evaluate") as evaluate:
                mteb_quality.main()
            evaluate.assert_called_once()
            self.assertIn("cache", evaluate.call_args.kwargs, "mteb.evaluate called with its default cache")
            self.assertIsNone(evaluate.call_args.kwargs["cache"])
            meta = json.loads((Path(d) / "out" / "encoder.json").read_text())
            self.assertEqual(meta["main_score"], "cosine_spearman")


if __name__ == "__main__":
    unittest.main()

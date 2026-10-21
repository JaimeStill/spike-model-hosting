// Package suites is the quality suites' uv project, evaluation/suites, seen from Go: it embeds
// the project's pyproject.toml, so the versions the project pins are read from the one place
// they are set. The quality stage (package evaluation/gate) checks that a result was written by
// the pinned MTEB or Inspect AI. Its test also runs the project's own Python tests, test_*.py,
// through the pinned uv and Python, offline: the claude-code Inspect provider against a fake
// claude executable (test_claude_code.py), the tiers' early stops (test_early_stop.py), the
// --slots every suite that keeps requests in flight takes from evaluate (test_slots.py), and MTEB
// run without its result cache (test_caches.py). On a machine without the consumers environment, or without the project's locked packages synced,
// the test fails, naming the commands that set them up, rather than skipping: a passing check
// has always run them.
//
// The API:
//   - Pin returns the exact version pyproject.toml pins a package to.
package suites

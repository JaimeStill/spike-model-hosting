"""Run one τ2 domain with Inspect AI on the router's agent model: a seeded task list of
inspect_evals' stock τ2 task, airline or retail, each task run for several trials, its simulated
user Sonnet through the laptop's pinned Claude Code (claude_code.py's provider).

    uv run --project evaluation/suites python evaluation/suites/tau2_quality.py eval \
        --router http://framework:8080 --model ggml-org/gpt-oss-120b-GGUF:MXFP4 \
        --domain airline --list evaluation/tiers/deep-tau2-airline.txt --trials 2 \
        --claude /path/to/pinned/claude --claude-config .state/claude --output DIR

    uv run --project evaluation/suites python evaluation/suites/tau2_quality.py ids --domain retail

eval runs the domain and writes its Inspect JSON log under DIR; evaluate's deep stage (package
evaluation/gate) reads it. ids prints the domain's dataset, as a tier list's "# dataset" header,
then every task ID of it the domain draws from: the pool the command evaluation/cmd/tierlist
draws a list from (mise run //evaluation:deep-tiers).

The tasks are inspect_evals' own tau2_airline and tau2_retail, unforked, unshuffled, their
samples picked by Inspect's sample-id filtering from the list. Their data ships inside
inspect_evals, from tau2-bench at TAU2_COMMIT, and each draws from its domain's base split (50
airline tasks, 114 retail). Telecom is left out: its simulated user needs tools.

  - The agent under test is the model, as the router serves it, through Inspect's openai-api
    provider at <router>/v1. The stock task sends it τ2's agent prompt and the domain's policy,
    and offers the domain's tools at temperature 0, its setting; nothing else is set.
  - The simulated user is the stock task's "user" model role, claude-code/sonnet: Claude Code in
    print mode on the subscription login of --claude-config, tools off, the task's simulator
    prompt as its whole system prompt, no session persistence (claude_code.py). The stock task
    asks it for temperature 0, which Claude Code can't take.
  - Scoring is the stock task's: the database the agent leaves against the one the task's
    required actions produce, and the information the task needs the agent to tell the user,
    found in what it said. inspect_evals' airline scorer has no model grader (its natural-language
    assertions aren't judged), so no grader runs, and the simulator is the run's only Claude
    Code use.

Each task runs --trials times (Inspect's epochs), so the log holds one sample a task and trial,
from which evaluate takes pass^k. Four samples run at once, the router's slots; the simulator
keeps at most its provider's two calls in flight. The log's metadata names the domain, its
dataset, the trials, the simulator, and the Claude Code version that simulated.
"""

import argparse
import subprocess
import sys

import claude_code  # noqa: F401  (registers the claude-code provider)
from inspect_ai import eval as inspect_eval
from inspect_ai.model import GenerateConfig, get_model
from inspect_evals.tau2 import tau2_airline, tau2_retail
from inspect_evals.tau2.tau2 import EVAL_VERSION

from pass_stop import read_list

# The tau2-bench commit inspect_evals' τ2 data and domains are adapted from.
TAU2_COMMIT = "377cecff73b873e63430a0acfce00e0eff024855"

# The domains, each its stock task.
DOMAINS = {"airline": tau2_airline, "retail": tau2_retail}

# The simulator, as an Inspect model.
SIMULATOR = "claude-code/sonnet"

# The router's slots: the samples a domain runs at once.
SLOTS = 4


def dataset(domain: str) -> str:
    """The domain's dataset, as its tier list's header names it."""
    return f"inspect_evals tau2@{EVAL_VERSION.full_version} {domain} base split, sierra-research/tau2-bench@{TAU2_COMMIT}"


def main():
    p = argparse.ArgumentParser()
    sub = p.add_subparsers(dest="cmd", required=True)
    pi = sub.add_parser("ids", help="print every task ID a domain draws from")
    pi.add_argument("--domain", required=True, choices=DOMAINS)
    pe = sub.add_parser("eval", help="run a domain with Inspect and write its JSON log")
    for flag in ("--router", "--model", "--list", "--output", "--claude", "--claude-config"):
        pe.add_argument(flag, required=True)
    pe.add_argument("--domain", required=True, choices=DOMAINS)
    pe.add_argument("--trials", type=int, required=True)
    a = p.parse_args()

    stock = DOMAINS[a.domain](shuffle=False)
    if a.cmd == "ids":
        print(f"# dataset {dataset(a.domain)}")
        print("\n".join(str(s.id) for s in stock.dataset))
        return

    listed, ids = read_list(a.list)
    if listed != dataset(a.domain):
        raise SystemExit(f"{a.list} is drawn from {listed!r}, not the domain's {dataset(a.domain)!r}")
    held = {str(s.id) for s in stock.dataset}
    if missing := sorted(set(ids) - held):
        raise SystemExit(f"{a.domain}: {len(missing)} listed IDs aren't in the domain's base split, such as {missing[0]}")
    version = subprocess.run([a.claude, "--version"], capture_output=True, text=True, check=True).stdout.split()[0]

    # The router needs no key; autoload=false keeps every request from loading a model.
    agent = get_model(
        f"openai-api/router/{a.model}", base_url=a.router.rstrip("/") + "/v1", api_key="none",
        default_query={"autoload": "false"},
    )
    user = get_model(SIMULATOR, command=a.claude, config_dir=a.claude_config,
                     config=GenerateConfig(max_connections=claude_code.MAX_CONNECTIONS))
    [log] = inspect_eval(
        stock, model=agent, model_roles={"user": user}, sample_id=ids, epochs=a.trials,
        log_dir=a.output, log_format="json",
        metadata={
            "domain": a.domain, "dataset": dataset(a.domain), "trials": a.trials,
            "simulator": SIMULATOR, "claude_code_version": version, "simulator_settings": claude_code.GENERATE,
        },
        max_connections=SLOTS, max_samples=SLOTS, display="plain",
    )
    if log.status != "success":
        sys.exit(f"inspect eval tau2 {a.domain}: {log.status}: {log.error.message if log.error else ''}")


if __name__ == "__main__":
    main()

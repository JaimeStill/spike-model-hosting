"""Run one BFCL tier with Inspect AI on the router's agent model: a seeded tier list of
inspect_evals' stock BFCL task, its single-turn or its multi-turn categories.

    uv run --project evaluation/suites python evaluation/suites/agent_quality.py eval \
        --router http://framework:8080 --model ggml-org/gpt-oss-120b-GGUF:MXFP4 \
        --tier bfcl-single --list evaluation/tiers/agent-bfcl-single.txt --slots 4 --output DIR [--bar 0.8]

    uv run --project evaluation/suites python evaluation/suites/agent_quality.py ids --tier bfcl-multi

eval runs the tier through Inspect's stock openai-api provider, which offers BFCL's functions as
OpenAI chat tools, the router's shape, and writes the task's Inspect JSON log under DIR;
evaluate's quality stage (package evaluation/gate) reads it. ids prints the tier's dataset, as a
tier list's "# dataset" header, then every sample ID of it the tier draws from: the pool the
command evaluation/cmd/tierlist draws a tier list from.

The task is inspect_evals' own bfcl, unforked: the tier's categories are its categories
parameter, its samples are picked with Inspect's sample-id filtering, and the early stop is set
on the stock task with task_with. bfcl pins the Gorilla repository's commit its data comes from
(BFCL_GITHUB_COMMIT), which the dataset header names, so a tier list names the data it was drawn
from. The data, and the multi-turn categories' backend classes, download by a sparse git clone
into INSPECT_EVALS_CACHE_DIR's BFCL/ on first use.

  - bfcl-single: the leaderboard's single-turn categories, BFCL v1 and v2 (live): simple
    Python, Java and JavaScript, multiple, parallel, parallel multiple and irrelevance, and
    their live counterparts with live relevance. Each sample is one user turn, its functions
    offered as tools, scored by BFCL's AST match of the calls the model makes (Inspect captures
    them without executing them), or, for relevance and irrelevance, by whether it calls at
    all. Left out: rest, which inspect_evals doesn't implement, and the exec and sql categories,
    which the Gorilla repository keeps under unused_datasets, retired from the leaderboard.
  - bfcl-multi: the leaderboard's multi-turn categories, BFCL v3: base, missing function,
    missing parameter and long context. Each sample is a conversation of user turns against
    BFCL's stateful backends (a file system, a trading bot, travel booking and the rest), the
    model's tool calls executed in Inspect's tool loop, scored by the backends' final state and
    the calls' results against the ground truth's. Left out: composite, under unused_datasets
    and outside the leaderboard's multi-turn score.

Generation: the agent model as it is served, at the router's defaults: the card's sampling and
the template's default reasoning (gpt-oss's medium), since BFCL measures the model as the
consumers get it; no temperature, reasoning or token limit is set, only parallel tool calls
allowed (GENERATE). --slots requests in flight, the model's slots (slots.py); the early stop
judges samples as they complete.

Early stop (pass_stop.py's PassStop, with a bar): the tier's screening fifth runs whole, then,
before each further sample, the tier stops failed once the one-sided 95% Wilson upper bound on
its pass rate is below the bar, and passed once the lower bound is at or over it; between the
two it runs on. A BFCL sample scores 1 or 0, so the pass rate is the tier's accuracy. The tier's
account of its early stop, the side it stopped on with it, is written beside the log as
early_stop.json. Without a bar the list runs whole: evaluate's full run sends the samples its
screen didn't run as a list of their own, with no bar.
"""

import argparse
import contextlib
import json
import sys
from pathlib import Path

from inspect_ai import task_with
from inspect_ai import eval as inspect_eval
from inspect_ai.model import GenerateConfig, get_model
from inspect_evals.bfcl import bfcl
from inspect_evals.bfcl.bfcl import BFCL_GITHUB_COMMIT

from pass_stop import EARLY_STOP_FILE, PassStop, read_list
from slots import add_slots

# The tiers' categories, as the leaderboard groups them.
SINGLE_TURN = [
    "simple_python", "simple_java", "simple_javascript", "multiple", "parallel", "parallel_multiple", "irrelevance",
    "live_simple", "live_multiple", "live_parallel", "live_parallel_multiple", "live_relevance", "live_irrelevance",
]
MULTI_TURN = ["multi_turn_base", "multi_turn_miss_func", "multi_turn_miss_param", "multi_turn_long_context"]

# Each tier's categories, and its dataset as a tier list's header names it.
TIERS = {
    "bfcl-single": (SINGLE_TURN, f"ShishirPatil/gorilla@{BFCL_GITHUB_COMMIT} BFCL single-turn: {','.join(SINGLE_TURN)}"),
    "bfcl-multi": (MULTI_TURN, f"ShishirPatil/gorilla@{BFCL_GITHUB_COMMIT} BFCL multi-turn: {','.join(MULTI_TURN)}"),
}

# The request's one setting: parallel tool calls allowed. llama.cpp's server defaults
# parallel_tool_calls to false and then keeps a turn's first call alone, so a model that answers
# BFCL's parallel questions with several calls would be cut to one; allowing them is the request
# offering what the question asks, not constraining the answer. On gpt-oss-120b it changes
# nothing: harmony emits one call a message, and its parallel and parallel-multiple questions
# scored 0/11 with it allowed, as without.
GENERATE = GenerateConfig(parallel_tool_calls=True)


def parser() -> argparse.ArgumentParser:
    """The command's arguments: ids, or eval with the model's --slots, the requests a tier keeps in
    flight."""
    p = argparse.ArgumentParser()
    sub = p.add_subparsers(dest="cmd", required=True)
    pi = sub.add_parser("ids", help="print every sample ID a tier draws from")
    pi.add_argument("--tier", required=True, choices=TIERS)
    pe = sub.add_parser("eval", help="run a tier with Inspect and write its JSON log")
    for flag in ("--router", "--model", "--tier", "--list", "--output"):
        pe.add_argument(flag, required=True)
    pe.add_argument("--bar", type=float)
    add_slots(pe)
    return p


def main():
    a = parser().parse_args()

    if a.tier not in TIERS:
        raise SystemExit(f"no tier {a.tier!r}; the tiers are {', '.join(TIERS)}")
    categories, dataset = TIERS[a.tier]
    # The loader prints its progress to stdout, which ids keeps for the IDs alone.
    with contextlib.redirect_stdout(sys.stderr):
        stock = bfcl(categories=categories)
    if a.cmd == "ids":
        print(f"# dataset {dataset}")
        print("\n".join(str(s.id) for s in stock.dataset))
        return

    listed, ids = read_list(a.list)
    if listed != dataset:
        raise SystemExit(f"{a.list} is drawn from {listed!r}, not the tier's {dataset!r}")
    held = {str(s.id) for s in stock.dataset}
    if missing := sorted(set(ids) - held):
        raise SystemExit(f"{a.tier}: {len(missing)} listed IDs aren't in the tier's categories, such as {missing[0]}")
    stop = PassStop(a.bar, len(ids)) if a.bar is not None else None
    task = task_with(stock, config=GENERATE, early_stopping=stop)

    # The router needs no key; autoload=false keeps every request from loading a model.
    model = get_model(
        f"openai-api/router/{a.model}", base_url=a.router.rstrip("/") + "/v1", api_key="none",
        default_query={"autoload": "false"},
    )
    [log] = inspect_eval(
        task, model=model, sample_id=ids, log_dir=a.output, log_format="json",
        metadata={"tier": a.tier, "dataset": dataset},
        max_connections=a.slots, max_samples=a.slots, display="plain",
    )
    if log.status != "success":
        sys.exit(f"inspect eval {a.tier}: {log.status}: {log.error.message if log.error else ''}")
    if stop is not None:
        (Path(a.output) / EARLY_STOP_FILE).write_text(json.dumps(stop.summary(), indent=1) + "\n")


if __name__ == "__main__":
    main()

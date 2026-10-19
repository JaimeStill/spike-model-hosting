"""Run one vision tier with Inspect AI on the router's vision model: the probe's image five times,
or a seeded tier list of a stock inspect_evals benchmark, V*Bench, DocVQA or MMMU.

    uv run --project evaluation/suites python evaluation/suites/vision_quality.py eval \
        --router http://framework:8080 --model ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0 \
        --tier vstar --list evaluation/tiers/vision-vstar.txt --output DIR [--bar 0.7]

    uv run --project evaluation/suites python evaluation/suites/vision_quality.py eval \
        --router ... --model ... --tier probe --image hosting/probe/testdata/shapes.png --output DIR

    uv run --project evaluation/suites python evaluation/suites/vision_quality.py ids --tier mmmu

    uv run --project evaluation/suites python evaluation/suites/vision_quality.py peak \
        --tiers evaluation/tiers --output DIR

eval runs the tier through Inspect's stock openai-api provider, which sends each image as an
OpenAI chat image_url part, the router's shape, and writes one Inspect JSON log a task under DIR;
evaluate's quality stage (package evaluation/gate) reads them. ids prints the benchmark's
dataset, as a tier list's "# dataset" header, then every sample ID of it the tier draws from: the
pool the command evaluation/cmd/tierlist draws a tier list from. peak finds the largest image,
by pixels, that the benchmarks' committed tier lists (vision-<tier>.txt under --tiers) send, and
copies it into DIR as peak.<ext> beside peak.json, which names its tier, sample, file and size:
the image evaluate's warm-up sends the vision model before a run, to drive its image encoder to
its high-water mark. The server caps each image at its ubatch's tokens and encodes each in a batch
of its own (the profile's mtmd-batch-max-tokens), so the largest image is the largest batch the
encoder runs.

The benchmarks are inspect_evals' own tasks, unforked: the tier's samples are picked with
Inspect's sample-id filtering, and the generation settings and the early stop are set on the
stock task with task_with. Each pins its dataset's revision in inspect_evals itself, which ids
reads, so a tier list names the revision it was drawn from.

  - vstar: V*Bench (craigwu/vstar_bench, test), both of its tasks, attribute recognition (115)
    and spatial relationship reasoning (76), multiple choice, scored by choice(). Its IDs are
    unique across the two, so a tier list holds them bare and eval scopes each to its task
    (Inspect's <task>:<id> selector); the attribute questions run first, then the spatial.
  - docvqa: DocVQA (lmms-lab-encoder/DocVQA, validation; the test split's answers are held
    back), free-form "ANSWER: ...", scored by ANLS: per question 1 less the least normalized
    Levenshtein distance to an accepted answer, or 0 when that distance is 0.5 or more. The task
    runs unshuffled, so the tier's questions go in the dataset's order.
  - mmmu: MMMU (MMMU/MMMU, validation, all 30 subjects), its multiple-choice task alone, 847 of
    the 900 questions: the open questions' task grades with model_graded_fact, a second model's
    judgement, and the vision model would be grading itself. 42 of the multiple-choice
    questions carry two to five images, which the router takes in one request; they stay in.

The probe tier asks the probe's question of the probe's image, five times, and passes a sample
whose answer names each of its three shapes and three colours: a pass^5 smoke, gated 5 of 5.

Generation: the benchmarks at temperature 0 with reasoning off (the Gemma 4 template's
enable_thinking, which defaults to on; templates without the switch ignore it), the prompt cache
on, and room for an answer; the probe as a client sends it, at the router's defaults, the card's
sampling and the template's default reasoning, since pass^5 measures the served model's
reliability. Four requests in flight, the profile's slots; the early stop judges samples as they
complete.

Early stop (pass_stop.py's PassStop, shared with agent_quality.py, with a bar): the tier's
screening fifth runs whole, then, before each further sample, the tier stops failed once the
one-sided 95% Wilson upper bound on its pass rate is below the bar, and passed once the Wilson
lower bound on its mean score is at or over it; between the two it runs on. A sample passes at a
score of 0.5 or more: a correct choice, or an ANLS of 0.5 or more, which ANLS's own threshold
makes any ANLS above 0. The bar judges the tier's mean score, the accuracy or the mean ANLS; a
sample's ANLS is never above its pass, so the mean ANLS is never above the pass rate, and a
bound that puts the pass rate under the bar puts the mean ANLS under it too; the lower bound is
on the mean score itself. The tier's account of its early stop, the samples it stopped, why, and
the side it stopped on, is written beside the logs as early_stop.json. Without a bar the list
runs whole: evaluate's full run sends the samples its screen didn't run as a list of their own,
with no bar, and a task that holds none of them is left out.
"""

import argparse
import contextlib
import json
import shutil
import sys
from pathlib import Path

from PIL import Image

from inspect_ai import Task, task, task_with
from inspect_ai import eval as inspect_eval
from inspect_ai.dataset import MemoryDataset, Sample
from inspect_ai.model import ChatMessageUser, ContentImage, ContentText, GenerateConfig, get_model
from inspect_ai.scorer import CORRECT, INCORRECT, Score, Target, accuracy, scorer
from inspect_ai.solver import TaskState, generate
from inspect_evals.docvqa import docvqa
from inspect_evals.docvqa.docvqa import DATASET_PATH as DOCVQA_REPO
from inspect_evals.docvqa.docvqa import DOCVQA_DATASET_REVISION
from inspect_evals.mmmu import mmmu_multiple_choice
from inspect_evals.mmmu.utils import MMMU_DATASET_REVISION
from inspect_evals.vstar_bench import vstar_bench_attribute_recognition, vstar_bench_spatial_relationship_reasoning
from inspect_evals.vstar_bench.vstar_bench import HUGGINGFACE_REPO_ID as VSTAR_REPO
from inspect_evals.vstar_bench.vstar_bench import VSTAR_BENCH_DATASET_REVISION

from pass_stop import EARLY_STOP_FILE, PassStop, read_list

# The tiers' tasks, in the order they run, and each one's dataset as a tier list's header names
# it.
BENCHMARKS = {
    "vstar": ([vstar_bench_attribute_recognition, vstar_bench_spatial_relationship_reasoning],
              f"{VSTAR_REPO}@{VSTAR_BENCH_DATASET_REVISION} test"),
    "docvqa": ([lambda: docvqa(shuffle=False)], f"{DOCVQA_REPO}@{DOCVQA_DATASET_REVISION} DocVQA validation"),
    "mmmu": ([mmmu_multiple_choice], f"MMMU/MMMU@{MMMU_DATASET_REVISION} validation multiple-choice"),
}

# The benchmarks' generation: greedy, the closest a run comes to repeating its score (four
# requests batched together still vary: one V*Bench question flipped between two runs), reasoning
# off, and room for the working Gemma 4 writes into its answer before the "ANSWER: $LETTER" or
# short phrase the task asks for, even with reasoning off. At 1024 tokens, 32 of the MMMU tier's
# 200 answers were cut off before their answer line (scored wrong) and MMMU scored 66.0%, under
# the card's harder MMMU Pro 73.8%; rerun at 4096, 17 of those 32 answered right and 8 still ran
# out, looping, and the whole tier at 4096 scored 72.5%, 12 of its answers running out. Reasoning off, though the template's default is on: on the first 40 questions of the
# MMMU tier, at four in flight, reasoning off took 211 s, 20.7 s a request, and scored 27/40;
# reasoning on wrote ~2000 tokens a question, 83 s a request, and after 50 minutes had logged 9,
# 7 right where reasoning off had 6. A gain that small, at four times the time, doesn't fit the
# tier's budget.
GENERATE = GenerateConfig(
    temperature=0,
    max_tokens=4096,
    extra_body={"chat_template_kwargs": {"enable_thinking": False}},
)

# The probe's request and check (hosting/probe's router test): shapes.png is a red circle, a
# blue square and a green triangle; the answer must name each, in any phrasing.
PROBE_PROMPT = "Describe this image in one sentence."
PROBE_WORDS = ["circle", "square", "triangle", "red", "blue", "green"]
PROBE_REPEATS = 5

# The router's slots: the requests a tier keeps in flight.
SLOTS = 4


@scorer(metrics=[accuracy()])
def names_shapes():
    """The probe's check: the answer names each shape and colour."""

    async def score(state: TaskState, target: Target) -> Score:
        text = state.output.completion.lower()
        missing = [w for w in PROBE_WORDS if w not in text]
        return Score(
            value=CORRECT if not missing else INCORRECT, answer=state.output.completion,
            explanation=f"doesn't name {', '.join(missing)}" if missing else None,
        )

    return score


@task
def vision_probe(image: str):
    """The probe's image and question, PROBE_REPEATS times, at the router's defaults."""
    samples = [
        Sample(id=f"probe-{i + 1}", input=[ChatMessageUser(content=[ContentText(text=PROBE_PROMPT), ContentImage(image=image)])])
        for i in range(PROBE_REPEATS)
    ]
    return Task(dataset=MemoryDataset(samples, name="probe"), solver=generate(), scorer=names_shapes(),
                config=GenerateConfig(max_tokens=4096))


def peak(tiers: Path, output: Path) -> dict:
    """The largest image, by pixels, any benchmark's tier list sends, copied into output beside
    peak.json; the first of equal size, in BENCHMARKS' and each list's order, wins."""
    best = None
    for name, (makers, dataset) in BENCHMARKS.items():
        path = tiers / f"vision-{name}.txt"
        listed, ids = read_list(path)
        if listed != dataset:
            raise SystemExit(f"{path} is drawn from {listed!r}, not the tier's {dataset!r}")
        want = set(ids)
        # The loaders print their progress to stdout, which holds the result alone.
        with contextlib.redirect_stdout(sys.stderr):
            samples = [s for make in makers for s in make().dataset if str(s.id) in want]
        for s in samples:
            messages = s.input if isinstance(s.input, list) else []
            for m in messages:
                for c in m.content if isinstance(m.content, list) else []:
                    if not isinstance(c, ContentImage):
                        continue
                    with Image.open(c.image) as im:
                        width, height = im.size
                    if best is None or width * height > best["width"] * best["height"]:
                        best = {"tier": f"vision-{name}", "id": str(s.id), "source": c.image, "width": width, "height": height}
    if best is None:
        raise SystemExit(f"no image in the tier lists under {tiers}")
    output.mkdir(parents=True, exist_ok=True)
    best["file"] = "peak" + Path(best["source"]).suffix.lower()
    shutil.copyfile(best["source"], output / best["file"])
    (output / "peak.json").write_text(json.dumps(best, indent=1) + "\n")
    return best


def main():
    p = argparse.ArgumentParser()
    sub = p.add_subparsers(dest="cmd", required=True)
    pi = sub.add_parser("ids", help="print every sample ID of a benchmark")
    pi.add_argument("--tier", required=True, choices=BENCHMARKS)
    pp = sub.add_parser("peak", help="find the largest image the tier lists send")
    pp.add_argument("--tiers", required=True)
    pp.add_argument("--output", required=True)
    pe = sub.add_parser("eval", help="run a tier with Inspect and write its JSON logs")
    for flag in ("--router", "--model", "--tier", "--output"):
        pe.add_argument(flag, required=True)
    pe.add_argument("--list", help="the tier list, for a benchmark")
    pe.add_argument("--image", help="the probe's image, for the probe")
    pe.add_argument("--bar", type=float)
    a = p.parse_args()

    if a.cmd == "ids":
        makers, dataset = BENCHMARKS[a.tier]
        # The loaders print their progress to stdout, which holds the IDs alone.
        with contextlib.redirect_stdout(sys.stderr):
            ids = [str(s.id) for make in makers for s in make().dataset]
        print(f"# dataset {dataset}")
        print("\n".join(ids))
        return

    if a.cmd == "peak":
        print(json.dumps(peak(Path(a.tiers), Path(a.output))))
        return

    if a.tier == "probe":
        if not a.image:
            raise SystemExit("the probe tier needs --image")
        tasks, sample_id, stop = [vision_probe(image=a.image)], None, None
        metadata = {"tier": "probe", "dataset": "hosting/probe/testdata/shapes.png"}
    elif a.tier in BENCHMARKS:
        makers, dataset = BENCHMARKS[a.tier]
        listed, ids = read_list(a.list)
        if listed != dataset:
            raise SystemExit(f"{a.list} is drawn from {listed!r}, not the tier's {dataset!r}")
        stop = PassStop(a.bar, len(ids)) if a.bar is not None else None
        tasks = [task_with(make(), config=GENERATE, early_stopping=stop) for make in makers]
        # Each ID scoped to the task that holds it, so no task warns of IDs that are another's;
        # a task holding none of the list's, as a full run's rest can leave V*Bench's, is left out.
        held = {t.name: {str(s.id) for s in t.dataset} for t in tasks}
        tasks = [t for t in tasks if held[t.name] & set(ids)]
        held = {t.name: held[t.name] for t in tasks}
        sample_id = [f"{name}:{i}" for name, owned in held.items() for i in ids if i in owned]
        if len(sample_id) != len(ids):
            missing = sorted(set(ids) - set().union(*held.values()))
            raise SystemExit(f"{a.tier}: {len(missing)} listed IDs aren't in the benchmark, such as {missing[0]}")
        metadata = {"tier": a.tier, "dataset": dataset}
    else:
        raise SystemExit(f"no tier {a.tier!r}; the tiers are probe, {', '.join(BENCHMARKS)}")

    # The router needs no key; autoload=false keeps every request from loading a model.
    model = get_model(
        f"openai-api/router/{a.model}", base_url=a.router.rstrip("/") + "/v1", api_key="none",
        default_query={"autoload": "false"},
    )
    logs = inspect_eval(
        tasks, model=model, sample_id=sample_id, log_dir=a.output, log_format="json", metadata=metadata,
        max_tasks=1, max_connections=SLOTS, max_samples=SLOTS, display="plain", log_images=False,
    )
    for log in logs:
        if log.status != "success":
            sys.exit(f"inspect eval {a.tier} {log.eval.task}: {log.status}: {log.error.message if log.error else ''}")
    if stop is not None:
        (Path(a.output) / EARLY_STOP_FILE).write_text(json.dumps(stop.summary(), indent=1) + "\n")


if __name__ == "__main__":
    main()

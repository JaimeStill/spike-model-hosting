"""Transcribe one tier of utterances from an Open ASR Leaderboard English set with Inspect AI, on
the router's audio model, and score it by word error rate.

    uv run --project evaluation/suites python evaluation/suites/asr_quality.py eval \
        --router http://framework:8080 --model ggml-org/gemma-4-E4B-it-GGUF:Q8_0 \
        --set librispeech-clean --tier evaluation/tiers/asr-librispeech-clean.txt \
        --output DIR [--bar 0.05]

    uv run --project evaluation/suites python evaluation/suites/asr_quality.py ids --set ami

eval runs the tier through Inspect's stock openai-api provider, which sends each clip as an
OpenAI chat input_audio part, the router's shape, one request at a time, and writes Inspect's
JSON log under DIR; evaluate's quality stage (package evaluation/gate) reads it. ids prints the
set's dataset, as a tier list's "# dataset" header, then every utterance ID of it the leaderboard
scores: the pool the command evaluation/cmd/tierlist draws a tier list from.

The sets are the leaderboard's English test sets in hf-audio/esb-datasets-test-only-sorted, which
its run scripts load, and FLEURS English (google/fleurs, en_us test), each at a fixed revision. A
set's parquet files download, whole, into the Hugging Face cache under HF_HOME; each tier's clips
are kept as WAV under ASR_CACHE, so a rerun reads neither the parquet nor the network again.

Scoring is the leaderboard's: both texts through its English normalizer (asr_normalizer, vendored),
then kaldialign's edit distance with merged compounds, and the set's WER is the corpus WER, every
utterance's word errors over every reference word.
"""

import argparse
import glob
import hashlib
import io
import json
import math
import os
import sys
from dataclasses import dataclass
from pathlib import Path

import pyarrow.parquet as pq
import soundfile as sf
from huggingface_hub import snapshot_download
from inspect_ai import Task, task
from inspect_ai import eval as inspect_eval
from inspect_ai.dataset import MemoryDataset, Sample
from inspect_ai.model import ChatMessageUser, ContentAudio, ContentText, GenerateConfig, get_model
from inspect_ai.scorer import Metric, SampleScore, Score, Target, metric, scorer
from inspect_ai.solver import TaskState, generate
from inspect_ai.util import EarlyStop
from kaldialign import edit_distance

from asr_normalizer import SOURCE as NORMALIZER
from asr_normalizer import EnglishTextNormalizer

ESB = ("hf-audio/esb-datasets-test-only-sorted", "de79e57adf9983993bcc72586d7313460825f5eb")
FLEURS = ("google/fleurs", "70bb2e84b976b7e960aa89f1c648e09c59f894dd")


@dataclass(frozen=True)
class AsrSet:
    """A set: its repository and revision, its test split's parquet files, and its reference
    text's column. ESB's sets carry a unique id column; FLEURS repeats an id for each speaker of
    a sentence, so its utterance ID is the clip's file name, path's stem."""

    repo: str
    revision: str
    files: str
    text: str
    id_from_path: bool = False

    @property
    def dataset(self) -> str:
        """The set as a tier list's dataset header names it."""
        return f"{self.repo}@{self.revision} {self.files}"


SETS = {
    "librispeech-clean": AsrSet(*ESB, "librispeech/test.clean-*.parquet", "text"),
    "librispeech-other": AsrSet(*ESB, "librispeech/test.other-*.parquet", "text"),
    "voxpopuli": AsrSet(*ESB, "voxpopuli/test-*.parquet", "text"),
    "ami": AsrSet(*ESB, "ami/test-*.parquet", "text"),
    "earnings22": AsrSet(*ESB, "earnings22/test-*.parquet", "text"),
    "fleurs-en": AsrSet(*FLEURS, "parquet-data/en_us/test-*.parquet", "raw_transcription", True),
}

# The card's ASR prompt (https://huggingface.co/google/gemma-4-E4B-it, "6. Audio"), the language
# filled in, without its formatting bullets ("Only output the transcription, with no newlines";
# "When transcribing numbers, write the digits"). On a clip of a word or two, gemma-4-E4B reads
# the bullets back as the transcript: on AMI's 40 shortest tier clips the full prompt scored
# 165% WER and the sentence alone 47%, and on 40 LibriSpeech test.other utterances both scored
# 6.3%. The normalizer writes numbers as digits whichever way they come, so the bullets don't
# bear on the score. The prompt goes first and the clip after it, the card's modality order for
# audio ("4. Modality order"), not the clip-first shape the fitness and capacity stages send:
# on those LibriSpeech utterances clip-first scored 8.4%, dropping the start of some, and on the
# short AMI clips it read the bullets back 17 times in 40.
PROMPT = "Transcribe the following speech segment in English into English text."

# Greedy decoding, room for the longest clip's transcript, and the router's settings for a timed
# transcription: the prompt cache off (llama.cpp's cache_prompt) and reasoning off (the
# template's enable_thinking), as the capacity stage sends them.
GENERATE = GenerateConfig(
    temperature=0,
    max_tokens=1024,
    extra_body={"cache_prompt": False, "chat_template_kwargs": {"enable_thinking": False}},
)

normalize = EnglishTextNormalizer()


def scored(reference: str) -> bool:
    """Whether the leaderboard scores an utterance: its normalized reference isn't empty, nor
    the "ignore time segment in scoring" marker (data_utils.is_target_text_in_range)."""
    norm = normalize(reference).strip()
    return norm != "" and norm != "ignore time segment in scoring"


def parquet_files(spec: AsrSet) -> list[str]:
    root = snapshot_download(spec.repo, repo_type="dataset", revision=spec.revision, allow_patterns=[spec.files])
    files = sorted(glob.glob(os.path.join(root, spec.files)))
    if not files:
        raise SystemExit(f"{spec.dataset}: no parquet files")
    return files


def rows(spec: AsrSet, columns: list[str]):
    """Each row of the set's parquet, the columns named, with its utterance ID as "uid"."""
    key = "path" if spec.id_from_path else "id"
    for f in parquet_files(spec):
        for batch in pq.ParquetFile(f).iter_batches(batch_size=256, columns=sorted({key, *columns})):
            for row in batch.to_pylist():
                row["uid"] = Path(row["path"]).stem if spec.id_from_path else row["id"]
                yield row


def ids(spec: AsrSet) -> list[str]:
    return [r["uid"] for r in rows(spec, [spec.text]) if scored(r[spec.text])]


def read_tier(path: str) -> tuple[str, list[str]]:
    """A tier list's dataset header and IDs (package evaluation/tier's format)."""
    dataset, uids = "", []
    for line in Path(path).read_text().splitlines():
        line = line.strip()
        if line.startswith("# dataset "):
            dataset = line.removeprefix("# dataset ")
        elif line and not line.startswith("#"):
            uids.append(line)
    return dataset, uids


def clips(name: str, spec: AsrSet, uids: list[str]) -> dict[str, dict]:
    """Each utterance's reference, length and WAV file, kept under ASR_CACHE/<set>@<revision>.

    The clip is sent as WAV, the format Inspect's ContentAudio and the router both take: FLEURS's
    original WAV bytes as they are, and ESB's FLAC decoded and written as WAV in the FLAC's own
    PCM depth (16-bit throughout), which is lossless. The length is the original file header's."""
    cache = Path(os.environ["ASR_CACHE"]) / f"{name}@{spec.revision[:12]}"
    manifest = cache / "manifest.json"
    known = json.loads(manifest.read_text()) if manifest.exists() else {}
    want = {u for u in uids if u not in known or not Path(known[u]["wav"]).exists()}
    if want:
        cache.mkdir(parents=True, exist_ok=True)
        for r in rows(spec, ["audio", spec.text]):
            if r["uid"] not in want:
                continue
            raw = r["audio"]["bytes"]
            info = sf.info(io.BytesIO(raw))
            # Named by a hash of the ID: Earnings-22's IDs hold a slash.
            wav = cache / (hashlib.sha256(r["uid"].encode()).hexdigest()[:24] + ".wav")
            if info.format == "WAV":
                wav.write_bytes(raw)
            else:
                data, rate = sf.read(io.BytesIO(raw))
                sf.write(wav, data, rate, format="WAV", subtype=info.subtype)
            known[r["uid"]] = {"text": r[spec.text], "audio_s": info.frames / info.samplerate, "wav": str(wav)}
            want.discard(r["uid"])
        if want:
            raise SystemExit(f"{name}: {len(want)} tier IDs aren't in {spec.dataset}, such as {sorted(want)[0]}")
        manifest.write_text(json.dumps(known, indent=1) + "\n")
    return {u: known[u] for u in uids}


@metric
def corpus_wer() -> Metric:
    """Every utterance's word errors over every reference word, as the leaderboard scores a set."""

    def compute(scores: list[SampleScore]) -> float:
        errors = sum(s.score.metadata["errors"] for s in scores)
        words = sum(s.score.metadata["words"] for s in scores)
        return errors / words if words else float("nan")

    return compute


@scorer(metrics=[corpus_wer()])
def wer():
    """One utterance's word errors against its reference, after the leaderboard's normalizer;
    its value is the utterance's own WER, and its metadata the counts the corpus WER sums."""

    async def score(state: TaskState, target: Target) -> Score:
        ref, hyp = normalize(target.text), normalize(state.output.completion)
        d = edit_distance(ref.split(), hyp.split(), merge_compounds=True)
        return Score(
            value=d["total"] / d["ref_len"] if d["ref_len"] else float(d["total"]),
            answer=state.output.completion,
            metadata={
                "errors": d["total"], "words": d["ref_len"], "ins": d["ins"], "del": d["del"],
                "sub": d["sub"], "reference": ref, "hypothesis": hyp,
            },
        )

    return score


# The standard normal quantile of a one-sided 95% bound.
Z95 = 1.6448536269514722


def wer_lower_bound(errors: list[int], words: list[int]) -> float:
    """The one-sided 95% lower bound on a corpus WER from the utterances run so far.

    The corpus WER is a ratio estimator, R = sum(e) / sum(w), over utterances sampled from the
    set; its standard error is the delta method's, utterance-clustered:
    SE^2 = sum((e_i - R w_i)^2) / (m (m - 1) wbar^2), wbar the mean reference length of the m
    utterances. Words within an utterance aren't independent, so a per-word binomial bound would
    be too narrow; the clustered one isn't. The bound is R - z SE, normal-approximate."""
    m, words_total = len(errors), sum(words)
    if m < 2 or words_total == 0:
        return 0.0
    r = sum(errors) / words_total
    wbar = words_total / m
    var = sum((e - r * w) ** 2 for e, w in zip(errors, words)) / (m * (m - 1) * wbar * wbar)
    return max(0.0, r - Z95 * math.sqrt(var))


class WERStop:
    """Inspect early stopping on a WER bar, lower being better: the screening fifth runs whole,
    then, before each further utterance, the set stops once the lower bound on its corpus WER
    (wer_lower_bound) is above the bar, since no utterance still to come could bring the WER to
    the bar with that confidence. It needs the utterances sent one at a time, in order."""

    def __init__(self, bar: float):
        self.bar = bar
        self.errors: list[int] = []
        self.words: list[int] = []
        self.size = self.screening = 0
        self.stopped_at: int | None = None
        self.bound = 0.0

    async def start_task(self, task, samples, epochs) -> str:
        self.size, self.screening = len(samples), math.ceil(len(samples) / 5)
        return "wer_lower_bound"

    async def schedule_sample(self, id, epoch) -> EarlyStop | None:
        run = len(self.errors)
        if self.stopped_at is None and self.screening <= run < self.size:
            self.bound = wer_lower_bound(self.errors, self.words)
            if self.bound > self.bar:
                self.stopped_at = run
        if self.stopped_at is None:
            return None
        return EarlyStop(id=id, epoch=epoch, reason=f"the 95% lower bound {self.bound:.4f} on the WER is above the bar {self.bar}")

    async def complete_sample(self, id, epoch, scores) -> None:
        md = scores["wer"].score.metadata
        self.errors.append(md["errors"])
        self.words.append(md["words"])

    async def complete_task(self) -> dict:
        return {
            "bar": self.bar, "size": self.size, "screening": self.screening, "run": len(self.errors),
            "stopped": self.stopped_at is not None, "lower_bound": wer_lower_bound(self.errors, self.words),
        }


@task
def asr(name: str, tier: str, bar: float | None = None):
    """One tier of the set name's utterances, transcribed and scored by WER; a bar stops it
    early."""
    spec = SETS[name]
    dataset, uids = read_tier(tier)
    if dataset != spec.dataset:
        raise SystemExit(f"{tier} is drawn from {dataset!r}, not the set's {spec.dataset!r}")
    found = clips(name, spec, uids)
    samples = [
        Sample(
            id=u,
            input=[ChatMessageUser(content=[ContentText(text=PROMPT), ContentAudio(audio=found[u]["wav"], format="wav")])],
            target=found[u]["text"],
            metadata={"audio_s": found[u]["audio_s"]},
        )
        for u in uids
    ]
    return Task(
        dataset=MemoryDataset(samples, name=name),
        solver=generate(),
        scorer=wer(),
        config=GENERATE,
        early_stopping=WERStop(bar) if bar is not None else None,
        metadata={"set": name, "dataset": spec.dataset, "normalizer": NORMALIZER, "prompt": PROMPT},
    )


def main():
    p = argparse.ArgumentParser()
    sub = p.add_subparsers(dest="cmd", required=True)
    pi = sub.add_parser("ids", help="print every scored utterance ID of a set")
    pi.add_argument("--set", required=True, choices=SETS)
    pe = sub.add_parser("eval", help="transcribe a tier with Inspect and write its JSON log")
    for flag in ("--router", "--model", "--set", "--tier", "--output"):
        pe.add_argument(flag, required=True)
    pe.add_argument("--bar", type=float)
    a = p.parse_args()
    if a.cmd == "ids":
        # The dataset header first, as a tier list carries it, then the IDs, for tierlist.
        print(f"# dataset {SETS[a.set].dataset}")
        for u in ids(SETS[a.set]):
            print(u)
        return
    if a.set not in SETS:
        raise SystemExit(f"no set {a.set!r}; the sets are {', '.join(SETS)}")
    # The router needs no key; autoload=false keeps every request from loading a model.
    model = get_model(
        f"openai-api/router/{a.model}", base_url=a.router.rstrip("/") + "/v1", api_key="none",
        default_query={"autoload": "false"},
    )
    # One request at a time, in the list's order: RTFx is single-stream, and early stopping
    # judges the utterances in order.
    [log] = inspect_eval(
        asr(name=a.set, tier=a.tier, bar=a.bar), model=model, log_dir=a.output, log_format="json",
        max_connections=1, max_samples=1, display="plain", log_images=False,
    )
    if log.status != "success":
        sys.exit(f"inspect eval {a.set}: {log.status}: {log.error.message if log.error else ''}")


if __name__ == "__main__":
    main()

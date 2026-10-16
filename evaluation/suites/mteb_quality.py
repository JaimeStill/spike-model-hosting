"""Run one MTEB task on the router's embedding model, through an encoder that adds the model's
task prompts, and leave MTEB's result JSON in the output directory.

    uv run --project evaluation/suites python evaluation/suites/mteb_quality.py \
        --router http://framework:8080 --model ggml-org/embeddinggemma-2-GGUF:Q8_0 \
        --task STSBenchmark --output DIR

evaluate's quality stage (package evaluation/gate) runs it once a task and reads, under DIR, the
task's <task>.json that MTEB writes, and encoder.json: the task's main score name and the prompt
the encoder applied for each prompt type it was asked for. The datasets come from Hugging Face,
cached under HF_HOME, which evaluate sets under the state directory.
"""

import argparse
import json
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from urllib.request import Request, urlopen

import mteb
import numpy as np
from mteb.models.abs_encoder import AbsEncoder, get_prompt
from mteb.models.model_meta import ModelMeta, ScoringFunction

# EmbeddingGemma 2's prompts: the "prompts" of its sentence-transformers configuration,
# https://huggingface.co/google/embeddinggemma-2/blob/main/config_sentence_transformers.json,
# the card's "Task Instruction Prefixes" table (https://huggingface.co/google/embeddinggemma-2)
# as prompt names. Kept are the keys MTEB resolves (task types, prompt types, and type-prompt
# compounds), verbatim; the card's own names (SearchQuery, Document, FactChecking, ...) are
# sentence-transformers' prompt_name values, which MTEB never looks up. MTEB picks one by its
# order (mteb.models.abs_encoder.get_prompt_name): task-prompt type, task, type-prompt type,
# type, prompt type.
PROMPTS = {
    "BitextMining": "task: search result | query: ",
    "Classification": "task: classification | query: ",
    "Clustering": "task: clustering | query: ",
    "InstructionRetrieval": "task: code retrieval | query: ",
    "MultilabelClassification": "task: classification | query: ",
    "PairClassification": "task: sentence similarity | query: ",
    "Reranking": "task: search result | query: ",
    "Retrieval": "task: search result | query: ",
    "Retrieval-document": "title: none | text: ",
    "Retrieval-query": "task: search result | query: ",
    "STS": "task: sentence similarity | query: ",
    "Summarization": "task: sentence similarity | query: ",
    "document": "title: none | text: ",
    "query": "task: search result | query: ",
}
DOCUMENT_PROMPT = PROMPTS["document"]

# Inputs a request, and requests in flight: the profile's four slots, each request well inside
# what one answer carries. Each input is one sequence of at most the model's c = b = ub = 8192
# tokens, so the router never splits one across physical batches; a longer one fails its request,
# and so the task, rather than being cut short silently. None of the four tasks has one.
BATCH, IN_FLIGHT = 64, 4


class RouterEncoder(AbsEncoder):
    """An MTEB encoder on the router's OpenAI-compatible /v1/embeddings, prompts added."""

    def __init__(self, router: str, model: str):
        self.url, self.model = router.rstrip("/") + "/v1/embeddings?autoload=false", model
        self.model_prompts = PROMPTS
        self.mteb_model_meta = ModelMeta.create_empty(
            {"name": model, "revision": "router", "similarity_fn_name": ScoringFunction.COSINE})
        self.applied = {}  # prompt type -> the prompt applied, for encoder.json

    def encode(self, inputs, *, task_metadata, hf_split, hf_subset, prompt_type=None, **kwargs):
        prompt = get_prompt(PROMPTS, task_metadata, prompt_type) or ""
        texts = []
        for batch in inputs:
            if prompt == DOCUMENT_PROMPT and "body" in batch:
                # The card's document form: a corpus item's own title where it has one, as
                # "title: {title} | text: {content}", else title: none.
                titles = batch.get("title") or [None] * len(batch["body"])
                texts += [f"title: {t or 'none'} | text: {b}" for t, b in zip(titles, batch["body"])]
                self.applied[prompt_type.value if prompt_type else "all"] = "title: {title or none} | text: "
            else:
                texts += [prompt + t for t in batch["text"]]
                self.applied[prompt_type.value if prompt_type else "all"] = prompt
        chunks = [texts[i:i + BATCH] for i in range(0, len(texts), BATCH)]
        with ThreadPoolExecutor(IN_FLIGHT) as pool:
            vectors = [v for chunk in pool.map(self._embed, chunks) for v in chunk]
        return np.asarray(vectors, dtype=np.float32)

    def _embed(self, texts):
        body = json.dumps({"model": self.model, "input": texts, "encoding_format": "float"}).encode()
        req = Request(self.url, body, {"Content-Type": "application/json"})
        with urlopen(req, timeout=600) as resp:
            data = json.load(resp)["data"]
        return [d["embedding"] for d in sorted(data, key=lambda d: d["index"])]


def main():
    p = argparse.ArgumentParser()
    for flag in ("--router", "--model", "--task", "--output"):
        p.add_argument(flag, required=True)
    a = p.parse_args()
    out = Path(a.output)
    out.mkdir(parents=True, exist_ok=True)
    task = mteb.get_task(a.task)
    enc = RouterEncoder(a.router, a.model)
    # No result cache: every run evaluates in full, and MTEB's TaskResult is written here instead.
    result = mteb.evaluate(enc, task, cache=None, co2_tracker=False)
    result.task_results[0].to_disk(out / f"{a.task}.json")
    meta = {"main_score": task.metadata.main_score, "prompts": enc.applied}
    (out / "encoder.json").write_text(json.dumps(meta, indent=2) + "\n")


if __name__ == "__main__":
    main()

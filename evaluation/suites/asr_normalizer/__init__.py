"""The Open ASR Leaderboard's English text normalizer, vendored.

normalizer.py and english_abbreviations.py are copied verbatim from the leaderboard's repository
at commit 1b85e2790d532b074888caa4add3ee5bef2f60cd:

    https://github.com/huggingface/open_asr_leaderboard/tree/1b85e2790d532b074888caa4add3ee5bef2f60cd/normalizer

They are Whisper's EnglishTextNormalizer as the leaderboard extends it (filler words, compound
words, spellings and names), under the Apache License 2.0 in LICENSE, the leaderboard's. They are
vendored rather than installed because no package publishes them: the leaderboard's own package
pulls in its dataset loading, and transformers' copy brings torch. They need only the regex
package. SOURCE names the commit, which asr_quality.py records in every log.
"""

from .normalizer import EnglishTextNormalizer

SOURCE = (
    "huggingface/open_asr_leaderboard@1b85e2790d532b074888caa4add3ee5bef2f60cd"
    " normalizer/normalizer.py EnglishTextNormalizer"
)

__all__ = ["EnglishTextNormalizer", "SOURCE"]

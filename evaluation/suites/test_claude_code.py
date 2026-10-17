"""The claude-code provider (claude_code.py) against a fake claude executable, with no network and
no login: the command line and environment it runs Claude Code with, the prompt it sends, the
reported model it records, and its retries. The Go test in this directory (suites_test.go) runs
it through the pinned uv and Python, offline:

    uv run --project evaluation/suites --locked --offline python -m unittest test_claude_code

The fake answers each call with the next of the responses a test queues, and logs each call's
arguments, stdin, working directory and the environment's ANTHROPIC_ and CLAUDE variables.
"""

import asyncio
import json
import os
import stat
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path

from inspect_ai import Task
from inspect_ai import eval as inspect_eval
from inspect_ai.dataset import Sample
from inspect_ai.model import (
    ChatMessageAssistant,
    ChatMessageSystem,
    ChatMessageUser,
    get_model,
)
from inspect_ai.solver import generate
from inspect_ai.tool import ToolInfo

import claude_code

# The fake: pops the next queued response from FAKE_RESPONSES, logs the call to FAKE_LOG, and
# prints the response's stdout, exiting with its code.
FAKE = textwrap.dedent("""\
    #!{python}
    import json, os, sys
    queue = os.environ["FAKE_RESPONSES"]
    responses = json.load(open(queue))
    response, rest = responses[0], responses[1:]
    json.dump(rest, open(queue, "w"))
    import select, stat, time
    began = time.time()
    # Claude Code waits 3 seconds for stdin, then gives up; the fake waits 0.2.
    if not select.select([sys.stdin], [], [], 0.2)[0]:
        sys.stdout.write("Warning: no stdin data received in 3s, proceeding without it.")
        sys.exit(1)
    if "sleep" in response:
        time.sleep(response["sleep"])
    with open(os.environ["FAKE_LOG"], "a") as log:
        log.write(json.dumps({{
            "args": sys.argv[1:], "stdin_file": stat.S_ISREG(os.fstat(0).st_mode), "stdin": sys.stdin.read(), "cwd": os.getcwd(), "began": began, "ended": time.time(),
            "env": {{k: v for k, v in os.environ.items() if k.startswith(("ANTHROPIC_", "CLAUDE"))}},
        }}) + "\\n")
    sys.stdout.write(response["stdout"])
    sys.exit(response.get("code", 0))
""")

SONNET = "claude-sonnet-5-5"


def success(text: str, model: str = SONNET) -> dict:
    """A successful call's response, as Claude Code 2.1.301's --output-format json writes it."""
    return {"stdout": json.dumps({
        "type": "result", "subtype": "success", "is_error": False, "api_error_status": None,
        "result": text, "duration_ms": 1591, "num_turns": 1,
        "modelUsage": {model: {"inputTokens": 2, "outputTokens": 21, "cacheReadInputTokens": 0,
                               "cacheCreationInputTokens": 616, "costBasis": "list"}},
    })}


def failure(status: int) -> dict:
    """A call that failed at the API with status."""
    return {"code": 1, "stdout": json.dumps({
        "type": "result", "subtype": "success", "is_error": True, "api_error_status": status,
        "result": f"API Error: {status}", "modelUsage": {},
    })}


class ClaudeCodeTest(unittest.TestCase):
    def setUp(self):
        self.dir = Path(tempfile.mkdtemp())
        self.claude = self.dir / "claude"
        self.claude.write_text(FAKE.format(python=sys.executable))
        self.claude.chmod(self.claude.stat().st_mode | stat.S_IXUSR)
        self.config = self.dir / "config"
        self.log = self.dir / "log"
        self.queue = self.dir / "responses"
        os.environ["FAKE_LOG"], os.environ["FAKE_RESPONSES"] = str(self.log), str(self.queue)
        # The caller's own Claude and Anthropic settings, which must never reach the call.
        self.saved = {k: os.environ.get(k) for k in ("ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "CLAUDECODE")}
        os.environ.update({"ANTHROPIC_API_KEY": "sk-not-this", "ANTHROPIC_BASE_URL": "http://router", "CLAUDECODE": "1"})

    def tearDown(self):
        for k, v in self.saved.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v

    def respond(self, *responses: dict):
        self.queue.write_text(json.dumps(list(responses)))

    def calls(self) -> list[dict]:
        if not self.log.exists():
            return []
        return [json.loads(line) for line in self.log.read_text().splitlines()]

    def model(self, **args):
        return get_model("claude-code/sonnet", command=str(self.claude), config_dir=str(self.config), retry_wait=0.01, **args)

    def test_call(self):
        """One call runs print mode on Sonnet, tools, MCP, skills, settings and session persistence
        off, the conversation's system prompt in place of Claude Code's, from an empty directory, on
        the login of its configuration directory and no other credential; it answers with the
        result and names the model Claude Code reported."""
        self.respond(success("I need to change my flight."))
        out = asyncio.run(self.model().generate([
            ChatMessageSystem(content="You are a customer."), ChatMessageUser(content="Begin."),
        ]))
        self.assertEqual(out.completion, "I need to change my flight.")
        self.assertEqual(out.model, SONNET)
        self.assertEqual(out.metadata["claude_code_model"], SONNET)
        self.assertEqual(out.usage.output_tokens, 21)
        [call] = self.calls()
        self.assertEqual(call["args"], [
            "-p", "--model", "sonnet", "--tools", "", "--strict-mcp-config", "--disable-slash-commands",
            "--system-prompt", "You are a customer.", "--no-session-persistence", "--setting-sources", "",
            "--output-format", "json",
        ])
        self.assertEqual(call["stdin"], "Begin.")
        # The prompt is a file on stdin, whole before Claude Code starts, not a pipe this event loop
        # fills: a busy loop (τ2's samples each serialize a database) filled the pipe after Claude
        # Code's 3-second wait for stdin, and it exited.
        self.assertTrue(call["stdin_file"])
        self.assertEqual(os.listdir(call["cwd"]), [])
        self.assertEqual(call["env"], {
            "CLAUDE_CONFIG_DIR": str(self.config), "CLAUDE_CODE_DISABLE_AUTO_MEMORY": "1",
        })

    def test_transcript(self):
        """A conversation of several turns goes as one prompt: the turns tagged, oldest first, then
        the request for the assistant's next message."""
        self.respond(success("Yes, ABC123."))
        asyncio.run(self.model().generate([
            ChatMessageSystem(content="You are a customer."), ChatMessageUser(content="Begin."),
            ChatMessageAssistant(content="Hi, I want to cancel."), ChatMessageUser(content="Your reservation ID?"),
        ]))
        [call] = self.calls()
        stdin = call["stdin"]
        self.assertIn("<user>\nBegin.\n</user>\n<assistant>\nHi, I want to cancel.\n</assistant>\n<user>\nYour reservation ID?\n</user>", stdin)
        self.assertTrue(stdin.rstrip().endswith("nothing else: no tag, no commentary."))
        self.assertEqual(call["args"][call["args"].index("--system-prompt") + 1], "You are a customer.")

    def test_transient_retry(self):
        """An overloaded or rate-limited call is retried, and the retry recorded."""
        self.respond(failure(529), failure(429), success("ok"))
        out = asyncio.run(self.model().generate([ChatMessageUser(content="Begin.")]))
        self.assertEqual(out.completion, "ok")
        self.assertEqual(out.metadata["claude_code_retries"], 2)
        self.assertEqual(len(self.calls()), 3)

    def test_timeout_retried(self):
        """A call past its timeout is killed and retried, as transient."""
        self.respond({"sleep": 5, "stdout": ""}, success("ok"))
        out = asyncio.run(self.model(timeout=0.5).generate([ChatMessageUser(content="Begin.")]))
        self.assertEqual(out.completion, "ok")
        self.assertEqual(out.metadata["claude_code_retries"], 1)

    def test_connections(self):
        """At most two calls run at once, however many generates are in flight."""
        self.respond(*[dict(success("ok"), sleep=0.3)] * 5)
        model = self.model()

        async def five():
            await asyncio.gather(*[model.generate([ChatMessageUser(content=f"Begin {i}.")]) for i in range(5)])

        asyncio.run(five())
        calls = self.calls()
        self.assertEqual(len(calls), 5)
        most = max(sum(1 for o in calls if o["began"] < c["began"] + 0.05 < o["ended"]) for c in calls)
        self.assertEqual(most, 2)

    def test_retries_bounded(self):
        """Transient failures stop being retried after max_retries."""
        self.respond(*[failure(529)] * 3)
        with self.assertRaises(Exception):
            asyncio.run(self.model(max_retries=2).generate([ChatMessageUser(content="Begin.")]))
        self.assertEqual(len(self.calls()), 3)

    def test_no_retry(self):
        """Any other failure isn't retried: an auth error, or output that isn't Claude Code's JSON."""
        for response in (failure(401), {"code": 1, "stdout": "Invalid API key"}):
            self.log.unlink(missing_ok=True)
            self.respond(response, success("never"))
            with self.assertRaises(Exception):
                asyncio.run(self.model().generate([ChatMessageUser(content="Begin.")]))
            self.assertEqual(len(self.calls()), 1)

    def test_tools_refused(self):
        """A generate that offers tools is refused, without running Claude Code."""
        self.respond(success("never"))
        tool = ToolInfo(name="lookup", description="Look up a booking.")
        with self.assertRaises(Exception):
            asyncio.run(self.model().generate([ChatMessageUser(content="Begin.")], tools=[tool]))
        self.assertEqual(self.calls(), [])

    def test_sample_store(self):
        """In an eval, each call is recorded in its sample's store, kept in the log: the calls, each
        reported model with its count, the retries and Claude Code's seconds."""
        self.respond(failure(503), success("one", "claude-sonnet-5-5"), success("two", "claude-sonnet-5-5"))
        task = Task(dataset=[Sample(input="Begin.", id="s1")], solver=[generate(), generate()])
        [log] = inspect_eval(task, model=self.model(), log_dir=str(self.dir / "logs"), display="none")
        self.assertEqual(log.status, "success")
        store = log.samples[0].store
        self.assertEqual(store[claude_code.STORE_CALLS], 2)
        self.assertEqual(store[claude_code.STORE_MODELS], {SONNET: 2})
        self.assertEqual(store[claude_code.STORE_RETRIES], 1)
        self.assertGreater(store[claude_code.STORE_SECONDS], 0)


if __name__ == "__main__":
    unittest.main()

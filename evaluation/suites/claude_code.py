"""An Inspect AI model provider, claude-code, that answers through the laptop's pinned Claude Code
in print mode (`claude -p`) on the subscription login: the deep stage's τ2 user simulator, as
Sonnet, `claude-code/sonnet` (tau2_quality.py). Importing this module registers it.

    model = get_model("claude-code/sonnet", command=CLAUDE, config_dir=CONFIG_DIR)

Each generate runs Claude Code once, as a plain chat model, with no API key and no cloud API of
its own: the binary the consumers mise environment pins (command; evaluate passes it, found as
the conform tier finds it, consumers.Executable), signed in to the architect's subscription in the
spike's own configuration directory (config_dir, the state directory's claude/, which conform's
Claude Code uses too). The pinned version's flags (claude --help, 2.1.301) set each part of that:

  - -p: print mode, non-interactive: one answer to the prompt on stdin, then exit.
  - --model <model>: the model, the provider's model name (sonnet, Claude Code's alias for its
    newest Sonnet).
  - --tools "": no built-in tool at all (`Use "" to disable all tools`); --strict-mcp-config with
    no --mcp-config, no MCP server either; --disable-slash-commands, no skill. A generate that
    offers tools is refused: the provider is a model without tools.
  - --system-prompt <prompt>: the conversation's own system messages replace Claude Code's
    system prompt, rather than --append-system-prompt adding to it.
  - --no-session-persistence: the session is never saved to disk, so it can't be resumed, and
    no call sees another's.
  - --setting-sources "": no user, project or local settings, so no hooks, permissions or
    CLAUDE.md the settings would bring; the working directory is an empty one of the provider's
    own, so no project CLAUDE.md is found either.
  - --output-format json: one JSON result, whose modelUsage names the model Claude Code reports
    serving the call, its usage, and, on an error, api_error_status.

Its environment drops every ANTHROPIC_ and CLAUDE variable the caller has (an API key, a base URL
pointing elsewhere, an enclosing Claude Code session's), then sets CLAUDE_CONFIG_DIR to
config_dir, and, as spike-harness-driver's Claude Code driver does, turns auto memory and the
self-updater off.

A conversation of more than one message after the system prompt goes as one prompt, since print
mode takes a single user turn: the turns so far as a tagged transcript, oldest first, then the
request for the assistant's next message (render). Claude Code takes no temperature or seed, so a
config's are not applied; GENERATE notes it.

Rate and volume: at most max_connections calls at once (MAX_CONNECTIONS, two, by default), and a
call that fails transiently (an overloaded, rate-limited or server-error status, or a timeout) is
retried after RETRY_WAIT seconds, doubling, at most MAX_RETRIES times; any other failure is not
retried. Inspect's own retries are off for this provider (should_retry), so the backoff is this
module's alone.

Each call is recorded in its sample's store (Inspect's per-sample record, kept in the log): the
calls (STORE_CALLS), each reported model ID with its count (STORE_MODELS), the retries
(STORE_RETRIES) and the seconds Claude Code took (STORE_SECONDS). Each call's ModelOutput names the
reported model too, with Claude Code's usage, cost basis and duration in its metadata.

Its test, test_claude_code.py, points command at a fake claude executable.
"""

import asyncio
import json
import os
import tempfile
import time
from typing import Any

from inspect_ai.model import (
    ChatMessage,
    ChatMessageAssistant,
    ChatMessageSystem,
    ChatMessageTool,
    GenerateConfig,
    ModelAPI,
    ModelOutput,
    ModelUsage,
    modelapi,
)
from inspect_ai.tool import ToolChoice, ToolInfo
from inspect_ai.util import store

# The provider's name, as a model string's prefix: claude-code/sonnet.
PROVIDER = "claude-code"

# The default bound on calls in flight: low, since every call is the architect's subscription.
MAX_CONNECTIONS = 2

# A transient failure's retries, and the first retry's wait in seconds, doubling after.
MAX_RETRIES = 4
RETRY_WAIT = 15.0

# The bound on one call, in seconds.
TIMEOUT = 300.0

# The API statuses retried: request timeout, rate limit, server errors and overloaded.
TRANSIENT_STATUSES = {408, 429, 500, 502, 503, 504, 529}

# The sample store's keys.
STORE_CALLS = "claude_code:calls"
STORE_MODELS = "claude_code:models"
STORE_RETRIES = "claude_code:retries"
STORE_SECONDS = "claude_code:seconds"

# The system prompt when a conversation brings none, so Claude Code's own never applies.
DEFAULT_SYSTEM = "You are a helpful assistant."

# The settings a caller can't give Claude Code, noted where a run records its settings.
GENERATE = "temperature, seed and token limits are not applied: Claude Code's print mode takes none"


class ClaudeCodeError(Exception):
    """A Claude Code call that failed: its API status, when it reported one, and whether a retry
    may succeed."""

    def __init__(self, message: str, status: int | None = None, transient: bool = False):
        super().__init__(message)
        self.status, self.transient = status, transient


def render(messages: list[ChatMessage]) -> tuple[str, str]:
    """The system prompt and the prompt for a conversation: its system messages joined, and its
    other messages, one user message alone as it is, or else as a tagged transcript, oldest
    first, followed by the request for the assistant's next message."""
    system = [m.text for m in messages if isinstance(m, ChatMessageSystem)]
    turns = [m for m in messages if not isinstance(m, ChatMessageSystem)]
    for m in turns:
        if isinstance(m, ChatMessageTool) or (isinstance(m, ChatMessageAssistant) and m.tool_calls):
            raise ClaudeCodeError("the claude-code provider has no tools, and the conversation holds a tool call")
    if not turns:
        raise ClaudeCodeError("the conversation has no message to answer")
    if len(turns) == 1 and turns[0].role == "user":
        prompt = turns[0].text
    else:
        lines = ["The conversation so far, oldest first; you are the assistant.", "", "<transcript>"]
        for m in turns:
            lines += [f"<{m.role}>", m.text, f"</{m.role}>"]
        lines += ["</transcript>", "", "Write the assistant's next message, and nothing else: no tag, no commentary."]
        prompt = "\n".join(lines)
    return "\n\n".join(system) or DEFAULT_SYSTEM, prompt


def environment(config_dir: str) -> dict[str, str]:
    """Claude Code's environment: the caller's without any ANTHROPIC_ or CLAUDE variable, then the
    configuration directory, auto memory off and the self-updater off."""
    env = {k: v for k, v in os.environ.items() if not k.startswith(("ANTHROPIC_", "CLAUDE"))}
    env.update({
        "CLAUDE_CONFIG_DIR": config_dir,
        "CLAUDE_CODE_DISABLE_AUTO_MEMORY": "1",
        "DISABLE_AUTOUPDATER": "1",
    })
    return env


def arguments(command: str, model: str, system: str) -> list[str]:
    """The command line of one call; each flag's purpose is in the module's docstring."""
    return [
        command, "-p",
        "--model", model,
        "--tools", "",
        "--strict-mcp-config",
        "--disable-slash-commands",
        "--system-prompt", system,
        "--no-session-persistence",
        "--setting-sources", "",
        "--output-format", "json",
    ]


def parse(stdout: str, returncode: int, stderr: str) -> dict[str, Any]:
    """The call's JSON result, or a ClaudeCodeError: transient for a transient API status."""
    try:
        result = json.loads(stdout)
    except json.JSONDecodeError:
        raise ClaudeCodeError(f"claude exited {returncode} without a JSON result: {(stderr or stdout).strip()[:500]}")
    status = result.get("api_error_status")
    if result.get("is_error") or result.get("subtype") != "success" or returncode != 0:
        transient = isinstance(status, int) and status in TRANSIENT_STATUSES
        raise ClaudeCodeError(
            f"claude exited {returncode}, {result.get('subtype')}, status {status}: {str(result.get('result', ''))[:500]}",
            status=status, transient=transient,
        )
    if not result.get("modelUsage"):
        raise ClaudeCodeError("claude's result names no model")
    return result


def reported_model(result: dict[str, Any]) -> str:
    """The model Claude Code reports serving the call: modelUsage's, the one with the most output
    tokens when it names several."""
    usage = result["modelUsage"]
    return max(usage, key=lambda m: usage[m].get("outputTokens", 0))


def record(model: str, retries: int, seconds: float) -> None:
    """Adds one call to the active sample's store."""
    s = store()
    s.set(STORE_CALLS, s.get(STORE_CALLS, 0) + 1)
    models = dict(s.get(STORE_MODELS, {}))
    models[model] = models.get(model, 0) + 1
    s.set(STORE_MODELS, models)
    s.set(STORE_RETRIES, s.get(STORE_RETRIES, 0) + retries)
    s.set(STORE_SECONDS, s.get(STORE_SECONDS, 0.0) + seconds)


@modelapi(name=PROVIDER)
class ClaudeCodeAPI(ModelAPI):
    """Claude Code in print mode as an Inspect model. model_args: command, the claude executable
    (required: evaluate passes the pinned one); config_dir, its configuration directory holding the
    subscription login (required); timeout, retry_wait and max_retries, a call's bound and its
    transient retries."""

    def __init__(
        self,
        model_name: str,
        base_url: str | None = None,
        api_key: str | None = None,
        config: GenerateConfig = GenerateConfig(),
        command: str | None = None,
        config_dir: str | None = None,
        timeout: float = TIMEOUT,
        retry_wait: float = RETRY_WAIT,
        max_retries: int = MAX_RETRIES,
    ) -> None:
        super().__init__(model_name=model_name, base_url=base_url, api_key=api_key, config=config)
        if not command or not config_dir:
            raise ValueError("the claude-code provider needs command, the claude executable, and config_dir, its login's directory")
        self.command, self.config_dir = command, config_dir
        self.timeout, self.retry_wait_s, self.retries = timeout, retry_wait, max_retries
        # An empty working directory, so Claude Code finds no project CLAUDE.md.
        self.workdir = tempfile.mkdtemp(prefix="claude-code-")

    def max_connections(self) -> int:
        return MAX_CONNECTIONS

    def should_retry(self, ex: Exception) -> bool:
        # The provider retries transient failures itself (generate); Inspect's retries would add to
        # them.
        return False

    async def generate(
        self,
        input: list[ChatMessage],
        tools: list[ToolInfo],
        tool_choice: ToolChoice,
        config: GenerateConfig,
    ) -> ModelOutput:
        if tools:
            raise ClaudeCodeError("the claude-code provider runs Claude Code with its tools off, and was offered tools")
        system, prompt = render(input)
        args = arguments(self.command, self.model_name, system)
        env = environment(self.config_dir)
        wait = self.retry_wait_s
        began = time.monotonic()
        for attempt in range(self.retries + 1):
            try:
                result = await self.call(args, prompt, env)
                break
            except ClaudeCodeError as e:
                if not e.transient or attempt == self.retries:
                    raise
                await asyncio.sleep(wait)
                wait *= 2
        seconds = time.monotonic() - began
        model = reported_model(result)
        record(model, attempt, seconds)
        usage = result["modelUsage"][model]
        output = ModelOutput.from_content(model=model, content=str(result.get("result", "")))
        output.usage = ModelUsage(
            input_tokens=usage.get("inputTokens", 0),
            output_tokens=usage.get("outputTokens", 0),
            total_tokens=usage.get("inputTokens", 0) + usage.get("outputTokens", 0),
            input_tokens_cache_read=usage.get("cacheReadInputTokens"),
            input_tokens_cache_write=usage.get("cacheCreationInputTokens"),
        )
        output.time = seconds
        output.metadata = {
            "claude_code_model": model,
            "claude_code_models": sorted(result["modelUsage"]),
            "claude_code_retries": attempt,
            "claude_code_duration_ms": result.get("duration_ms"),
            "claude_code_cost_basis": usage.get("costBasis"),
        }
        return output

    async def call(self, args: list[str], prompt: str, env: dict[str, str]) -> dict[str, Any]:
        """Runs Claude Code once, the prompt on stdin, and returns its JSON result."""
        proc = await asyncio.create_subprocess_exec(
            *args, stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE,
            env=env, cwd=self.workdir,
        )
        try:
            stdout, stderr = await asyncio.wait_for(proc.communicate(prompt.encode()), self.timeout)
        except TimeoutError:
            proc.kill()
            await proc.wait()
            raise ClaudeCodeError(f"claude took longer than {self.timeout:.0f}s", transient=True)
        return parse(stdout.decode(errors="replace"), proc.returncode or 0, stderr.decode(errors="replace"))

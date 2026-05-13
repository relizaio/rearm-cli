# AI Agent Commands

The `rearm agent` namespace covers the AI-Agent monitoring flow: a
coding agent (Claude Code, Cursor, Codex CLI, GitHub Copilot, Devin,
Amp, …) opens a Session on ReARM at the start of a working window,
emits commit trailers that ReARM uses to attribute every commit /
release back to the session, and closes the session when the work
window ends.

The full design (entities, hierarchy, policies) lives in
[`rearm-saas/backend/ai-plans/agentic/README.md`](https://github.com/relizaio/rearm-saas/blob/agentic/backend/ai-plans/agentic/README.md).
This file documents the **CLI side**.

## Table of Contents

1. [Session lifecycle commands](#1-session-lifecycle-commands)
2. [Commit trailer format](#2-commit-trailer-format)
3. [Shipping commit metadata to ReARM with trailers](#3-shipping-commit-metadata-to-rearm-with-trailers)
4. [Authentication scope (v1)](#4-authentication-scope-v1)

---

## 1. Session lifecycle commands

All session-lifecycle commands live under `rearm agent session`.
They each require a **FREEFORM** API key — v1 does not accept
COMPONENT keys (the agent identity needs to be resolvable
independent of any single component).

### `rearm agent session init`

Opens a new session for the named agent. On first call from a
previously-unbound key, the server auto-registers the agent and
its model ontology row.

```bash
rearm agent session init \
    -i "$REARM_APIKEYID" \
    -k "$REARM_APIKEY" \
    -u "$REARM_URI" \
    --agent-name "Claude Code" \
    --agent-model "claude-sonnet" \
    --agent-model-version "4.5" \
    --agent-vendor "Anthropic" \
    --client-session-id "ci-run-${GITHUB_RUN_ID}" \
    --title "Refactor billing webhook idempotency" \
    --branch "fix/billing-idempotency"
```

Required flags:

- `--agent-name` — display name; identity key for the agent within
  the org (case-insensitive). Multi-root-per-key is allowed: the
  same FREEFORM key can carry several agents distinguished by name.
- `--agent-model` — model name. Auto-upserts a `ModelOntology` row
  on first use.

Optional flags:

- `--agent-model-version` — defaults to `"unknown"`.
- `--agent-vendor` — display-only publisher (e.g. `"Anthropic"`).
- `--agent-icon`, `--agent-color` — dashboard chrome.
- `--client-session-id` — agent-supplied natural session id;
  **must be space-free** so it survives the commit-trailer wire
  format. Defaults to the new row uuid when omitted.
- `--title`, `--branch` — informational metadata for the dashboard.

Output (JSON):

```json
{
  "uuid": "01f8d9c3-…",
  "agent": "8a44b1ce-…",
  "clientSessionId": "ci-run-12345",
  "status": "OPEN",
  "title": "Refactor billing webhook idempotency",
  "branch": "fix/billing-idempotency",
  "startedAt": "2026-05-13T14:42:11Z"
}
```

The `uuid` and `clientSessionId` are what the agent will reference
later (uuid for the `ReARM-Agent` trailer's target session, and
`clientSessionId` for the `ReARM-Agentic-Session` trailer value).

**Idempotency.** Calling `init` twice with the same
`--client-session-id` while an OPEN session for that id exists
returns the existing row instead of inserting a duplicate —
typical agent crash-recovery shape.

### `rearm agent session touch <session-uuid>`

Heartbeat. Bumps `lastActivityAt` so the dashboard's "connected"
pill stays honest. No-op on a CLOSED session.

```bash
rearm agent session touch "01f8d9c3-…"
```

### `rearm agent session close <session-uuid>`

Closes the session. Terminal — a closed session cannot be
re-opened; a subsequent `init` with the same `--client-session-id`
creates a fresh row. Idempotent on already-closed sessions.

```bash
rearm agent session close "01f8d9c3-…"
```

---

## 2. Commit trailer format

Each commit an agent (or sub-agent) authors carries two trailers
in the commit message body, conforming to git's standard trailer
parser:

```
ReARM-Agentic-Session: <clientSessionId>
ReARM-Agent: <agentUuid>
```

- `clientSessionId` — must be **space-free** (alphanumeric, dashes,
  underscores). The on-the-wire encoding bounds trailer values by
  whitespace (see §3 below).
- `agentUuid` — the agent's row uuid (any tree level — the
  ReARM server resolves leaf → root automatically).

Both trailers required for full SCE-to-Session attribution.

Example commit message:

```
checkout: wire OTel SDK and resource attrs

Adds OpenTelemetry instrumentation to the checkout service per the
plan attached to this session.

ReARM-Agentic-Session: ci-run-12345
ReARM-Agent: 8a44b1ce-7e29-4a6f-9c87-1f0a45e9d8b1
```

The agent embeds these trailers via its prompt instructions or a
post-edit hook on every commit it authors during the session.

---

## 3. Shipping commit metadata to ReARM with trailers

ReARM stores only the commit **subject** (`%s` in `git log
--pretty`), never the full body — agent-generated commits routinely
have multi-kilobyte bodies that have no value in the audit log.
To preserve the agent attribution on commits ReARM ingests, the
canonical metadata-collection pattern appends the matching
trailers to the same line as the subject.

### Canonical `git log` invocation

```bash
AGENTIC_TRAILER_FMT='%(trailers:key=ReARM-Agent,key=ReARM-Agentic-Session,unfold,separator=%x20)'

# Single commit (used for the head SCE / --commitmessage flag)
COMMIT_MESSAGE=$(git log -1 \
    --pretty="%s ${AGENTIC_TRAILER_FMT}" \
    "$COMMIT" -- "$REPO_PATH")

# Commit range (used for --commits — base64-encoded multi-row payload)
COMMITS_OUTPUT=$(git log "${LAST_COMMIT}..${COMMIT}" \
    --date=iso-strict \
    --pretty="%H|||%ad|||%s ${AGENTIC_TRAILER_FMT}|||%an|||%ae" \
    -- "$REPO_PATH")
COMMITS_BASE64=$(echo "$COMMITS_OUTPUT" | base64 -w 0)
```

The resulting subject field looks like:

```
checkout: wire OTel SDK and resource attrs ReARM-Agentic-Session: ci-run-12345 ReARM-Agent: 8a44b1ce-7e29-4a6f-9c87-1f0a45e9d8b1
```

Per-commit lines stay newline-delimited and `|||`-field-delimited
as before — only the third field grows when trailers are present.
Commits with no trailers have a single trailing space on the
subject (harmless; the parser tolerates it).

### What ReARM does with this

The backend's commit-trailer parser pulls `ReARM-Agent:` and
`ReARM-Agentic-Session:` off the subject line (whitespace-bounded,
case-insensitive on the key), resolves the leaf agent to its
owning root, looks up the session by
`(org, root, clientSessionId)`, and persists:

- `SourceCodeEntry.agent` — the leaf agent (preserves sub-agent
  identity even when the session belongs to the root).
- `SourceCodeEntry.agentSession` — the resolved session uuid.
- `AgentSession.commits[]` — reverse index of SCEs attributed to
  the session; powers `Release.sessions[]` lookups without
  scanning every SCE.

### When using `rearm-actions`

`rearm-actions/initialize/action.yaml` already emits the canonical
format — no caller-side change required.

### When invoking the CLI directly

Build the `--commitmessage` and `--commits` values using the
canonical pattern above. The CLI passes them through unchanged.

---

## 4. Authentication scope (v1)

`rearm agent session …` commands require a **FREEFORM** API key.
COMPONENT keys are not accepted in v1 — the agent identity needs
to be resolvable independent of any single component, and the
FREEFORM key carries the org context the lookup needs.

The same FREEFORM key may be bound to multiple agents (e.g. one
key serving "Claude Code" and "Cursor Agent" rows in the same
org); the `--agent-name` flag disambiguates on each `init` call.

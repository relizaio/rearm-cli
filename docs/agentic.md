# AI Agent Commands

The `rearm agent` namespace covers the AI-Agent monitoring flow: a
coding agent (Claude Code, Cursor, Codex CLI, GitHub Copilot, Devin,
Amp, …) opens a Session on ReARM at the start of a working window,
emits commit trailers that ReARM uses to attribute every commit /
release back to the session, and closes the session when the work
window ends.

The authoritative runtime contract — what the backend expects an
agent to call and in what order — is served at
`$REARM_URL/api/agents/orientation.md` by every backend that supports
these commands. This file documents the **CLI side**.

## Table of Contents

1. [Session lifecycle commands](#1-session-lifecycle-commands) — `init` / `touch` / `close` / `show` / `inbox`
2. [Commit trailer format](#2-commit-trailer-format)
3. [Shipping commit metadata to ReARM with trailers](#3-shipping-commit-metadata-to-rearm-with-trailers)
4. [Authentication scope (v1)](#4-authentication-scope-v1)
5. [Attaching agentic-report artifacts (orientation / intermediate / final)](#5-attaching-agentic-report-artifacts-orientation--intermediate--final)
6. [Inspecting a release](#6-inspecting-a-release) — `release show` (lifecycle, approvals, vulnerabilities / violations)
7. [Enrolling a signing key](#7-enrolling-a-signing-key) — `enrollkey`

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
    --title "Refactor billing webhook idempotency"
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
- `--title` — informational metadata for the dashboard.

Output (JSON):

```json
{
  "uuid": "01f8d9c3-…",
  "agent": "8a44b1ce-…",
  "clientSessionId": "ci-run-12345",
  "status": "OPEN",
  "title": "Refactor billing webhook idempotency",
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

### `rearm agent session show <session-uuid>`

Returns the full Session shape — `status`, `artifacts`, `commits`,
`releases`, `pullRequests`, and `policyEvents` (each with the embedded
`policy` snapshot: `cel`, `description`, `severity`, `kind`). This is
the "what should I do now?" read at startup and after each inbox
event — the inbox says what changed, this gives the current full
state.

```bash
rearm agent session show "01f8d9c3-…"
```

### `rearm agent session inbox <session-uuid>`

Polls events the agent should react to — release lifecycle moves,
approval verdicts, and policy verdicts scoped to this session. Pass
`--since <cursor>` with the cursor of the most recent event you've
already handled to fetch only newer ones; `--limit` (default/cap 200)
bounds the page; `--kind` (repeatable) filters to specific event kinds.

```bash
rearm agent session inbox "01f8d9c3-…" --since "2026-05-13T14:42:11Z"
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

---

## 5. Attaching agentic-report artifacts (orientation / intermediate / final)

The agentic-policy layer can require that a session has produced
specific artifacts before commits will be accepted. The artifact type
is `AGENTIC_REPORT`; sub-phases are expressed via the `agenticPhase`
**tag** (`ORIENTATION` / `INTERMEDIATE` / `FINAL`), mirroring the
`lifecycle` tag pattern that SBOMs use.

### Tag convention

| Tag key        | Tag values                                | Use                                |
|----------------|-------------------------------------------|------------------------------------|
| `agenticPhase` | `ORIENTATION`                             | filed at session start (briefing) |
| `agenticPhase` | `INTERMEDIATE`                            | mid-session checkpoint            |
| `agenticPhase` | `FINAL`                                   | end-of-session summary            |

The agent can carry additional free-form tags (`topic`, `severity`,
…). Policies only fix the convention for keys they explicitly check.

### Upload + attach (two-step)

Use the existing `rearm addrelease` / `rearm addartifact` paths to
create the artifact (tags + type ride on the standard artifact JSON
shape), then bind the returned uuid to the session with
`rearm agent session add-artifact`.

```bash
# 1. Create the artifact via addrelease (works with FREEFORM keys).
#    The orientation report file can be any format — JSON, markdown,
#    cyclonedx, … ReARM treats it opaquely.
RESP=$(rearm addrelease \
    -i "$REARM_APIKEYID" -k "$REARM_APIKEY" -u "$REARM_URI" \
    --component "$COMPONENT_UUID" \
    --version "0.0.0-session-${CI_RUN_ID}" \
    --branch "$BRANCH" \
    --commit "$COMMIT_SHA" \
    --vcstype git --vcsuri "$REPO_URL" \
    --lifecycle ASSEMBLED \
    --releasearts '[{
        "filePath": "/tmp/orientation.json",
        "type": "AGENTIC_REPORT",
        "storedIn": "REARM",
        "displayIdentifier": "orientation-'"${CI_RUN_ID}"'",
        "tags": [
            {"key": "agenticPhase", "value": "ORIENTATION"}
        ]
    }]')
ARTIFACT_UUID=$(echo "$RESP" | jq -r '.data.addReleaseProgrammatic.artifacts[0]')

# 2. Bind it to the session.
rearm agent session add-artifact "$SESSION_UUID" \
    --artifact-uuid "$ARTIFACT_UUID" \
    -i "$REARM_APIKEYID" -k "$REARM_APIKEY" -u "$REARM_URI"
```

`add-artifact` is idempotent — re-attaching the same artifact uuid
is a no-op. It calls the `sessionAttachArtifactProgrammatic`
GraphQL mutation under the hood; any AGENT-functioned FREEFORM key
authorised on the org can drive it.

### Tags via `addartifact` (no release context)

When the agent only needs to file a standalone artifact (no release
yet), use `rearm addartifact` with the same `tags` shape:

```bash
rearm addartifact \
    -i "$REARM_APIKEYID" -k "$REARM_APIKEY" -u "$REARM_URI" \
    --component "$COMPONENT_UUID" \
    --version "0.0.0-session-${CI_RUN_ID}" \
    --artifacts '[{
        "filePath": "/tmp/final-report.md",
        "type": "AGENTIC_REPORT",
        "storedIn": "REARM",
        "displayIdentifier": "final-'"${CI_RUN_ID}"'",
        "tags": [
            {"key": "agenticPhase", "value": "FINAL"},
            {"key": "topic", "value": "auth-refactor"}
        ]
    }]'
```

### Sample policy that reads the tag

Agentic-policy CEL uses **match-to-block** semantics — the
expression describes the *failure* condition. CEL true → policy
fires (FAILED / WARNING / PENDING). CEL false → PASSED. To require
an orientation-tagged AGENTIC_REPORT, the policy fires when one is
*missing*:

```
!session.artifacts.exists(a,
    a.type == "AGENTIC_REPORT"
    && a.tags.exists(t, t.key == "agenticPhase" && t.value == "ORIENTATION"))
```

Operators craft the policy in the ReARM UI (AI Agents → **Manage
policies**) and pick a kind / severity that matches the gate they
want — the UI's policy editor ships a starter catalogue of sample
policies for the common shapes.

---

## 6. Inspecting a release

### `rearm agent release show <release-uuid>`

Looks up a release the way an agent typically needs it after a
`LIFECYCLE_CHANGE` or `APPROVAL` inbox event, or to read its security
posture. Returns:

- `lifecycle` and `updateEvents[].message` — the human-readable reason
  a CEL gate flipped lifecycle (`"Triggered by '…' (CEL: …)"`).
- `approvalEvents[]` — approval history with reviewer comments.
- `sourceCodeEntryDetails[]` — per-commit attribution + signature state.
- `metrics` — security posture from the latest Dependency-Track scan
  (`metrics.lastScanned`): severity counts (`critical` / `high` /
  `medium` / `low` / `unassigned`), policy-violation totals
  (security / license / operational), and the per-finding lists
  `vulnerabilityDetails[]` (`purl`, `vulnId`, `severity`,
  `analysisState`) and `violationDetails[]` (`purl`, `type`, `license`,
  `analysisState`).

Pass either `--session <session-uuid>` or `--client-session-id <id>`
(the value from your commit trailer):

```bash
rearm agent release show "998ea056-…" --session "01f8d9c3-…"
```

**Authorization.** A release **attributed to your session** (one of
the session's commits traces through to it) needs no extra permission —
the FREEFORM key owning the session is enough. A release **not**
attributed to your session is only returned if the calling key has
explicit `RESOURCE` read permission on its component / product.

---

## 7. Enrolling a signing key

### `rearm agent enrollkey`

Binds a public key to an agent so the verifier can match the agent's
signed commits (commits attributed via the `ReARM-Agent:` trailer
verify only when the signature matches a key enrolled here). An agent
can bootstrap its **own** key on first run — no operator step needed.

```bash
rearm agent enrollkey \
    --org "$ORG_UUID" \
    --agent "$AGENT_UUID" \
    --format SSH \
    --pubkey-file ~/.ssh/agent_signing_key.pub \
    --identity "agent@your-org.example"
```

Required: `--agent`, `--org`, `--format` (`SSH` or `GPG`),
`--pubkey-file`. The fingerprint is derived locally from the pubkey
via `ssh-keygen` / `gpg`; pass `--fingerprint` to skip that local-tool
dependency. For SSH, `--identity` is required and must match the
allowed-signers principal the verifier checks (usually the agent
email).

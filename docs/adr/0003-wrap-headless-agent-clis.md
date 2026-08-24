# ADR-0003: Sessions wrap headless agent CLIs; Dispatch builds no agent loop

- Status: accepted
- Date: 2026-08-24

## Context

Something has to run the actual agent loop inside a session. Four options:

1. Build the loop on the Anthropic API / Agent SDK.
2. Use Anthropic **Managed Agents** (hosted loop; optionally a self-hosted sandbox worker
   that executes tools on our infra; scheduled deployments; webhooks).
3. Adopt an agent framework (LangGraph and friends).
4. Wrap existing agent CLIs headless — `claude -p` today, t3code's equivalent as soon as
   its headless surface is confirmed, other vendors' CLIs later.

The decisive requirement: **"utilize my current subscriptions."** Options 1–3 are all
API-token-billed. Only the CLIs authenticate with consumer subscriptions (Claude Pro/Max
OAuth). A Max plan that is already paid for is the cheapest frontier-model compute
available to this project by a wide margin.

Verified headless facts (Claude Code docs, 2026-08-24) that shape the runner:

- `claude -p` with `--output-format stream-json --verbose --include-partial-messages`
  yields a machine-readable transcript stream; the final `result` line carries session id
  and a cost estimate (`total_cost_usd`).
- **`--bare` cannot use subscription auth** (it never reads OAuth credentials). Runners
  therefore run *non-bare*, with OAuth credentials mounted from a Secret and per-agent
  config (CLAUDE.md, hooks, MCP servers) coming from the workspace — which is a feature:
  workspace config *is* the agent's persistent memory and toolbelt.
- `--permission-mode dontAsk` plus explicit `--allowedTools` rules gives a locked-down,
  non-interactive permission profile suited to unattended pods.
- SIGTERM → exit 143 with the turn unfinished; `--resume <session-id>` continues it, even
  from another directory. This is the pod-eviction recovery mechanism.
- `--append-system-prompt-file`, `--mcp-config`, `--agents`, `--json-schema` cover prompt
  injection points, tool wiring, and structured results without touching CLI internals.

## Decision

Option 4. A Dispatch **runner** is a container image + a small shim (`dispatch-run`) with
this contract:

```
in:  /task/task.json        rendered prompt, event payload, budgets, output spec
     workspace mount        NAS-backed project cache (git clone inside)
     credentials Secret     keys injected as env (CLAUDE_CODE_OAUTH_TOKEN) and
                            mounted at /credentials for file-shaped creds
out: transcript.jsonl       streamed to NAS as the session runs
     result.json            outcome, usage, cost estimate, artifacts list, summary
     git branches           dispatch/<session-id> pushed to the project remote
     OTel                   metrics/logs to the collector (native in Claude Code)
```

The shim owns: checkout/refresh, invoking the CLI with the flags above, relaying SIGTERM
as SIGINT first (finish the turn) then resuming on reschedule, scrubbing known secret
patterns from the transcript, and posting status back to the Session CR.

Anything satisfying the contract is a valid runner — which is how other subscriptions
(Copilot, Codex, Gemini CLIs) join later without touching the operator, and how a Managed
Agents-backed runner could exist as an *opt-in, API-billed* flavor if a workload ever
wants Anthropic-hosted execution.

## Consequences

- Subscription economics work; the control plane stays deterministic and free (ADR-0004).
- Sessions inherit the whole CLI ecosystem for free: skills, hooks, MCP servers, plugins —
  configured per-workspace, versioned in git.
- We accept less mid-turn control than owning the loop (acceptable: hooks + MCP +
  `input-required` handling cover the known needs), and we accept tracking a fast-moving
  CLI surface (mitigated by pinning the CLI version in the runner image and upgrading
  deliberately; the flagged future change of `--bare` becoming the `-p` default is exactly
  the kind of thing the pin absorbs).
- Terms-of-service posture: personal automation on a personal subscription, with budgets
  and window-awareness, not resale or multi-tenant service. Revisit if Anthropic's terms
  move.

## Alternatives rejected

- **Agent SDK loop** — maximal control, API-billed, and we'd own context management,
  permissions, and a tool harness that Claude Code already ships. Wrong layer for a
  two-person-hours-a-week homelab project.
- **Managed Agents** — genuinely close on paper (sessions, environments, self-hosted
  sandbox workers, cron deployments, webhooks). Rejected as the *platform* because it is
  API-billed, session history lives in Anthropic's cloud rather than on the NAS, and the
  event model would still need an Alertmanager/t3 bridge on our side. Kept in view as a
  possible runner flavor.
- **Frameworks** — a second agent runtime to maintain, API-billed, and none of the t3
  affinity that CLI-native sessions have.

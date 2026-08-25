# Coverage — what the docs promise vs what runs

Audited 2026-08-25 against the deployed fleet. `docs/design.md`, `docs/roadmap.md`
and the ADRs describe the intended system; this file is the honest delta, so nobody
has to read code to find out which parts are real.

Legend: **done** — implemented and exercised on-cluster. **partial** — works for the
common path, with the gap named. **declared** — the API field or doc exists but no
code reads it. **not started**.

## Decisions (ADRs)

| ADR | Status | Delta |
|---|---|---|
| 0001 Kubernetes operator | done | CRDs, controllers, Helm chart, Flux-managed |
| 0002 A2A over ACP | partial | Session phases mirror A2A task states and expose `a2aTaskState`. No A2A server, no agent cards, no `message/send`, no SSE. `Agent.spec.a2a` is **declared**. |
| 0003 Wrap headless CLIs | partial | Runner contract, git flow, transcript, result harvest all work; a second provider (MiniMax via litellm) proved the contract generalizes. **Missing:** SIGTERM → `--resume` recovery. A pod evicted mid-session restarts from nothing rather than resuming, so the ADR's eviction story is aspirational. |
| 0004 Zero-token control plane | partial | Control plane is genuinely zero-token: CEL gates, dedupe, daily session caps, suppression records. **Missing:** the entire tiered-spend half — no triage call, no `budgetPolicy` branching, no `maxCostUSD` enforcement, no window awareness. See Inert fields. |
| 0005 Git bus, NAS substrate | partial | Sessions push `dispatch/*` branches; workspaces are per-project PVCs with a lease. **Missing:** `Dispatch-Session` commit trailers (the loop-prevention marker the ADR relies on), shared package caches, retention. |
| 0006 Toolchain | partial | Curated runner image with a pinned CLI, kubectl, git, jq, ripgrep. **Missing:** shared NAS caches, the install-telemetry promotion loop, the `Toolchain` CRD. |

## Event pipeline (design §5)

| Capability | Status |
|---|---|
| Alertmanager ingest, generic HMAC webhook, cron | done |
| CEL `when` gates, dedupe by fingerprint + cooldown | done |
| Self-event loop protection (`allowSelfEvents`) | done |
| Suppression as a `Suppressed` Session record | done |
| Daily `maxSessions` cap | done |
| `kubernetesEvent` and `a2aMessage` sources | not started (accepted in the API, never fire) |
| Debounce, triage, budget policy, window reserve | declared |

## Sessions and runners (design §6)

| Capability | Status |
|---|---|
| Session → task ConfigMap → Job, workspace lease, concurrency cap | done |
| Phase from Job status; usage, summary, artifacts harvested from the pod termination message | done |
| Quota exhaustion ends the session immediately | done |
| Record stream to stdout (start, assistant, tool, report, end) | done |
| `InputRequired` — the human-in-the-loop pause | declared (phase exists, nothing sets it) |
| Provenance `runnerImageDigest` / `cliVersion` | declared (fields never populated) |

## Observability (design §8)

| Capability | Status |
|---|---|
| Session records searchable in a log store, reports included | done |
| `dispatch_sessions_total`, `dispatch_events_total` | done |
| `dispatch_tokens_total`, `dispatch_cost_usd_total`, `dispatch_session_seconds` | done |
| Grafana dashboard as code | done |
| Transcript retention / pruning | not started |

Note: gateway counters live in operator memory and reset on restart. Session objects
and the log store are the durable record.

## Security (design §9)

| Capability | Status |
|---|---|
| Event payloads fenced as untrusted data in prompt templates | done |
| Least-privilege service accounts; agents read-only toward the cluster | done |
| Credential scrubbing before anything is persisted | done |
| NetworkPolicy egress allowlist | not started — runner pods reach anything the cluster allows |

## Interfaces (design §10)

| Capability | Status |
|---|---|
| kubectl as the review surface | done |
| Dispatch MCP server, A2A edge, `dispatch` CLI, push notifications | not started |

A deployment can substitute its own MCP gateway for reading — the fleet in `ai` is
reachable today through existing kubectl / logs / prometheus MCP servers — but
Dispatch ships no interface of its own.

## Inert fields — API that currently lies

These are settable, schema-valid, and silently ignored. Each is either implemented or
removed before v1alpha2; until then, treat them as documentation of intent:

- `Trigger.spec.debounce`
- `Trigger.spec.triage` (all of it)
- `Trigger.spec.budgetPolicy`
- `Agent.spec.budgets.daily.maxCostUSD`
- `Agent.spec.budgets.subscriptionWindowReserve`
- `Agent.spec.a2a`
- `Workspace.spec.caches`, `Workspace.spec.retention`
- `Session.status.provenance.runnerImageDigest` / `cliVersion`

## Recommended order

1. **`subscriptionWindowReserve` + `budgetPolicy`.** The only gap that has already
   caused an outage: a Fable guardian drained the shared Max window in an hour, and
   nothing deferred or degraded the queue. Highest value per line of code.
2. **Dispatch MCP server.** Turns the fleet from reviewable into drivable, and is the
   cheapest real cockpit (design §10, L1).
3. **`Dispatch-Session` trailers + NetworkPolicy.** Small, and they close the two
   safety claims the docs currently make without backing.
4. **Triage.** Only worth it once alert volume justifies a cheap gate; the CEL `when`
   filter carries the load today.
5. **`--resume` on eviction.** Matters when sessions get long enough that losing one
   hurts.

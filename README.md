# dispatch

**Dispatch turns a homelab Kubernetes cluster into a 24/7 duty station for coding agents.**
Events wake the right agent, sessions run in isolated NAS-backed workspaces on the model
subscriptions you already pay for, and every run is metered, recorded, and reviewable —
from t3, including your phone. Your laptop stays off.

> Status: **running**. Operator, event gateway, runner shim, Helm chart, and a live
> guardian fleet on a homelab cluster. Start at [`docs/design.md`](docs/design.md) for
> the intended system and [`docs/coverage.md`](docs/coverage.md) for what actually
> works today.

## Why

The agentic-AI conversation is long on capability demos and short on operations. The missing
pieces are boring: *when* should an agent wake up, *where* does it run, *what* may it touch,
*how much* may it spend, and *who can audit what it did*. Kubernetes already solves scheduling,
isolation, and reconciliation. Agent CLIs (t3code / Claude Code) already solve the agent loop.
Dispatch is the thin, opinionated layer that binds them.

## What it is

Three primitives, expressed as CRDs, on top of your cluster:

| Primitive | Question it answers | Analogy |
|---|---|---|
| `Trigger` | When should an agent wake, and with what context? | the duty roster |
| `Session` | One budgeted, recorded run — transcript, cost, artifacts, summary | the logbook |
| `Agent` | Who runs, with which model, credentials, workspace, and limits | the crew |

Plus `Workspace` (a project home: git origin + NAS-backed cache) and, later, `Toolchain`.

## How a session happens

1. An event arrives at the gateway: an Alertmanager webhook, a K8s event, a GitHub webhook,
   a cron tick, or a message from you via t3 (A2A).
2. Deterministic rules (CEL) filter, dedupe, and debounce it. **Zero tokens spent.**
3. Optionally, a cheap triage call (Haiku, ~$0.003) decides if it deserves an expensive session.
4. The operator creates a `Session` and launches a Job: the agent CLI runs headless in the
   agent's workspace, authenticated with *your subscription*, under budget guardrails.
5. Telemetry streams out via OpenTelemetry → Prometheus/Loki/Grafana. The transcript (JSONL),
   report, and artifacts land on the NAS. Code changes leave as git branches, never pushes to main.
6. `kubectl get sessions` — or t3 on your phone — shows what ran, what it cost, and what it found.

```
alertmanager ─┐
k8s events ───┤                                       ┌─ Job: agent CLI, headless
github hooks ─┼─▶ gateway ─▶ CEL rules ─▶ [triage] ─▶ Session CR ─▶ workspace (NAS)
cron ─────────┤    (CloudEvents)   zero tokens  Haiku      │        creds (subscription)
t3 / A2A ─────┘                                            ▼
                                    transcripts+artifacts → NAS   code → git branches
                                    metrics/logs/traces  → OTel → Grafana
```

## What it is not

- Not an agent framework — the loop belongs to t3code/Claude Code, wrapped headless.
- Not a chat app — t3 is the cockpit; Dispatch is the runtime behind it.
- Not SaaS — the point is your cluster, your NAS, your subscriptions, your audit trail.
- Not a cluster-ops bot — kagent/HolmesGPT focus on operating Kubernetes; Dispatch runs
  *your projects* (ops work included, but as one agent among several).

## Repository layout

```
api/v1alpha1/       the typed API: Agent, Trigger, Session, Workspace
cmd/                operator entrypoint (cmd) and runner shim (cmd/dispatch-run)
internal/           controllers, shim, task contract, metrics
config/             generated CRDs and RBAC
build/runner/       runner image: agent CLI + dispatch-run
.github/workflows/  ci (generate+test+drift) and images (ghcr publish)
docs/design.md      the full design
docs/coverage.md    honest delta: what is done, partial, or declared-but-inert
docs/roadmap.md     phased plan with demo criteria
docs/adr/           decision records (A2A vs ACP, CLI-wrapping, storage, tooling, …)
examples/           the API in use — concrete CRs, schema-checked by tests
```

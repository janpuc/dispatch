# Dispatch — Design

- Status: draft for review, 2026-08-24
- Decisions with alternatives live in [`adr/`](adr/); this document is the integrated view.

## 1. Problem

Agent tooling today assumes a human at a keyboard on a powered-on machine. What's missing
for "some projects I work on actively, some rarely, one or two always-on agents, access
from my phone" is not model capability — it's operations:

- **Triggering** — agents should wake on events (an Alertmanager alert, a K8s event, a
  GitHub issue, a cron tick, a message from me), not on my presence.
- **Placement** — runs need a home that isn't my laptop, with isolation and scheduling.
- **Budgeting** — "24/7" must not mean "burns tokens 24/7"; subscriptions have windows.
- **Custody** — workspaces, credentials, and what an agent may touch need boundaries.
- **Audit** — every run must be reviewable after the fact: transcript, cost, diff, outcome.

Kubernetes solves placement and reconciliation. Agent CLIs solve the loop. Observability
stacks solve metrics. Dispatch is the thin layer that binds them — *agent operations*,
deliberately boring.

**Identity in one sentence:** an event-driven runtime that keeps a fleet of coding agents
on call against your own cluster, spending your existing subscriptions, with every run on
the record — and t3 as the cockpit.

## 2. What Dispatch is / is not

Is: a Kubernetes operator + event gateway + runner contract + observability conventions.
Is not: an agent framework, a model proxy, a chat app, a SaaS, or a cluster-ops bot.

## 3. System overview

```
            ┌────────────────────────── control plane (zero tokens, ADR-0004) ─────────────────────────┐
            │                                                                                          │
 events     │   dispatch-gateway                          dispatch-operator                            │
 ───────────┼─▶ normalize → CloudEvents                   reconciles CRDs:                             │
 alertmanager│  CEL match / dedupe / debounce / cooldown  Agent, Trigger, Session, Workspace           │
 k8s events │   budget + window check                     Session → Job, status, retries, budgets      │
 webhooks   │   [optional Haiku triage]                                                                │
 cron       │   A2A server (cards, message/send, SSE)                                                  │
 t3 / A2A   │   MCP server (sessions_*, dispatch_send)                                                 │
            └───────────────┬──────────────────────────────────────┬───────────────────────────────────┘
                            │ creates                              │ launches
                            ▼                                      ▼
                       Session CR                     Job: runner image + dispatch-run shim (ADR-0003)
                    (A2A task states)                 workspace PVC (NAS)  creds Secret  OTel env
                            │                                      │
            ┌───────────────┴───────────────┐         ┌────────────┴──────────────┐
            ▼                               ▼         ▼                           ▼
   status: usage, cost, summary     NAS: transcript, report,        git remote: dispatch/<id> branches
   artifacts, follow-ups            artifacts, caches (ADR-0005)    (review gate = merge gate)
                            │
                            ▼
        OTel collector → Prometheus (metrics) / Loki (events, transcripts) / Tempo (traces) → Grafana
```

Components: **gateway** (events in, A2A/MCP edge), **operator** (reconciliation),
**runner** (contract in ADR-0003), **dispatch CLI** (thin kubectl-plugin-style UX),
dashboards-as-code.

## 4. The API

API group `dispatch.janpuc.com/v1alpha1`. Concrete, fully-worked CRs live in
[`../examples/`](../examples/); this section defines the semantics.

### Agent

Identity + defaults + limits. Key spec fields:

- `runner`: image, CLI flavor, `credentialsRef` (Secret with provider OAuth/API creds).
- `models`: `session` (e.g. `claude-fable-5` / `claude-opus-5`), `triage`
  (`claude-haiku-4-5`).
- `workspaceRef`, `serviceAccountName` (least privilege; read-only cluster access unless
  stated), `git` (author identity, allowed push branch patterns).
- `budgets`: `maxConcurrentSessions`, per-session `maxTurns`/`timeout`, daily
  `maxSessions`/`maxCostUSD`, `subscriptionWindowReserve` (fraction of the 5-hour window
  held back for interactive use).
- `a2a`: whether to expose a card, advertised skills.

### Trigger

Binds an event source to an agent with deterministic gates in front:

- `source`: one of `alertmanager | webhook | kubernetesEvent | schedule | a2aMessage`.
- `when`: CEL over the normalized CloudEvent — the zero-token filter.
- `dedupe` (key expression + cooldown), `debounce`, `priority`.
- `triage`: `enabled`, question, daily budget, `onBudgetExhausted: allow|deny`.
- `budgetPolicy`: `deferIfExhausted | degradeModel | drop` when the window is spent.
- `session.prompt`: template rendering event payload into the task, with the payload
  fenced as untrusted data (see §9).

### Session

One run; immutable record. Spec: agent ref, rendered input, trigger provenance. Status:

- `phase` mirroring A2A task states (`Submitted`, `Working`, `InputRequired`,
  `Completed`, `Failed`, `Canceled`, `Rejected`) plus Dispatch-only `Suppressed` (created
  then vetoed by dedupe/budget — kept as a record of the decision).
- `usage` (tokens by direction incl. cache, billing=subscription|api, API-equivalent cost
  estimate), `artifacts` (transcript/report/branch URIs), `summary`, `followUps`.
- Kubernetes Events on lifecycle transitions so `kubectl describe session` tells the story.

### Workspace

Project home: `origin` (git URL + default branch), NAS-backed PVC template, cache
selections, retention, and the single-active-session lease (ADR-0005).

### Toolchain (Phase 3)

Declarative tool sets compiled into runner images; until then the Dockerfile is the API
(ADR-0006).

## 5. Event pipeline

1. **Ingest**: Alertmanager webhook receiver, generic webhook (GitHub etc., HMAC-verified),
   K8s watch, cron, A2A messages. Everything normalizes to CloudEvents with a stable
   fingerprint (alert fingerprint, delivery id, schedule tick, message id).
2. **Gate** (all deterministic): CEL `when` → dedupe by fingerprint within cooldown →
   debounce window → agent/global concurrency and budget checks → window check.
3. **Triage** (optional): one Haiku call, classification-only output schema, hard daily
   budget. At ~$1/MTok input, a triaged alert costs tenths of a cent; 100 noisy alerts a
   day ≈ $0.30 (API-billed on purpose — never the subscription, ADR-0004).
4. **Dispatch**: create Session; operator renders the Job.
5. **Suppression is data**: budget-vetoed events become `Suppressed` sessions with the
   reason — so "why didn't it run?" has a kubectl answer. Dedupe repeats (an alert
   refiring inside its cooldown) are counted in trigger status and metrics only;
   recording an object per refire would drown the record in noise.

Loop protection, layered: the gateway drops events naming Dispatch's own session
workloads unless a Trigger sets `allowSelfEvents` — this is not theoretical, it fired on
day one: session Jobs that failed on an exhausted model quota raised `KubeJobFailed`
alerts, which created fresh sessions, which failed and alerted again (2026-08-24; 20 of
26 firing alerts were Dispatch's own Jobs). Beyond that: agent commits carry
`Dispatch-Session` trailers and push only to `dispatch/*` branches (their CI can be
configured to skip or to never re-trigger Dispatch); per-fingerprint cooldowns; global
concurrency cap; one-command kill switch (`dispatch pause` — annotates the gateway to
drop all non-manual events).

## 6. Sessions and runners

The runner contract, auth constraints (subscription requires non-bare mode with mounted
OAuth credentials), SIGTERM→`--resume` recovery, and the multi-provider path are specified
in ADR-0003. Additional semantics:

- **Interaction points**: when the CLI needs a human answer, the shim parks the session in
  `InputRequired`, pushes a notification (ntfy first; A2A push once the gateway supports
  it), and resumes with `--resume` when the answer arrives from t3/phone. MVP may simply
  fail-fast instead; the state exists from day one so the upgrade is additive.
- **Timeouts and turns** come from the Agent's budget, passed to the shim; the shim ends
  the session cleanly (SIGINT, wait, record) rather than letting the Job deadline kill it.
- **Provenance**: every session records trigger, event fingerprint, prompt hash, runner
  image digest, and CLI version — enough to reproduce or explain any run months later.

## 7. Workspaces, git, NAS

ADR-0005 in one line: code moves through git (branches + review gates, Forgejo/NAS remotes
for private projects); the NAS holds workspaces-as-cache, session records, shared caches,
and agent memory (in-workspace notes/CLAUDE.md, versioned with the project).

## 8. Observability and review

Requirement: "literally all sessions should be review-able." Three layers:

1. **Record** — `sessions/<agent>/<date>/<id>/` on the NAS: `transcript.jsonl` (the
   stream-json feed, secret-scrubbed), `result.json`, `report.md`, artifacts. Reviewable
   raw, via `dispatch show <id>` (rendered), or grep-able in bulk; transcripts also ship
   to Loki so Grafana can search the fleet's entire life.
2. **Metrics** — two sources:
   - Claude Code native OTel (`CLAUDE_CODE_ENABLE_TELEMETRY=1`, OTLP exporters): token
     usage, per-session cost estimates, tool decisions, lines changed, commits/PRs.
   - Operator/gateway Prometheus metrics: `dispatch_events_total{source,disposition}`,
     `dispatch_sessions_total{agent,trigger,outcome}`, `dispatch_session_seconds`,
     `dispatch_tokens_total{agent,model,type}`, `dispatch_cost_usd_total{billing}`,
     `dispatch_window_used_ratio{credential}`, `dispatch_triage_total{verdict}`,
     `dispatch_queue_depth{priority}`, `dispatch_budget_denials_total`.
3. **Dashboards as code** — Fleet (sessions, outcomes, cost by agent, window burn), Event
   pipeline (noise → suppressed → triaged → dispatched funnel), Session explorer (drill
   from panel to transcript), Budgets (reserve headroom, denials).

The 2 AM test drives all of it: *alert fired at 02:00; by 02:15 the transcript and a
root-cause report are on the NAS; Grafana shows the run and its cost; nothing needed the
laptop.* (Phase 0's demo, roadmap.)

## 9. Security model

- **Event payloads are untrusted input.** Alert annotations and webhook bodies can carry
  prompt injection. Prompts fence payloads as data; sessions run least-privilege
  (namespace-scoped SA, read-only cluster by default); NetworkPolicy egress allowlist
  (model API, git remote, package registries, telemetry); code lands behind git review
  gates, never on main.
- **Credentials**: per-provider Secrets (SOPS/sealed-secrets friendly), mounted only into
  runner pods, never into the gateway; transcript scrubber for known token patterns
  before anything is persisted.
- **Blast radius**: agents cannot mutate the cluster unless an Agent explicitly grants a
  scoped role; the duty agent investigates read-only and *proposes* fixes as branches in
  the gitops repo.
- **Exposure**: no public ingress; t3/phone reach the gateway over the tailnet.

## 10. Interfaces

- **t3 (cockpit)**: integration ladder. L1 — Dispatch MCP server (`sessions_list`,
  `session_show`, `dispatch_send`, `budget_status`): works with any MCP client today,
  which t3 demonstrably is. L2 — A2A: t3 as an A2A client gets streaming tasks and push;
  cards make the fleet discoverable. L3 — native session attach/render inside t3 (open
  question: t3's external-session surface; investigate before promising).
- **A2A edge** (ADR-0002): card per agent at `/.well-known/agent-card.json`, JSON-RPC +
  SSE, session states already aligned.
- **CLI**: `dispatch get sessions|agents|triggers`, `dispatch show <session>`,
  `dispatch send <agent> "task"`, `dispatch logs -f`, `dispatch pause|resume`,
  `dispatch top` (budget/window burn).

## 11. Prior art and positioning

| Project | What it is | Why Dispatch differs |
|---|---|---|
| kagent (CNCF sandbox) | K8s operator for AI agents, own framework, A2A support | ops-agent focus, framework-coupled, API-billed; Dispatch wraps your CLIs/subscriptions and centers session review. Watch it; steal patterns. |
| Anthropic Managed Agents | Hosted loop, self-hosted sandbox workers, cron deployments, webhooks | closest hosted analog; API-billed, cloud-resident sessions (ADR-0003). Candidate runner flavor, not the platform. |
| Claude Code cloud / GitHub Actions agents | event→session for GitHub-hosted work | SaaS/CI-resident; Dispatch is the self-hosted, subscription-using, private-project version. |
| HolmesGPT, k8sgpt | alert investigation / cluster diagnosis | single-purpose ops tools; could inspire the duty agent's investigation playbooks. |
| Argo Events | mature K8s event bus | heavier than MVP needs; if sources balloon, swap gateway ingestion for it without touching the CRDs. |

## 12. Open questions

1. **t3 internals** — what is t3's surface for listing/attaching external sessions, and
   does t3 mobile support custom MCP servers? Determines how far past L1 the cockpit goes.
2. **t3code headless** — flags/transcript format parity with `claude -p`? Until confirmed,
   the reference runner is Claude Code.
3. **Subscription behavior in practice** — headless OAuth in pods, window/cap mechanics
   under automation, ToS drift. Phase 0 exists to measure this.
4. **NFS performance** for hot build dirs (Phase 0; escape hatch in ADR-0005).
5. **Which second provider** (if any) is worth a runner flavor in Phase 3.

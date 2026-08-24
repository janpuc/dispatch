# Roadmap

Each phase ends with a demo that must work end-to-end before moving on. The demos are the
spec: if a phase's demo is unconvincing, the phase is not done.

## Phase 0 — Prove the loop (no operator, ~a weekend)

Static manifests only, one namespace, zero CRDs.

- A tiny webhook receiver (or even a `kubectl create job` by hand) that renders a Job from
  an Alertmanager payload.
- The Job runs `claude -p` headless in an NFS-mounted workspace, authenticated with the
  Max-subscription OAuth credentials mounted from a Secret (no `--bare` — bare mode cannot
  use subscription auth).
- Transcript (`--output-format stream-json`) written to the NAS; Claude Code OTel metrics
  (`CLAUDE_CODE_ENABLE_TELEMETRY=1`) scraped into the existing Prometheus/Grafana.

**Demo:** an alert fires at 02:00; by 02:15 a root-cause report sits on the NAS, Grafana
shows the session and its token usage, and the laptop was asleep the whole time.

Phase 0 also answers the open risk questions cheaply: does subscription auth work headless
in a pod, how do the 5-hour usage windows feel in practice, is NFS workspace performance
acceptable.

## Phase 1 — Operator MVP

- CRDs: `Agent`, `Trigger`, `Session`, `Workspace` (API group `dispatch.janpuc.com`).
- Controller (Go, controller-runtime / kubebuilder) reconciling Sessions into Jobs.
- Gateway: Alertmanager webhook, generic webhook, cron, K8s event watch → CloudEvents →
  CEL rules → dedupe/debounce/cooldown → Session creation.
- Budgets enforced: per-session turns/timeout, per-agent daily caps, global concurrency,
  subscription-window reserve. Kill switch (`dispatch pause`).
- Prometheus metrics from the controller + Grafana dashboards as code.
- `dispatch` CLI: `get sessions`, `show <session>` (rendered transcript), `send`, `pause`.

**Demo:** `kubectl apply` the two agents from `examples/`; a firing alert produces exactly
one Session despite refiring for 3 hours (dedupe works); a deliberately looping trigger is
caught by the cooldown; `dispatch show` renders the transcript.

## Phase 2 — Cockpit: A2A + t3 + phone

- A2A server in the gateway: agent card per Agent at `/.well-known/agent-card.json`,
  `message/send` + SSE streaming; Session states already mirror A2A task states.
- Dispatch MCP server (`sessions_list`, `session_show`, `dispatch_send`, `budget_status`)
  so t3 — or any MCP client — gets first-class access today, without waiting for deeper
  t3 integration.
- Push notifications for `input-required` and completed sessions (ntfy and/or A2A push).
- Reachability from the phone over the tailnet; no public ingress.

**Demo:** from the phone, send the duty agent a task, watch it stream, answer one
`input-required` question, and read yesterday's sessions.

## Phase 3 — Fleet maturity

- `Toolchain` CRD + shared package-manager caches on NAS + tool-install telemetry and the
  promotion loop (observed installs → PR against the runner image Dockerfile).
- Second runner flavor (another provider's CLI subscription) behind the same runner contract.
- Agent→agent handoff over A2A (duty agent files work to project agents).
- Optional model triage tier tuning, window-aware queueing refinements, session retention
  policies and transcript search (Loki).

**Demo:** the duty agent's nightly patrol reviews yesterday's sessions and files one task
to a project agent, which completes it on a different model — all visible in one Grafana
fleet dashboard.

## Explicitly deferred

- Multi-user / multi-tenant anything.
- Public ingress; everything stays on the tailnet.
- A web UI beyond Grafana + t3 (revisit only if t3 integration stalls).
- Building our own agent loop (see ADR-0003).

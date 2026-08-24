# ADR-0004: Zero-token control plane, tiered model spend, window-aware budgets

- Status: accepted
- Date: 2026-08-24

## Context

The brief asks for a "low token churn operator that works 24/7" next to "high token usage
models like Fable or Opus for orchestration." Naively, an always-on agent burns tokens
around the clock; a 24/7 LLM daemon on Opus would exhaust a Max plan's 5-hour windows and
weekly caps on idle chatter alone.

The reframe: **"always alive" is a property of latency-to-wake and continuity of memory,
not of a hot loop.** An agent that wakes in seconds, remembers its prior sessions (its
workspace CLAUDE.md and notes), and is guaranteed to be watching (because the trigger
pipeline is) feels alive — at zero standing cost.

## Decision

Three spend tiers with a hard architectural boundary between them:

| Tier | What | Cost | Where |
|---|---|---|---|
| 0 | Operator + gateway: watch, filter (CEL), dedupe, debounce, schedule | zero tokens, always on | deterministic Go |
| 1 | Triage: "is this event worth an expensive session?" | Haiku via API, ~$0.002–0.005/event | opt-in per Trigger |
| 2 | Sessions: the real work on Fable/Opus | subscription windows | dispatched Jobs only |

Rules:

- **The control plane never calls a model.** Tier 1 exists only where a Trigger explicitly
  enables it, with its own daily budget and a deterministic fallback (fail-open or
  fail-closed, per trigger) when that budget is gone. Triage runs on API billing precisely
  so it can never eat the subscription window.
- **Budgets are first-class API fields**, not conventions: per-session max turns and
  timeout; per-agent daily session/cost caps and max concurrency; a global concurrency
  cap; and `subscriptionWindowReserve` — a fraction of the current 5-hour window kept free
  for interactive use, so the fleet cannot starve the human.
- **Window-aware scheduling:** the operator tracks subscription usage (Claude Code's OTel
  token metrics + per-session cost estimates) per credential. When a window is exhausted
  beyond the reserve, queued sessions defer to the next window by priority; a Trigger may
  instead opt into degrade-to-cheaper-model or drop. This is the feature that makes
  "utilize my current subscriptions" real rather than aspirational.
- **Cost visibility everywhere:** every Session records tokens and an API-equivalent cost
  estimate even when subscription-billed, so dashboards can answer "what would this fleet
  cost on API pricing" — the number that justifies the whole design.

## Consequences

- 24/7 presence costs ~nothing; spend correlates with events actually worth acting on.
- The duty agent's "always alive" feel is delivered by a cron heartbeat Trigger (nightly
  patrol) + event Triggers + persistent workspace memory, not a resident process.
- Usage accounting is estimation (client-side costs, OTel counters), not billing truth;
  acceptable for scheduling decisions, and the reserve absorbs the error bar.

## Alternatives rejected

- **Resident LLM daemon** — burns windows on idle, adds nothing reconciliation doesn't.
- **LLM-polling loops** ("every 5 minutes, look around") — token churn with worse latency
  than event push; exactly the pattern the brief complains about.
- **Triage on the subscription** — couples the cheap tier to the expensive tier's quota;
  a noisy alert storm could lock the human out of their own plan.

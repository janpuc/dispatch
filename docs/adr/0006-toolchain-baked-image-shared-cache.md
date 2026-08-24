# ADR-0006: Tooling = curated runner image + shared caches + observed promotion

- Status: accepted
- Date: 2026-08-24

## Context

Agents must be able to install CLI tools, but per-session reinstalling is churn (slow,
flaky, token-wasteful — the agent spends turns on apt/npm) and per-agent persistent
toolsets drift into unauditable snowflakes. The brief suggests a shared tool cache.

## Decision

Three layers, from most to least curated:

1. **Baked runner image.** A single OCI image with the standing toolbelt: git, gh,
   kubectl, helm, jq, ripgrep, fd, node+pnpm, python+uv, go, build essentials, the agent
   CLI pinned to a known version. Rebuilt weekly by CI; the Dockerfile is the audited
   source of truth for "what can every agent assume exists."
2. **Shared package-manager caches on the NAS**, mounted into every session and pointed
   at via environment (`npm_config_cache`, `UV_CACHE_DIR`, `PIP_CACHE_DIR`, `GOMODCACHE`,
   `CARGO_HOME`-cache). Installs that do happen are warm. These caches are concurrent-safe
   for the managers listed; anything that isn't stays per-workspace.
3. **Ephemeral per-session overlay.** Agents may install freely into `$HOME`/workspace-
   local prefixes (`pipx`, `npm i -g` with a redirected prefix). It works immediately and
   evaporates with the session — by design, so novel tools don't silently become load-
   bearing.

**Promotion loop instead of drift:** the runner shim logs every runtime tool install as a
structured event. A periodic report (later: a "toolsmith" cron agent) turns repeated
installs into a PR against the runner-image Dockerfile. Tools graduate into the baked
layer through review, not through accretion.

## Consequences

- Sessions start warm; the common path never installs anything.
- One image to scan, pin, and roll back; agent capabilities are diffable in git history.
- A per-run overlay install is repeated until promotion — accepted cost, softened by the
  warm caches, and it is precisely the signal the promotion loop feeds on.

## Alternatives rejected

- **Nix store on the NAS** — the theoretically ideal shared tool cache (content-addressed,
  per-agent profiles, zero duplication). Rejected for MVP on operational complexity for a
  one-person fleet; explicitly worth revisiting in Phase 3 if install telemetry shows the
  Dockerfile loop thrashing.
- **Per-agent persistent tool volumes** — snowflakes with no audit trail; the exact churn
  the brief wants to avoid, just moved to disk.
- **Fully ephemeral installs only** — maximal cleanliness, maximal churn; fails the
  low-churn requirement outright.

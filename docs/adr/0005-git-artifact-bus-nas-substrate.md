# ADR-0005: Git is the artifact bus; the NAS is the storage substrate

- Status: accepted
- Date: 2026-08-24

## Context

The brief proposes "exchanging files could be my NAS" so the laptop can stay off. The
tempting literal reading — one shared NFS workspace per project that both the laptop and
the agents mount and edit — creates the classic shared-mutable-directory problems:
concurrent edits with no merge semantics, NFS locking quirks, no review point, no history
of what an agent changed, and a single corrupted directory as the failure mode.

## Decision

Split the two concerns the NAS was covering:

1. **Code moves through git.** Every project has a git remote reachable from the cluster —
   GitHub for public work; for laptop-off private work, a lightweight self-hosted forge
   (Forgejo) or bare repos served from the NAS over SSH. Agents clone/fetch from the
   remote, commit with a `Dispatch-Session: <id>` trailer, and push only to
   `dispatch/<session-id>` branches. Merging to main is a human act (a PR/MR where the
   forge supports it). Git supplies merge semantics, review gates, provenance, and
   loop-prevention markers — everything the shared directory lacked.
2. **Everything else lives on the NAS** over NFS (RWX storage class where available;
   deployment note 2026-08-24: the current cluster's miroir CSI provisions RWO only, so
   workspaces default to ReadWriteOnce and session records fall back into each workspace
   until an RWX class exists — both are spec fields, not code changes):
   - `workspaces/<project>/` — per-project clone + build caches, mounted into sessions.
     Treated as *disposable cache*: correctness always flows through git, so a workspace
     can be deleted and rebuilt at any time.
   - `sessions/<agent>/<date>/<id>/` — transcript.jsonl, result.json, reports, artifacts.
     Append-only, retention-managed; this is the reviewable record.
   - `caches/` — shared package-manager caches (ADR-0006).
   - agent memory — each agent's notes/CLAUDE.md live inside its workspace, so memory
     versions with the project.

Concurrency rule: **one active session per workspace** (the Workspace holds a lease).
Parallelism, when wanted later, comes from git worktrees inside the workspace, not from
relaxing the lease.

## Consequences

- The laptop-off story works: phone → trigger → session → branch on the forge + report on
  the NAS. Later, the laptop pulls like it would from any collaborator.
- Requires standing up Forgejo (or NAS bare repos) for projects not on GitHub — small,
  well-trodden ops work, and it doubles as backup-by-clone.
- NFS performance for hot build dirs is a known risk; Phase 0 measures it. Escape hatch:
  node-local scratch (emptyDir) for builds with NAS-side clone + artifacts, without
  changing the API.
- MinIO/S3 on the NAS is *not* required for MVP; plain NFS files are greppable and
  Loki-shippable. Revisit only if something concrete needs S3 semantics.

## Alternatives rejected

- **Shared live workspace as source of truth** — no merge semantics, no review gate, no
  audit; one bad `rm -rf` by an agent destroys state for everyone.
- **Ephemeral clone per session, no persistent workspace** — correct but slow (full clone
  + cold caches every run) and directly contradicts the low-churn requirement.
- **Object storage as the primary substrate** — adds an S3 gateway between agents and
  their files for no MVP benefit.

# Examples — the target API

These manifests are **design artifacts**: the CRDs behind them are not implemented yet
(see `docs/roadmap.md`, Phase 1). They exist so the API is designed from the user's side
first — this is what operating Dispatch should feel like.

The set models the two situations from the original brief:

- **duty** — the always-alive agent: woken by Alertmanager alerts
  (`trigger-alerts-to-duty.yaml`) and by a nightly patrol (`trigger-duty-patrol.yaml`),
  investigating with read-only cluster access, remembering via its workspace.
- **sidequest** — a personal project worked on rarely: woken by labeled GitHub issues
  (`trigger-sidequest-issues.yaml`), exchanging code through the NAS-hosted git remote,
  so the laptop can stay off for weeks.

`session-completed.yaml` shows a finished Session **including its status** — the record
you would review from t3 the morning after. The CRD schemas behind these manifests are
generated from the Go types in `api/v1alpha1/` into `config/crd/bases/`.

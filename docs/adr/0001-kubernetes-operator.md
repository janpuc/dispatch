# ADR-0001: Run Dispatch as a Kubernetes operator

- Status: accepted
- Date: 2026-08-24

## Context

Dispatch needs a place to run agents 24/7 that is not a laptop. The candidates:

1. A single always-on box with systemd timers + a small webhook daemon.
2. A Kubernetes operator with CRDs on the existing homelab cluster.
3. A hosted control plane (Anthropic Managed Agents, Claude Code cloud sessions).

The homelab already runs Kubernetes with Prometheus, Alertmanager, Grafana, and the NAS
exposed via NFS. The stated requirements include Alertmanager-triggered work, deep
observability, and control from t3/phone.

## Decision

Option 2: CRDs + a Go controller (controller-runtime / kubebuilder).

- The cluster already exists and already hosts the event sources (Alertmanager, K8s events)
  and the observability stack Dispatch must feed. Running anywhere else means bridging all
  of that across a boundary.
- CRDs make the fleet declarative and auditable: `kubectl get sessions` is the review
  surface, RBAC is the permission model, GitOps applies to agents like everything else.
- Reconciliation semantics (create Job, watch it, record status, retry) are exactly the
  controller pattern; hand-rolling them under systemd reimplements a worse Kubernetes.

## Consequences

- Kubernetes becomes a hard dependency. Mitigation: the runner contract (task in → Job →
  transcript + result out) is a plain container; if the cluster ever feels heavy, the same
  runner ports to systemd with modest glue.
- Go + kubebuilder is the implementation stack; this is the community default and keeps the
  door open to publishing the operator later.

## Alternatives rejected

- **Systemd box** — viable and simpler, but duplicates scheduling/retry/isolation that the
  cluster provides for free, and leaves the declarative fleet + RBAC + GitOps story behind.
- **Hosted control planes** — conflict with three requirements at once: subscription usage
  (they bill API tokens), NAS-local data, and self-hosted auditability. Recorded in detail
  in ADR-0003; they remain interesting as an optional runner backend, not as the platform.

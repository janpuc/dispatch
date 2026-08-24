package controller

import (
	"context"
	"encoding/json"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dispatchv1alpha1 "github.com/janpuc/dispatch/api/v1alpha1"
	"github.com/janpuc/dispatch/internal/metrics"
	"github.com/janpuc/dispatch/internal/shim"
)

const (
	conditionDispatched = "Dispatched"

	reasonAgentMissing      = "AgentMissing"
	reasonWorkspaceMissing  = "WorkspaceMissing"
	reasonWorkspaceNotReady = "WorkspaceNotReady"
	reasonLeaseHeld         = "LeaseHeld"
	reasonConcurrencyHeld   = "ConcurrencyHeld"
	reasonJobCreated        = "JobCreated"

	holdRequeueInterval = 30 * time.Second
)

// SessionReconciler turns Sessions into runner Jobs and records their outcome
// (design §6).
type SessionReconciler struct {
	client.Client
	Recorder record.EventRecorder
	Runner   RunnerConfig
}

// +kubebuilder:rbac:groups=dispatch.janpuc.com,resources=sessions;agents;workspaces;triggers,verbs=get;list;watch
// +kubebuilder:rbac:groups=dispatch.janpuc.com,resources=sessions,verbs=update;patch
// +kubebuilder:rbac:groups=dispatch.janpuc.com,resources=sessions/status;workspaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile drives one Session toward its terminal, immutable record.
func (r *SessionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var session dispatchv1alpha1.Session
	if err := r.Get(ctx, req.NamespacedName, &session); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if session.Status.Phase.IsTerminal() {
		return ctrl.Result{}, nil
	}

	var agent dispatchv1alpha1.Agent
	if err := r.Get(ctx, types.NamespacedName{Namespace: session.Namespace, Name: session.Spec.AgentRef.Name}, &agent); err != nil {
		if apierrors.IsNotFound(err) {
			return r.hold(ctx, &session, reasonAgentMissing, "agent "+session.Spec.AgentRef.Name+" not found")
		}
		return ctrl.Result{}, err
	}

	if err := r.ensureLabels(ctx, &session, &agent); err != nil {
		return ctrl.Result{}, err
	}

	var workspace dispatchv1alpha1.Workspace
	if err := r.Get(ctx, types.NamespacedName{Namespace: session.Namespace, Name: agent.Spec.WorkspaceRef.Name}, &workspace); err != nil {
		if apierrors.IsNotFound(err) {
			return r.hold(ctx, &session, reasonWorkspaceMissing, "workspace "+agent.Spec.WorkspaceRef.Name+" not found")
		}
		return ctrl.Result{}, err
	}
	if workspace.Status.PVCName == "" {
		return r.hold(ctx, &session, reasonWorkspaceNotReady, "workspace PVC not provisioned yet")
	}
	if leased(&workspace) && workspace.Status.ActiveSession != "" && workspace.Status.ActiveSession != session.Name {
		return r.hold(ctx, &session, reasonLeaseHeld, "workspace lease held by "+workspace.Status.ActiveSession)
	}

	if held, holder, err := r.concurrencyHeld(ctx, &session, &agent); err != nil {
		return ctrl.Result{}, err
	} else if held {
		return r.hold(ctx, &session, reasonConcurrencyHeld, holder)
	}

	job, created, err := r.ensureJob(ctx, &session, &agent, &workspace)
	if err != nil {
		return ctrl.Result{}, err
	}
	if created {
		if err := r.claimLease(ctx, &workspace, session.Name); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Eventf(&session, corev1.EventTypeNormal, reasonJobCreated, "created job %s", job.Name)
	}

	if err := r.recordProgress(ctx, &session, &agent, &workspace, job); err != nil {
		return ctrl.Result{}, err
	}
	log.V(1).Info("reconciled session", "phase", session.Status.Phase, "job", job.Name)
	return ctrl.Result{}, nil
}

func leased(ws *dispatchv1alpha1.Workspace) bool {
	return ws.Spec.Lease != nil && ws.Spec.Lease.SingleActiveSession
}

func (r *SessionReconciler) ensureLabels(ctx context.Context, session *dispatchv1alpha1.Session, agent *dispatchv1alpha1.Agent) error {
	desired := sessionLabels(session, agent)
	changed := false
	if session.Labels == nil {
		session.Labels = map[string]string{}
	}
	for key, value := range desired {
		if session.Labels[key] != value {
			session.Labels[key] = value
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return r.Update(ctx, session)
}

func (r *SessionReconciler) concurrencyHeld(ctx context.Context, session *dispatchv1alpha1.Session, agent *dispatchv1alpha1.Agent) (bool, string, error) {
	max := agent.Spec.Budgets.MaxConcurrentSessions
	if max <= 0 {
		return false, "", nil
	}
	var peers dispatchv1alpha1.SessionList
	if err := r.List(ctx, &peers,
		client.InNamespace(session.Namespace),
		client.MatchingLabels{dispatchv1alpha1.LabelAgent: agent.Name},
	); err != nil {
		return false, "", err
	}
	running := 0
	for _, peer := range peers.Items {
		if peer.Name == session.Name {
			continue
		}
		if peer.Status.JobName != "" && !peer.Status.Phase.IsTerminal() {
			running++
		}
	}
	if running >= int(max) {
		return true, "agent at maxConcurrentSessions", nil
	}
	return false, "", nil
}

func (r *SessionReconciler) ensureJob(
	ctx context.Context,
	session *dispatchv1alpha1.Session,
	agent *dispatchv1alpha1.Agent,
	workspace *dispatchv1alpha1.Workspace,
) (*batchv1.Job, bool, error) {
	var existing batchv1.Job
	err := r.Get(ctx, types.NamespacedName{Namespace: session.Namespace, Name: SessionJobName(session)}, &existing)
	if err == nil {
		return &existing, false, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, false, err
	}

	configMap, err := BuildTaskConfigMap(session, agent, workspace)
	if err != nil {
		return nil, false, err
	}
	if err := ctrl.SetControllerReference(session, configMap, r.Scheme()); err != nil {
		return nil, false, err
	}
	if err := r.Create(ctx, configMap); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, false, err
	}

	job := BuildSessionJob(session, agent, workspace, r.Runner)
	if err := ctrl.SetControllerReference(session, job, r.Scheme()); err != nil {
		return nil, false, err
	}
	if err := r.Create(ctx, job); err != nil {
		return nil, false, err
	}
	return job, true, nil
}

func (r *SessionReconciler) claimLease(ctx context.Context, workspace *dispatchv1alpha1.Workspace, sessionName string) error {
	if !leased(workspace) || workspace.Status.ActiveSession == sessionName {
		return nil
	}
	workspace.Status.ActiveSession = sessionName
	return r.Status().Update(ctx, workspace)
}

func (r *SessionReconciler) releaseLease(ctx context.Context, workspace *dispatchv1alpha1.Workspace, sessionName string) error {
	if workspace.Status.ActiveSession != sessionName {
		return nil
	}
	workspace.Status.ActiveSession = ""
	return r.Status().Update(ctx, workspace)
}

func (r *SessionReconciler) recordProgress(
	ctx context.Context,
	session *dispatchv1alpha1.Session,
	agent *dispatchv1alpha1.Agent,
	workspace *dispatchv1alpha1.Workspace,
	job *batchv1.Job,
) error {
	previous := session.Status.Phase
	phase := previous
	if phase == "" {
		phase = dispatchv1alpha1.SessionSubmitted
	}
	switch {
	case job.Status.Succeeded > 0:
		phase = dispatchv1alpha1.SessionCompleted
	case jobFailed(job):
		phase = dispatchv1alpha1.SessionFailed
	case job.Status.Active > 0:
		phase = dispatchv1alpha1.SessionWorking
	}

	now := metav1.Now()
	session.Status.JobName = job.Name
	session.Status.Phase = phase
	session.Status.A2ATaskState = phase.A2ATaskState()
	if session.Status.Model == "" {
		session.Status.Model = agent.Spec.Models.Session
	}
	if phase == dispatchv1alpha1.SessionWorking && session.Status.StartedAt == nil {
		session.Status.StartedAt = &now
	}
	if phase.IsTerminal() && session.Status.CompletedAt == nil {
		session.Status.CompletedAt = &now
		r.harvestResult(ctx, session)
	}
	meta.SetStatusCondition(&session.Status.Conditions, metav1.Condition{
		Type:               conditionDispatched,
		Status:             metav1.ConditionTrue,
		Reason:             reasonJobCreated,
		Message:            "job " + job.Name,
		ObservedGeneration: session.Generation,
	})
	if err := r.Status().Update(ctx, session); err != nil {
		return err
	}

	if phase != previous && phase.IsTerminal() {
		metrics.SessionsTotal.WithLabelValues(agent.Name, string(phase)).Inc()
		r.Recorder.Eventf(session, corev1.EventTypeNormal, string(phase), "session reached %s", phase)
		return r.releaseLease(ctx, workspace, session.Name)
	}
	return nil
}

func (r *SessionReconciler) harvestResult(ctx context.Context, session *dispatchv1alpha1.Session) {
	var pods corev1.PodList
	if err := r.List(ctx, &pods,
		client.InNamespace(session.Namespace),
		client.MatchingLabels{dispatchv1alpha1.LabelSession: session.Name},
	); err != nil {
		return
	}
	for _, pod := range pods.Items {
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name != runnerContainerName || status.State.Terminated == nil {
				continue
			}
			var result shim.Result
			if json.Unmarshal([]byte(status.State.Terminated.Message), &result) != nil {
				continue
			}
			applyResult(session, result)
			return
		}
	}
}

func applyResult(session *dispatchv1alpha1.Session, result shim.Result) {
	if result.Summary != "" {
		session.Status.Summary = result.Summary
	}
	session.Status.FollowUps = result.FollowUps
	usage := &dispatchv1alpha1.SessionUsage{
		Billing:         result.Usage.Billing,
		InputTokens:     result.Usage.InputTokens,
		OutputTokens:    result.Usage.OutputTokens,
		CacheReadTokens: result.Usage.CacheReadTokens,
		Turns:           result.Usage.Turns,
	}
	if result.Usage.APIEquivalentUSD != "" {
		if quantity, err := resource.ParseQuantity(result.Usage.APIEquivalentUSD); err == nil {
			usage.APIEquivalentUSD = &quantity
		}
	}
	session.Status.Usage = usage
	session.Status.Artifacts = &dispatchv1alpha1.SessionArtifacts{
		Transcript: result.Artifacts.Transcript,
		Report:     result.Artifacts.Report,
		Branches:   result.Artifacts.Branches,
	}
}

func jobFailed(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *SessionReconciler) hold(ctx context.Context, session *dispatchv1alpha1.Session, reason, message string) (ctrl.Result, error) {
	if session.Status.Phase == "" {
		session.Status.Phase = dispatchv1alpha1.SessionSubmitted
		session.Status.A2ATaskState = session.Status.Phase.A2ATaskState()
	}
	meta.SetStatusCondition(&session.Status.Conditions, metav1.Condition{
		Type:               conditionDispatched,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: session.Generation,
	})
	if err := r.Status().Update(ctx, session); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: holdRequeueInterval}, nil
}

// SetupWithManager wires the reconciler into the manager.
func (r *SessionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dispatchv1alpha1.Session{}).
		Owns(&batchv1.Job{}).
		Named("session").
		Complete(r)
}

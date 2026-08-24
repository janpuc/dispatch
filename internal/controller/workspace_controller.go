package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dispatchv1alpha1 "github.com/janpuc/dispatch/api/v1alpha1"
)

const (
	conditionReady = "Ready"

	reasonProvisioned = "Provisioned"
)

// WorkspaceReconciler provisions the NAS-backed PVC behind each Workspace and
// heals stale single-active-session leases (ADR-0005).
type WorkspaceReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=dispatch.janpuc.com,resources=workspaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=dispatch.janpuc.com,resources=workspaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create

// Reconcile drives one Workspace to a provisioned, lease-consistent state.
func (r *WorkspaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var workspace dispatchv1alpha1.Workspace
	if err := r.Get(ctx, req.NamespacedName, &workspace); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	pvcName := WorkspacePVCName(&workspace)
	var pvc corev1.PersistentVolumeClaim
	err := r.Get(ctx, types.NamespacedName{Namespace: workspace.Namespace, Name: pvcName}, &pvc)
	if apierrors.IsNotFound(err) {
		desired := buildWorkspacePVC(&workspace, pvcName)
		if err := ctrl.SetControllerReference(&workspace, desired, r.Scheme()); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
	} else if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.healLease(ctx, &workspace); err != nil {
		return ctrl.Result{}, err
	}

	workspace.Status.PVCName = pvcName
	meta.SetStatusCondition(&workspace.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             reasonProvisioned,
		Message:            "pvc " + pvcName,
		ObservedGeneration: workspace.Generation,
	})
	return ctrl.Result{}, r.Status().Update(ctx, &workspace)
}

func (r *WorkspaceReconciler) healLease(ctx context.Context, workspace *dispatchv1alpha1.Workspace) error {
	holder := workspace.Status.ActiveSession
	if holder == "" {
		return nil
	}
	var session dispatchv1alpha1.Session
	err := r.Get(ctx, types.NamespacedName{Namespace: workspace.Namespace, Name: holder}, &session)
	if err == nil && !session.Status.Phase.IsTerminal() {
		return nil
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	workspace.Status.ActiveSession = ""
	return nil
}

func buildWorkspacePVC(workspace *dispatchv1alpha1.Workspace, name string) *corev1.PersistentVolumeClaim {
	accessModes := workspace.Spec.Storage.AccessModes
	if len(accessModes) == 0 {
		accessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: workspace.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: accessModes,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: workspace.Spec.Storage.Size,
				},
			},
		},
	}
	if class := workspace.Spec.Storage.StorageClassName; class != "" {
		pvc.Spec.StorageClassName = &class
	}
	return pvc
}

func (r *WorkspaceReconciler) workspaceForSession(ctx context.Context, obj client.Object) []reconcile.Request {
	session, ok := obj.(*dispatchv1alpha1.Session)
	if !ok {
		return nil
	}
	var agent dispatchv1alpha1.Agent
	if err := r.Get(ctx, types.NamespacedName{Namespace: session.Namespace, Name: session.Spec.AgentRef.Name}, &agent); err != nil {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{Namespace: session.Namespace, Name: agent.Spec.WorkspaceRef.Name},
	}}
}

// SetupWithManager wires the reconciler into the manager.
func (r *WorkspaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dispatchv1alpha1.Workspace{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Watches(&dispatchv1alpha1.Session{}, handler.EnqueueRequestsFromMapFunc(r.workspaceForSession)).
		Named("workspace").
		Complete(r)
}

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GitOrigin is the project's git remote — the artifact bus (ADR-0005).
type GitOrigin struct {
	// GitURL is the clone URL of the project remote.
	// +kubebuilder:validation:MinLength=1
	GitURL string `json:"gitUrl"`

	// DefaultBranch is the branch sessions base their work on.
	// +kubebuilder:default=main
	DefaultBranch string `json:"defaultBranch,omitempty"`
}

// WorkspaceStorage shapes the NAS-backed PVC behind the workspace.
type WorkspaceStorage struct {
	// StorageClassName selects the backing storage class.
	StorageClassName string `json:"storageClassName,omitempty"`

	// Size of the workspace volume.
	Size resource.Quantity `json:"size"`

	// AccessModes of the claim; defaults to ReadWriteOnce, which the
	// single-active-session lease is designed around. Set ReadWriteMany when
	// the storage class supports shared attach.
	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
}

// LeasePolicy serializes access to the workspace.
type LeasePolicy struct {
	// SingleActiveSession allows at most one running session in this
	// workspace at a time (ADR-0005).
	SingleActiveSession bool `json:"singleActiveSession,omitempty"`
}

// RetentionPolicy bounds how long disposable state is kept.
type RetentionPolicy struct {
	// Scratch is how long session scratch directories are retained.
	Scratch metav1.Duration `json:"scratch,omitempty"`
}

// WorkspaceSpec defines a project home: origin, storage, caches, lease.
type WorkspaceSpec struct {
	// Origin is the project's git remote.
	Origin GitOrigin `json:"origin"`

	// Storage shapes the backing PVC.
	Storage WorkspaceStorage `json:"storage"`

	// Caches selects shared package-manager caches mounted into sessions
	// (ADR-0006), e.g. node, uv, go.
	Caches []string `json:"caches,omitempty"`

	// Lease serializes workspace access.
	Lease *LeasePolicy `json:"lease,omitempty"`

	// Retention bounds scratch lifetime.
	Retention *RetentionPolicy `json:"retention,omitempty"`
}

// WorkspaceStatus is the observed state of a Workspace.
type WorkspaceStatus struct {
	// PVCName is the provisioned backing PersistentVolumeClaim.
	PVCName string `json:"pvcName,omitempty"`

	// ActiveSession holds the lease when SingleActiveSession is set.
	ActiveSession string `json:"activeSession,omitempty"`

	// Conditions describe provisioning state.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Workspace is a project home on the NAS: git origin plus disposable cache.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories=dispatch,shortName=ws
// +kubebuilder:printcolumn:name="PVC",type=string,JSONPath=`.status.pvcName`
// +kubebuilder:printcolumn:name="Active",type=string,JSONPath=`.status.activeSession`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Workspace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkspaceSpec   `json:"spec,omitempty"`
	Status WorkspaceStatus `json:"status,omitempty"`
}

// WorkspaceList contains a list of Workspace.
// +kubebuilder:object:root=true
type WorkspaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workspace `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Workspace{}, &WorkspaceList{})
}

// Package v1alpha1 contains the dispatch.janpuc.com/v1alpha1 API group: the
// Agent, Trigger, Session, and Workspace types that form Dispatch's control
// plane contract (docs/design.md §4).
// +kubebuilder:object:generate=true
// +groupName=dispatch.janpuc.com
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

// Group is the API group served by the Dispatch operator.
const Group = "dispatch.janpuc.com"

var (
	// GroupVersion is the group and version this package registers.
	GroupVersion = schema.GroupVersion{Group: Group, Version: "v1alpha1"}

	// SchemeBuilder collects the package's types for scheme registration.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme registers the package's types with a scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

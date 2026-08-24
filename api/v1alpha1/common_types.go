package v1alpha1

// NamedRef points at a namespace-local object by name.
type NamedRef struct {
	// Name of the referenced object.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// Labels stamped by the operator onto Sessions, Jobs, and pods so that every
// artifact of a run is traceable back to its agent, trigger, and originating
// event.
const (
	LabelAgent       = Group + "/agent"
	LabelTrigger     = Group + "/trigger"
	LabelSession     = Group + "/session"
	LabelFingerprint = Group + "/fingerprint"
)

// AnnotationSuppressedReason marks a Session the gateway created as a
// suppression record: the session controller finalizes it as Suppressed
// without running anything (design §5).
const AnnotationSuppressedReason = Group + "/suppressed"

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SessionPhase is the session lifecycle state. It mirrors the A2A 1.0 task
// lifecycle (ADR-0002) plus the Dispatch-only Suppressed record.
// +kubebuilder:validation:Enum=Submitted;Working;InputRequired;Completed;Failed;Canceled;Rejected;Suppressed
type SessionPhase string

// SessionPhase values.
const (
	SessionSubmitted     SessionPhase = "Submitted"
	SessionWorking       SessionPhase = "Working"
	SessionInputRequired SessionPhase = "InputRequired"
	SessionCompleted     SessionPhase = "Completed"
	SessionFailed        SessionPhase = "Failed"
	SessionCanceled      SessionPhase = "Canceled"
	SessionRejected      SessionPhase = "Rejected"
	SessionSuppressed    SessionPhase = "Suppressed"
)

// IsTerminal reports whether the phase is final and the session immutable.
func (p SessionPhase) IsTerminal() bool {
	switch p {
	case SessionCompleted, SessionFailed, SessionCanceled, SessionRejected, SessionSuppressed:
		return true
	}
	return false
}

// A2ATaskState maps the phase onto the A2A task-state vocabulary exposed at
// the gateway edge; Suppressed maps to rejected.
func (p SessionPhase) A2ATaskState() string {
	switch p {
	case SessionSubmitted:
		return "submitted"
	case SessionWorking:
		return "working"
	case SessionInputRequired:
		return "input-required"
	case SessionCompleted:
		return "completed"
	case SessionFailed:
		return "failed"
	case SessionCanceled:
		return "canceled"
	case SessionRejected, SessionSuppressed:
		return "rejected"
	}
	return ""
}

// SessionInput is the rendered task handed to the runner.
type SessionInput struct {
	// Prompt is the fully rendered task prompt, event payload fenced as
	// untrusted data.
	Prompt string `json:"prompt,omitempty"`
}

// Provenance records where a session came from, precisely enough to explain
// or reproduce the run months later (design §6).
type Provenance struct {
	// Trigger is the Trigger that created this session.
	Trigger string `json:"trigger,omitempty"`

	// EventType is the normalized CloudEvent type, e.g. alertmanager.firing.
	EventType string `json:"eventType,omitempty"`

	// Fingerprint is the dedupe fingerprint of the originating event.
	Fingerprint string `json:"fingerprint,omitempty"`

	// TriageVerdict is the triage classification, when triage ran.
	TriageVerdict string `json:"triageVerdict,omitempty"`

	// RunnerImageDigest pins the runner image that executed the session.
	RunnerImageDigest string `json:"runnerImageDigest,omitempty"`

	// CLIVersion is the agent CLI version inside the runner.
	CLIVersion string `json:"cliVersion,omitempty"`
}

// SessionUsage is the metered spend of one session. Costs are estimates
// (API-equivalent) even when subscription-billed (ADR-0004).
type SessionUsage struct {
	// Billing is subscription or api.
	// +kubebuilder:validation:Enum=subscription;api
	Billing string `json:"billing,omitempty"`

	// InputTokens consumed, excluding cache reads.
	InputTokens int64 `json:"inputTokens,omitempty"`

	// OutputTokens produced.
	OutputTokens int64 `json:"outputTokens,omitempty"`

	// CacheReadTokens served from prompt cache.
	CacheReadTokens int64 `json:"cacheReadTokens,omitempty"`

	// APIEquivalentUSD is the estimated cost had this run been API-billed.
	APIEquivalentUSD *resource.Quantity `json:"apiEquivalentUSD,omitempty"`

	// Turns the session used.
	Turns int32 `json:"turns,omitempty"`
}

// SessionArtifacts locates the reviewable record on the NAS and in git.
type SessionArtifacts struct {
	// Transcript is the URI of the stream-json transcript.
	Transcript string `json:"transcript,omitempty"`

	// Report is the URI of the session's report, when one was produced.
	Report string `json:"report,omitempty"`

	// Branches pushed by the session, always matching the agent's allowed
	// patterns.
	Branches []string `json:"branches,omitempty"`
}

// SessionSpec defines one run: who, on what input, and where it came from.
type SessionSpec struct {
	// AgentRef names the Agent executing this session.
	AgentRef NamedRef `json:"agentRef"`

	// Input is the rendered task.
	Input SessionInput `json:"input,omitempty"`

	// Provenance records the originating trigger and event.
	Provenance Provenance `json:"provenance,omitempty"`
}

// SessionStatus is the immutable record a finished session leaves behind.
type SessionStatus struct {
	// Phase is the lifecycle state.
	Phase SessionPhase `json:"phase,omitempty"`

	// A2ATaskState is the phase in A2A vocabulary, for the gateway edge.
	A2ATaskState string `json:"a2aTaskState,omitempty"`

	// JobName is the Job executing this session.
	JobName string `json:"jobName,omitempty"`

	// Model that served the session.
	Model string `json:"model,omitempty"`

	// StartedAt is when execution began.
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// CompletedAt is when the session reached a terminal phase.
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// Usage is the metered spend, reported by the runner shim.
	Usage *SessionUsage `json:"usage,omitempty"`

	// Artifacts locate the transcript, report, and branches.
	Artifacts *SessionArtifacts `json:"artifacts,omitempty"`

	// Summary is the runner-reported outcome in one paragraph.
	Summary string `json:"summary,omitempty"`

	// FollowUps are runner-proposed next actions.
	FollowUps []string `json:"followUps,omitempty"`

	// Conditions describe scheduling and execution detail.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Session is the logbook entry: one budgeted, recorded run.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories=dispatch,shortName=sess
// +kubebuilder:printcolumn:name="Agent",type=string,JSONPath=`.spec.agentRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=`.status.model`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Session struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SessionSpec   `json:"spec,omitempty"`
	Status SessionStatus `json:"status,omitempty"`
}

// SessionList contains a list of Session.
// +kubebuilder:object:root=true
type SessionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Session `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Session{}, &SessionList{})
}

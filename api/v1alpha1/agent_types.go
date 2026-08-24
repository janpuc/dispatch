package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RunnerSpec selects the container image and agent CLI flavor sessions run
// with, plus the Secret holding that provider's credentials (ADR-0003).
type RunnerSpec struct {
	// Image is the runner OCI image: the curated toolbelt plus a pinned agent
	// CLI and the dispatch-run shim as entrypoint.
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// CLI names the agent CLI flavor inside the image.
	// +kubebuilder:default=claude
	CLI string `json:"cli,omitempty"`

	// CredentialsRef names the Secret with provider credentials. Its keys are
	// injected into runner pods as environment variables (e.g.
	// CLAUDE_CODE_OAUTH_TOKEN for subscription auth) and the Secret is also
	// mounted read-only at /credentials for file-shaped credentials
	// (ADR-0003).
	CredentialsRef NamedRef `json:"credentialsRef"`
}

// ModelPolicy fixes which models an agent uses per spend tier (ADR-0004).
type ModelPolicy struct {
	// Session is the model for dispatched sessions, e.g. claude-fable-5.
	// +kubebuilder:validation:MinLength=1
	Session string `json:"session"`

	// Triage is the cheap, API-billed model used when a Trigger enables
	// triage, e.g. claude-haiku-4-5.
	Triage string `json:"triage,omitempty"`
}

// GitIdentity is how an agent appears in git history and where it may push.
type GitIdentity struct {
	// Author is the commit author identity, "Name <email>".
	Author string `json:"author,omitempty"`

	// PushBranches are glob patterns of branches the agent may push to;
	// everything else is denied by the runner shim.
	PushBranches []string `json:"pushBranches,omitempty"`
}

// SessionBudget bounds a single session.
type SessionBudget struct {
	// MaxTurns caps agent turns within one session.
	MaxTurns int32 `json:"maxTurns,omitempty"`

	// Timeout is the wall-clock bound for one session; the shim ends the
	// session cleanly before the Job deadline would kill it.
	Timeout metav1.Duration `json:"timeout,omitempty"`
}

// DailyBudget bounds an agent's spend per calendar day.
type DailyBudget struct {
	// MaxSessions caps sessions started per day.
	MaxSessions int32 `json:"maxSessions,omitempty"`

	// MaxCostUSD caps the day's estimated API-equivalent cost.
	MaxCostUSD *resource.Quantity `json:"maxCostUSD,omitempty"`
}

// AgentBudgets are the guardrails from ADR-0004.
type AgentBudgets struct {
	// MaxConcurrentSessions caps simultaneously running sessions for this
	// agent; zero means unlimited.
	MaxConcurrentSessions int32 `json:"maxConcurrentSessions,omitempty"`

	// PerSession bounds each individual session.
	PerSession SessionBudget `json:"perSession,omitempty"`

	// Daily bounds the agent's spend per day.
	Daily DailyBudget `json:"daily,omitempty"`

	// SubscriptionWindowReserve is the fraction (0–1) of the provider's
	// current usage window kept free for interactive use; the scheduler
	// defers sessions that would eat into it.
	SubscriptionWindowReserve *resource.Quantity `json:"subscriptionWindowReserve,omitempty"`
}

// A2ASkill is one advertised capability on the agent's A2A card.
type A2ASkill struct {
	// ID is the skill identifier on the card.
	ID string `json:"id"`

	// Description is the human-readable skill summary.
	Description string `json:"description,omitempty"`
}

// A2AExposure controls the agent's presence on the gateway's A2A edge
// (ADR-0002).
type A2AExposure struct {
	// Expose publishes an Agent Card at /.well-known/agent-card.json for this
	// agent and accepts A2A messages for it.
	Expose bool `json:"expose,omitempty"`

	// Skills advertised on the card.
	Skills []A2ASkill `json:"skills,omitempty"`
}

// AgentSpec defines who runs: model, credentials, workspace, and limits.
type AgentSpec struct {
	// Description is a human-readable summary of the agent's charter.
	Description string `json:"description,omitempty"`

	// Runner selects image, CLI flavor, and credentials.
	Runner RunnerSpec `json:"runner"`

	// Models fixes the session and triage models.
	Models ModelPolicy `json:"models"`

	// WorkspaceRef names the Workspace this agent operates in.
	WorkspaceRef NamedRef `json:"workspaceRef"`

	// ServiceAccountName is the ServiceAccount runner pods use; grants should
	// be least-privilege and read-only toward the cluster by default.
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// Git is the agent's git identity and push policy.
	Git GitIdentity `json:"git,omitempty"`

	// Budgets are the agent's spend and concurrency guardrails.
	Budgets AgentBudgets `json:"budgets,omitempty"`

	// A2A controls exposure on the A2A edge.
	A2A *A2AExposure `json:"a2a,omitempty"`
}

// AgentStatus is the observed state of an Agent.
type AgentStatus struct {
	// ObservedGeneration is the last spec generation the operator acted on.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions describe the agent's readiness.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Agent is a member of the Dispatch fleet: identity, defaults, and limits.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories=dispatch
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=`.spec.models.session`
// +kubebuilder:printcolumn:name="Workspace",type=string,JSONPath=`.spec.workspaceRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentSpec   `json:"spec,omitempty"`
	Status AgentStatus `json:"status,omitempty"`
}

// AgentList contains a list of Agent.
// +kubebuilder:object:root=true
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Agent{}, &AgentList{})
}

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Priority orders queued sessions when budgets or windows are contended.
// +kubebuilder:validation:Enum=low;normal;high
type Priority string

// Priority levels.
const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
)

// BudgetPolicy says what happens to a matched event when the agent's budget
// or subscription window is exhausted (ADR-0004).
// +kubebuilder:validation:Enum=deferIfExhausted;degradeModel;drop
type BudgetPolicy string

// BudgetPolicy values.
const (
	BudgetDeferIfExhausted BudgetPolicy = "deferIfExhausted"
	BudgetDegradeModel     BudgetPolicy = "degradeModel"
	BudgetDrop             BudgetPolicy = "drop"
)

// AlertmanagerSource receives Alertmanager webhook notifications.
type AlertmanagerSource struct{}

// WebhookSource receives generic HMAC-verified webhooks, e.g. from GitHub.
type WebhookSource struct {
	// Path is the gateway HTTP path this webhook is served on.
	// +kubebuilder:validation:Pattern=`^/`
	Path string `json:"path"`

	// HMACSecretRef names the Secret holding the shared HMAC key used to
	// verify deliveries.
	HMACSecretRef NamedRef `json:"hmacSecretRef,omitempty"`
}

// KubernetesEventSource matches cluster Events watched by the gateway.
type KubernetesEventSource struct{}

// ScheduleSource fires on a cron schedule.
type ScheduleSource struct {
	// Cron is a standard five-field cron expression.
	// +kubebuilder:validation:MinLength=1
	Cron string `json:"cron"`

	// Timezone is an IANA timezone name; UTC when empty.
	Timezone string `json:"timezone,omitempty"`
}

// A2AMessageSource fires on A2A messages addressed to the agent, e.g. tasks
// sent from t3.
type A2AMessageSource struct{}

// TriggerSource selects exactly one event source.
type TriggerSource struct {
	Alertmanager    *AlertmanagerSource    `json:"alertmanager,omitempty"`
	Webhook         *WebhookSource         `json:"webhook,omitempty"`
	KubernetesEvent *KubernetesEventSource `json:"kubernetesEvent,omitempty"`
	Schedule        *ScheduleSource        `json:"schedule,omitempty"`
	A2AMessage      *A2AMessageSource      `json:"a2aMessage,omitempty"`
}

// DedupePolicy suppresses repeats of the same underlying event.
type DedupePolicy struct {
	// Key is a CEL expression over the normalized event yielding the dedupe
	// fingerprint.
	Key string `json:"key"`

	// Cooldown is how long a fingerprint stays suppressed after firing.
	Cooldown metav1.Duration `json:"cooldown,omitempty"`
}

// TriagePolicy optionally puts a cheap, API-billed model call in front of the
// expensive session (ADR-0004).
type TriagePolicy struct {
	// Enabled turns triage on for this trigger.
	Enabled bool `json:"enabled,omitempty"`

	// Question is the classification question the triage model answers.
	Question string `json:"question,omitempty"`

	// DailyBudgetUSD caps triage spend per day for this trigger.
	DailyBudgetUSD *resource.Quantity `json:"dailyBudgetUSD,omitempty"`

	// OnBudgetExhausted picks the deterministic fallback once the triage
	// budget is spent: allow dispatches without triage, deny suppresses.
	// +kubebuilder:validation:Enum=allow;deny
	OnBudgetExhausted string `json:"onBudgetExhausted,omitempty"`
}

// TriggerSessionTemplate shapes the session a firing trigger creates.
type TriggerSessionTemplate struct {
	// Prompt is the task template; event payload fields are rendered into it
	// fenced as untrusted data (design §9).
	// +kubebuilder:validation:MinLength=1
	Prompt string `json:"prompt"`
}

// TriggerSpec binds an event source to an agent behind deterministic gates.
type TriggerSpec struct {
	// AgentRef names the Agent woken by this trigger.
	AgentRef NamedRef `json:"agentRef"`

	// Source selects the event source.
	Source TriggerSource `json:"source"`

	// When is a CEL expression over the normalized CloudEvent; the event is
	// dropped unless it evaluates true. Zero tokens are spent here.
	When string `json:"when,omitempty"`

	// Dedupe suppresses repeats within a cooldown.
	Dedupe *DedupePolicy `json:"dedupe,omitempty"`

	// Debounce delays dispatch to coalesce bursts.
	Debounce *metav1.Duration `json:"debounce,omitempty"`

	// Priority orders this trigger's sessions in the queue.
	// +kubebuilder:default=normal
	Priority Priority `json:"priority,omitempty"`

	// Triage optionally gates dispatch behind a cheap model call.
	Triage *TriagePolicy `json:"triage,omitempty"`

	// BudgetPolicy says what to do when budgets or windows are exhausted.
	// +kubebuilder:default=deferIfExhausted
	BudgetPolicy BudgetPolicy `json:"budgetPolicy,omitempty"`

	// Session shapes the created session.
	Session TriggerSessionTemplate `json:"session"`
}

// TriggerStatus is the observed state of a Trigger.
type TriggerStatus struct {
	// LastFiredAt is when this trigger last created a session.
	LastFiredAt *metav1.Time `json:"lastFiredAt,omitempty"`

	// FiredTotal counts sessions this trigger created.
	FiredTotal int64 `json:"firedTotal,omitempty"`

	// SuppressedTotal counts events vetoed by gates after matching.
	SuppressedTotal int64 `json:"suppressedTotal,omitempty"`

	// Conditions describe the trigger's health.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Trigger is the duty roster: when an agent wakes, and with what context.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories=dispatch
// +kubebuilder:printcolumn:name="Agent",type=string,JSONPath=`.spec.agentRef.name`
// +kubebuilder:printcolumn:name="Priority",type=string,JSONPath=`.spec.priority`
// +kubebuilder:printcolumn:name="Fired",type=integer,JSONPath=`.status.firedTotal`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Trigger struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TriggerSpec   `json:"spec,omitempty"`
	Status TriggerStatus `json:"status,omitempty"`
}

// TriggerList contains a list of Trigger.
// +kubebuilder:object:root=true
type TriggerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Trigger `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Trigger{}, &TriggerList{})
}

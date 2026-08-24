// Package task defines the file contract between the operator and the
// dispatch-run shim inside runner pods (ADR-0003): the operator renders a
// task.json into a ConfigMap, the shim reads it at /task/task.json.
package task

import "encoding/json"

// MountPath is where the task document is mounted inside runner pods.
const MountPath = "/task"

// FileName is the task document's file name inside MountPath.
const FileName = "task.json"

// ReportPathToken is the placeholder the gateway renders for
// {{ .Session.ReportPath }}; the runner shim substitutes the session's real
// report path before invoking the CLI, since only the shim knows it.
const ReportPathToken = "__DISPATCH_REPORT_PATH__"

// File is the task.json document.
type File struct {
	// Session is the Session object name.
	Session string `json:"session"`

	// Agent is the Agent object name.
	Agent string `json:"agent"`

	// Prompt is the rendered task prompt.
	Prompt string `json:"prompt"`

	// Model the CLI must run with.
	Model string `json:"model"`

	// CLI flavor to invoke, e.g. claude.
	CLI string `json:"cli"`

	// GitURL is the workspace's origin remote; empty runs the session in a
	// bare scratch directory without git.
	GitURL string `json:"gitUrl,omitempty"`

	// DefaultBranch is the branch the session branch is cut from.
	DefaultBranch string `json:"defaultBranch,omitempty"`

	// MaxTurns caps agent turns; zero means the CLI default.
	MaxTurns int32 `json:"maxTurns,omitempty"`

	// TimeoutSeconds is the wall-clock budget the shim enforces by ending the
	// session cleanly; zero means none.
	TimeoutSeconds int64 `json:"timeoutSeconds,omitempty"`

	// GitAuthor is the commit identity, "Name <email>".
	GitAuthor string `json:"gitAuthor,omitempty"`

	// PushBranches are the branch glob patterns the shim allows pushes to.
	PushBranches []string `json:"pushBranches,omitempty"`

	// Trigger is the originating Trigger name, for provenance.
	Trigger string `json:"trigger,omitempty"`

	// Fingerprint is the originating event fingerprint, for provenance.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// Marshal renders the task document as indented JSON.
func (f File) Marshal() ([]byte, error) {
	return json.MarshalIndent(f, "", "  ")
}

package shim

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Outcome values for Result.
const (
	OutcomeCompleted = "completed"
	OutcomeFailed    = "failed"
)

// TerminationLogPath is where the compact result is written so the operator
// can harvest it from the pod's termination message.
const TerminationLogPath = "/dev/termination-log"

const (
	resultFileName          = "result.json"
	terminationMessageLimit = 4000
	summaryLimit            = 2000
)

// Usage is the metered spend of the session, in Session status vocabulary.
type Usage struct {
	Billing          string `json:"billing,omitempty"`
	InputTokens      int64  `json:"inputTokens,omitempty"`
	OutputTokens     int64  `json:"outputTokens,omitempty"`
	CacheReadTokens  int64  `json:"cacheReadTokens,omitempty"`
	APIEquivalentUSD string `json:"apiEquivalentUSD,omitempty"`
	Turns            int32  `json:"turns,omitempty"`
}

// Artifacts locate the session record.
type Artifacts struct {
	Transcript string   `json:"transcript,omitempty"`
	Report     string   `json:"report,omitempty"`
	Branches   []string `json:"branches,omitempty"`
}

// Result is the shim's terminal report: written in full to the session
// directory and in compact form to the termination log.
type Result struct {
	Outcome   string    `json:"outcome"`
	Summary   string    `json:"summary,omitempty"`
	SessionID string    `json:"sessionId,omitempty"`
	Usage     Usage     `json:"usage,omitempty"`
	Artifacts Artifacts `json:"artifacts,omitempty"`
	FollowUps []string  `json:"followUps,omitempty"`
}

// Write persists result.json into the session directory.
func (r Result) Write(dir string) error {
	payload, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, resultFileName), payload, 0o644)
}

// WriteTerminationMessage writes the compact result, shrinking the summary
// until the payload fits the kubelet's 4096-byte termination message cap.
func (r Result) WriteTerminationMessage(path string) error {
	compact := r
	compact.Summary = truncate(compact.Summary, summaryLimit)
	payload, err := json.Marshal(compact)
	if err != nil {
		return err
	}
	for len(payload) > terminationMessageLimit && len(compact.Summary) > 0 {
		compact.Summary = truncate(compact.Summary, len(compact.Summary)/2)
		if payload, err = json.Marshal(compact); err != nil {
			return err
		}
	}
	return os.WriteFile(path, payload, 0o644)
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}

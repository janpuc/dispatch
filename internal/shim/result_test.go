package shim

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTerminationMessageTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "termination-log")
	result := Result{
		Outcome: OutcomeCompleted,
		Summary: strings.Repeat("finding ", 2000),
		Usage:   Usage{Billing: "subscription", InputTokens: 100, Turns: 5},
	}
	if err := result.WriteTerminationMessage(path); err != nil {
		t.Fatalf("WriteTerminationMessage: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if len(raw) > 4096 {
		t.Errorf("termination message is %d bytes, exceeds kubelet cap", len(raw))
	}
	var parsed Result
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("termination message is not JSON: %v", err)
	}
	if parsed.Outcome != OutcomeCompleted || parsed.Usage.InputTokens != 100 {
		t.Errorf("parsed = %+v", parsed)
	}
}

package shim

import (
	"slices"
	"testing"

	"github.com/janpuc/dispatch/internal/task"
)

func TestBuildInvocation(t *testing.T) {
	invocation := BuildInvocation(task.File{
		CLI:      "",
		Model:    "claude-fable-5",
		MaxTurns: 60,
		Prompt:   "do the thing",
	})
	if invocation.Bin != "claude" {
		t.Errorf("bin = %q", invocation.Bin)
	}
	if invocation.Prompt != "do the thing" {
		t.Errorf("prompt = %q", invocation.Prompt)
	}
	for _, required := range [][]string{
		{"-p"},
		{"--output-format", "stream-json"},
		{"--model", "claude-fable-5"},
		{"--max-turns", "60"},
		{"--permission-mode", "acceptEdits"},
	} {
		if !containsSequence(invocation.Args, required) {
			t.Errorf("args %v missing %v", invocation.Args, required)
		}
	}
}

func TestBuildInvocationOmitsUnsetFlags(t *testing.T) {
	invocation := BuildInvocation(task.File{Prompt: "p"})
	if slices.Contains(invocation.Args, "--max-turns") {
		t.Errorf("unexpected --max-turns in %v", invocation.Args)
	}
	if slices.Contains(invocation.Args, "--model") {
		t.Errorf("unexpected --model in %v", invocation.Args)
	}
}

func containsSequence(haystack, needle []string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if slices.Equal(haystack[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}

package shim

import (
	"strings"
	"testing"
)

func TestScrubber(t *testing.T) {
	scrubber := NewScrubber()
	cases := []string{
		"key sk-ant-api03-abcdef123456789",
		"token ghp_abcdefghijklmnopqrstuv0123456789",
		"pat github_pat_11ABCDEFG0123456789_abcdefghijklmnop",
		"slack xoxb-123456789012-abcdefghijkl",
		"aws AKIAIOSFODNN7EXAMPLE",
	}
	for _, line := range cases {
		scrubbed := scrubber.Scrub(line)
		if !strings.Contains(scrubbed, redactedPlaceholder) {
			t.Errorf("not redacted: %q -> %q", line, scrubbed)
		}
	}
	benign := `{"type":"assistant","text":"refactored the session controller"}`
	if scrubber.Scrub(benign) != benign {
		t.Errorf("benign line altered: %q", scrubber.Scrub(benign))
	}
}

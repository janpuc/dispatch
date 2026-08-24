package shim

import "regexp"

const redactedPlaceholder = "[REDACTED]"

// Scrubber redacts credential-shaped substrings from transcript lines before
// they are persisted (design §9).
type Scrubber struct {
	patterns []*regexp.Regexp
}

// NewScrubber compiles the built-in credential patterns: Anthropic keys and
// OAuth tokens, GitHub tokens, Slack tokens, AWS access key ids, and JWTs.
func NewScrubber() *Scrubber {
	raw := []string{
		`sk-ant-[A-Za-z0-9_-]{8,}`,
		`ghp_[A-Za-z0-9]{20,}`,
		`gho_[A-Za-z0-9]{20,}`,
		`github_pat_[A-Za-z0-9_]{20,}`,
		`xox[baprs]-[A-Za-z0-9-]{10,}`,
		`AKIA[0-9A-Z]{16}`,
		`eyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{10,}`,
	}
	s := &Scrubber{}
	for _, pattern := range raw {
		s.patterns = append(s.patterns, regexp.MustCompile(pattern))
	}
	return s
}

// Scrub returns the line with credential-shaped substrings replaced.
func (s *Scrubber) Scrub(line string) string {
	for _, pattern := range s.patterns {
		line = pattern.ReplaceAllString(line, redactedPlaceholder)
	}
	return line
}

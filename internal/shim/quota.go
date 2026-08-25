package shim

import "strings"

// QuotaExhaustedMarkers are phrases the agent CLI emits when the provider
// subscription window is spent. The CLI can sit in a long retry loop after
// printing one, so the shim treats a marker as terminal and ends the session
// rather than burning its whole timeout budget.
var QuotaExhaustedMarkers = []string{
	"out of usage credits",
	"usage limit reached",
	"upgrade to increase your usage limit",
}

// QuotaExhausted reports whether a transcript line signals an exhausted
// provider quota.
func QuotaExhausted(line string) bool {
	lowered := strings.ToLower(line)
	for _, marker := range QuotaExhaustedMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

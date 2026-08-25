package gateway

import (
	"strings"

	"github.com/janpuc/dispatch/internal/controller"
)

const selfReferenceMaxDepth = 6

// IsSelfEvent reports whether an event describes Dispatch's own session
// workloads. A failing session Job raises a KubeJobFailed alert naming that
// Job; dispatching on it creates another session that fails the same way, so
// the gateway drops these unless a Trigger sets allowSelfEvents.
func IsSelfEvent(event Event) bool {
	return referencesSessionJob(event.Data, 0)
}

func referencesSessionJob(value any, depth int) bool {
	if depth > selfReferenceMaxDepth {
		return false
	}
	switch typed := value.(type) {
	case string:
		return strings.HasPrefix(typed, controller.SessionJobPrefix)
	case map[string]any:
		for _, nested := range typed {
			if referencesSessionJob(nested, depth+1) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if referencesSessionJob(nested, depth+1) {
				return true
			}
		}
	}
	return false
}

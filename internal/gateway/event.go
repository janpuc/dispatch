// Package gateway implements Dispatch's event edge (design §5): sources
// normalize into Events, CEL gates filter them for zero tokens, dedupe and
// budgets veto deterministically, and survivors become Sessions.
package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

const fingerprintHashChars = 16

// Event is the normalized event every source produces: what CEL gates,
// dedupe keys, and prompt templates operate on.
type Event struct {
	Type        string
	Source      string
	Fingerprint string
	Time        time.Time
	Data        map[string]any
}

// CELValue renders the event as the `event` variable for Trigger CEL
// expressions.
func (e Event) CELValue() map[string]any {
	return map[string]any{
		"type":        e.Type,
		"source":      e.Source,
		"fingerprint": e.Fingerprint,
		"time":        e.Time.UTC().Format(time.RFC3339),
		"data":        e.Data,
	}
}

// HashFingerprint reduces an arbitrary dedupe key to a label-safe value.
func HashFingerprint(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])[:fingerprintHashChars]
}

package shim

import (
	"encoding/json"
	"fmt"

	"github.com/janpuc/dispatch/internal/task"
)

func unmarshalTask(raw []byte, doc *task.File) error {
	if err := json.Unmarshal(raw, doc); err != nil {
		return fmt.Errorf("parsing task file: %w", err)
	}
	if doc.Session == "" || doc.Agent == "" {
		return fmt.Errorf("task file missing session or agent name")
	}
	if doc.Prompt == "" {
		return fmt.Errorf("task file missing prompt")
	}
	return nil
}

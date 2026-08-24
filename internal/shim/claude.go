package shim

import (
	"strconv"

	"github.com/janpuc/dispatch/internal/task"
)

// CLIInvocation is the rendered headless agent CLI command for one task.
type CLIInvocation struct {
	Bin    string
	Args   []string
	Prompt string
}

// BuildInvocation renders the CLI command from the task document. The flag
// surface is the one verified in ADR-0003: non-bare -p mode so subscription
// OAuth works, stream-json transcript, acceptEdits with Bash allowed since
// the pod itself is the sandbox boundary.
func BuildInvocation(doc task.File) CLIInvocation {
	bin := doc.CLI
	if bin == "" {
		bin = "claude"
	}
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--permission-mode", "acceptEdits",
		"--allowedTools", "Bash",
	}
	if doc.Model != "" {
		args = append(args, "--model", doc.Model)
	}
	if doc.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(int(doc.MaxTurns)))
	}
	return CLIInvocation{Bin: bin, Args: args, Prompt: doc.Prompt}
}

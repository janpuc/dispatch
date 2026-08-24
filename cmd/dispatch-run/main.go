// dispatch-run is the runner-pod entrypoint: it executes one Dispatch session
// per the task contract and reports the result (ADR-0003).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/janpuc/dispatch/internal/shim"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	result, err := shim.Run(ctx, shim.ConfigFromEnv())
	if err != nil {
		fmt.Fprintln(os.Stderr, "dispatch-run:", err)
		os.Exit(1)
	}
	if result.Outcome != shim.OutcomeCompleted {
		os.Exit(1)
	}
}

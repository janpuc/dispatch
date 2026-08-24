package gateway

import (
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
	"k8s.io/apimachinery/pkg/types"

	dispatchv1alpha1 "github.com/janpuc/dispatch/api/v1alpha1"
)

type compiledTrigger struct {
	generation int64
	when       cel.Program
	dedupeKey  cel.Program
}

// CELGate compiles and caches Trigger CEL expressions, keyed by trigger UID
// and invalidated on generation change.
type CELGate struct {
	env   *cel.Env
	mu    sync.Mutex
	cache map[types.UID]*compiledTrigger
}

// NewCELGate builds the CEL environment Trigger expressions evaluate in: a
// single `event` variable holding the normalized event.
func NewCELGate() (*CELGate, error) {
	env, err := cel.NewEnv(cel.Variable("event", cel.MapType(cel.StringType, cel.DynType)))
	if err != nil {
		return nil, err
	}
	return &CELGate{env: env, cache: map[types.UID]*compiledTrigger{}}, nil
}

func (g *CELGate) compile(trigger *dispatchv1alpha1.Trigger) (*compiledTrigger, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if cached, ok := g.cache[trigger.UID]; ok && cached.generation == trigger.Generation {
		return cached, nil
	}
	compiled := &compiledTrigger{generation: trigger.Generation}
	if expr := trigger.Spec.When; expr != "" {
		program, err := g.compileExpression(expr)
		if err != nil {
			return nil, fmt.Errorf("when: %w", err)
		}
		compiled.when = program
	}
	if trigger.Spec.Dedupe != nil && trigger.Spec.Dedupe.Key != "" {
		program, err := g.compileExpression(trigger.Spec.Dedupe.Key)
		if err != nil {
			return nil, fmt.Errorf("dedupe key: %w", err)
		}
		compiled.dedupeKey = program
	}
	g.cache[trigger.UID] = compiled
	return compiled, nil
}

func (g *CELGate) compileExpression(expr string) (cel.Program, error) {
	ast, issues := g.env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	return g.env.Program(ast)
}

// Evaluate runs the trigger's when-gate and dedupe-key expressions against
// the event. The dedupe key falls back to the event fingerprint.
func (g *CELGate) Evaluate(trigger *dispatchv1alpha1.Trigger, event Event) (bool, string, error) {
	compiled, err := g.compile(trigger)
	if err != nil {
		return false, "", err
	}
	activation := map[string]any{"event": event.CELValue()}

	if compiled.when != nil {
		out, _, err := compiled.when.Eval(activation)
		if err != nil {
			return false, "", fmt.Errorf("when: %w", err)
		}
		matched, ok := out.Value().(bool)
		if !ok {
			return false, "", fmt.Errorf("when: expression yields %T, want bool", out.Value())
		}
		if !matched {
			return false, "", nil
		}
	}

	key := event.Fingerprint
	if compiled.dedupeKey != nil {
		out, _, err := compiled.dedupeKey.Eval(activation)
		if err != nil {
			return false, "", fmt.Errorf("dedupe key: %w", err)
		}
		key = fmt.Sprintf("%v", out.Value())
	}
	return true, key, nil
}

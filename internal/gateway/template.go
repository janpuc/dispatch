package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	dispatchv1alpha1 "github.com/janpuc/dispatch/api/v1alpha1"
	"github.com/janpuc/dispatch/internal/task"
)

// SessionsPathHint is what {{ .Paths.Sessions }} renders to: prose the agent
// can act on, since the concrete path is only known inside the runner.
const SessionsPathHint = ".dispatch/sessions inside the workspace (or /sessions when a shared records volume is mounted)"

type promptEvent struct {
	Type        string
	Source      string
	Fingerprint string
	Data        map[string]any
}

type promptSession struct {
	ID         string
	Name       string
	ReportPath string
}

type promptRef struct {
	Name string
}

type promptPaths struct {
	Sessions string
}

type promptContext struct {
	Event   promptEvent
	Session promptSession
	Trigger promptRef
	Agent   promptRef
	Paths   promptPaths
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"toJson": func(v any) string {
			raw, err := json.Marshal(v)
			if err != nil {
				return fmt.Sprintf("%v", v)
			}
			return string(raw)
		},
	}
}

// RenderPrompt renders the trigger's prompt template with the event fenced in
// and session-scoped paths tokenized for the runner shim to resolve.
func RenderPrompt(trigger *dispatchv1alpha1.Trigger, agentName, sessionName string, event Event) (string, error) {
	parsed, err := template.New("prompt").Funcs(templateFuncs()).Parse(trigger.Spec.Session.Prompt)
	if err != nil {
		return "", fmt.Errorf("parsing prompt template: %w", err)
	}
	context := promptContext{
		Event: promptEvent{
			Type:        event.Type,
			Source:      event.Source,
			Fingerprint: event.Fingerprint,
			Data:        event.Data,
		},
		Session: promptSession{
			ID:         sessionName,
			Name:       sessionName,
			ReportPath: task.ReportPathToken,
		},
		Trigger: promptRef{Name: trigger.Name},
		Agent:   promptRef{Name: agentName},
		Paths:   promptPaths{Sessions: SessionsPathHint},
	}
	var rendered strings.Builder
	if err := parsed.Execute(&rendered, context); err != nil {
		return "", fmt.Errorf("rendering prompt template: %w", err)
	}
	return rendered.String(), nil
}

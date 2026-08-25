package shim

import (
	"encoding/json"
	"io"
	"time"
)

const (
	recordTextLimit  = 2000
	recordInputLimit = 500
)

// Record event names. Every line carries stream: "dispatch-session" so a log
// query can select session records without matching on anything else.
const (
	recordStreamName = "dispatch-session"

	EventStart     = "start"
	EventAssistant = "assistant"
	EventTool      = "tool"
	EventReport    = "report"
	EventEnd       = "end"
)

// Recorder writes one JSON line per session milestone to the runner's stdout,
// where the cluster's log collector picks it up. This is what makes a session
// reviewable after its pod is gone: the transcript file lives on a workspace
// volume nothing else can reach (design §8).
type Recorder struct {
	out     io.Writer
	session string
	agent   string
	model   string
	now     func() time.Time
}

// NewRecorder builds a Recorder for one session.
func NewRecorder(out io.Writer, session, agent, model string, now func() time.Time) *Recorder {
	if now == nil {
		now = time.Now
	}
	return &Recorder{out: out, session: session, agent: agent, model: model, now: now}
}

// Emit writes one record, merging the session identity into every line.
func (r *Recorder) Emit(event string, fields map[string]any) {
	if r == nil || r.out == nil {
		return
	}
	line := map[string]any{
		"time":    r.now().UTC().Format(time.RFC3339),
		"stream":  recordStreamName,
		"event":   event,
		"session": r.session,
		"agent":   r.agent,
		"model":   r.model,
	}
	for key, value := range fields {
		line[key] = value
	}
	payload, err := json.Marshal(line)
	if err != nil {
		return
	}
	_, _ = r.out.Write(append(payload, '\n'))
}

// EmitStreamLine distills one agent-CLI transcript line into a record. Only
// assistant text and tool calls are surfaced; everything else stays in the
// transcript file.
func (r *Recorder) EmitStreamLine(line string) {
	if r == nil {
		return
	}
	var envelope struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Type  string          `json:"type"`
				Text  string          `json:"text"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(line), &envelope) != nil || envelope.Type != "assistant" {
		return
	}
	for _, block := range envelope.Message.Content {
		switch block.Type {
		case "text":
			if text := truncate(block.Text, recordTextLimit); text != "" {
				r.Emit(EventAssistant, map[string]any{"text": text})
			}
		case "tool_use":
			r.Emit(EventTool, map[string]any{
				"tool":  block.Name,
				"input": truncate(string(block.Input), recordInputLimit),
			})
		}
	}
}

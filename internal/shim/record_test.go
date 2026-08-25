package shim

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func decodeRecords(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("record is not JSON: %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func TestRecorderEmitsIdentityOnEveryLine(t *testing.T) {
	var out bytes.Buffer
	clock := func() time.Time { return time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC) }
	recorder := NewRecorder(&out, "guardian-x", "guardian", "claude-opus-5", clock)

	recorder.Emit(EventStart, "session started", map[string]any{"trigger": "guardian-alerts"})
	recorder.Emit(EventEnd, "", map[string]any{"outcome": OutcomeCompleted, "turns": 4})

	records := decodeRecords(t, out.String())
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	for _, record := range records {
		for field, want := range map[string]string{
			"stream":  recordStreamName,
			"session": "guardian-x",
			"agent":   "guardian",
			"model":   "claude-opus-5",
		} {
			if record[field] != want {
				t.Errorf("%s = %v, want %v", field, record[field], want)
			}
		}
		if record["time"] != "2026-08-25T09:00:00Z" {
			t.Errorf("time = %v", record["time"])
		}
	}
	if records[0]["trigger"] != "guardian-alerts" || records[1]["outcome"] != OutcomeCompleted {
		t.Errorf("payload fields lost: %+v %+v", records[0], records[1])
	}
	if records[0]["_msg"] != "session started" {
		t.Errorf("_msg = %v, want the supplied message", records[0]["_msg"])
	}
	if records[1]["_msg"] != EventEnd {
		t.Errorf("_msg = %v, want the event name as fallback", records[1]["_msg"])
	}
}

func TestRecorderDistillsStreamLines(t *testing.T) {
	var out bytes.Buffer
	recorder := NewRecorder(&out, "s", "a", "m", nil)

	recorder.EmitStreamLine(`{"type":"assistant","message":{"content":[{"type":"text","text":"checking the pods"}]}}`)
	recorder.EmitStreamLine(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"kubectl get pods"}}]}}`)
	recorder.EmitStreamLine(`{"type":"system","subtype":"init"}`)
	recorder.EmitStreamLine(`{"type":"result","result":"done"}`)

	records := decodeRecords(t, out.String())
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2 (system and result lines stay in the transcript)", len(records))
	}
	if records[0]["event"] != EventAssistant || records[0]["text"] != "checking the pods" {
		t.Errorf("assistant record = %+v", records[0])
	}
	if records[1]["event"] != EventTool || records[1]["tool"] != "Bash" {
		t.Errorf("tool record = %+v", records[1])
	}
	if input, _ := records[1]["input"].(string); !strings.Contains(input, "kubectl get pods") {
		t.Errorf("tool input = %v", records[1]["input"])
	}
}

func TestRecorderTruncatesLongText(t *testing.T) {
	var out bytes.Buffer
	recorder := NewRecorder(&out, "s", "a", "m", nil)
	recorder.Emit(EventAssistant, "long", map[string]any{"text": truncate(strings.Repeat("x", 9000), recordTextLimit)})

	records := decodeRecords(t, out.String())
	if text, _ := records[0]["text"].(string); len(text) != recordTextLimit {
		t.Errorf("text length = %d, want %d", len(text), recordTextLimit)
	}
}

func TestNilRecorderIsSafe(t *testing.T) {
	var recorder *Recorder
	recorder.Emit(EventStart, "m", nil)
	recorder.EmitStreamLine(`{"type":"assistant"}`)
}

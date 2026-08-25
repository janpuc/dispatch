package shim

import (
	"bytes"
	"strings"
	"testing"
)

func TestProcessStream(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"abc"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"token sk-ant-abcdefgh12345678 leaked"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":3,"result":"done things","session_id":"abc","total_cost_usd":1.25,"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":100}}`,
	}, "\n")

	var out bytes.Buffer
	scrubber := NewScrubber()
	result, err := ProcessStream(strings.NewReader(input), &out, StreamHandlers{Scrub: scrubber.Scrub})
	if err != nil {
		t.Fatalf("ProcessStream: %v", err)
	}
	if result == nil {
		t.Fatal("no result line captured")
	}
	if result.NumTurns != 3 || result.TotalCostUSD != 1.25 || result.SessionID != "abc" {
		t.Errorf("result = %+v", result)
	}
	if result.Usage.InputTokens != 10 || result.Usage.CacheReadInputTokens != 100 {
		t.Errorf("usage = %+v", result.Usage)
	}
	if strings.Contains(out.String(), "sk-ant-abcdefgh") {
		t.Error("transcript still contains the credential")
	}
	if !strings.Contains(out.String(), redactedPlaceholder) {
		t.Error("transcript missing redaction placeholder")
	}
}

func TestProcessStreamWithoutResult(t *testing.T) {
	var out bytes.Buffer
	result, err := ProcessStream(strings.NewReader(`{"type":"assistant"}`), &out, StreamHandlers{})
	if err != nil {
		t.Fatalf("ProcessStream: %v", err)
	}
	if result != nil {
		t.Errorf("result = %+v, want nil", result)
	}
}

func TestProcessStreamSignalsQuotaExhaustion(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"You're out of usage credits. Switch to another model to continue."}]}}`,
	}, "\n")

	var out bytes.Buffer
	signalled := 0
	if _, err := ProcessStream(strings.NewReader(input), &out, StreamHandlers{OnQuotaExhausted: func() { signalled++ }}); err != nil {
		t.Fatalf("ProcessStream: %v", err)
	}
	if signalled != 1 {
		t.Errorf("quota callback fired %d times, want exactly 1", signalled)
	}
	if QuotaExhausted(`{"type":"assistant","text":"all good"}`) {
		t.Error("ordinary line misdetected as quota exhaustion")
	}
}

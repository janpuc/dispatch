package shim

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

const maxTranscriptLineBytes = 16 * 1024 * 1024

// StreamUsage mirrors the usage block of the CLI's final result message.
type StreamUsage struct {
	InputTokens          int64 `json:"input_tokens"`
	OutputTokens         int64 `json:"output_tokens"`
	CacheReadInputTokens int64 `json:"cache_read_input_tokens"`
}

// StreamResult is the final result line of a stream-json transcript.
type StreamResult struct {
	Type         string      `json:"type"`
	Subtype      string      `json:"subtype"`
	IsError      bool        `json:"is_error"`
	NumTurns     int32       `json:"num_turns"`
	Result       string      `json:"result"`
	SessionID    string      `json:"session_id"`
	TotalCostUSD float64     `json:"total_cost_usd"`
	Usage        StreamUsage `json:"usage"`
}

// StreamHandlers customize how ProcessStream treats each transcript line.
type StreamHandlers struct {
	// Scrub redacts credential-shaped text before anything is persisted.
	Scrub func(string) string

	// OnQuotaExhausted fires the first time a line signals a spent provider
	// quota, letting the caller stop a CLI that would otherwise retry until
	// the session times out.
	OnQuotaExhausted func()

	// OnLine sees every scrubbed line, for callers that mirror the transcript
	// somewhere reviewable.
	OnLine func(string)
}

func (h StreamHandlers) scrub(line string) string {
	if h.Scrub == nil {
		return line
	}
	return h.Scrub(line)
}

// ProcessStream copies stream-json lines from r to w, scrubbing each line,
// and returns the last result-typed line when one was seen.
func ProcessStream(r io.Reader, w io.Writer, handlers StreamHandlers) (*StreamResult, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxTranscriptLineBytes)

	var last *StreamResult
	quotaSignalled := false
	for scanner.Scan() {
		line := handlers.scrub(scanner.Text())
		if _, err := fmt.Fprintln(w, line); err != nil {
			return last, err
		}
		if handlers.OnLine != nil {
			handlers.OnLine(line)
		}
		if !quotaSignalled && handlers.OnQuotaExhausted != nil && QuotaExhausted(line) {
			quotaSignalled = true
			handlers.OnQuotaExhausted()
		}
		var probe struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(line), &probe) != nil || probe.Type != "result" {
			continue
		}
		var result StreamResult
		if json.Unmarshal([]byte(line), &result) == nil {
			last = &result
		}
	}
	return last, scanner.Err()
}

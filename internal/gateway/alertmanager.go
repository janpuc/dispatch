package gateway

import (
	"encoding/json"
	"fmt"
	"time"
)

// AlertmanagerPayload is the Alertmanager webhook body (version 4).
type AlertmanagerPayload struct {
	Version      string              `json:"version"`
	Status       string              `json:"status"`
	Receiver     string              `json:"receiver"`
	GroupLabels  map[string]string   `json:"groupLabels"`
	CommonLabels map[string]string   `json:"commonLabels"`
	Alerts       []AlertmanagerAlert `json:"alerts"`
}

// AlertmanagerAlert is one alert within the webhook body.
type AlertmanagerAlert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

// ParseAlertmanager fans a webhook body out into one Event per alert, typed
// alertmanager.firing or alertmanager.resolved, with the alert object as the
// event data.
func ParseAlertmanager(body []byte, now time.Time) ([]Event, error) {
	var payload AlertmanagerPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parsing alertmanager payload: %w", err)
	}
	events := make([]Event, 0, len(payload.Alerts))
	for _, alert := range payload.Alerts {
		raw, err := json.Marshal(alert)
		if err != nil {
			return nil, err
		}
		var data map[string]any
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, err
		}
		events = append(events, Event{
			Type:        "alertmanager." + alert.Status,
			Source:      "alertmanager",
			Fingerprint: alert.Fingerprint,
			Time:        now,
			Data:        data,
		})
	}
	return events, nil
}

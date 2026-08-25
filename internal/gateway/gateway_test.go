package gateway

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	dispatchv1alpha1 "github.com/janpuc/dispatch/api/v1alpha1"
	"github.com/janpuc/dispatch/internal/task"
)

func firingEvent(severity, namespace string) Event {
	return Event{
		Type:        "alertmanager.firing",
		Source:      "alertmanager",
		Fingerprint: "f00d",
		Time:        time.Now(),
		Data: map[string]any{
			"status":      "firing",
			"fingerprint": "f00d",
			"labels": map[string]any{
				"alertname": "NodeDiskIOSaturation",
				"severity":  severity,
				"namespace": namespace,
			},
			"annotations": map[string]any{"summary": "disk io saturated"},
		},
	}
}

func loadExampleTrigger(t *testing.T, name string) *dispatchv1alpha1.Trigger {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	var trigger dispatchv1alpha1.Trigger
	if err := yaml.UnmarshalStrict(raw, &trigger); err != nil {
		t.Fatalf("decoding %s: %v", name, err)
	}
	trigger.UID = types.UID("uid-" + trigger.Name)
	trigger.Generation = 1
	return &trigger
}

func TestParseAlertmanager(t *testing.T) {
	body := []byte(`{
		"version": "4",
		"status": "firing",
		"groupLabels": {"alertname": "NodeDiskIOSaturation"},
		"alerts": [
			{"status": "firing", "labels": {"severity": "warning", "namespace": "default"}, "annotations": {"summary": "s"}, "fingerprint": "abc123"},
			{"status": "resolved", "labels": {"severity": "critical", "namespace": "kube-system"}, "fingerprint": "def456"}
		]
	}`)
	events, err := ParseAlertmanager(body, time.Now())
	if err != nil {
		t.Fatalf("ParseAlertmanager: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Type != "alertmanager.firing" || events[0].Fingerprint != "abc123" {
		t.Errorf("event[0] = %+v", events[0])
	}
	if events[1].Type != "alertmanager.resolved" {
		t.Errorf("event[1].Type = %q", events[1].Type)
	}
	labels, ok := events[0].Data["labels"].(map[string]any)
	if !ok || labels["severity"] != "warning" {
		t.Errorf("event[0] labels = %+v", events[0].Data["labels"])
	}
}

func TestExampleTriggerCEL(t *testing.T) {
	gate, err := NewCELGate()
	if err != nil {
		t.Fatal(err)
	}
	trigger := loadExampleTrigger(t, "trigger-alerts-to-duty.yaml")

	matched, key, err := gate.Evaluate(trigger, firingEvent("critical", "default"))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !matched || key != "f00d" {
		t.Errorf("matched=%v key=%q, want true/f00d", matched, key)
	}

	for name, event := range map[string]Event{
		"excluded namespace": firingEvent("critical", "media"),
		"info severity":      firingEvent("info", "default"),
	} {
		if matched, _, err := gate.Evaluate(trigger, event); err != nil || matched {
			t.Errorf("%s: matched=%v err=%v, want false/nil", name, matched, err)
		}
	}
}

func TestExampleTriggerPrompts(t *testing.T) {
	cases := map[string]Event{
		"trigger-alerts-to-duty.yaml": firingEvent("critical", "default"),
		"trigger-duty-patrol.yaml": {
			Type: "schedule.tick", Source: "schedule", Fingerprint: "tick",
			Data: map[string]any{"trigger": "duty-patrol"},
		},
		"trigger-sidequest-issues.yaml": {
			Type: "github.issues", Source: "webhook", Fingerprint: "delivery-1",
			Data: map[string]any{
				"action": "labeled",
				"issue": map[string]any{
					"number": float64(7),
					"title":  "add feature",
					"labels": []any{map[string]any{"name": "agent"}},
				},
			},
		},
	}
	for name, event := range cases {
		trigger := loadExampleTrigger(t, name)
		prompt, err := RenderPrompt(trigger, "someagent", "someagent-20260824-1200-abc", event)
		if err != nil {
			t.Errorf("%s: render: %v", name, err)
			continue
		}
		if strings.Contains(prompt, "<no value>") {
			t.Errorf("%s: prompt has unresolved fields:\n%s", name, prompt)
		}
	}

	alertPrompt, err := RenderPrompt(loadExampleTrigger(t, "trigger-alerts-to-duty.yaml"), "duty", "duty-x", firingEvent("critical", "default"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(alertPrompt, `"severity":"critical"`) || !strings.Contains(alertPrompt, task.ReportPathToken) {
		t.Errorf("alert prompt missing payload or report token:\n%s", alertPrompt)
	}
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := dispatchv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func testAgent(maxDaily int32) *dispatchv1alpha1.Agent {
	return &dispatchv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "duty", Namespace: "dispatch"},
		Spec: dispatchv1alpha1.AgentSpec{
			Runner: dispatchv1alpha1.RunnerSpec{
				Image:          "runner:test",
				CredentialsRef: dispatchv1alpha1.NamedRef{Name: "creds"},
			},
			Models:       dispatchv1alpha1.ModelPolicy{Session: "claude-fable-5"},
			WorkspaceRef: dispatchv1alpha1.NamedRef{Name: "homelab"},
			Budgets: dispatchv1alpha1.AgentBudgets{
				Daily: dispatchv1alpha1.DailyBudget{MaxSessions: maxDaily},
			},
		},
	}
}

func TestDispatcherFlow(t *testing.T) {
	scheme := testScheme(t)
	trigger := loadExampleTrigger(t, "trigger-alerts-to-duty.yaml")
	trigger.Namespace = "dispatch"

	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	gate, err := NewCELGate()
	if err != nil {
		t.Fatal(err)
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(testAgent(0), trigger).
		WithStatusSubresource(&dispatchv1alpha1.Trigger{}, &dispatchv1alpha1.Session{}).
		Build()
	dispatcher := &Dispatcher{Client: fakeClient, Gate: gate, Clock: func() time.Time { return now }}

	disposition, err := dispatcher.HandleEvent(context.Background(), trigger, firingEvent("critical", "default"))
	if err != nil || disposition != DispositionDispatched {
		t.Fatalf("disposition = %v err = %v", disposition, err)
	}

	var sessions dispatchv1alpha1.SessionList
	if err := fakeClient.List(context.Background(), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions.Items) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions.Items))
	}
	created := sessions.Items[0]
	if created.Labels[dispatchv1alpha1.LabelFingerprint] != HashFingerprint("f00d") {
		t.Errorf("fingerprint label = %q", created.Labels[dispatchv1alpha1.LabelFingerprint])
	}
	if created.Spec.Provenance.EventType != "alertmanager.firing" {
		t.Errorf("provenance = %+v", created.Spec.Provenance)
	}
	if !strings.Contains(created.Spec.Input.Prompt, "untrusted") {
		t.Errorf("prompt not rendered: %q", created.Spec.Input.Prompt)
	}

	var freshTrigger dispatchv1alpha1.Trigger
	if err := fakeClient.Get(context.Background(), ctrlclient.ObjectKeyFromObject(trigger), &freshTrigger); err != nil {
		t.Fatal(err)
	}
	if freshTrigger.Status.FiredTotal != 1 || freshTrigger.Status.LastFiredAt == nil {
		t.Errorf("trigger status = %+v", freshTrigger.Status)
	}

	if disposition, _ = dispatcher.HandleEvent(context.Background(), trigger, firingEvent("critical", "default")); disposition != DispositionDeduped {
		t.Errorf("repeat disposition = %v, want deduped", disposition)
	}

	if disposition, _ = dispatcher.HandleEvent(context.Background(), trigger, firingEvent("info", "default")); disposition != DispositionFiltered {
		t.Errorf("info disposition = %v, want filtered", disposition)
	}
}

func TestDispatcherDailyBudget(t *testing.T) {
	scheme := testScheme(t)
	trigger := loadExampleTrigger(t, "trigger-alerts-to-duty.yaml")
	trigger.Namespace = "dispatch"
	trigger.Spec.Dedupe = nil

	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	existing := &dispatchv1alpha1.Session{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "duty-earlier",
			Namespace:         "dispatch",
			Labels:            map[string]string{dispatchv1alpha1.LabelAgent: "duty"},
			CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Hour)),
		},
		Spec: dispatchv1alpha1.SessionSpec{AgentRef: dispatchv1alpha1.NamedRef{Name: "duty"}},
	}

	gate, err := NewCELGate()
	if err != nil {
		t.Fatal(err)
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(testAgent(1), trigger, existing).
		WithStatusSubresource(&dispatchv1alpha1.Trigger{}, &dispatchv1alpha1.Session{}).
		Build()
	dispatcher := &Dispatcher{Client: fakeClient, Gate: gate, Clock: func() time.Time { return now }}

	disposition, err := dispatcher.HandleEvent(context.Background(), trigger, firingEvent("critical", "default"))
	if err != nil || disposition != DispositionSuppressed {
		t.Fatalf("disposition = %v err = %v, want suppressed", disposition, err)
	}

	var sessions dispatchv1alpha1.SessionList
	if err := fakeClient.List(context.Background(), &sessions); err != nil {
		t.Fatal(err)
	}
	suppressed := 0
	for _, session := range sessions.Items {
		if session.Annotations[dispatchv1alpha1.AnnotationSuppressedReason] != "" {
			suppressed++
		}
	}
	if suppressed != 1 {
		t.Errorf("suppressed records = %d, want 1", suppressed)
	}
}

func TestCronSpecTimezone(t *testing.T) {
	spec := cronSpec(&dispatchv1alpha1.ScheduleSource{Cron: "15 5 * * *", Timezone: "Europe/Warsaw"})
	if spec != "CRON_TZ=Europe/Warsaw 15 5 * * *" {
		t.Errorf("spec = %q", spec)
	}
	if cronSpec(&dispatchv1alpha1.ScheduleSource{Cron: "* * * * *"}) != "* * * * *" {
		t.Error("bare spec altered")
	}
}

func TestSelfEventFiltering(t *testing.T) {
	selfAlert := Event{
		Type:        "alertmanager.firing",
		Source:      "alertmanager",
		Fingerprint: "selfjob",
		Data: map[string]any{
			"status":      "firing",
			"fingerprint": "selfjob",
			"labels": map[string]any{
				"alertname": "KubeJobFailed",
				"severity":  "warning",
				"namespace": "ai",
				"job_name":  "sess-guardian-plex-1",
			},
		},
	}
	if !IsSelfEvent(selfAlert) {
		t.Error("alert naming a dispatch session Job is not detected as self-referential")
	}
	if IsSelfEvent(firingEvent("critical", "media")) {
		t.Error("ordinary alert misdetected as self-referential")
	}

	scheme := testScheme(t)
	trigger := loadExampleTrigger(t, "trigger-alerts-to-duty.yaml")
	trigger.Namespace = "dispatch"
	trigger.Spec.When = ""

	gate, err := NewCELGate()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(testAgent(0), trigger).
		WithStatusSubresource(&dispatchv1alpha1.Trigger{}, &dispatchv1alpha1.Session{}).
		Build()
	dispatcher := &Dispatcher{Client: fakeClient, Gate: gate, Clock: func() time.Time { return now }}

	disposition, err := dispatcher.HandleEvent(context.Background(), trigger, selfAlert)
	if err != nil || disposition != DispositionSelf {
		t.Fatalf("disposition = %v err = %v, want self", disposition, err)
	}
	var sessions dispatchv1alpha1.SessionList
	if err := fakeClient.List(context.Background(), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions.Items) != 0 {
		t.Errorf("self event created %d sessions, want 0", len(sessions.Items))
	}

	trigger.Spec.AllowSelfEvents = true
	if disposition, _ = dispatcher.HandleEvent(context.Background(), trigger, selfAlert); disposition != DispositionDispatched {
		t.Errorf("with allowSelfEvents disposition = %v, want dispatched", disposition)
	}
}

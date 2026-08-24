package controller

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dispatchv1alpha1 "github.com/janpuc/dispatch/api/v1alpha1"
	"github.com/janpuc/dispatch/internal/task"
)

func fixtures() (*dispatchv1alpha1.Session, *dispatchv1alpha1.Agent, *dispatchv1alpha1.Workspace) {
	session := &dispatchv1alpha1.Session{
		ObjectMeta: metav1.ObjectMeta{Name: "duty-x1", Namespace: "dispatch"},
		Spec: dispatchv1alpha1.SessionSpec{
			AgentRef: dispatchv1alpha1.NamedRef{Name: "duty"},
			Input:    dispatchv1alpha1.SessionInput{Prompt: "investigate the alert"},
			Provenance: dispatchv1alpha1.Provenance{
				Trigger:     "alerts-to-duty",
				Fingerprint: "f00d",
			},
		},
	}
	agent := &dispatchv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "duty", Namespace: "dispatch"},
		Spec: dispatchv1alpha1.AgentSpec{
			Runner: dispatchv1alpha1.RunnerSpec{
				Image:          "ghcr.io/janpuc/dispatch-runner:base",
				CLI:            "claude",
				CredentialsRef: dispatchv1alpha1.NamedRef{Name: "claude-max-primary"},
			},
			Models:             dispatchv1alpha1.ModelPolicy{Session: "claude-fable-5"},
			WorkspaceRef:       dispatchv1alpha1.NamedRef{Name: "homelab"},
			ServiceAccountName: "dispatch-agent-readonly",
			Git: dispatchv1alpha1.GitIdentity{
				Author:       "Duty Agent <duty@dispatch.local>",
				PushBranches: []string{"dispatch/*"},
			},
			Budgets: dispatchv1alpha1.AgentBudgets{
				PerSession: dispatchv1alpha1.SessionBudget{
					MaxTurns: 60,
					Timeout:  metav1.Duration{Duration: 45 * time.Minute},
				},
			},
		},
	}
	workspace := &dispatchv1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "homelab", Namespace: "dispatch"},
		Spec: dispatchv1alpha1.WorkspaceSpec{
			Origin: dispatchv1alpha1.GitOrigin{
				GitURL:        "ssh://git@nas.home:2222/jan/homelab.git",
				DefaultBranch: "main",
			},
		},
	}
	return session, agent, workspace
}

func TestBuildTaskConfigMap(t *testing.T) {
	session, agent, workspace := fixtures()

	configMap, err := BuildTaskConfigMap(session, agent, workspace)
	if err != nil {
		t.Fatalf("BuildTaskConfigMap: %v", err)
	}
	if configMap.Name != "task-duty-x1" {
		t.Errorf("name = %q, want task-duty-x1", configMap.Name)
	}

	var doc task.File
	if err := json.Unmarshal([]byte(configMap.Data[task.FileName]), &doc); err != nil {
		t.Fatalf("task.json does not parse: %v", err)
	}
	if doc.Prompt != "investigate the alert" {
		t.Errorf("prompt = %q", doc.Prompt)
	}
	if doc.Model != "claude-fable-5" {
		t.Errorf("model = %q", doc.Model)
	}
	if doc.MaxTurns != 60 {
		t.Errorf("maxTurns = %d", doc.MaxTurns)
	}
	if doc.TimeoutSeconds != int64((45 * time.Minute).Seconds()) {
		t.Errorf("timeoutSeconds = %d", doc.TimeoutSeconds)
	}
	if len(doc.PushBranches) != 1 || doc.PushBranches[0] != "dispatch/*" {
		t.Errorf("pushBranches = %v", doc.PushBranches)
	}
	if doc.GitURL != "ssh://git@nas.home:2222/jan/homelab.git" || doc.DefaultBranch != "main" {
		t.Errorf("origin = %q %q", doc.GitURL, doc.DefaultBranch)
	}
}

func TestBuildSessionJob(t *testing.T) {
	session, agent, workspace := fixtures()

	job := BuildSessionJob(session, agent, workspace, RunnerConfig{OTLPEndpoint: "http://otel:4317"})

	if job.Name != "sess-duty-x1" {
		t.Errorf("job name = %q", job.Name)
	}
	if got := job.Labels[dispatchv1alpha1.LabelAgent]; got != "duty" {
		t.Errorf("agent label = %q", got)
	}
	if got := job.Labels[dispatchv1alpha1.LabelTrigger]; got != "alerts-to-duty" {
		t.Errorf("trigger label = %q", got)
	}
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 45*60+timeoutGraceSeconds {
		t.Errorf("activeDeadlineSeconds = %v", job.Spec.ActiveDeadlineSeconds)
	}

	pod := job.Spec.Template.Spec
	if pod.ServiceAccountName != "dispatch-agent-readonly" {
		t.Errorf("serviceAccountName = %q", pod.ServiceAccountName)
	}
	if pod.SecurityContext == nil || pod.SecurityContext.FSGroup == nil || *pod.SecurityContext.FSGroup != 1000 {
		t.Errorf("fsGroup = %+v", pod.SecurityContext)
	}
	if len(pod.InitContainers) != 1 {
		t.Fatalf("initContainers = %+v", pod.InitContainers)
	}
	perms := pod.InitContainers[0]
	if perms.SecurityContext == nil || perms.SecurityContext.RunAsUser == nil || *perms.SecurityContext.RunAsUser != 0 {
		t.Errorf("volume-perms securityContext = %+v", perms.SecurityContext)
	}
	if !strings.Contains(perms.Command[2], "chown 1000:1000 /workspace") {
		t.Errorf("volume-perms command = %v", perms.Command)
	}
	envFrom := pod.Containers[0].EnvFrom
	if len(envFrom) != 1 || envFrom[0].SecretRef == nil || envFrom[0].SecretRef.Name != "claude-max-primary" {
		t.Errorf("envFrom = %+v", envFrom)
	}
	if len(pod.Containers) != 1 || pod.Containers[0].Image != "ghcr.io/janpuc/dispatch-runner:base" {
		t.Fatalf("containers = %+v", pod.Containers)
	}
	if len(pod.Volumes) != 3 {
		t.Fatalf("volumes = %d, want 3", len(pod.Volumes))
	}
	if claim := pod.Volumes[0].PersistentVolumeClaim; claim == nil || claim.ClaimName != "ws-homelab" {
		t.Errorf("workspace volume = %+v", pod.Volumes[0])
	}

	env := map[string]string{}
	for _, item := range pod.Containers[0].Env {
		env[item.Name] = item.Value
	}
	if env["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
		t.Errorf("telemetry env missing: %v", env)
	}
	if env["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://otel:4317" {
		t.Errorf("otlp endpoint = %q", env["OTEL_EXPORTER_OTLP_ENDPOINT"])
	}
	if env["DISPATCH_TASK_FILE"] != "/task/task.json" {
		t.Errorf("task file env = %q", env["DISPATCH_TASK_FILE"])
	}
	if env["HOME"] != "/workspace/.dispatch-home" {
		t.Errorf("HOME = %q", env["HOME"])
	}
}

func TestBuildSessionJobWithSessionsPVC(t *testing.T) {
	session, agent, workspace := fixtures()

	job := BuildSessionJob(session, agent, workspace, RunnerConfig{SessionsPVC: "dispatch-sessions"})

	pod := job.Spec.Template.Spec
	if len(pod.Volumes) != 4 {
		t.Fatalf("volumes = %d, want 4", len(pod.Volumes))
	}
	last := pod.Volumes[len(pod.Volumes)-1]
	if last.PersistentVolumeClaim == nil || last.PersistentVolumeClaim.ClaimName != "dispatch-sessions" {
		t.Errorf("sessions volume = %+v", last)
	}
	env := map[string]string{}
	for _, item := range pod.Containers[0].Env {
		env[item.Name] = item.Value
	}
	if env["DISPATCH_SESSIONS"] != "/sessions" {
		t.Errorf("DISPATCH_SESSIONS = %q", env["DISPATCH_SESSIONS"])
	}
	perms := pod.InitContainers[0]
	if !strings.Contains(perms.Command[2], "/sessions") || len(perms.VolumeMounts) != 2 {
		t.Errorf("volume-perms with sessions = %v %+v", perms.Command, perms.VolumeMounts)
	}
}

func TestBuildSessionJobWithoutTelemetry(t *testing.T) {
	session, agent, workspace := fixtures()

	job := BuildSessionJob(session, agent, workspace, RunnerConfig{})

	for _, item := range job.Spec.Template.Spec.Containers[0].Env {
		if item.Name == "CLAUDE_CODE_ENABLE_TELEMETRY" {
			t.Errorf("telemetry env present without endpoint")
		}
	}
}

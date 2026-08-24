package controller

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	dispatchv1alpha1 "github.com/janpuc/dispatch/api/v1alpha1"
	"github.com/janpuc/dispatch/internal/task"
)

// RunnerConfig carries operator-level settings stamped into every session Job.
type RunnerConfig struct {
	// OTLPEndpoint enables Claude Code native telemetry export when non-empty.
	OTLPEndpoint string

	// SessionsPVC names a shared RWX claim for session records; empty keeps
	// records inside each workspace (ADR-0005).
	SessionsPVC string
}

const (
	workspaceMountPath   = "/workspace"
	credentialsMountPath = "/credentials"
	sessionsMountPath    = "/sessions"
	runnerHomeDir        = workspaceMountPath + "/.dispatch-home"

	workspaceVolume   = "workspace"
	taskVolume        = "task"
	credentialsVolume = "credentials"
	sessionsVolume    = "sessions"

	runnerContainerName = "runner"

	finishedJobTTLSeconds  = int64(24 * 60 * 60)
	timeoutGraceSeconds    = int64(600)
	kubernetesNameMaxChars = 63
)

// SessionJobName is the Job name executing the given Session.
func SessionJobName(s *dispatchv1alpha1.Session) string {
	return truncateName("sess-" + s.Name)
}

// TaskConfigMapName is the ConfigMap carrying the Session's task.json.
func TaskConfigMapName(s *dispatchv1alpha1.Session) string {
	return truncateName("task-" + s.Name)
}

// WorkspacePVCName is the PersistentVolumeClaim backing the given Workspace.
func WorkspacePVCName(ws *dispatchv1alpha1.Workspace) string {
	return truncateName("ws-" + ws.Name)
}

func truncateName(name string) string {
	if len(name) <= kubernetesNameMaxChars {
		return name
	}
	return name[:kubernetesNameMaxChars]
}

func sessionLabels(session *dispatchv1alpha1.Session, agent *dispatchv1alpha1.Agent) map[string]string {
	labels := map[string]string{
		dispatchv1alpha1.LabelSession: session.Name,
		dispatchv1alpha1.LabelAgent:   agent.Name,
	}
	if session.Spec.Provenance.Trigger != "" {
		labels[dispatchv1alpha1.LabelTrigger] = session.Spec.Provenance.Trigger
	}
	return labels
}

// BuildTaskConfigMap renders the Session's task.json document (the shim
// contract from ADR-0003) into a ConfigMap.
func BuildTaskConfigMap(
	session *dispatchv1alpha1.Session,
	agent *dispatchv1alpha1.Agent,
	workspace *dispatchv1alpha1.Workspace,
) (*corev1.ConfigMap, error) {
	doc := task.File{
		Session:       session.Name,
		Agent:         agent.Name,
		Prompt:        session.Spec.Input.Prompt,
		Model:         agent.Spec.Models.Session,
		CLI:           agent.Spec.Runner.CLI,
		GitURL:        workspace.Spec.Origin.GitURL,
		DefaultBranch: workspace.Spec.Origin.DefaultBranch,
		MaxTurns:      agent.Spec.Budgets.PerSession.MaxTurns,
		GitAuthor:     agent.Spec.Git.Author,
		PushBranches:  agent.Spec.Git.PushBranches,
		Trigger:       session.Spec.Provenance.Trigger,
		Fingerprint:   session.Spec.Provenance.Fingerprint,
	}
	if timeout := agent.Spec.Budgets.PerSession.Timeout.Duration; timeout > 0 {
		doc.TimeoutSeconds = int64(timeout.Seconds())
	}
	payload, err := doc.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshaling task file: %w", err)
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TaskConfigMapName(session),
			Namespace: session.Namespace,
			Labels:    sessionLabels(session, agent),
		},
		Data: map[string]string{task.FileName: string(payload)},
	}, nil
}

// BuildSessionJob renders the Job that executes a Session: the runner image
// with the workspace PVC, credentials Secret, task ConfigMap, and telemetry
// environment wired in.
func BuildSessionJob(
	session *dispatchv1alpha1.Session,
	agent *dispatchv1alpha1.Agent,
	workspace *dispatchv1alpha1.Workspace,
	cfg RunnerConfig,
) *batchv1.Job {
	labels := sessionLabels(session, agent)

	env := []corev1.EnvVar{
		{Name: "DISPATCH_SESSION", Value: session.Name},
		{Name: "DISPATCH_AGENT", Value: agent.Name},
		{Name: "DISPATCH_TASK_FILE", Value: task.MountPath + "/" + task.FileName},
		{Name: "DISPATCH_WORKSPACE", Value: workspaceMountPath},
		{Name: "DISPATCH_CREDENTIALS", Value: credentialsMountPath},
		{Name: "HOME", Value: runnerHomeDir},
	}
	if cfg.SessionsPVC != "" {
		env = append(env, corev1.EnvVar{Name: "DISPATCH_SESSIONS", Value: sessionsMountPath})
	}
	if cfg.OTLPEndpoint != "" {
		env = append(env,
			corev1.EnvVar{Name: "CLAUDE_CODE_ENABLE_TELEMETRY", Value: "1"},
			corev1.EnvVar{Name: "OTEL_METRICS_EXPORTER", Value: "otlp"},
			corev1.EnvVar{Name: "OTEL_LOGS_EXPORTER", Value: "otlp"},
			corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: cfg.OTLPEndpoint},
			corev1.EnvVar{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: fmt.Sprintf(
				"service.name=dispatch-runner,dispatch.agent=%s,dispatch.session=%s", agent.Name, session.Name,
			)},
		)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SessionJobName(session),
			Namespace: session.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(int32(0)),
			TTLSecondsAfterFinished: ptr.To(int32(finishedJobTTLSeconds)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: agent.Spec.ServiceAccountName,
					Containers: []corev1.Container{{
						Name:       runnerContainerName,
						Image:      agent.Spec.Runner.Image,
						WorkingDir: workspaceMountPath,
						Env:        env,
						VolumeMounts: []corev1.VolumeMount{
							{Name: workspaceVolume, MountPath: workspaceMountPath},
							{Name: taskVolume, MountPath: task.MountPath, ReadOnly: true},
							{Name: credentialsVolume, MountPath: credentialsMountPath, ReadOnly: true},
						},
					}},
					Volumes: []corev1.Volume{
						{
							Name: workspaceVolume,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: WorkspacePVCName(workspace),
								},
							},
						},
						{
							Name: taskVolume,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: TaskConfigMapName(session)},
								},
							},
						},
						{
							Name: credentialsVolume,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: agent.Spec.Runner.CredentialsRef.Name,
								},
							},
						},
					},
				},
			},
		},
	}

	if timeout := agent.Spec.Budgets.PerSession.Timeout.Duration; timeout > 0 {
		job.Spec.ActiveDeadlineSeconds = ptr.To(int64(timeout.Seconds()) + timeoutGraceSeconds)
	}
	if cfg.SessionsPVC != "" {
		podSpec := &job.Spec.Template.Spec
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: sessionsVolume,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: cfg.SessionsPVC,
				},
			},
		})
		podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      sessionsVolume,
			MountPath: sessionsMountPath,
		})
	}
	return job
}

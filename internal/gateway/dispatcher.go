package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dispatchv1alpha1 "github.com/janpuc/dispatch/api/v1alpha1"
	"github.com/janpuc/dispatch/internal/metrics"
)

// Disposition is what the gateway decided about one event for one trigger.
type Disposition string

// Disposition values, mirrored into the events metric.
const (
	DispositionFiltered   Disposition = "filtered"
	DispositionSelf       Disposition = "self"
	DispositionDeduped    Disposition = "deduped"
	DispositionSuppressed Disposition = "suppressed"
	DispositionDispatched Disposition = "dispatched"
	DispositionError      Disposition = "error"
)

const (
	conditionAccepting = "Accepting"

	reasonInvalidExpression = "InvalidExpression"
	reasonAgentMissing      = "AgentMissing"
	reasonAccepting         = "Accepting"
	reasonDailyBudgetSpent  = "DailyBudgetSpent"
)

// Dispatcher turns gated events into Sessions and keeps Trigger status
// truthful about what fired and what was vetoed.
type Dispatcher struct {
	client.Client
	Gate  *CELGate
	Clock func() time.Time
}

// HandleEvent runs one event through one trigger's gates and, when it
// survives, creates the Session.
func (d *Dispatcher) HandleEvent(ctx context.Context, trigger *dispatchv1alpha1.Trigger, event Event) (Disposition, error) {
	log := logf.FromContext(ctx).WithValues("trigger", trigger.Name, "event", event.Type, "fingerprint", event.Fingerprint)

	if !trigger.Spec.AllowSelfEvents && IsSelfEvent(event) {
		log.V(1).Info("dropped self-referential event")
		metrics.EventsTotal.WithLabelValues(event.Source, string(DispositionSelf)).Inc()
		return DispositionSelf, nil
	}

	matched, dedupeKey, err := d.Gate.Evaluate(trigger, event)
	if err != nil {
		d.setCondition(ctx, trigger, reasonInvalidExpression, err.Error())
		metrics.EventsTotal.WithLabelValues(event.Source, string(DispositionError)).Inc()
		return DispositionError, err
	}
	if !matched {
		metrics.EventsTotal.WithLabelValues(event.Source, string(DispositionFiltered)).Inc()
		return DispositionFiltered, nil
	}

	var agent dispatchv1alpha1.Agent
	if err := d.Get(ctx, types.NamespacedName{Namespace: trigger.Namespace, Name: trigger.Spec.AgentRef.Name}, &agent); err != nil {
		d.setCondition(ctx, trigger, reasonAgentMissing, "agent "+trigger.Spec.AgentRef.Name+": "+err.Error())
		metrics.EventsTotal.WithLabelValues(event.Source, string(DispositionError)).Inc()
		return DispositionError, err
	}

	fingerprintHash := HashFingerprint(dedupeKey)
	deduped, err := d.withinCooldown(ctx, trigger, fingerprintHash)
	if err != nil {
		return DispositionError, err
	}
	if deduped {
		log.V(1).Info("deduped within cooldown")
		metrics.EventsTotal.WithLabelValues(event.Source, string(DispositionDeduped)).Inc()
		d.bumpStatus(ctx, trigger, func(status *dispatchv1alpha1.TriggerStatus) {
			status.SuppressedTotal++
		})
		return DispositionDeduped, nil
	}

	overBudget, budgetMessage, err := d.dailyBudgetSpent(ctx, trigger, &agent)
	if err != nil {
		return DispositionError, err
	}

	sessionName := d.sessionName(agent.Name)
	prompt, err := RenderPrompt(trigger, agent.Name, sessionName, event)
	if err != nil {
		d.setCondition(ctx, trigger, reasonInvalidExpression, err.Error())
		metrics.EventsTotal.WithLabelValues(event.Source, string(DispositionError)).Inc()
		return DispositionError, err
	}

	session := &dispatchv1alpha1.Session{
		ObjectMeta: metav1.ObjectMeta{
			Name:              sessionName,
			Namespace:         trigger.Namespace,
			CreationTimestamp: metav1.NewTime(d.Clock()),
			Labels: map[string]string{
				dispatchv1alpha1.LabelAgent:       agent.Name,
				dispatchv1alpha1.LabelTrigger:     trigger.Name,
				dispatchv1alpha1.LabelFingerprint: fingerprintHash,
			},
		},
		Spec: dispatchv1alpha1.SessionSpec{
			AgentRef: dispatchv1alpha1.NamedRef{Name: agent.Name},
			Input:    dispatchv1alpha1.SessionInput{Prompt: prompt},
			Provenance: dispatchv1alpha1.Provenance{
				Trigger:     trigger.Name,
				EventType:   event.Type,
				Fingerprint: event.Fingerprint,
			},
		},
	}
	if overBudget {
		session.Annotations = map[string]string{
			dispatchv1alpha1.AnnotationSuppressedReason: budgetMessage,
		}
	}
	if err := d.Create(ctx, session); err != nil {
		metrics.EventsTotal.WithLabelValues(event.Source, string(DispositionError)).Inc()
		return DispositionError, err
	}

	if overBudget {
		log.Info("suppressed by daily budget", "session", sessionName)
		metrics.EventsTotal.WithLabelValues(event.Source, string(DispositionSuppressed)).Inc()
		d.bumpStatus(ctx, trigger, func(status *dispatchv1alpha1.TriggerStatus) {
			status.SuppressedTotal++
		})
		return DispositionSuppressed, nil
	}

	log.Info("dispatched session", "session", sessionName, "agent", agent.Name)
	metrics.EventsTotal.WithLabelValues(event.Source, string(DispositionDispatched)).Inc()
	now := metav1.NewTime(d.Clock())
	d.bumpStatus(ctx, trigger, func(status *dispatchv1alpha1.TriggerStatus) {
		status.FiredTotal++
		status.LastFiredAt = &now
	})
	return DispositionDispatched, nil
}

func (d *Dispatcher) withinCooldown(ctx context.Context, trigger *dispatchv1alpha1.Trigger, fingerprintHash string) (bool, error) {
	if trigger.Spec.Dedupe == nil || trigger.Spec.Dedupe.Cooldown.Duration <= 0 {
		return false, nil
	}
	var sessions dispatchv1alpha1.SessionList
	if err := d.List(ctx, &sessions,
		client.InNamespace(trigger.Namespace),
		client.MatchingLabels{
			dispatchv1alpha1.LabelTrigger:     trigger.Name,
			dispatchv1alpha1.LabelFingerprint: fingerprintHash,
		},
	); err != nil {
		return false, err
	}
	horizon := d.Clock().Add(-trigger.Spec.Dedupe.Cooldown.Duration)
	for _, session := range sessions.Items {
		if session.CreationTimestamp.Time.After(horizon) {
			return true, nil
		}
	}
	return false, nil
}

func (d *Dispatcher) dailyBudgetSpent(ctx context.Context, trigger *dispatchv1alpha1.Trigger, agent *dispatchv1alpha1.Agent) (bool, string, error) {
	max := agent.Spec.Budgets.Daily.MaxSessions
	if max <= 0 {
		return false, "", nil
	}
	var sessions dispatchv1alpha1.SessionList
	if err := d.List(ctx, &sessions,
		client.InNamespace(trigger.Namespace),
		client.MatchingLabels{dispatchv1alpha1.LabelAgent: agent.Name},
	); err != nil {
		return false, "", err
	}
	midnight := d.Clock().UTC().Truncate(24 * time.Hour)
	today := 0
	for _, session := range sessions.Items {
		if session.Status.Phase == dispatchv1alpha1.SessionSuppressed {
			continue
		}
		if session.CreationTimestamp.Time.After(midnight) {
			today++
		}
	}
	if today >= int(max) {
		return true, fmt.Sprintf("agent %s reached daily maxSessions (%d)", agent.Name, max), nil
	}
	return false, "", nil
}

func (d *Dispatcher) sessionName(agentName string) string {
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	name := fmt.Sprintf("%s-%s-%s", agentName, d.Clock().UTC().Format("20060102-1504"), hex.EncodeToString(suffix))
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

func (d *Dispatcher) bumpStatus(ctx context.Context, trigger *dispatchv1alpha1.Trigger, mutate func(*dispatchv1alpha1.TriggerStatus)) {
	var fresh dispatchv1alpha1.Trigger
	if err := d.Get(ctx, client.ObjectKeyFromObject(trigger), &fresh); err != nil {
		return
	}
	mutate(&fresh.Status)
	meta.SetStatusCondition(&fresh.Status.Conditions, metav1.Condition{
		Type:               conditionAccepting,
		Status:             metav1.ConditionTrue,
		Reason:             reasonAccepting,
		Message:            "gateway is evaluating events for this trigger",
		ObservedGeneration: fresh.Generation,
	})
	if err := d.Status().Update(ctx, &fresh); err != nil {
		logf.FromContext(ctx).V(1).Info("trigger status update failed", "trigger", trigger.Name, "error", err.Error())
	}
}

func (d *Dispatcher) setCondition(ctx context.Context, trigger *dispatchv1alpha1.Trigger, reason, message string) {
	var fresh dispatchv1alpha1.Trigger
	if err := d.Get(ctx, client.ObjectKeyFromObject(trigger), &fresh); err != nil {
		return
	}
	meta.SetStatusCondition(&fresh.Status.Conditions, metav1.Condition{
		Type:               conditionAccepting,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: fresh.Generation,
	})
	if err := d.Status().Update(ctx, &fresh); err != nil {
		logf.FromContext(ctx).V(1).Info("trigger status update failed", "trigger", trigger.Name, "error", err.Error())
	}
}

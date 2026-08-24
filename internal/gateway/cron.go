package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dispatchv1alpha1 "github.com/janpuc/dispatch/api/v1alpha1"
)

const (
	cronResyncInterval    = 30 * time.Second
	reasonInvalidSchedule = "InvalidSchedule"
)

type cronEntry struct {
	generation int64
	id         cron.EntryID
}

// CronScheduler fires schedule-sourced Triggers: it resyncs the trigger list
// on an interval and emits one schedule.tick event per firing.
type CronScheduler struct {
	Client     client.Client
	Dispatcher *Dispatcher
}

// Start runs the scheduler until the manager context ends.
func (c *CronScheduler) Start(ctx context.Context) error {
	runner := cron.New()
	runner.Start()
	defer runner.Stop()

	entries := map[types.NamespacedName]cronEntry{}
	ticker := time.NewTicker(cronResyncInterval)
	defer ticker.Stop()

	for {
		c.resync(ctx, runner, entries)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (c *CronScheduler) resync(ctx context.Context, runner *cron.Cron, entries map[types.NamespacedName]cronEntry) {
	log := logf.FromContext(ctx)
	var triggers dispatchv1alpha1.TriggerList
	if err := c.Client.List(ctx, &triggers); err != nil {
		log.Error(err, "cron resync: listing triggers")
		return
	}

	seen := map[types.NamespacedName]bool{}
	for i := range triggers.Items {
		trigger := triggers.Items[i]
		if trigger.Spec.Source.Schedule == nil {
			continue
		}
		key := types.NamespacedName{Namespace: trigger.Namespace, Name: trigger.Name}
		seen[key] = true
		if existing, ok := entries[key]; ok {
			if existing.generation == trigger.Generation {
				continue
			}
			runner.Remove(existing.id)
			delete(entries, key)
		}

		id, err := runner.AddFunc(cronSpec(trigger.Spec.Source.Schedule), c.fire(key))
		if err != nil {
			log.Error(err, "cron resync: invalid schedule", "trigger", key.String())
			c.Dispatcher.setCondition(ctx, &trigger, reasonInvalidSchedule, err.Error())
			continue
		}
		entries[key] = cronEntry{generation: trigger.Generation, id: id}
	}

	for key, entry := range entries {
		if !seen[key] {
			runner.Remove(entry.id)
			delete(entries, key)
		}
	}
}

func cronSpec(schedule *dispatchv1alpha1.ScheduleSource) string {
	if schedule.Timezone != "" {
		return "CRON_TZ=" + schedule.Timezone + " " + schedule.Cron
	}
	return schedule.Cron
}

func (c *CronScheduler) fire(key types.NamespacedName) func() {
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		var trigger dispatchv1alpha1.Trigger
		if err := c.Client.Get(ctx, key, &trigger); err != nil {
			return
		}
		now := time.Now()
		tick := now.UTC().Truncate(time.Minute).Format(time.RFC3339)
		event := Event{
			Type:        "schedule.tick",
			Source:      "schedule",
			Fingerprint: fmt.Sprintf("%s@%s", key.String(), tick),
			Time:        now,
			Data: map[string]any{
				"trigger":     key.Name,
				"scheduledAt": tick,
			},
		}
		_, _ = c.Dispatcher.HandleEvent(ctx, &trigger, event)
	}
}

package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dispatchv1alpha1 "github.com/janpuc/dispatch/api/v1alpha1"
	"github.com/janpuc/dispatch/internal/metrics"
)

const (
	alertmanagerPath = "/hooks/alertmanager"
	webhookPrefix    = "/hooks/"
	maxBodyBytes     = 5 * 1024 * 1024

	headerGitHubEvent    = "X-GitHub-Event"
	headerGitHubDelivery = "X-GitHub-Delivery"
	headerHubSignature   = "X-Hub-Signature-256"

	hmacSecretKey = "secret"
)

// +kubebuilder:rbac:groups=dispatch.janpuc.com,resources=sessions,verbs=create
// +kubebuilder:rbac:groups=dispatch.janpuc.com,resources=triggers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Server is the gateway's HTTP edge: Alertmanager and generic HMAC-verified
// webhooks in, Sessions out (design §5).
type Server struct {
	Client     client.Client
	APIReader  client.Reader
	Dispatcher *Dispatcher
	Addr       string
}

// Start runs the HTTP server until the manager context ends.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc(alertmanagerPath, s.handleAlertmanager)
	mux.HandleFunc(webhookPrefix, s.handleWebhook)

	server := &http.Server{
		Addr:              s.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	logf.FromContext(ctx).Info("gateway listening", "addr", s.Addr)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errs:
		return err
	}
}

func (s *Server) handleAlertmanager(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, err := readBody(w, r)
	if err != nil {
		return
	}
	events, err := ParseAlertmanager(body, time.Now())
	if err != nil {
		metrics.EventsTotal.WithLabelValues("alertmanager", string(DispositionError)).Inc()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	triggers, err := s.listTriggers(ctx, func(t *dispatchv1alpha1.Trigger) bool {
		return t.Spec.Source.Alertmanager != nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	dispositions := map[string]int{}
	for i := range triggers {
		for _, event := range events {
			disposition, _ := s.Dispatcher.HandleEvent(ctx, &triggers[i], event)
			dispositions[string(disposition)]++
		}
	}
	respond(w, dispositions)
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	triggers, err := s.listTriggers(ctx, func(t *dispatchv1alpha1.Trigger) bool {
		return t.Spec.Source.Webhook != nil && t.Spec.Source.Webhook.Path == r.URL.Path
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(triggers) == 0 {
		http.NotFound(w, r)
		return
	}
	body, err := readBody(w, r)
	if err != nil {
		return
	}

	dispositions := map[string]int{}
	for i := range triggers {
		trigger := &triggers[i]
		if err := s.verifySignature(ctx, trigger, r, body); err != nil {
			metrics.EventsTotal.WithLabelValues("webhook", string(DispositionError)).Inc()
			http.Error(w, "signature verification failed", http.StatusUnauthorized)
			return
		}
		event, err := webhookEvent(r, body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		disposition, _ := s.Dispatcher.HandleEvent(ctx, trigger, event)
		dispositions[string(disposition)]++
	}
	respond(w, dispositions)
}

func webhookEvent(r *http.Request, body []byte) (Event, error) {
	var data map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &data); err != nil {
			return Event{}, fmt.Errorf("parsing webhook body: %w", err)
		}
	}
	eventType := "webhook"
	if gitHubEvent := r.Header.Get(headerGitHubEvent); gitHubEvent != "" {
		eventType = "github." + gitHubEvent
	}
	fingerprint := r.Header.Get(headerGitHubDelivery)
	if fingerprint == "" {
		sum := sha256.Sum256(body)
		fingerprint = hex.EncodeToString(sum[:])[:fingerprintHashChars]
	}
	return Event{
		Type:        eventType,
		Source:      "webhook",
		Fingerprint: fingerprint,
		Time:        time.Now(),
		Data:        data,
	}, nil
}

func (s *Server) verifySignature(ctx context.Context, trigger *dispatchv1alpha1.Trigger, r *http.Request, body []byte) error {
	secretRef := trigger.Spec.Source.Webhook.HMACSecretRef
	if secretRef.Name == "" {
		return nil
	}
	var secret corev1.Secret
	if err := s.APIReader.Get(ctx, types.NamespacedName{Namespace: trigger.Namespace, Name: secretRef.Name}, &secret); err != nil {
		return err
	}
	key, ok := secret.Data[hmacSecretKey]
	if !ok && len(secret.Data) == 1 {
		for _, value := range secret.Data {
			key = value
		}
		ok = true
	}
	if !ok {
		return fmt.Errorf("secret %s has no %q key", secretRef.Name, hmacSecretKey)
	}

	signature := strings.TrimPrefix(r.Header.Get(headerHubSignature), "sha256=")
	if signature == "" {
		return fmt.Errorf("missing %s header", headerHubSignature)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func (s *Server) listTriggers(ctx context.Context, keep func(*dispatchv1alpha1.Trigger) bool) ([]dispatchv1alpha1.Trigger, error) {
	var all dispatchv1alpha1.TriggerList
	if err := s.Client.List(ctx, &all); err != nil {
		return nil, err
	}
	matched := make([]dispatchv1alpha1.Trigger, 0, len(all.Items))
	for _, trigger := range all.Items {
		if keep(&trigger) {
			matched = append(matched, trigger)
		}
	}
	return matched, nil
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return nil, err
	}
	return body, nil
}

func respond(w http.ResponseWriter, dispositions map[string]int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(dispositions)
}

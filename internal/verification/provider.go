package verification

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"
)

type Outcome string

const (
	Success Outcome = "success"
	Failure Outcome = "failure"
	Timeout Outcome = "timeout"
	Unknown Outcome = "unknown"
)

type Request struct {
	SourceNamespace string        `json:"sourceNamespace"`
	SourceWorkload  string        `json:"sourceWorkload"`
	Destination     string        `json:"destination"`
	Protocol        string        `json:"protocol"`
	Port            int           `json:"port"`
	Timeout         time.Duration `json:"-"`
	TimeoutMillis   int           `json:"timeoutMillis"`
}

type Evidence struct {
	ID              string        `json:"id"`
	SourceNamespace string        `json:"sourceNamespace"`
	SourceWorkload  string        `json:"sourceWorkload"`
	Destination     string        `json:"destination"`
	Protocol        string        `json:"protocol"`
	Port            int           `json:"port"`
	AttemptedAt     time.Time     `json:"attemptedAt"`
	Duration        time.Duration `json:"duration"`
	Outcome         Outcome       `json:"outcome"`
	State           string        `json:"state"`
	ModelComparison string        `json:"modelComparison"`
	Method          string        `json:"method"`
	ErrorClass      string        `json:"errorClass,omitempty"`
	Cluster         string        `json:"cluster,omitempty"`
	Context         string        `json:"context,omitempty"`
	Limitations     []string      `json:"limitations"`
	Provenance      string        `json:"provenance"`
}

type Provider interface {
	Name() string
	Verify(context.Context, Request) (Evidence, error)
}

type Authorizer interface {
	Authorize(context.Context, Request) error
}

// AllowListAuthorizer permits only exact configured sources, destinations,
// protocol/port pairs. Empty lists deny; no wildcard behavior is provided.
type AllowListAuthorizer struct {
	Sources      []string
	Destinations []string
	Protocols    []string
	Ports        []int
}

func (authorizer AllowListAuthorizer) Authorize(_ context.Context, request Request) error {
	source := request.SourceNamespace + "/" + request.SourceWorkload
	if !contains(authorizer.Sources, source) ||
		!contains(authorizer.Destinations, request.Destination) ||
		!contains(authorizer.Protocols, strings.ToUpper(request.Protocol)) ||
		!containsPort(authorizer.Ports, request.Port) {
		return ErrUnauthorized
	}
	return nil
}

var ErrDisabled = errors.New("runtime verification is disabled")
var ErrUnauthorized = errors.New("runtime verification is not authorized")

func ValidateRequest(request Request) error {
	if len(validation.IsDNS1123Label(request.SourceNamespace)) > 0 {
		return errors.New("sourceNamespace must be a valid Kubernetes namespace")
	}
	if len(validation.IsDNS1123Subdomain(request.SourceWorkload)) > 0 {
		return errors.New("sourceWorkload must be a valid workload name")
	}
	if strings.TrimSpace(request.Destination) == "" || len(request.Destination) > 253 {
		return errors.New("destination is required and must be at most 253 characters")
	}
	if net.ParseIP(request.Destination) == nil && len(validation.IsDNS1123Subdomain(request.Destination)) > 0 {
		return errors.New("destination must be an IP address or DNS name")
	}
	switch strings.ToUpper(request.Protocol) {
	case "TCP", "UDP":
	default:
		return errors.New("protocol must be TCP or UDP")
	}
	if request.Port < 1 || request.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	timeout := request.TimeoutMillis
	if request.Timeout > 0 {
		timeout = int(request.Timeout / time.Millisecond)
	}
	if timeout < 1 || timeout > 10000 {
		return errors.New("timeoutMillis must be between 1 and 10000")
	}
	return nil
}

// FakeProvider is only for deterministic tests. Its evidence is explicitly
// marked synthetic and must never be presented as a real runtime observation.
type FakeProvider struct {
	Result Outcome
	Delay  time.Duration
}

func (FakeProvider) Name() string { return "synthetic-test-provider" }

func (provider FakeProvider) Verify(ctx context.Context, request Request) (Evidence, error) {
	started := time.Now().UTC()
	timer := time.NewTimer(provider.Delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Evidence{}, ctx.Err()
	case <-timer.C:
	}
	outcome := provider.Result
	if outcome == "" {
		outcome = Unknown
	}
	evidence := Evidence{
		SourceNamespace: request.SourceNamespace,
		SourceWorkload:  request.SourceWorkload,
		Destination:     request.Destination,
		Protocol:        strings.ToUpper(request.Protocol),
		Port:            request.Port,
		AttemptedAt:     started,
		Duration:        time.Since(started),
		Outcome:         outcome,
		State:           stateForOutcome(outcome),
		ModelComparison: "not-evaluated",
		Method:          "synthetic-test",
		Limitations:     []string{"Synthetic test evidence; no network probe was executed.", "A connection outcome cannot by itself prove NetworkPolicy causation."},
		Provenance:      "synthetic",
	}
	return evidence, nil
}

func stateForOutcome(outcome Outcome) string {
	switch outcome {
	case Success:
		return "observed_success"
	case Failure:
		return "observed_failure"
	default:
		return "unknown"
	}
}

func contains(allowed []string, value string) bool {
	for _, candidate := range allowed {
		if candidate == value {
			return true
		}
	}
	return false
}

func containsPort(allowed []int, value int) bool {
	for _, candidate := range allowed {
		if candidate == value {
			return true
		}
	}
	return false
}

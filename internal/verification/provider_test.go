package verification

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestValidateRequest(t *testing.T) {
	valid := Request{
		SourceNamespace: "payments", SourceWorkload: "api-7f9c",
		Destination: "db.example.test", Protocol: "TCP", Port: 5432, TimeoutMillis: 1000,
	}
	tests := []struct {
		name    string
		mutate  func(*Request)
		wantErr bool
	}{
		{name: "valid request"},
		{name: "reject command-shaped protocol", mutate: func(r *Request) { r.Protocol = "TCP; id" }, wantErr: true},
		{name: "reject unsupported icmp", mutate: func(r *Request) { r.Protocol = "ICMP" }, wantErr: true},
		{name: "reject invalid port", mutate: func(r *Request) { r.Port = 0 }, wantErr: true},
		{name: "reject unbounded timeout", mutate: func(r *Request) { r.TimeoutMillis = 10001 }, wantErr: true},
		{name: "reject invalid workload identity", mutate: func(r *Request) { r.SourceWorkload = "../pod" }, wantErr: true},
		{name: "reject invalid destination", mutate: func(r *Request) { r.Destination = "host;command" }, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			if test.mutate != nil {
				test.mutate(&request)
			}
			err := ValidateRequest(request)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateRequest() error = %v; want error %v", err, test.wantErr)
			}
		})
	}
}

func TestFakeProviderHonorsCancellationAndMarksSynthetic(t *testing.T) {
	provider := FakeProvider{Result: Success, Delay: time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := provider.Verify(ctx, Request{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Verify error = %v; want deadline exceeded", err)
	}

	evidence, err := (FakeProvider{Result: Failure}).Verify(context.Background(), Request{
		SourceNamespace: "payments", SourceWorkload: "api", Destination: "db.example.test", Protocol: "TCP", Port: 5432,
	})
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if evidence.Provenance != "synthetic" || evidence.State != "observed_failure" || evidence.ModelComparison != "not-evaluated" {
		t.Fatalf("fake provider returned misleading evidence: %#v", evidence)
	}
}

func TestAllowListAuthorizerRequiresExactTargetsAndPorts(t *testing.T) {
	authorizer := AllowListAuthorizer{
		Sources:      []string{"payments/api"},
		Destinations: []string{"db.example.test"},
		Protocols:    []string{"TCP"},
		Ports:        []int{5432},
	}
	request := Request{SourceNamespace: "payments", SourceWorkload: "api", Destination: "db.example.test", Protocol: "TCP", Port: 5432}
	if err := authorizer.Authorize(context.Background(), request); err != nil {
		t.Fatalf("expected exact allowlisted request to pass: %v", err)
	}
	request.Port = 5433
	if err := authorizer.Authorize(context.Background(), request); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("non-allowlisted port error = %v, want unauthorized", err)
	}
}

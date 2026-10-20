package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pci/internal/config"
	"pci/internal/verification"
)

func TestValidateListenAddressRequiresLoopbackByDefault(t *testing.T) {
	t.Setenv("PCI_ALLOW_NON_LOOPBACK", "")
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	if err := validateListenAddress("127.0.0.1"); err != nil {
		t.Fatalf("loopback IPv4 should be allowed: %v", err)
	}
	if err := validateListenAddress("::1"); err != nil {
		t.Fatalf("loopback IPv6 should be allowed: %v", err)
	}
	if err := validateListenAddress("localhost"); err != nil {
		t.Fatalf("localhost should be allowed: %v", err)
	}
	if err := validateListenAddress(""); err == nil {
		t.Fatal("empty address must be rejected without override")
	}
	if err := validateListenAddress("0.0.0.0"); err == nil {
		t.Fatal("0.0.0.0 must be rejected without override")
	}
	t.Setenv("PCI_ALLOW_NON_LOOPBACK", "1")
	if err := validateListenAddress("0.0.0.0"); err != nil {
		t.Fatalf("override should allow non-loopback bind: %v", err)
	}
}

func TestValidateListenAddressAllowsInClusterEnvironment(t *testing.T) {
	t.Setenv("PCI_ALLOW_NON_LOOPBACK", "")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.96.0.1")
	if err := validateListenAddress("0.0.0.0"); err != nil {
		t.Fatalf("in-cluster environment should allow Service bind: %v", err)
	}
}

func TestReadyzAndStaticUI(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pci.db")
	uiDir := t.TempDir()
	index := filepath.Join(uiDir, "index.html")
	if err := os.WriteFile(index, []byte("<html>pci</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := NewServer(config.Config{DataDir: dbPath, StaticDir: uiDir, MaxBody: 1 << 20})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	ready := mustJSON[map[string]any](t, testServer.URL+"/readyz", http.MethodGet, nil)
	if ready["ok"] != true {
		t.Fatalf("readyz: %#v", ready)
	}
	resp, err := http.Get(testServer.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "pci") {
		t.Fatalf("static index: status=%d body=%q", resp.StatusCode, body)
	}
}

func TestServerOfflineWorkflow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pci.db")
	server := NewServer(config.Config{DataDir: dbPath, MaxBody: 1 << 20})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	basePath := filepath.Join("..", "..", "fixtures", "base")
	proposedPath := filepath.Join("..", "..", "fixtures", "proposed")

	analysisResp := mustJSON[map[string]any](t, testServer.URL+"/api/v1/analyses", http.MethodPost, map[string]any{"path": basePath})
	analysisID, ok := analysisResp["id"].(string)
	if !ok || analysisID == "" {
		t.Fatalf("analysis id missing from POST response: %#v", analysisResp)
	}

	findingsResp := mustJSON[map[string]any](t, testServer.URL+"/api/v1/analyses/"+analysisID+"/findings", http.MethodGet, nil)
	if _, ok := findingsResp["findings"].([]any); !ok {
		t.Fatalf("findings payload did not contain an array: %#v", findingsResp)
	}

	graphResp := mustJSON[map[string]any](t, testServer.URL+"/api/v1/analyses/"+analysisID+"/graph", http.MethodGet, nil)
	if _, ok := graphResp["nodes"].([]any); !ok {
		t.Fatalf("graph payload did not contain nodes: %#v", graphResp)
	}

	diffResp := mustJSON[map[string]any](t, testServer.URL+"/api/v1/diff", http.MethodPost, map[string]any{"basePath": basePath, "proposedPath": proposedPath})
	if _, ok := diffResp["modified"].([]any); !ok {
		t.Fatalf("diff response missing modified list: %#v", diffResp)
	}

	snapshotResp := mustJSON[map[string]any](t, testServer.URL+"/api/v1/snapshots", http.MethodPost, map[string]any{"path": proposedPath, "name": "proposed"})
	snapshotID, ok := snapshotResp["id"].(string)
	if !ok || snapshotID == "" {
		t.Fatalf("snapshot id missing: %#v", snapshotResp)
	}

	loadedSnapshot := mustJSON[map[string]any](t, testServer.URL+"/api/v1/snapshots/"+snapshotID, http.MethodGet, nil)
	if loadedSnapshot["id"] != snapshotID {
		t.Fatalf("snapshot id mismatch: %#v", loadedSnapshot)
	}

	reportResp := mustJSON[map[string]any](t, testServer.URL+"/api/v1/reports", http.MethodPost, map[string]any{"path": basePath})
	if _, ok := reportResp["summary"].(map[string]any); !ok {
		t.Fatalf("report missing summary: %#v", reportResp)
	}

	compareResp := mustJSON[map[string]any](t, testServer.URL+"/api/v1/snapshots/"+snapshotID+"/compare", http.MethodPost, map[string]any{"targetId": snapshotID})
	if compareResp["beforeSnapshotId"] == nil {
		t.Fatalf("compare response missing beforeSnapshotId: %#v", compareResp)
	}
}

func TestClusterDiscoveryOfflineResponseIsExplicitAndEmpty(t *testing.T) {
	server := NewServer(config.Config{
		DataDir:        filepath.Join(t.TempDir(), "pci.db"),
		MaxBody:        1 << 20,
		Namespace:      "backend",
		KubeconfigPath: filepath.Join(t.TempDir(), "missing-kubeconfig"),
	})
	defer func() { _ = server.Store.Close() }()
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	response := mustJSON[map[string]any](t, testServer.URL+"/api/v1/clusters/dev/discover", http.MethodGet, nil)
	if response["mode"] != "offline" || response["status"] != "not-configured" || response["complete"] != false {
		t.Fatalf("offline discovery must be explicit and incomplete: %#v", response)
	}
	if len(response["workloads"].([]any)) != 0 || len(response["networkPolicies"].([]any)) != 0 {
		t.Fatalf("offline discovery must not invent resources: %#v", response)
	}
	settings, _ := response["kubernetesSettings"].(map[string]any)
	if settings == nil || settings["configured"] != false {
		t.Fatalf("offline discovery must report kubernetesSettings.configured=false: %#v", response)
	}
}

func TestCapabilitiesIncludeKubernetesSettingsWhenKubeconfigMissing(t *testing.T) {
	server := NewServer(config.Config{
		DataDir:        filepath.Join(t.TempDir(), "pci.db"),
		MaxBody:        1 << 20,
		KubeconfigPath: filepath.Join(t.TempDir(), "missing-kubeconfig"),
		KubeContext:    "kind-kind",
	})
	defer func() { _ = server.Store.Close() }()
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	response := mustJSON[map[string]any](t, testServer.URL+"/api/v1/capabilities", http.MethodGet, nil)
	settings, _ := response["kubernetesSettings"].(map[string]any)
	if settings == nil || settings["configured"] != false || settings["context"] != "kind-kind" {
		t.Fatalf("capabilities must expose intended kube settings without fabricating cluster data: %#v", response)
	}
	kubernetes, _ := response["kubernetes"].(map[string]any)
	if kubernetes["offline"] != true || kubernetes["status"] != "not-configured" {
		t.Fatalf("capabilities must remain offline when kubeconfig is missing: %#v", response)
	}
}

func TestConnectivityAPIUsesSharedAnalyzerAndPreservesUnknown(t *testing.T) {
	server := NewServer(config.Config{DataDir: filepath.Join(t.TempDir(), "pci.db"), MaxBody: 1 << 20})
	defer func() { _ = server.Store.Close() }()
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	manifest := `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: api-ingress
  namespace: server
spec:
  podSelector:
    matchLabels:
      app: api
  policyTypes: [Ingress]
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          team: frontend
      podSelector:
        matchLabels:
          app: web
    ports:
    - protocol: TCP
      port: 8080
`
	query := map[string]any{
		"source":      map[string]any{"namespace": "client", "name": "web", "labels": map[string]string{"app": "web"}, "labelsComplete": true, "namespaceLabels": map[string]string{"team": "frontend"}, "namespaceLabelsKnown": true},
		"destination": map[string]any{"namespace": "server", "name": "api", "labels": map[string]string{"app": "api"}, "labelsComplete": true, "namespaceLabels": map[string]string{"team": "backend"}, "namespaceLabelsKnown": true},
		"protocol":    "TCP", "port": 8080, "policyInventoryComplete": true,
	}
	result := mustJSON[map[string]any](t, testServer.URL+"/api/v1/connectivity", http.MethodPost, map[string]any{"manifest": manifest, "query": query})
	if result["status"] != "allowed_by_model" || result["provenance"] != "modeled" {
		t.Fatalf("connectivity API returned unexpected modeled result: %#v", result)
	}

	query["policyInventoryComplete"] = false
	result = mustJSON[map[string]any](t, testServer.URL+"/api/v1/connectivity", http.MethodPost, map[string]any{
		"manifest": `apiVersion: v1
kind: ConfigMap
metadata:
  name: non-policy
`,
		"query": query,
	})
	if result["status"] != "unknown" {
		t.Fatalf("incomplete inventory should produce unknown, got %#v", result)
	}

	baseline := `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: api-isolated
  namespace: server
spec:
  podSelector:
    matchLabels:
      app: api
  policyTypes: [Ingress]
`
	proposed := strings.Replace(baseline, "  policyTypes: [Ingress]\n", "  policyTypes: [Ingress]\n  ingress:\n  - {}\n", 1)
	query["policyInventoryComplete"] = true
	diff := mustJSON[map[string]any](t, testServer.URL+"/api/v1/connectivity/diff", http.MethodPost, map[string]any{
		"baselineManifest": baseline, "proposedManifest": proposed, "query": query,
	})
	if diff["change"] != "newly_allowed" || diff["before"] != "denied_by_model" || diff["after"] != "allowed_by_model" {
		t.Fatalf("connectivity diff did not report newly allowed flow: %#v", diff)
	}
	beforeResult, ok := diff["beforeResult"].(map[string]any)
	if !ok || beforeResult["source"] != "client/web" || beforeResult["destination"] != "server/api" {
		t.Fatalf("connectivity diff should retain endpoint-specific baseline evidence: %#v", diff)
	}
	policyChanges, ok := diff["policyChanges"].([]any)
	if !ok || len(policyChanges) != 1 {
		t.Fatalf("connectivity diff should identify changed policy evidence: %#v", diff)
	}
}

func TestVerificationDisabledByDefaultAndRejectsInvalidRequests(t *testing.T) {
	server := NewServer(config.Config{DataDir: filepath.Join(t.TempDir(), "pci.db"), MaxBody: 1 << 20})
	defer func() { _ = server.Store.Close() }()
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	capabilities := mustJSON[map[string]any](t, testServer.URL+"/api/v1/capabilities", http.MethodGet, nil)
	verificationCapabilities, ok := capabilities["verification"].(map[string]any)
	if !ok || verificationCapabilities["enabled"] != false {
		t.Fatalf("verification capability should be disabled by default: %#v", capabilities)
	}

	response, err := http.Post(testServer.URL+"/api/v1/verifications", "application/json", strings.NewReader(`{"sourceNamespace":"default","sourceWorkload":"api","destination":"db.example.test","protocol":"TCP","port":5432,"timeoutMillis":500}`))
	if err != nil {
		t.Fatalf("POST verification: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("verification status = %d, want %d", response.StatusCode, http.StatusServiceUnavailable)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode disabled response: %v", err)
	}
	if body["code"] != "VERIFICATION_DISABLED" {
		t.Fatalf("unexpected disabled response: %#v", body)
	}

	response, err = http.Post(testServer.URL+"/api/v1/verifications", "application/json", strings.NewReader(`{"sourceNamespace":"default","sourceWorkload":"api","destination":"db.example.test","protocol":"TCP;echo bad","port":5432,"timeoutMillis":500}`))
	if err != nil {
		t.Fatalf("POST invalid verification: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid request status = %d, want %d", response.StatusCode, http.StatusBadRequest)
	}
}

func TestVerificationEvidenceIsPersistedAndNotMisrepresented(t *testing.T) {
	server := NewServer(config.Config{DataDir: filepath.Join(t.TempDir(), "pci.db"), MaxBody: 1 << 20})
	defer func() { _ = server.Store.Close() }()
	server.VerificationProvider = verification.FakeProvider{Result: verification.Success}
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	requestBody := `{"sourceNamespace":"default","sourceWorkload":"api","destination":"db.example.test","protocol":"TCP","port":5432,"timeoutMillis":500}`
	response, err := http.Post(testServer.URL+"/api/v1/verifications", "application/json", strings.NewReader(requestBody))
	if err != nil {
		t.Fatalf("POST without authorization: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("missing authorization status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}

	allowTestVerification(server)
	if err := server.Store.SaveAnalysis("analysis-test", "analysis", "test", "", `{}`); err != nil {
		t.Fatalf("save referenced analysis: %v", err)
	}
	requestBody = `{"sourceNamespace":"default","sourceWorkload":"api","destination":"db.example.test","protocol":"TCP","port":5432,"timeoutMillis":500,"analysisId":"analysis-test"}`
	response, err = http.Post(testServer.URL+"/api/v1/verifications", "application/json", strings.NewReader(requestBody))
	if err != nil {
		t.Fatalf("POST verification: %v", err)
	}
	var evidence verification.Evidence
	if err := json.NewDecoder(response.Body).Decode(&evidence); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("verification status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if evidence.ID == "" || evidence.State != "observed_success" || evidence.Provenance != "synthetic" ||
		evidence.ModelComparison != "not-evaluated" || evidence.AttemptedAt.IsZero() ||
		evidence.TimeoutMillis != 500 || evidence.AnalysisID != "analysis-test" {
		t.Fatalf("evidence claims more than the provider established: %#v", evidence)
	}
	if evidence.Method != "synthetic-test" {
		t.Fatalf("unexpected evidence method: %q", evidence.Method)
	}

	missingReferenceResponse, err := http.Post(testServer.URL+"/api/v1/verifications", "application/json", strings.NewReader(
		`{"sourceNamespace":"default","sourceWorkload":"api","destination":"db.example.test","protocol":"TCP","port":5432,"timeoutMillis":500,"analysisId":"missing-analysis"}`,
	))
	if err != nil {
		t.Fatalf("POST verification with missing analysis reference: %v", err)
	}
	_ = missingReferenceResponse.Body.Close()
	if missingReferenceResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("missing analysis reference status = %d, want %d", missingReferenceResponse.StatusCode, http.StatusNotFound)
	}

	response, err = http.Get(testServer.URL + "/api/v1/verifications/" + evidence.ID)
	if err != nil {
		t.Fatalf("GET evidence: %v", err)
	}
	defer response.Body.Close()
	var reopened verification.Evidence
	if err := json.NewDecoder(response.Body).Decode(&reopened); err != nil {
		t.Fatalf("decode persisted evidence: %v", err)
	}
	if reopened.ID != evidence.ID || reopened.Provenance != "synthetic" {
		t.Fatalf("persisted evidence changed: %#v", reopened)
	}
}

type countingVerificationProvider struct {
	calls atomic.Int32
	delay time.Duration
}

func (provider *countingVerificationProvider) Name() string { return "counting-test-provider" }
func (provider *countingVerificationProvider) Verify(ctx context.Context, request verification.Request) (verification.Evidence, error) {
	provider.calls.Add(1)
	return (verification.FakeProvider{Result: verification.Unknown, Delay: provider.delay}).Verify(ctx, request)
}

func TestVerificationIdempotencyReplaysEvidenceWithoutRepeatingProbe(t *testing.T) {
	server := NewServer(config.Config{DataDir: filepath.Join(t.TempDir(), "pci.db"), MaxBody: 1 << 20})
	defer func() { _ = server.Store.Close() }()
	provider := &countingVerificationProvider{}
	server.VerificationProvider = provider
	allowTestVerification(server)

	body := `{"sourceNamespace":"default","sourceWorkload":"api","destination":"db.example.test","protocol":"TCP","port":5432,"timeoutMillis":500}`
	post := func(requestBody string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/verifications", strings.NewReader(requestBody))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "probe-attempt-01")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		return response
	}

	first := post(body)
	var firstEvidence verification.Evidence
	if first.Code != http.StatusOK || json.Unmarshal(first.Body.Bytes(), &firstEvidence) != nil {
		t.Fatalf("first verification failed: status=%d body=%s", first.Code, first.Body.String())
	}
	retry := post(body)
	var replayed verification.Evidence
	if retry.Code != http.StatusOK || json.Unmarshal(retry.Body.Bytes(), &replayed) != nil {
		t.Fatalf("idempotent retry failed: status=%d body=%s", retry.Code, retry.Body.String())
	}
	if firstEvidence.ID == "" || replayed.ID != firstEvidence.ID || provider.calls.Load() != 1 {
		t.Fatalf("retry did not replay the original evidence: first=%#v retry=%#v calls=%d", firstEvidence, replayed, provider.calls.Load())
	}

	changedBody := strings.Replace(body, `"timeoutMillis":500`, `"timeoutMillis":501`, 1)
	conflict := post(changedBody)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "IDEMPOTENCY_KEY_CONFLICT") {
		t.Fatalf("same key with different request must conflict: status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	if provider.calls.Load() != 1 {
		t.Fatalf("conflicting retry executed another probe: calls=%d", provider.calls.Load())
	}
}

func TestVerificationCancellationBeforeExecutionDoesNotReserveKey(t *testing.T) {
	server := NewServer(config.Config{DataDir: filepath.Join(t.TempDir(), "pci.db"), MaxBody: 1 << 20})
	defer func() { _ = server.Store.Close() }()
	provider := &countingVerificationProvider{}
	server.VerificationProvider = provider
	allowTestVerification(server)

	body := `{"sourceNamespace":"default","sourceWorkload":"api","destination":"db.example.test","protocol":"TCP","port":5432,"timeoutMillis":500}`
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/verifications", strings.NewReader(body)).WithContext(ctx)
	request.Header.Set("Idempotency-Key", "cancelled-before-start")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusRequestTimeout || provider.calls.Load() != 0 {
		t.Fatalf("cancelled request executed: status=%d calls=%d body=%s", response.Code, provider.calls.Load(), response.Body.String())
	}

	retry := httptest.NewRequest(http.MethodPost, "/api/v1/verifications", strings.NewReader(body))
	retry.Header.Set("Idempotency-Key", "cancelled-before-start")
	retryResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(retryResponse, retry)
	if retryResponse.Code != http.StatusOK || provider.calls.Load() != 1 {
		t.Fatalf("pre-execution cancellation incorrectly reserved idempotency key: status=%d calls=%d body=%s", retryResponse.Code, provider.calls.Load(), retryResponse.Body.String())
	}
}

func TestVerificationPersistenceFailureIsVisibleAndNotRetried(t *testing.T) {
	server := NewServer(config.Config{DataDir: filepath.Join(t.TempDir(), "pci.db"), MaxBody: 1 << 20})
	defer func() { _ = server.Store.Close() }()
	provider := &countingVerificationProvider{}
	server.VerificationProvider = provider
	allowTestVerification(server)
	if _, err := server.Store.DB.Exec(`CREATE TRIGGER fail_verification_insert BEFORE INSERT ON verification_evidence BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	body := `{"sourceNamespace":"default","sourceWorkload":"api","destination":"db.example.test","protocol":"TCP","port":5432,"timeoutMillis":500}`
	post := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/verifications", strings.NewReader(body))
		request.Header.Set("Idempotency-Key", "failed-persistence-attempt")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		return response
	}
	first := post()
	if first.Code != http.StatusInternalServerError || !strings.Contains(first.Body.String(), "STORE_ERROR") {
		t.Fatalf("persistence failure was not returned as an error: status=%d body=%s", first.Code, first.Body.String())
	}
	retry := post()
	if retry.Code != http.StatusConflict || provider.calls.Load() != 1 {
		t.Fatalf("ambiguous retry was repeated: status=%d calls=%d body=%s", retry.Code, provider.calls.Load(), retry.Body.String())
	}
	if evidence, err := server.Store.ListVerificationEvidence(10); err != nil || len(evidence) != 0 {
		t.Fatalf("failed persistence created evidence: records=%v err=%v", evidence, err)
	}
}

func TestMalformedPersistedVerificationIsAnAPIError(t *testing.T) {
	server := NewServer(config.Config{DataDir: filepath.Join(t.TempDir(), "pci.db"), MaxBody: 1 << 20})
	defer func() { _ = server.Store.Close() }()
	if err := server.Store.SaveVerificationEvidence("bad-record", `{not-json`); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/api/v1/verifications/bad-record", "/api/v1/verifications"} {
		request := httptest.NewRequest(http.MethodGet, endpoint, nil)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "STORE_ERROR") {
			t.Errorf("%s returned malformed evidence as success: status=%d body=%s", endpoint, response.Code, response.Body.String())
		}
	}
}

type invalidEvidenceProvider struct{}

func (invalidEvidenceProvider) Name() string { return "invalid-evidence-provider" }
func (invalidEvidenceProvider) Verify(context.Context, verification.Request) (verification.Evidence, error) {
	return verification.Evidence{
		AttemptedAt: time.Now().UTC(),
		Duration:    time.Millisecond,
		Outcome:     verification.Success,
		Method:      "invalid-evidence-provider",
		Limitations: []string{"synthetic"},
	}, nil
}

func TestVerificationRejectsMissingProvenance(t *testing.T) {
	server := NewServer(config.Config{DataDir: filepath.Join(t.TempDir(), "pci.db"), MaxBody: 1 << 20})
	defer func() { _ = server.Store.Close() }()
	server.VerificationProvider = invalidEvidenceProvider{}
	allowTestVerification(server)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/verifications", strings.NewReader(
		`{"sourceNamespace":"default","sourceWorkload":"api","destination":"db.example.test","protocol":"TCP","port":5432,"timeoutMillis":500}`,
	))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "INVALID_PROVIDER_EVIDENCE") {
		t.Fatalf("missing provenance was not rejected: status=%d body=%s", response.Code, response.Body.String())
	}
	evidence, err := server.Store.ListVerificationEvidence(10)
	if err != nil || len(evidence) != 0 {
		t.Fatalf("invalid evidence was persisted: records=%v err=%v", evidence, err)
	}
}

type secretErrorVerificationProvider struct{}

func (secretErrorVerificationProvider) Name() string { return "failing-test-provider" }
func (secretErrorVerificationProvider) Verify(context.Context, verification.Request) (verification.Evidence, error) {
	return verification.Evidence{}, errors.New("request failed with bearer-token=do-not-leak")
}

func TestVerificationProviderErrorsAreRedacted(t *testing.T) {
	server := NewServer(config.Config{DataDir: filepath.Join(t.TempDir(), "pci.db"), MaxBody: 1 << 20})
	defer func() { _ = server.Store.Close() }()
	server.VerificationProvider = secretErrorVerificationProvider{}
	allowTestVerification(server)
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	requestBody := `{"sourceNamespace":"default","sourceWorkload":"api","destination":"db.example.test","protocol":"TCP","port":5432,"timeoutMillis":500}`
	response, err := http.Post(testServer.URL+"/api/v1/verifications", "application/json", strings.NewReader(requestBody))
	if err != nil {
		t.Fatalf("POST verification: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read error response: %v", err)
	}
	if response.StatusCode != http.StatusBadGateway || strings.Contains(string(body), "do-not-leak") {
		t.Fatalf("provider error was not safely redacted: status=%d body=%s", response.StatusCode, body)
	}
}

type unauthorizedVerificationProvider struct{}

func (unauthorizedVerificationProvider) Name() string { return "unauthorized-test-provider" }
func (unauthorizedVerificationProvider) Verify(context.Context, verification.Request) (verification.Evidence, error) {
	return verification.Evidence{}, verification.ErrUnauthorized
}

func TestVerificationProviderTimeoutAfterReservationDoesNotRetry(t *testing.T) {
	server := NewServer(config.Config{DataDir: filepath.Join(t.TempDir(), "pci.db"), MaxBody: 1 << 20})
	defer func() { _ = server.Store.Close() }()
	provider := &countingVerificationProvider{delay: 100 * time.Millisecond}
	server.VerificationProvider = provider
	allowTestVerification(server)

	body := `{"sourceNamespace":"default","sourceWorkload":"api","destination":"db.example.test","protocol":"TCP","port":5432,"timeoutMillis":5}`
	post := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/verifications", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "timeout-after-reserve")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		return response
	}

	first := post()
	if first.Code != http.StatusGatewayTimeout || provider.calls.Load() != 1 {
		t.Fatalf("timeout after reservation: status=%d calls=%d body=%s", first.Code, provider.calls.Load(), first.Body.String())
	}
	retry := post()
	if retry.Code != http.StatusConflict || !strings.Contains(retry.Body.String(), "VERIFICATION_IN_PROGRESS") || provider.calls.Load() != 1 {
		t.Fatalf("provider timeout retry re-executed probe: status=%d calls=%d body=%s", retry.Code, provider.calls.Load(), retry.Body.String())
	}
}

func TestConcurrentIdenticalIdempotencyKeysExecuteOnce(t *testing.T) {
	server := NewServer(config.Config{DataDir: filepath.Join(t.TempDir(), "pci.db"), MaxBody: 1 << 20})
	defer func() { _ = server.Store.Close() }()
	provider := &countingVerificationProvider{delay: 25 * time.Millisecond}
	server.VerificationProvider = provider
	allowTestVerification(server)

	const workers = 8
	body := `{"sourceNamespace":"default","sourceWorkload":"api","destination":"db.example.test","protocol":"TCP","port":5432,"timeoutMillis":500}`
	results := make(chan int, workers)
	var start sync.WaitGroup
	start.Add(1)
	var running sync.WaitGroup
	for range workers {
		running.Add(1)
		go func() {
			defer running.Done()
			start.Wait()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/verifications", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "concurrent-identical-key")
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			results <- response.Code
		}()
	}
	start.Done()
	running.Wait()
	close(results)

	okCount := 0
	conflictCount := 0
	for code := range results {
		switch code {
		case http.StatusOK:
			okCount++
		case http.StatusConflict:
			conflictCount++
		default:
			t.Fatalf("unexpected concurrent status %d", code)
		}
	}
	if provider.calls.Load() != 1 {
		t.Fatalf("concurrent identical keys executed %d probes, want 1", provider.calls.Load())
	}
	if okCount < 1 || okCount+conflictCount != workers {
		t.Fatalf("concurrent outcomes: ok=%d conflict=%d workers=%d", okCount, conflictCount, workers)
	}
}

func TestVerificationAuthorizationAndTimeoutErrors(t *testing.T) {
	server := NewServer(config.Config{DataDir: filepath.Join(t.TempDir(), "pci.db"), MaxBody: 1 << 20})
	defer func() { _ = server.Store.Close() }()
	server.VerificationProvider = unauthorizedVerificationProvider{}
	allowTestVerification(server)
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	requestBody := `{"sourceNamespace":"default","sourceWorkload":"api","destination":"db.example.test","protocol":"TCP","port":5432,"timeoutMillis":500}`

	response, err := http.Post(testServer.URL+"/api/v1/verifications", "application/json", strings.NewReader(requestBody))
	if err != nil {
		t.Fatalf("POST unauthorized verification: %v", err)
	}

	_ = response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("authorization status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}

	server.VerificationProvider = verification.FakeProvider{Result: verification.Success, Delay: 100 * time.Millisecond}
	shortRequest := `{"sourceNamespace":"default","sourceWorkload":"api","destination":"db.example.test","protocol":"TCP","port":5432,"timeoutMillis":5}`
	response, err = http.Post(testServer.URL+"/api/v1/verifications", "application/json", strings.NewReader(shortRequest))
	if err != nil {
		t.Fatalf("POST timed verification: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("timeout status = %d, want %d", response.StatusCode, http.StatusGatewayTimeout)
	}

	provider := &countingVerificationProvider{}
	server.VerificationProvider = provider
	server.VerificationAuthorizer = deadlineAuthorizer{}
	response, err = http.Post(testServer.URL+"/api/v1/verifications", "application/json", strings.NewReader(shortRequest))
	if err != nil {
		t.Fatalf("POST authorization-timeout verification: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusGatewayTimeout || provider.calls.Load() != 0 {
		t.Fatalf("authorization timeout executed probe or returned wrong status: status=%d calls=%d", response.StatusCode, provider.calls.Load())
	}
}

type deadlineAuthorizer struct{}

func (deadlineAuthorizer) Authorize(ctx context.Context, _ verification.Request) error {
	<-ctx.Done()
	return ctx.Err()
}

func allowTestVerification(server *Server) {
	server.VerificationAuthorizer = verification.AllowListAuthorizer{
		Sources:      []string{"default/api"},
		Destinations: []string{"db.example.test"},
		Protocols:    []string{"TCP"},
		Ports:        []int{5432},
	}
}

func mustJSON[T any](t *testing.T, url, method string, payload any) T {
	t.Helper()
	var body string
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		body = string(encoded)
	}
	request, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer response.Body.Close()
	var out T
	if err := json.NewDecoder(response.Body).Decode(&out); err != nil {
		t.Fatalf("decode response %s: %v", response.Status, err)
	}
	if response.StatusCode >= http.StatusBadRequest {
		t.Fatalf("unexpected status %s for %s: %#v", response.Status, url, out)
	}
	return out
}

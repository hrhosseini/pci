package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	corekubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"pci/internal/analyzer"
	"pci/internal/config"
	"pci/internal/graph"
	k8sdiscovery "pci/internal/kubernetes"
	"pci/internal/storage"
	"pci/internal/verification"
)

// Server is the default HTTP API used by the CLI and front-end.
type Server struct {
	mu                     sync.Mutex
	Config                 config.Config
	Store                  *storage.Store
	analyses               map[string]analyzer.Analysis
	snapshots              map[string]analyzer.Snapshot
	VerificationProvider   verification.Provider
	VerificationAuthorizer verification.Authorizer
	verificationSlots      chan struct{}
}

func NewServer(cfg config.Config) *Server {
	path := cfg.DataDir
	if path == "" {
		if configDir, err := os.UserConfigDir(); err == nil {
			path = filepath.Join(configDir, "pci", "pci.db")
		} else {
			path = filepath.Join(os.TempDir(), "pci", "pci.db")
		}
	}
	store, err := storage.Open(path)
	if err != nil {
		store = nil
	}
	return &Server{
		Config:            cfg,
		Store:             store,
		analyses:          map[string]analyzer.Analysis{},
		snapshots:         map[string]analyzer.Snapshot{},
		verificationSlots: make(chan struct{}, 2),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/version", s.handleVersion)
	mux.HandleFunc("/api/v1/capabilities", s.handleCapabilities)
	mux.HandleFunc("/api/v1/clusters/", s.handleCluster)
	mux.HandleFunc("/api/v1/diff", s.handleDiff)
	mux.HandleFunc("/api/v1/connectivity/diff", s.handleConnectivityDiff)
	mux.HandleFunc("/api/v1/connectivity", s.handleConnectivity)
	mux.HandleFunc("/api/v1/analyses", s.handleAnalyses)
	mux.HandleFunc("/api/v1/analyses/", s.handleAnalysis)
	mux.HandleFunc("/api/v1/snapshots", s.handleSnapshots)
	mux.HandleFunc("/api/v1/snapshots/", s.handleSnapshot)
	mux.HandleFunc("/api/v1/verifications", s.handleVerifications)
	mux.HandleFunc("/api/v1/verifications/", s.handleVerification)
	mux.HandleFunc("/api/v1/reports", s.handleReports)
	return mux
}

func (s *Server) Serve(addr string, port int) error {
	if err := validateListenAddress(addr); err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              addr + ":" + strconv.Itoa(port),
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	return httpServer.ListenAndServe()
}

// validateListenAddress keeps the unauthenticated API on loopback by default.
// An empty address would become ":port" and listen on all interfaces in Go.
// Operators who intentionally expose a non-loopback bind must set
// PCI_ALLOW_NON_LOOPBACK=1 (or "true") after reviewing SECURITY.md.
func validateListenAddress(addr string) error {
	if allowNonLoopbackBind() {
		return nil
	}
	host := strings.TrimSpace(addr)
	if host == "" {
		return errors.New("refusing empty bind address (would listen on all interfaces); use 127.0.0.1 or ::1, or set PCI_ALLOW_NON_LOOPBACK=1 after reviewing that the API has no authentication")
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("refusing non-loopback bind address %q for the unauthenticated API; use 127.0.0.1 or ::1, or set PCI_ALLOW_NON_LOOPBACK=1 after reviewing SECURITY.md", addr)
}

func allowNonLoopbackBind() bool {
	value := strings.TrimSpace(os.Getenv("PCI_ALLOW_NON_LOOPBACK"))
	return value == "1" || strings.EqualFold(value, "true")
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "service": "pci", "version": "dev", "database": map[string]any{"sqlite": s.Store != nil}})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"version":      "dev",
		"capabilities": []string{"analyze", "diff", "explain", "snapshot", "compare", "verify", "graph", "report", "doctor", "serve"},
	})
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.clusterConfig()
	if err != nil {
		discovery := k8sdiscovery.CapabilityReport{Mode: "offline", Status: "not-configured", Offline: true, Warnings: []string{err.Error()}}
		writeJSON(w, map[string]any{
			"capabilities": []string{"offline-analysis", "semantic-diff", "graph", "snapshot", "verification-plan-validation", "kubernetes-read-only-discovery"},
			"status":       "ready",
			"kubernetes":   discovery,
			"verification": map[string]any{"enabled": s.verificationEnabled(), "provider": verificationProviderName(s.VerificationProvider), "runtimeProbeImplemented": false},
		})
		return
	}
	discovery, capErr := k8sdiscovery.ReadOnlyCapabilityReport(cfg)
	if capErr != nil && discovery.Status == "" {
		discovery = k8sdiscovery.CapabilityReport{Mode: "offline", Status: "not-configured", Offline: true, Warnings: []string{capErr.Error()}}
	}
	writeJSON(w, map[string]any{
		"capabilities": []string{"offline-analysis", "semantic-diff", "graph", "snapshot", "verification-plan-validation", "kubernetes-read-only-discovery"},
		"status":       "ready",
		"kubernetes":   discovery,
		"verification": map[string]any{"enabled": s.verificationEnabled(), "provider": verificationProviderName(s.VerificationProvider), "runtimeProbeImplemented": false},
	})
}

func (s *Server) handleCluster(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/clusters/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	clusterID := parts[0]
	resp := map[string]any{"id": clusterID, "name": clusterID, "status": "offline-read-only", "complete": false, "capabilities": []string{"networkpolicy", "analysis", "reports", "read-only-discovery"}}
	cfg, err := s.clusterConfig()
	if err != nil {
		resp["warnings"] = []string{err.Error()}
		resp["offline"] = true
		resp["mode"] = "offline"
		resp["status"] = "not-configured"
		resp["complete"] = false
		resp["collectedAt"] = time.Now().UTC().Format(time.RFC3339)
		resp["namespace"] = s.Config.Namespace
		resp["incomplete"] = []string{"cluster"}
		if len(parts) > 1 {
			switch parts[1] {
			case "pods":
				resp["pods"] = []string{}
				resp["count"] = 0
			case "network-policies":
				resp["networkPolicies"] = []string{}
				resp["count"] = 0
			case "capabilities":
				resp["capabilities"] = []string{"networkpolicy", "analysis", "reports", "read-only-discovery", "namespace-scope"}
			case "discover":
				resp["namespaces"] = []string{}
				resp["namespaceLabels"] = map[string]map[string]string{}
				resp["workloads"] = []any{}
				resp["networkPolicies"] = []any{}
			}
		}
		writeJSON(w, resp)
		return
	}
	client, clientErr := corekubernetes.NewForConfig(cfg)
	if clientErr != nil {
		writeError(w, http.StatusBadGateway, "CLUSTER_CLIENT_ERROR", clientErr.Error())
		return
	}
	result, discoveryErr := k8sdiscovery.DiscoverReadOnly(r.Context(), client, k8sdiscovery.DiscoveryOptions{
		Namespace:     s.Config.Namespace,
		AllNamespaces: s.Config.AllNamespaces,
		Context:       s.Config.KubeContext,
		Timeout:       10 * time.Second,
	})
	resp = map[string]any{
		"id":           clusterID,
		"name":         clusterID,
		"mode":         result.Mode,
		"status":       result.Status,
		"offline":      result.Offline,
		"complete":     result.Complete,
		"collectedAt":  result.CollectedAt.Format(time.RFC3339),
		"incomplete":   result.Incomplete,
		"namespace":    result.Namespace,
		"capabilities": []string{"networkpolicy", "analysis", "reports", "read-only-discovery", "namespace-scope"},
		"warnings":     result.Warnings,
		"errors":       result.Errors,
		"cluster":      map[string]any{"host": cfg.Host, "context": s.Config.KubeContext, "namespace": s.Config.Namespace},
	}
	if len(parts) > 1 {
		switch parts[1] {
		case "pods":
			podNames := make([]string, 0, len(result.Workloads))
			for _, workload := range result.Workloads {
				if workload.Kind == "Pod" {
					podNames = append(podNames, workload.Namespace+"/"+workload.Name)
				}
			}
			resp["pods"] = podNames
			resp["count"] = len(podNames)
			writeJSON(w, resp)
			return
		case "network-policies":
			policies := make([]string, 0, len(result.NetworkPolicies))
			for _, policy := range result.NetworkPolicies {
				policies = append(policies, policy.Namespace+"/"+policy.Name)
			}
			resp["networkPolicies"] = policies
			resp["count"] = len(policies)
			writeJSON(w, resp)
			return
		case "capabilities":
			writeJSON(w, resp)
			return
		case "discover":
			resp["namespaces"] = result.Namespaces
			resp["namespaceLabels"] = result.NamespaceLabels
			resp["workloads"] = result.Workloads
			resp["networkPolicies"] = result.NetworkPolicies
			writeJSON(w, resp)
			return
		}
	}
	if discoveryErr != nil {
		writeError(w, http.StatusBadGateway, "CLUSTER_DISCOVERY_ERROR", discoveryErr.Error())
		return
	}
	writeJSON(w, resp)
}

func (s *Server) clusterConfig() (*rest.Config, error) {
	contextName := s.Config.KubeContext
	if contextName == "" {
		contextName = s.Config.Context
	}
	return k8sdiscovery.LoadConfig(s.Config.KubeconfigPath, contextName)
}

func (s *Server) handleAnalyses(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if s.Store == nil {
			writeJSON(w, map[string]any{"analyses": []any{}})
			return
		}
		records, err := s.Store.ListAnalyses(50)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
			return
		}
		out := make([]map[string]any, 0, len(records))
		for _, record := range records {
			out = append(out, map[string]any{"id": record.ID, "kind": record.Kind, "name": record.Name, "path": record.Path, "createdAt": record.CreatedAt.Format(time.RFC3339)})
		}
		writeJSON(w, map[string]any{"analyses": out})
	case http.MethodPost:
		var req struct {
			Path     string `json:"path"`
			Manifest string `json:"manifest"`
			Name     string `json:"name"`
		}
		if err := decodeJSONBody(r, &req, s.Config.MaxBody); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid analysis request")
			return
		}
		analysis, err := s.handleAnalyzeRequest(req.Path, req.Manifest)
		if err != nil {
			writeError(w, http.StatusBadRequest, "ANALYSIS_ERROR", err.Error())
			return
		}
		id := fmt.Sprintf("analysis-%d", time.Now().UnixNano())
		analysis.ID = id
		s.mu.Lock()
		s.analyses[id] = analysis
		s.mu.Unlock()
		if s.Store != nil {
			payload, _ := json.Marshal(analysis)
			if err := s.Store.SaveAnalysis(id, "analysis", req.Name, analysis.Path, string(payload)); err != nil {
				writeError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
				return
			}
		}
		writeJSON(w, analysis)
	default:
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported method")
	}
}

func (s *Server) handleAnalysis(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/analyses/")
	id := strings.TrimSuffix(path, "/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	segments := strings.Split(strings.Trim(id, "/"), "/")
	if len(segments) >= 2 {
		analysisID := segments[0]
		switch segments[1] {
		case "findings":
			analysis, err := s.loadAnalysis(analysisID)
			if err != nil {
				writeError(w, http.StatusNotFound, "ANALYSIS_NOT_FOUND", err.Error())
				return
			}
			writeJSON(w, map[string]any{"findings": analysis.Findings, "analysisId": analysisID})
			return
		case "graph":
			analysis, err := s.loadAnalysis(analysisID)
			if err != nil {
				writeError(w, http.StatusNotFound, "ANALYSIS_NOT_FOUND", err.Error())
				return
			}
			writeJSON(w, graph.Build(analysis))
			return
		case "explain":
			if len(segments) < 3 {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "finding id is required")
				return
			}
			analysis, err := s.loadAnalysis(analysisID)
			if err != nil {
				writeError(w, http.StatusNotFound, "ANALYSIS_NOT_FOUND", err.Error())
				return
			}
			expl, err := analyzer.ExplainFinding(analysis, segments[2])
			if err != nil {
				writeError(w, http.StatusNotFound, "FINDING_NOT_FOUND", err.Error())
				return
			}
			writeJSON(w, expl)
			return
		}
	}

	analysis, err := s.loadAnalysis(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "ANALYSIS_NOT_FOUND", err.Error())
		return
	}
	writeJSON(w, analysis)
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported method")
		return
	}
	var req struct {
		BasePath         string `json:"basePath"`
		ProposedPath     string `json:"proposedPath"`
		BaseManifest     string `json:"baseManifest"`
		ProposedManifest string `json:"proposedManifest"`
	}
	if err := decodeJSONBody(r, &req, s.Config.MaxBody); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid diff request")
		return
	}
	basePath, err := s.resolveManifestPath(req.BasePath, req.BaseManifest)
	if err != nil {
		writeError(w, http.StatusBadRequest, "DIFF_ERROR", err.Error())
		return
	}
	proposedPath, err := s.resolveManifestPath(req.ProposedPath, req.ProposedManifest)
	if err != nil {
		writeError(w, http.StatusBadRequest, "DIFF_ERROR", err.Error())
		return
	}
	report, err := analyzer.DiffPaths(basePath, proposedPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "DIFF_ERROR", err.Error())
		return
	}
	writeJSON(w, report)
}

func (s *Server) handleConnectivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported method")
		return
	}
	var request struct {
		Manifest string                     `json:"manifest"`
		Query    analyzer.ConnectivityQuery `json:"query"`
	}
	if err := decodeJSONBody(r, &request, s.Config.MaxBody); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid connectivity request")
		return
	}
	if strings.TrimSpace(request.Manifest) == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "manifest is required; no cluster inventory is inferred")
		return
	}
	policies, err := analyzer.ParseNetworkPolicies(request.Manifest)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_POLICY_MANIFEST", err.Error())
		return
	}
	result := analyzer.EvaluateConnectivity(policies, request.Query)
	writeJSON(w, result)
}

func (s *Server) handleConnectivityDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported method")
		return
	}
	var request struct {
		BaselineManifest string                     `json:"baselineManifest"`
		ProposedManifest string                     `json:"proposedManifest"`
		Query            analyzer.ConnectivityQuery `json:"query"`
	}
	if err := decodeJSONBody(r, &request, s.Config.MaxBody); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid connectivity diff request")
		return
	}
	if strings.TrimSpace(request.BaselineManifest) == "" || strings.TrimSpace(request.ProposedManifest) == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "baselineManifest and proposedManifest are required")
		return
	}
	before, err := analyzer.ParseNetworkPolicies(request.BaselineManifest)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BASELINE_MANIFEST", err.Error())
		return
	}
	after, err := analyzer.ParseNetworkPolicies(request.ProposedManifest)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PROPOSED_MANIFEST", err.Error())
		return
	}
	writeJSON(w, analyzer.CompareConnectivity(before, after, request.Query))
}

func (s *Server) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if s.Store == nil {
			writeJSON(w, map[string]any{"snapshots": []any{}})
			return
		}
		records, err := s.Store.ListSnapshots(50)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
			return
		}
		out := make([]map[string]any, 0, len(records))
		for _, record := range records {
			out = append(out, map[string]any{"id": record.ID, "name": record.Name, "path": record.Path, "createdAt": record.CreatedAt.Format(time.RFC3339)})
		}
		writeJSON(w, map[string]any{"snapshots": out})
	case http.MethodPost:
		var req struct {
			Path     string `json:"path"`
			Manifest string `json:"manifest"`
			Name     string `json:"name"`
		}
		if err := decodeJSONBody(r, &req, s.Config.MaxBody); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid snapshot request")
			return
		}
		snapshot, err := s.createSnapshot(req.Path, req.Manifest, req.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, "SNAPSHOT_ERROR", err.Error())
			return
		}
		writeJSON(w, snapshot)
	default:
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported method")
	}
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/snapshots/")
	id := strings.TrimSuffix(path, "/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet {
		snapshot, err := s.loadSnapshot(id)
		if err != nil {
			writeError(w, http.StatusNotFound, "SNAPSHOT_NOT_FOUND", err.Error())
			return
		}
		writeJSON(w, snapshot)
		return
	}
	if r.Method == http.MethodPost && strings.Contains(id, "/compare") {
		parts := strings.Split(id, "/")
		if len(parts) >= 2 && parts[1] == "compare" {
			var req struct {
				TargetID string `json:"targetId"`
			}
			if err := decodeJSONBody(r, &req, s.Config.MaxBody); err != nil {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid compare request")
				return
			}
			before, err := s.loadSnapshot(parts[0])
			if err != nil {
				writeError(w, http.StatusNotFound, "SNAPSHOT_NOT_FOUND", err.Error())
				return
			}
			after, err := s.loadSnapshot(req.TargetID)
			if err != nil {
				writeError(w, http.StatusNotFound, "SNAPSHOT_NOT_FOUND", err.Error())
				return
			}
			cmp, err := analyzer.CompareSnapshots(before.Path, after.Path)
			if err != nil {
				writeError(w, http.StatusBadRequest, "COMPARE_ERROR", err.Error())
				return
			}
			writeJSON(w, map[string]any{"beforeSnapshotId": before.ID, "afterSnapshotId": after.ID, "diff": cmp})
			return
		}
	}
	writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported method")
}

func (s *Server) handleVerifications(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if s.Store == nil {
			writeJSON(w, map[string]any{"evidence": []any{}, "verificationEnabled": s.verificationEnabled()})
			return
		}
		payloads, err := s.Store.ListVerificationEvidence(100)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORE_ERROR", "could not load verification evidence")
			return
		}
		evidence := make([]verification.Evidence, 0, len(payloads))
		for _, payload := range payloads {
			var item verification.Evidence
			if err := json.Unmarshal([]byte(payload), &item); err != nil || !validPersistedEvidence(item, "") {
				writeError(w, http.StatusInternalServerError, "STORE_ERROR", "stored verification evidence is malformed")
				return
			}
			evidence = append(evidence, item)
		}
		writeJSON(w, map[string]any{"evidence": evidence, "verificationEnabled": s.verificationEnabled()})
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported method")
		return
	}
	var req verification.Request
	if err := decodeJSONBody(r, &req, s.Config.MaxBody); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid verification request")
		return
	}
	if err := verification.ValidateRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_VERIFICATION_REQUEST", err.Error())
		return
	}
	req.Protocol = strings.ToUpper(req.Protocol)
	if s.VerificationProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "VERIFICATION_DISABLED", "runtime verification is disabled; no live probe was executed")
		return
	}
	if s.VerificationAuthorizer == nil {
		writeError(w, http.StatusForbidden, "VERIFICATION_UNAUTHORIZED", "runtime verification requires an explicit allowlist authorization")
		return
	}
	if s.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "PERSISTENCE_UNAVAILABLE", "verification evidence cannot be persisted; no probe was executed")
		return
	}
	if req.AnalysisID != "" {
		if _, err := s.loadAnalysis(req.AnalysisID); err != nil {
			writeError(w, http.StatusNotFound, "ANALYSIS_NOT_FOUND", "the referenced analysis was not found")
			return
		}
	}
	if req.SnapshotID != "" {
		if _, err := s.loadSnapshot(req.SnapshotID); err != nil {
			writeError(w, http.StatusNotFound, "SNAPSHOT_NOT_FOUND", "the referenced snapshot was not found")
			return
		}
	}
	timeout := time.Duration(req.TimeoutMillis) * time.Millisecond
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	if err := verificationContextError(ctx); err != nil {
		writeVerificationContextError(w, err)
		return
	}
	if err := s.VerificationAuthorizer.Authorize(ctx, req); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			writeError(w, http.StatusGatewayTimeout, "VERIFICATION_TIMEOUT", "verification authorization exceeded the requested timeout")
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			writeError(w, http.StatusRequestTimeout, "VERIFICATION_CANCELLED", "verification authorization was cancelled")
			return
		}
		if errors.Is(err, verification.ErrUnauthorized) {
			writeError(w, http.StatusForbidden, "VERIFICATION_UNAUTHORIZED", "the requested source, destination, protocol, or port is not authorized")
			return
		}
		writeError(w, http.StatusForbidden, "VERIFICATION_UNAUTHORIZED", "verification authorization failed")
		return
	}
	if err := verificationContextError(ctx); err != nil {
		writeVerificationContextError(w, err)
		return
	}
	select {
	case s.verificationSlots <- struct{}{}:
		defer func() { <-s.verificationSlots }()
	case <-ctx.Done():
		writeVerificationContextError(w, ctx.Err())
		return
	}
	if err := verificationContextError(ctx); err != nil {
		writeVerificationContextError(w, err)
		return
	}
	idempotencyKey := r.Header.Get("Idempotency-Key")
	requestHash := ""
	if idempotencyKey != "" {
		if len(idempotencyKey) > 128 || strings.TrimSpace(idempotencyKey) != idempotencyKey ||
			strings.IndexFunc(idempotencyKey, func(r rune) bool { return r < 0x21 || r > 0x7e }) >= 0 {
			writeError(w, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must be 1 to 128 visible ASCII characters without spaces")
			return
		}
		encodedRequest, err := json.Marshal(req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ENCODING_ERROR", "could not encode verification request")
			return
		}
		sum := sha256.Sum256(encodedRequest)
		requestHash = hex.EncodeToString(sum[:])
		reservation, err := s.Store.StartVerificationRequest(idempotencyKey, requestHash)
		if errors.Is(err, storage.ErrIdempotencyConflict) {
			writeError(w, http.StatusConflict, "IDEMPOTENCY_KEY_CONFLICT", "Idempotency-Key was already used for a different request")
			return
		}
		if errors.Is(err, storage.ErrIdempotencyInProgress) {
			writeError(w, http.StatusConflict, "VERIFICATION_IN_PROGRESS", "this verification request may already have executed and will not be repeated")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORE_ERROR", "could not reserve verification request")
			return
		}
		if reservation.Complete {
			var saved verification.Evidence
			if json.Unmarshal([]byte(reservation.Payload), &saved) != nil || !validPersistedEvidence(saved, "") {
				writeError(w, http.StatusInternalServerError, "STORE_ERROR", "stored verification evidence is malformed")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, reservation.Payload)
			return
		}
	}
	evidence, err := s.VerificationProvider.Verify(ctx, req)
	if err != nil {
		if errors.Is(err, verification.ErrUnauthorized) {
			writeError(w, http.StatusForbidden, "VERIFICATION_UNAUTHORIZED", "the configured provider is not authorized to verify this source workload")
			return
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			writeError(w, http.StatusGatewayTimeout, "VERIFICATION_TIMEOUT", "verification exceeded the requested timeout")
			return
		}
		if errors.Is(err, context.Canceled) {
			writeError(w, http.StatusRequestTimeout, "VERIFICATION_CANCELLED", "verification was cancelled")
			return
		}
		writeError(w, http.StatusBadGateway, "VERIFICATION_PROVIDER_ERROR", "the verification provider failed; sensitive provider details were withheld")
		return
	}
	evidence.ModelComparison = "not-evaluated"
	evidence.State = stateForOutcome(evidence.Outcome)
	if evidence.AttemptedAt.IsZero() {
		writeError(w, http.StatusBadGateway, "INVALID_PROVIDER_EVIDENCE", "provider returned evidence without an attempt timestamp")
		return
	}
	if evidence.Duration < 0 || evidence.Duration > timeout {
		writeError(w, http.StatusBadGateway, "INVALID_PROVIDER_EVIDENCE", "provider returned invalid evidence")
		return
	}
	if !validVerificationOutcome(evidence.Outcome) {
		writeError(w, http.StatusBadGateway, "INVALID_PROVIDER_EVIDENCE", "provider returned an unsupported outcome")
		return
	}
	evidence.ID, err = newVerificationEvidenceID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ID_GENERATION_ERROR", "could not create verification evidence identifier")
		return
	}
	evidence.SourceNamespace = req.SourceNamespace
	evidence.SourceWorkload = req.SourceWorkload
	evidence.Destination = req.Destination
	evidence.Protocol = strings.ToUpper(req.Protocol)
	evidence.Port = req.Port
	evidence.TimeoutMillis = req.TimeoutMillis
	evidence.AnalysisID = req.AnalysisID
	evidence.SnapshotID = req.SnapshotID
	if evidence.Method == "" {
		evidence.Method = s.VerificationProvider.Name()
	}
	if len(evidence.Method) > 128 {
		evidence.Method = s.VerificationProvider.Name()
	}
	if !validEvidenceText(evidence.Provenance, 128, false) ||
		!validEvidenceText(evidence.Method, 128, false) ||
		!validEvidenceText(evidence.Cluster, 256, true) ||
		!validEvidenceText(evidence.Context, 256, true) ||
		!validEvidenceText(evidence.CNI, 256, true) {
		writeError(w, http.StatusBadGateway, "INVALID_PROVIDER_EVIDENCE", "provider returned invalid provenance or environment metadata")
		return
	}
	if !safeErrorClass(evidence.ErrorClass) {
		evidence.ErrorClass = "other"
	}
	if evidence.Limitations == nil {
		evidence.Limitations = []string{"An observed connection outcome alone does not establish NetworkPolicy causation."}
	}
	if len(evidence.Limitations) > 16 {
		evidence.Limitations = evidence.Limitations[:16]
	}
	for _, limitation := range evidence.Limitations {
		if !validEvidenceText(limitation, 512, false) {
			writeError(w, http.StatusBadGateway, "INVALID_PROVIDER_EVIDENCE", "provider returned invalid evidence limitations")
			return
		}
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ENCODING_ERROR", "could not encode verification evidence")
		return
	}
	if s.Store != nil {
		if idempotencyKey != "" {
			err = s.Store.CompleteVerificationRequest(idempotencyKey, requestHash, evidence.ID, string(encoded))
		} else {
			err = s.Store.SaveVerificationEvidence(evidence.ID, string(encoded))
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORE_ERROR", "could not persist verification evidence")
			return
		}
	}
	writeJSON(w, evidence)
}

func (s *Server) handleVerification(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/verifications/"), "/")
	if id == "" || r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported method")
		return
	}
	if s.Store == nil {
		writeError(w, http.StatusNotFound, "EVIDENCE_NOT_FOUND", "verification evidence was not found")
		return
	}
	payload, err := s.Store.GetVerificationEvidence(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "EVIDENCE_NOT_FOUND", "verification evidence was not found")
		return
	}
	var evidence verification.Evidence
	if err := json.Unmarshal([]byte(payload), &evidence); err != nil || !validPersistedEvidence(evidence, id) {
		writeError(w, http.StatusInternalServerError, "STORE_ERROR", "stored verification evidence is malformed")
		return
	}
	writeJSON(w, evidence)
}

func validVerificationOutcome(outcome verification.Outcome) bool {
	switch outcome {
	case verification.Success, verification.Failure, verification.Timeout, verification.Unknown:
		return true
	default:
		return false
	}
}

func newVerificationEvidenceID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "verification-" + hex.EncodeToString(random), nil
}

func validEvidenceText(value string, max int, optional bool) bool {
	if optional && value == "" {
		return true
	}
	if value == "" || len(value) > max || strings.TrimSpace(value) != value {
		return false
	}
	return strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
}

func verificationContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ctx.Err()
	}
	return nil
}

func writeVerificationContextError(w http.ResponseWriter, err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusGatewayTimeout, "VERIFICATION_TIMEOUT", "verification exceeded the requested timeout")
		return
	}
	writeError(w, http.StatusRequestTimeout, "VERIFICATION_CANCELLED", "verification was cancelled before execution")
}

func validPersistedEvidence(evidence verification.Evidence, expectedID string) bool {
	return evidence.ID != "" &&
		(expectedID == "" || evidence.ID == expectedID) &&
		validVerificationOutcome(evidence.Outcome) &&
		evidence.State == stateForOutcome(evidence.Outcome) &&
		evidence.ModelComparison == "not-evaluated" &&
		!evidence.AttemptedAt.IsZero() &&
		evidence.TimeoutMillis >= 1 && evidence.TimeoutMillis <= 10000 &&
		evidence.Duration >= 0 && evidence.Duration <= time.Duration(evidence.TimeoutMillis)*time.Millisecond &&
		validEvidenceText(evidence.Provenance, 128, false) &&
		validEvidenceText(evidence.Method, 128, false)
}

func verificationProviderName(provider verification.Provider) string {
	if provider == nil {
		return ""
	}
	return provider.Name()
}

func (s *Server) verificationEnabled() bool {
	return s.VerificationProvider != nil && s.VerificationAuthorizer != nil && s.Store != nil
}

func stateForOutcome(outcome verification.Outcome) string {
	switch outcome {
	case verification.Success:
		return "observed_success"
	case verification.Failure:
		return "observed_failure"
	default:
		return "unknown"
	}
}

func safeErrorClass(classification string) bool {
	switch classification {
	case "", "connection_refused", "dns_error", "timeout", "permission_denied", "unavailable", "other":
		return true
	default:
		return false
	}
}

func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported method")
		return
	}
	var req struct {
		AnalysisID string `json:"analysisId"`
		Path       string `json:"path"`
		Manifest   string `json:"manifest"`
		Name       string `json:"name"`
	}
	if err := decodeJSONBody(r, &req, s.Config.MaxBody); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid report request")
		return
	}
	var analysis analyzer.Analysis
	var err error
	if req.AnalysisID != "" {
		analysis, err = s.loadAnalysis(req.AnalysisID)
	} else {
		analysis, err = s.handleAnalyzeRequest(req.Path, req.Manifest)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "ANALYSIS_ERROR", err.Error())
		return
	}
	report := generateReport(analysis)
	if s.Store != nil {
		reportID := fmt.Sprintf("report-%d", time.Now().UnixNano())
		payload, _ := json.Marshal(report)
		if err := s.Store.SaveReport(reportID, "report", req.Name, string(payload)); err != nil {
			writeError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
			return
		}
		report["id"] = reportID
	}
	writeJSON(w, report)
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, fmt.Sprintf(`{"code":"ENCODING_ERROR","message":"%s"}`, err.Error()), http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}

func (s *Server) handleAnalyzeRequest(path, manifest string) (analyzer.Analysis, error) {
	resolvedPath, err := s.resolveManifestPath(path, manifest)
	if err != nil {
		return analyzer.Analysis{}, err
	}
	return analyzer.AnalyzePath(resolvedPath)
}

func (s *Server) resolveManifestPath(path, manifest string) (string, error) {
	if manifest != "" {
		file, err := os.CreateTemp("", "pci-manifest-*.yaml")
		if err != nil {
			return "", err
		}
		defer func() { _ = os.Remove(file.Name()) }()
		if _, err := file.WriteString(manifest); err != nil {
			_ = file.Close()
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
		return file.Name(), nil
	}
	if path == "" {
		return "", errors.New("path or manifest is required")
	}
	return path, nil
}

func (s *Server) createSnapshot(path, manifest, name string) (analyzer.Snapshot, error) {
	resolvedPath, err := s.resolveManifestPath(path, manifest)
	if err != nil {
		return analyzer.Snapshot{}, err
	}
	snapshot, err := analyzer.SnapshotFromPath(resolvedPath)
	if err != nil {
		return analyzer.Snapshot{}, err
	}
	if name != "" {
		snapshot.Name = name
	}
	if s.Store != nil {
		payload, _ := json.Marshal(snapshot)
		if err := s.Store.SaveSnapshot(snapshot.ID, "snapshot", snapshot.Name, resolvedPath, string(payload)); err != nil {
			return analyzer.Snapshot{}, err
		}
	}
	s.mu.Lock()
	s.snapshots[snapshot.ID] = snapshot
	s.mu.Unlock()
	return snapshot, nil
}

func (s *Server) loadAnalysis(id string) (analyzer.Analysis, error) {
	s.mu.Lock()
	analysis, ok := s.analyses[id]
	s.mu.Unlock()
	if ok {
		return analysis, nil
	}
	if s.Store == nil {
		return analyzer.Analysis{}, fmt.Errorf("analysis %q not found", id)
	}
	record, err := s.Store.GetAnalysis(id)
	if err != nil {
		return analyzer.Analysis{}, err
	}
	var analysisResult analyzer.Analysis
	if err := json.Unmarshal([]byte(record.Payload), &analysisResult); err != nil {
		return analyzer.Analysis{}, err
	}
	analysisResult.ID = record.ID
	return analysisResult, nil
}

func (s *Server) loadSnapshot(id string) (analyzer.Snapshot, error) {
	s.mu.Lock()
	snapshotValue, ok := s.snapshots[id]
	s.mu.Unlock()
	if ok {
		return snapshotValue, nil
	}
	if s.Store == nil {
		return analyzer.Snapshot{}, fmt.Errorf("snapshot %q not found", id)
	}
	record, err := s.Store.GetSnapshot(id)
	if err != nil {
		return analyzer.Snapshot{}, err
	}
	var snapshot analyzer.Snapshot
	if err := json.Unmarshal([]byte(record.Payload), &snapshot); err != nil {
		return analyzer.Snapshot{}, err
	}
	snapshot.ID = record.ID
	return snapshot, nil
}

func generateReport(analysis analyzer.Analysis) map[string]any {
	return map[string]any{
		"analysisId":     analysis.ID,
		"generatedAt":    analysis.GeneratedAt.Format(time.RFC3339),
		"path":           analysis.Path,
		"summary":        analysis.Summary,
		"policyCount":    len(analysis.Policies),
		"findingCount":   len(analysis.Findings),
		"findings":       analysis.Findings,
		"graph":          graph.Build(analysis),
		"policies":       analysis.Policies,
		"recommendation": "Use the modeled graph and findings as an offline review artifact; no live cluster evidence was collected.",
	}
}

func decodeJSONBody(r *http.Request, dst any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	if r.Body == nil {
		return errors.New("request body is required")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if len(body) > int(maxBytes) {
		return fmt.Errorf("request exceeds %d bytes", maxBytes)
	}
	if len(body) == 0 {
		return errors.New("request body is required")
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return err
	}
	return nil
}

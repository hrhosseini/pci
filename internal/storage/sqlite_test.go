package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStorePersistsAnalysisAndSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pci.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer func() { _ = store.Close() }()

	analysisID := "analysis-123"
	analysisPayload := `{"id":"analysis-123","path":"fixtures/base","summary":{"totalPolicies":2,"defaultDeny":1,"findingCount":2}}`
	if err := store.SaveAnalysis(analysisID, "analysis", "baseline", "fixtures/base", analysisPayload); err != nil {
		t.Fatalf("SaveAnalysis returned error: %v", err)
	}
	if err := store.SaveSnapshot("snapshot-123", "snapshot", "baseline", "fixtures/base", `{"id":"snapshot-123","name":"baseline"}`); err != nil {
		t.Fatalf("SaveSnapshot returned error: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Reopen returned error: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	analysis, err := reopened.GetAnalysis(analysisID)
	if err != nil {
		t.Fatalf("GetAnalysis returned error: %v", err)
	}
	if analysis.Payload != analysisPayload {
		t.Fatalf("stored analysis payload mismatch: got %q want %q", analysis.Payload, analysisPayload)
	}
	if _, err := reopened.GetSnapshot("snapshot-123"); err != nil {
		t.Fatalf("GetSnapshot returned error: %v", err)
	}
	list, err := reopened.ListAnalyses(10)
	if err != nil {
		t.Fatalf("ListAnalyses returned error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one persisted analysis, got %d", len(list))
	}
}

func TestOpenCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "state")
	path := filepath.Join(dir, "pci.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected directory to exist: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func TestStorePersistsVerificationEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	payload := `{"id":"verification-1","outcome":"success","state":"observed_success","provenance":"synthetic"}`
	if err := store.SaveVerificationEvidence("verification-1", payload); err != nil {
		t.Fatalf("SaveVerificationEvidence returned error: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	got, err := reopened.GetVerificationEvidence("verification-1")
	if err != nil {
		t.Fatalf("GetVerificationEvidence returned error: %v", err)
	}
	if got != payload {
		t.Fatalf("persisted evidence mismatch: got %q, want %q", got, payload)
	}
	list, err := reopened.ListVerificationEvidence(10)
	if err != nil || len(list) != 1 || list[0] != payload {
		t.Fatalf("ListVerificationEvidence = %#v, %v", list, err)
	}
}

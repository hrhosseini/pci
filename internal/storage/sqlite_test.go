package storage

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("expected directory to exist: %v", err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected sqlite file to exist: %v", err)
	}
	// Unix permission bits are enforced and tested here. On Windows, os.Chmod
	// does not provide equivalent ACL secrecy; see docs/SECURITY.md.
	if runtime.GOOS != "windows" {
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("sqlite directory mode = %o, want owner-only", info.Mode().Perm())
		}
		if fileInfo.Mode().Perm()&0o177 != 0 {
			t.Fatalf("sqlite file mode = %o, want 0600", fileInfo.Mode().Perm())
		}
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
	payload := `{"id":"verification-1","outcome":"success","state":"observed_success","provenance":"synthetic","timeoutMillis":500,"analysisId":"analysis-1","snapshotId":"snapshot-1","cni":"Calico v3.30.0","discoveryComplete":false,"discoveryCollectedAt":"2026-05-28T11:00:00Z"}`
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

func TestVerificationEvidenceInsertDoesNotOverwriteDuplicateID(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "duplicate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.SaveVerificationEvidence("same-id", `{"outcome":"success"}`); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveVerificationEvidence("same-id", `{"outcome":"failure"}`); err == nil {
		t.Fatal("duplicate evidence ID unexpectedly replaced the original record")
	}
	got, err := store.GetVerificationEvidence("same-id")
	if err != nil || got != `{"outcome":"success"}` {
		t.Fatalf("original evidence changed after duplicate insert: payload=%q err=%v", got, err)
	}
}

func TestIdempotentVerificationCompletionSurvivesReopenAndIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotency.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.StartVerificationRequest("request-1", "hash-1")
	if err != nil || state.Complete {
		t.Fatalf("reserve request: state=%#v err=%v", state, err)
	}
	if _, err := store.StartVerificationRequest("request-1", "hash-1"); !errors.Is(err, ErrIdempotencyInProgress) {
		t.Fatalf("second pending request error = %v, want ErrIdempotencyInProgress", err)
	}
	if _, err := store.StartVerificationRequest("request-1", "different-hash"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed request error = %v, want ErrIdempotencyConflict", err)
	}
	if _, err := store.DB.Exec(`CREATE TRIGGER fail_evidence_insert BEFORE INSERT ON verification_evidence BEGIN SELECT RAISE(ABORT, 'injected persistence failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteVerificationRequest("request-1", "hash-1", "evidence-1", `{"id":"evidence-1"}`); err == nil {
		t.Fatal("injected evidence transaction failure was not returned")
	}
	if _, err := store.GetVerificationEvidence("evidence-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed transaction left evidence behind: %v", err)
	}
	if _, err := store.DB.Exec(`DROP TRIGGER fail_evidence_insert`); err != nil {
		t.Fatal(err)
	}
	payload := `{"id":"evidence-1","outcome":"unknown"}`
	if err := store.CompleteVerificationRequest("request-1", "hash-1", "evidence-1", payload); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	state, err = reopened.StartVerificationRequest("request-1", "hash-1")
	if err != nil || !state.Complete || state.Payload != payload {
		t.Fatalf("completed request was not replayable after reopen: state=%#v err=%v", state, err)
	}
	if got, err := reopened.GetVerificationEvidence("evidence-1"); err != nil || got != payload {
		t.Fatalf("evidence not persisted atomically: payload=%q err=%v", got, err)
	}
}

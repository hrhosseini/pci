package analyzer

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

// TrafficStatus distinguishes model predictions, runtime observations, and unknowns.
type TrafficStatus string

const (
	AllowedByModel  TrafficStatus = "allowed_by_model"
	DeniedByModel   TrafficStatus = "denied_by_model"
	ObservedSuccess TrafficStatus = "observed_success"
	ObservedFailure TrafficStatus = "observed_failure"
	Unknown         TrafficStatus = "unknown"
)

// Explanation records how a finding was derived from static policy analysis.
type Explanation struct {
	FindingID           string        `json:"findingId"`
	TrafficStatus       TrafficStatus `json:"trafficStatus"`
	Summary             string        `json:"summary"`
	Direction           string        `json:"direction,omitempty"`
	SourceEndpoint      string        `json:"sourceEndpoint,omitempty"`
	DestinationEndpoint string        `json:"destinationEndpoint,omitempty"`
	Provenance          string        `json:"provenance"`
	Evidence            []string      `json:"evidence,omitempty"`
	ApplicablePolicies  []string      `json:"applicablePolicies,omitempty"`
	ApplicableRules     []string      `json:"applicableRules,omitempty"`
	Reason              string        `json:"reason"`
	Assumptions         []string      `json:"assumptions,omitempty"`
	Unsupported         []string      `json:"unsupported,omitempty"`
	GeneratedAt         time.Time     `json:"generatedAt"`
}

// Snapshot is a persisted point-in-time policy view.
type Snapshot struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Path        string          `json:"path"`
	GeneratedAt time.Time       `json:"generatedAt"`
	Summary     AnalysisSummary `json:"summary"`
	Policies    []PolicySummary `json:"policies"`
	Findings    []Finding       `json:"findings"`
}

func ExplainFinding(analysis Analysis, findingID string) (Explanation, error) {
	for _, finding := range analysis.Findings {
		if finding.ID != findingID {
			continue
		}
		policies := finding.RelevantPolicies
		if len(policies) == 0 {
			policies = []string{"<none>"}
		}
		provenance := finding.Provenance
		if provenance == "" {
			provenance = "modeled"
		}
		explanation := Explanation{
			FindingID:           finding.ID,
			TrafficStatus:       Unknown,
			Summary:             finding.Message,
			SourceEndpoint:      finding.SourceEndpoint,
			DestinationEndpoint: finding.DestinationEndpoint,
			Provenance:          provenance,
			Evidence:            append([]string{}, finding.Evidence...),
			ApplicablePolicies:  policies,
			Reason:              "This is a manifest-level policy finding, not an evaluated source-to-destination flow.",
			Assumptions:         append([]string{}, finding.Assumptions...),
			Unsupported:         []string{"No source/destination workload flow or runtime connectivity was evaluated."},
			GeneratedAt:         time.Now().UTC(),
		}
		return explanation, nil
	}
	return Explanation{}, fmt.Errorf("finding %q not found", findingID)
}

func SnapshotFromPath(path string) (Snapshot, error) {
	analysis, err := AnalyzePath(path)
	if err != nil {
		return Snapshot{}, err
	}
	name := filepath.Base(path)
	if name == "." || name == "/" || name == "" {
		name = "analysis"
	}
	return Snapshot{
		ID:          fmt.Sprintf("snapshot-%d", time.Now().UnixNano()),
		Name:        name,
		Path:        path,
		GeneratedAt: analysis.GeneratedAt,
		Summary:     analysis.Summary,
		Policies:    analysis.Policies,
		Findings:    analysis.Findings,
	}, nil
}

func CompareSnapshots(beforePath, afterPath string) (DiffReport, error) {
	return DiffPaths(beforePath, afterPath)
}

func SortFindingIDs(items []Finding) {
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
}

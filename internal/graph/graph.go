package graph

import "pci/internal/analyzer"

// Graph is a simple deterministic connectivity graph suitable for the dashboard.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

type Node struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Type  string `json:"type"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
}

type Edge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label"`
}

func Build(analysis analyzer.Analysis) Graph {
	g := Graph{Nodes: []Node{}, Edges: []Edge{}}
	seenNodes := map[string]bool{}
	for _, policy := range analysis.Policies {
		key := policy.Namespace + "/" + policy.Name
		if !seenNodes[key] {
			g.Nodes = append(g.Nodes, Node{ID: key, Label: policy.Name, Type: "policy", X: 100 + len(g.Nodes)*120, Y: 80 + len(g.Nodes)*80})
			seenNodes[key] = true
		}
	}
	if len(g.Nodes) == 0 {
		g.Nodes = []Node{{ID: "cluster", Label: "cluster", Type: "cluster", X: 100, Y: 80}}
	}
	for i, policy := range analysis.Policies {
		if i == 0 {
			g.Edges = append(g.Edges, Edge{ID: "cluster-" + policy.Name, Source: "cluster", Target: policy.Namespace + "/" + policy.Name, Label: "policy"})
		}
	}
	return g
}

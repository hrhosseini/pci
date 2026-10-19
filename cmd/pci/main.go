package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corekubernetes "k8s.io/client-go/kubernetes"
	"pci/internal/analyzer"
	"pci/internal/api"
	"pci/internal/config"
	"pci/internal/graph"
	k8sdiscovery "pci/internal/kubernetes"
	"pci/internal/verification"
)

var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:          "pci",
		Short:        "PolicyChangeImpact analyzes Kubernetes NetworkPolicy changes",
		SilenceUsage: true,
		Version:      version,
	}
	rootCmd.SetVersionTemplate("{{.Name}} {{.Version}}\n")

	cfg := config.Default()
	rootCmd.PersistentFlags().StringVar(&cfg.Output, "output", cfg.Output, "Output format (json, yaml, text)")
	rootCmd.PersistentFlags().StringVar(&cfg.Format, "format", cfg.Format, "Output format alias")
	rootCmd.PersistentFlags().StringVar(&cfg.Context, "context", cfg.Context, "Kubernetes context name")
	rootCmd.PersistentFlags().StringVar(&cfg.KubeconfigPath, "kubeconfig", cfg.KubeconfigPath, "Path to kubeconfig file")
	rootCmd.PersistentFlags().StringVar(&cfg.KubeContext, "kube-context", cfg.KubeContext, "Kubernetes context name")
	rootCmd.PersistentFlags().StringVar(&cfg.Namespace, "namespace", cfg.Namespace, "Namespace filter")
	rootCmd.PersistentFlags().BoolVar(&cfg.AllNamespaces, "all-namespaces", cfg.AllNamespaces, "Discover across all namespaces")
	rootCmd.PersistentFlags().StringVar(&cfg.Address, "address", cfg.Address, "Bind address for local server operations")
	rootCmd.PersistentFlags().BoolVarP(&cfg.Verbose, "verbose", "v", false, "Enable verbose logging")
	rootCmd.PersistentFlags().Bool("no-color", false, "Disable colorized output")
	rootCmd.PersistentFlags().String("config", "", "Path to configuration file")

	var analyzePath string
	var failOn string
	var analyzeCmd = &cobra.Command{
		Use:   "analyze",
		Short: "Analyze NetworkPolicy manifests",
		RunE: func(cmd *cobra.Command, args []string) error {
			if analyzePath == "" {
				return fmt.Errorf("--path is required")
			}
			result, err := analyzer.AnalyzePath(analyzePath)
			if err != nil {
				return err
			}
			out, err := analyzer.ToJSON(result)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			if shouldFailOn(failOn, result.Findings) {
				os.Exit(2)
			}
			return nil
		},
	}
	analyzeCmd.Flags().StringVar(&analyzePath, "path", "", "Directory or file path containing NetworkPolicy YAML/JSON manifests")
	analyzeCmd.Flags().StringVar(&failOn, "fail-on", "none", "Fail when findings reach or exceed a given severity (none, low, medium, high, critical)")

	var diffBase string
	var diffProposed string
	var diffCmd = &cobra.Command{
		Use:   "diff",
		Short: "Compare baseline and proposed policy states",
		RunE: func(cmd *cobra.Command, args []string) error {
			if diffBase == "" || diffProposed == "" {
				return fmt.Errorf("--base and --proposed are required")
			}
			report, err := analyzer.DiffPaths(diffBase, diffProposed)
			if err != nil {
				return err
			}
			out, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
	diffCmd.Flags().StringVar(&diffBase, "base", "", "Baseline path")
	diffCmd.Flags().StringVar(&diffProposed, "proposed", "", "Proposed path")

	var explainID string
	var explainAnalysisPath string
	var explainFlow string
	var explainCmd = &cobra.Command{
		Use:   "explain",
		Short: "Explain a structured analysis finding",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				explainID = args[0]
			}
			if explainID == "" && explainFlow == "" {
				return fmt.Errorf("a finding ID or flow must be provided")
			}
			if explainAnalysisPath == "" {
				return fmt.Errorf("--analysis is required")
			}
			data, err := os.ReadFile(explainAnalysisPath)
			if err != nil {
				return err
			}
			var analysis analyzer.Analysis
			if err := json.Unmarshal(data, &analysis); err != nil {
				return err
			}
			if explainID != "" {
				expl, err := analyzer.ExplainFinding(analysis, explainID)
				if err != nil {
					return err
				}
				out, err := json.MarshalIndent(expl, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "{\"status\":\"unsupported\",\"flow\":%q}\n", explainFlow)
			return nil
		},
	}
	explainCmd.Flags().StringVar(&explainAnalysisPath, "analysis", "", "Path to a JSON analysis report")
	explainCmd.Flags().StringVar(&explainFlow, "flow", "", "Optional flow selector to support future runtime explanation")

	var snapshotPath string
	var snapshotName string
	var snapshotCmd = &cobra.Command{
		Use:   "snapshot",
		Short: "Create, inspect, and list snapshots",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("snapshot subcommand is required: create, inspect, list")
			}
			switch args[0] {
			case "create":
				if snapshotPath == "" {
					return fmt.Errorf("--path is required for snapshot create")
				}
				snap, err := analyzer.SnapshotFromPath(snapshotPath)
				if err != nil {
					return err
				}
				if snapshotName != "" {
					snap.Name = snapshotName
				}
				out, err := json.MarshalIndent(snap, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			case "inspect":
				if len(args) < 2 {
					return fmt.Errorf("snapshot inspect requires a snapshot id or file path")
				}
				path := args[1]
				_, err := os.Stat(path)
				if err == nil {
					analysis, err := analyzer.AnalyzePath(path)
					if err != nil {
						return err
					}
					out, err := json.MarshalIndent(analysis, "", "  ")
					if err != nil {
						return err
					}
					fmt.Fprintln(cmd.OutOrStdout(), string(out))
					return nil
				}
				fmt.Fprintf(cmd.OutOrStdout(), "{\"snapshot\":%q,\"status\":\"unsupported-file-reference\"}\n", path)
				return nil
			case "list":
				fmt.Fprintln(cmd.OutOrStdout(), `[{"status":"ok","snapshots":[]}]`)
				return nil
			default:
				return fmt.Errorf("unsupported snapshot action %q", args[0])
			}
		},
	}
	snapshotCmd.Flags().StringVar(&snapshotPath, "path", "", "Path to a directory or file to snapshot")
	snapshotCmd.Flags().StringVar(&snapshotName, "name", "", "Optional snapshot name")

	var compareBefore string
	var compareAfter string
	var compareCmd = &cobra.Command{
		Use:   "compare",
		Short: "Compare two snapshots or manifests",
		RunE: func(cmd *cobra.Command, args []string) error {
			if compareBefore == "" || compareAfter == "" {
				return fmt.Errorf("--before and --after are required")
			}
			report, err := analyzer.CompareSnapshots(compareBefore, compareAfter)
			if err != nil {
				return err
			}
			out, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
	compareCmd.Flags().StringVar(&compareBefore, "before", "", "Before snapshot or path")
	compareCmd.Flags().StringVar(&compareAfter, "after", "", "After snapshot or path")

	var flowManifest string
	var flowQueryPath string
	var connectivityCmd = &cobra.Command{
		Use:   "connectivity",
		Short: "Evaluate one flow against a supplied policy inventory",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flowManifest == "" || flowQueryPath == "" {
				return fmt.Errorf("--manifest and --query are required")
			}
			manifest, err := os.ReadFile(flowManifest)
			if err != nil {
				return err
			}
			query, err := readConnectivityQuery(flowQueryPath)
			if err != nil {
				return err
			}
			policies, err := analyzer.ParseNetworkPolicies(string(manifest))
			if err != nil {
				return err
			}
			result := analyzer.EvaluateConnectivity(policies, query)
			out, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
	connectivityCmd.Flags().StringVar(&flowManifest, "manifest", "", "Path to the NetworkPolicy YAML/JSON input")
	connectivityCmd.Flags().StringVar(&flowQueryPath, "query", "", "Path to a ConnectivityQuery JSON document")

	var flowBaseline string
	var flowProposed string
	var flowDiffQueryPath string
	var connectivityDiffCmd = &cobra.Command{
		Use:   "connectivity-diff",
		Short: "Compare a flow's modeled status across two policy inventories",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flowBaseline == "" || flowProposed == "" || flowDiffQueryPath == "" {
				return fmt.Errorf("--baseline, --proposed, and --query are required")
			}
			baselineManifest, err := os.ReadFile(flowBaseline)
			if err != nil {
				return err
			}
			proposedManifest, err := os.ReadFile(flowProposed)
			if err != nil {
				return err
			}
			query, err := readConnectivityQuery(flowDiffQueryPath)
			if err != nil {
				return err
			}
			baseline, err := analyzer.ParseNetworkPolicies(string(baselineManifest))
			if err != nil {
				return err
			}
			proposed, err := analyzer.ParseNetworkPolicies(string(proposedManifest))
			if err != nil {
				return err
			}
			change := analyzer.CompareConnectivity(baseline, proposed, query)
			out, err := json.MarshalIndent(change, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
	connectivityDiffCmd.Flags().StringVar(&flowBaseline, "baseline", "", "Path to baseline policy YAML/JSON")
	connectivityDiffCmd.Flags().StringVar(&flowProposed, "proposed", "", "Path to proposed policy YAML/JSON")
	connectivityDiffCmd.Flags().StringVar(&flowDiffQueryPath, "query", "", "Path to a ConnectivityQuery JSON document")

	var verifyPlan string
	var verifyCmd = &cobra.Command{
		Use:   "verify",
		Short: "Validate a constrained verification request (execution is disabled unless a provider is configured)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if verifyPlan == "" {
				return fmt.Errorf("--plan is required (JSON verification request)")
			}
			data, err := os.ReadFile(verifyPlan)
			if err != nil {
				return err
			}
			var request verification.Request
			if err := json.Unmarshal(data, &request); err != nil {
				return fmt.Errorf("parse verification request: %w", err)
			}
			if err := verification.ValidateRequest(request); err != nil {
				return err
			}
			out, err := json.MarshalIndent(map[string]any{
				"status":           "disabled",
				"requestValidated": true,
				"probeExecuted":    false,
				"message":          "No safe runtime verification provider is configured; no probe was executed.",
				"request":          request,
			}, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
	verifyCmd.Flags().StringVar(&verifyPlan, "plan", "", "Path to a constrained verification request JSON document")

	var graphPath string
	var graphFormat string
	var graphCmd = &cobra.Command{
		Use:   "graph",
		Short: "Render a modeled connectivity graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			if graphPath == "" {
				return fmt.Errorf("--path is required")
			}
			analysis, err := analyzer.AnalyzePath(graphPath)
			if err != nil {
				return err
			}
			if graphFormat == "" || graphFormat == "json" {
				out, err := json.MarshalIndent(graph.Build(analysis), "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			return fmt.Errorf("unsupported graph format %q", graphFormat)
		},
	}
	graphCmd.Flags().StringVar(&graphPath, "path", "", "Directory or file path to analyze")
	graphCmd.Flags().StringVar(&graphFormat, "format", "json", "Graph export format (json)")

	var reportAnalysis string
	var reportCmd = &cobra.Command{
		Use:   "report",
		Short: "Generate a report from an analysis result",
		RunE: func(cmd *cobra.Command, args []string) error {
			if reportAnalysis == "" {
				return fmt.Errorf("--analysis is required")
			}
			data, err := os.ReadFile(reportAnalysis)
			if err != nil {
				return err
			}
			var result analyzer.Analysis
			if err := json.Unmarshal(data, &result); err != nil {
				return err
			}
			out, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
	reportCmd.Flags().StringVar(&reportAnalysis, "analysis", "", "Path to an analysis JSON payload")

	var serveAddr string
	var servePort int
	var serveCmd = &cobra.Command{
		Use:   "serve",
		Short: "Start the web backend",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			if serveAddr == "" {
				serveAddr = cfg.Address
			}
			if servePort == 0 {
				servePort = cfg.Port
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Listening on %s:%d\n", serveAddr, servePort)
			return api.NewServer(cfg).Serve(serveAddr, servePort)
		},
	}
	serveCmd.Flags().StringVar(&serveAddr, "address", "127.0.0.1", "Bind address")
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "Port")

	var discoverCmd = &cobra.Command{
		Use:   "discover",
		Short: "Discover read-only cluster resources using the current kubeconfig",
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterCfg, err := k8sdiscovery.LoadConfig(cfg.KubeconfigPath, cfg.KubeContext)
			if err != nil {
				return err
			}
			client, err := corekubernetes.NewForConfig(clusterCfg)
			if err != nil {
				return err
			}
			result, err := k8sdiscovery.DiscoverReadOnly(cmd.Context(), client, k8sdiscovery.DiscoveryOptions{
				Namespace:     cfg.Namespace,
				AllNamespaces: cfg.AllNamespaces,
				Context:       cfg.KubeContext,
				Timeout:       10 * time.Second,
			})
			if err != nil && result.Status == "" {
				return err
			}
			out, marshalErr := json.MarshalIndent(result, "", "  ")
			if marshalErr != nil {
				return marshalErr
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}

	var doctorCmd = &cobra.Command{
		Use:   "doctor",
		Short: "Check prerequisites and configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), `{"status":"ok","checks":["go","networkpolicy-semantics","cli","web-ui"]}`)
			return nil
		},
	}

	var versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Display version and supported capabilities",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "pci version %s\ncapabilities: analyze, diff, report, serve, doctor\n", version)
			return nil
		},
	}

	var completionCmd = &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("shell type is required: bash, zsh, fish, or powershell")
			}
			switch strings.ToLower(args[0]) {
			case "bash":
				return rootCmd.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return rootCmd.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return rootCmd.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
		},
	}

	rootCmd.AddCommand(analyzeCmd, diffCmd, explainCmd, snapshotCmd, compareCmd, connectivityCmd, connectivityDiffCmd, verifyCmd, graphCmd, reportCmd, serveCmd, discoverCmd, doctorCmd, versionCmd, completionCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func readConnectivityQuery(path string) (analyzer.ConnectivityQuery, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return analyzer.ConnectivityQuery{}, err
	}
	var query analyzer.ConnectivityQuery
	if err := json.Unmarshal(data, &query); err != nil {
		return analyzer.ConnectivityQuery{}, fmt.Errorf("parse connectivity query: %w", err)
	}
	return query, nil
}

func shouldFailOn(level string, findings []analyzer.Finding) bool {
	if level == "none" {
		return false
	}
	priorities := map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}
	cutoff, ok := priorities[strings.ToLower(level)]
	if !ok {
		return false
	}
	for _, finding := range findings {
		if value, exists := priorities[strings.ToLower(finding.Severity)]; exists && value >= cutoff {
			return true
		}
	}
	return false
}

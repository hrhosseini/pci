package main

import (
	"fmt"
	"os"

	"pci/internal/api"
	"pci/internal/config"
)

func main() {
	cfg := config.LoadFromEnv()
	fmt.Fprintf(os.Stderr, "pci-server listening on %s:%d mode=%q context=%q allNamespaces=%v staticDir=%q dataDir=%q\n",
		cfg.Address, cfg.Port, cfg.KubeMode, cfg.KubeContext, cfg.AllNamespaces, cfg.StaticDir, cfg.DataDir)
	server := api.NewServer(cfg)
	if err := server.Serve(cfg.Address, cfg.Port); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

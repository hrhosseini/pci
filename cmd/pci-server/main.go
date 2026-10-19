package main

import (
	"fmt"
	"os"

	"pci/internal/api"
	"pci/internal/config"
)

func main() {
	cfg := config.LoadFromEnv()
	server := api.NewServer(cfg)
	if err := server.Serve(cfg.Address, cfg.Port); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

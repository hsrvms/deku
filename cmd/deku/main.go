// Deku is a terminal-first coding agent that uses OpenAI-compatible models
// with built-in filesystem, search, Edit, command, and Git tools.
package main

import (
	"fmt"
	"os"

	"github.com/hsrvms/deku/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "deku: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("deku ready\n")
	fmt.Printf("  provider endpoint: %s\n", cfg.Provider.Endpoint)
	fmt.Printf("  model:             %s\n", cfg.Provider.Model)
}

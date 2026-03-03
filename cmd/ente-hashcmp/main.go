package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ente-hashcmp",
	Short: "Ente file hash comparison tool",
	Long: `A command-line tool for calculating and comparing file hashes.
Uses Blake2b algorithm (matching Ente's implementation) with support
for Live Photos, directory scanning, and bbolt database storage.`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

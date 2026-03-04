package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "fhash",
	Short: "File hash comparison tool",
	Long: `A command-line tool for calculating and comparing file hashes.
Uses Blake2b algorithm with support for Live Photos, directory scanning,
and SQLite database storage.`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

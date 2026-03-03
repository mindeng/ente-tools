package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ente-sync",
	Short: "Ente.io sync and upload tool",
	Long: `A command-line tool for syncing metadata with ente.io and uploading files.
Manage your ente accounts, sync library metadata, find missing files, and upload
photos/videos to your ente photos library.`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
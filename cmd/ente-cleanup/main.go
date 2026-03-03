package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	dryRun  bool
)

var rootCmd = &cobra.Command{
	Use:   "ente-cleanup",
	Short: "Ente.io orphan object cleanup tool",
	Long: `A command-line tool for identifying and cleaning up orphan objects
in Ente's S3-compatible storage. This tool helps manage storage costs by
finding objects that exist in storage but have no references in the database.`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ~/.ente/cleanup-config.yaml)")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "simulate run without making changes")
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		viper.AddConfigPath(home + "/.ente")
		viper.SetConfigType("yaml")
		viper.SetConfigName("cleanup-config")
	}

	viper.AutomaticEnv()
	if err := viper.ReadInConfig(); err == nil {
		// Config file found and successfully parsed
	}
}
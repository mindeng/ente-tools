package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"ente-hashcmp/internal/comparator"
	"ente-hashcmp/internal/database"
	"ente-hashcmp/internal/hash"
	"ente-hashcmp/internal/livephoto"
	"ente-hashcmp/internal/metasync"
	"ente-hashcmp/internal/scanner"
	"ente-hashcmp/internal/types"
	"ente-hashcmp/pkg/db"
)

var scanCmd = &cobra.Command{
	Use:   "scan <dir>",
	Short: "Scan directory and compute hashes",
	Args:  cobra.ExactArgs(1),
	Run:   runScan,
}

var compareCmd = &cobra.Command{
	Use:   "compare <dir1> <dir2>",
	Short: "Compare two directories' hash sets",
	Args:  cobra.ExactArgs(2),
	Run:   runCompare,
}

var hashCmd = &cobra.Command{
	Use:   "hash <file>",
	Short: "Compute hash of a single file",
	Args:  cobra.ExactArgs(1),
	Run:   runHash,
}

var dbPathCmd = &cobra.Command{
	Use:   "db-path <dir>",
	Short: "Get the database file path for a directory",
	Args:  cobra.ExactArgs(1),
	Run:   runDBPath,
}

// Meta commands
var metaCmd = &cobra.Command{
	Use:   "meta <subcommand>",
	Short: "Sync ente.io metadata to local database",
}

var metaAccountsCmd = &cobra.Command{
	Use:   "accounts",
	Short: "List configured ente accounts",
	Run:   runMetaAccounts,
}

var metaCollectionsCmd = &cobra.Command{
	Use:   "collections",
	Short: "List collections for an account",
	Run:   runMetaCollections,
}

var metaSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync metadata from ente to local database",
	Run:   runMetaSync,
}

var metaDebugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Debug ente CLI configuration",
	Run:   runMetaDebug,
}

// Flags
var (
	metaAccountFlag  string
	metaAppFlag      string
	metaOutputFlag   string
	metaVerboseFlag  bool
)

func init() {
	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(compareCmd)
	rootCmd.AddCommand(hashCmd)
	rootCmd.AddCommand(dbPathCmd)
	rootCmd.AddCommand(metaCmd)

	// Meta subcommands
	metaCmd.AddCommand(metaAccountsCmd)
	metaCmd.AddCommand(metaCollectionsCmd)
	metaCmd.AddCommand(metaSyncCmd)
	metaCmd.AddCommand(metaDebugCmd)

	// Flags
	metaCollectionsCmd.Flags().StringVar(&metaAccountFlag, "account", "", "Account email (required)")
	metaCollectionsCmd.Flags().StringVar(&metaAppFlag, "app", "photos", "App type (photos, auth, locker)")
	metaCollectionsCmd.Flags().BoolVarP(&metaVerboseFlag, "verbose", "v", false, "Verbose output")
	metaCollectionsCmd.MarkFlagRequired("account")

	metaSyncCmd.Flags().StringVar(&metaAccountFlag, "account", "", "Account email (required)")
	metaSyncCmd.Flags().StringVar(&metaAppFlag, "app", "photos", "App type (photos, auth, locker)")
	metaSyncCmd.Flags().StringVarP(&metaOutputFlag, "output", "o", "", "Output database path (default: ~/.ente/metasync.db)")
	metaSyncCmd.Flags().BoolVarP(&metaVerboseFlag, "verbose", "v", false, "Verbose output")
	metaSyncCmd.MarkFlagRequired("account")
}

func runScan(cmd *cobra.Command, args []string) {
	dir := args[0]

	// Resolve to absolute path
	absDir, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	// Check if directory exists
	if info, err := os.Stat(absDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error accessing directory: %v\n", err)
		os.Exit(1)
	} else if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: %s is not a directory\n", absDir)
		os.Exit(1)
	}

	fmt.Printf("Scanning %s...\n", absDir)

	// Create scanner
	scanner, err := scanner.New(absDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating scanner: %v\n", err)
		os.Exit(1)
	}
	defer scanner.Close()

	// Scan directory
	stats, err := scanner.Scan()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error scanning directory: %v\n", err)
		os.Exit(1)
	}

	// Print results
	fmt.Print(comparator.FormatScanStats(stats, scanner.GetDBPath()))
}

func runCompare(cmd *cobra.Command, args []string) {
	dirA := args[0]
	dirB := args[1]

	// Resolve to absolute paths
	absDirA, err := filepath.Abs(dirA)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	absDirB, err := filepath.Abs(dirB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Comparing %s with %s...\n", absDirA, absDirB)

	// Open databases
	dbA, err := database.Open(absDirA)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database for %s: %v\n", absDirA, err)
		os.Exit(1)
	}
	defer dbA.Close()

	dbB, err := database.Open(absDirB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database for %s: %v\n", absDirB, err)
		os.Exit(1)
	}
	defer dbB.Close()

	// Get all file entries
	entriesA, err := dbA.GetAllFileEntries()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading database for %s: %v\n", absDirA, err)
		os.Exit(1)
	}

	entriesB, err := dbB.GetAllFileEntries()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading database for %s: %v\n", absDirB, err)
		os.Exit(1)
	}

	// Compare
	result := comparator.CompareMaps(entriesA, entriesB)
	fmt.Print(comparator.FormatCompareResult(result))
}

func runHash(cmd *cobra.Command, args []string) {
	filePath := args[0]

	// Check if file exists
	if info, err := os.Stat(filePath); err != nil {
		fmt.Fprintf(os.Stderr, "Error accessing file: %v\n", err)
		os.Exit(1)
	} else if info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: %s is a directory, not a file\n", filePath)
		os.Exit(1)
	}

	// Check if it's a supported file type
	if !livephoto.IsSupportedFile(filepath.Base(filePath)) {
		fmt.Fprintf(os.Stderr, "Warning: %s may not be a supported media file\n", filepath.Base(filePath))
	}

	// Detect Live Photo
	fileType := livephoto.GetFileType(filePath)
	dir := filepath.Dir(filePath)
	baseName := filepath.Base(filePath)

	if fileType == types.FileTypeImage {
		// Check if there's a matching video for a Live Photo
		entries, err := os.ReadDir(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading directory: %v\n", err)
			os.Exit(1)
		}

		var videoPath string
		for _, entry := range entries {
			if !entry.IsDir() {
				if livephoto.MatchLivePhoto(baseName, entry.Name()) {
					videoPath = filepath.Join(dir, entry.Name())
					break
				}
			}
		}

		if videoPath != "" {
			// Compute Live Photo hash
			combinedHash, err := livephoto.CalculateLivePhotoHash(filePath, videoPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error computing Live Photo hash: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Live Photo detected (image + video)\n")
			fmt.Printf("Image: %s\n", baseName)
			fmt.Printf("Video: %s\n", filepath.Base(videoPath))
			fmt.Printf("Hash:  %s\n", combinedHash)
			return
		}
	}

	// Regular file hash
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	fileHash, err := hash.ComputeHash(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error computing hash: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s\n", fileHash)
}

func runDBPath(cmd *cobra.Command, args []string) {
	dir := args[0]

	// Resolve to absolute path
	absDir, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	dbPath := db.GetPath(absDir)
	fmt.Println(dbPath)
}

// Meta command handlers

func runMetaAccounts(cmd *cobra.Command, args []string) {
	// Get ente CLI database path
	cliConfigDir, err := metasync.GetEnteCLIConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting CLI config dir: %v\n", err)
		os.Exit(1)
	}
	cliDBPath := filepath.Join(cliConfigDir, "ente-cli.db")

	// Load accounts
	accounts, err := metasync.LoadAccounts(cliDBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading accounts: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d account(s):\n", len(accounts))
	fmt.Println("====================================")
	for _, acc := range accounts {
		fmt.Printf("Email:   %s\n", acc.Email)
		fmt.Printf("User ID: %d\n", acc.UserID)
		fmt.Printf("App:     %s\n", acc.App)
		fmt.Println("====================================")
	}
}

func runMetaCollections(cmd *cobra.Command, args []string) {
	// Get device key
	deviceKey, err := metasync.GetDeviceKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting device key: %v\n", err)
		fmt.Fprintf(os.Stderr, "Please make sure ente CLI is configured and you have access to the keyring.\n")
		os.Exit(1)
	}

	// Get ente CLI database path
	cliConfigDir, err := metasync.GetEnteCLIConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting CLI config dir: %v\n", err)
		os.Exit(1)
	}
	cliDBPath := filepath.Join(cliConfigDir, "ente-cli.db")

	// Load accounts
	accounts, err := metasync.LoadAccounts(cliDBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading accounts: %v\n", err)
		os.Exit(1)
	}

	// Find matching account
	var targetAccount *metasync.Account
	for i := range accounts {
		if accounts[i].Email == metaAccountFlag && accounts[i].App == metaAppFlag {
			targetAccount = &accounts[i]
			break
		}
	}
	if targetAccount == nil {
		fmt.Fprintf(os.Stderr, "Account not found: %s (app: %s)\n", metaAccountFlag, metaAppFlag)
		fmt.Fprintf(os.Stderr, "Run 'ente-hashcmp meta accounts' to list available accounts.\n")
		os.Exit(1)
	}

	// Decrypt account secrets
	accountSecret, err := metasync.DecryptAccountSecrets(*targetAccount, deviceKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error decrypting account secrets: %v\n", err)
		os.Exit(1)
	}

	// List collections
	ctx := context.Background()
	collections, err := metasync.ListCollections(ctx, metaAccountFlag, metaAppFlag, deviceKey, accountSecret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing collections: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d collection(s):\n", len(collections))
	fmt.Println("====================================")
	for _, coll := range collections {
		fmt.Printf("ID:      %d\n", coll.ID)
		fmt.Printf("Name:    %s\n", coll.Name)
		fmt.Printf("Owner:   %d\n", coll.OwnerID)
		if coll.IsShared {
			fmt.Printf("Type:    Shared\n")
		}
		fmt.Println("====================================")
	}
}

func runMetaSync(cmd *cobra.Command, args []string) {
	// Set default output path if not specified
	if metaOutputFlag == "" {
		cliConfigDir, err := metasync.GetEnteCLIConfigDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting CLI config dir: %v\n", err)
			os.Exit(1)
		}
		metaOutputFlag = filepath.Join(cliConfigDir, "metasync.db")
	}

	// Get device key
	deviceKey, err := metasync.GetDeviceKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting device key: %v\n", err)
		fmt.Fprintf(os.Stderr, "Please make sure ente CLI is configured and you have access to the keyring.\n")
		os.Exit(1)
	}

	// Get ente CLI database path
	cliConfigDir, err := metasync.GetEnteCLIConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting CLI config dir: %v\n", err)
		os.Exit(1)
	}
	cliDBPath := filepath.Join(cliConfigDir, "ente-cli.db")

	// Load accounts
	accounts, err := metasync.LoadAccounts(cliDBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading accounts: %v\n", err)
		os.Exit(1)
	}

	// Find matching account
	var targetAccount *metasync.Account
	for i := range accounts {
		if accounts[i].Email == metaAccountFlag && accounts[i].App == metaAppFlag {
			targetAccount = &accounts[i]
			break
		}
	}
	if targetAccount == nil {
		fmt.Fprintf(os.Stderr, "Account not found: %s (app: %s)\n", metaAccountFlag, metaAppFlag)
		fmt.Fprintf(os.Stderr, "Run 'ente-hashcmp meta accounts' to list available accounts.\n")
		os.Exit(1)
	}

	// Decrypt account secrets
	accountSecret, err := metasync.DecryptAccountSecrets(*targetAccount, deviceKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error decrypting account secrets: %v\n", err)
		os.Exit(1)
	}

	// Run sync
	ctx := context.Background()
	opts := metasync.SyncOptions{
		AccountEmail:  metaAccountFlag,
		App:           metaAppFlag,
		DeviceKey:     deviceKey,
		AccountSecret: accountSecret,
		DBPath:        metaOutputFlag,
		Verbose:       metaVerboseFlag,
	}

	result, err := metasync.Sync(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during sync: %v\n", err)
		os.Exit(1)
	}

	// Print results
	fmt.Printf("Sync completed in %v\n", result.Duration)
	fmt.Printf("Collections pulled: %d\n", result.CollectionsPulled)
	fmt.Printf("Files pulled: %d\n", result.FilesPulled)

	if len(result.Errors) > 0 {
		fmt.Printf("\nEncountered %d error(s):\n", len(result.Errors))
		for i, e := range result.Errors {
			fmt.Printf("%d. %v\n", i+1, e)
		}
	}

	fmt.Printf("\nDatabase saved to: %s\n", metaOutputFlag)
}

func runMetaDebug(cmd *cobra.Command, args []string) {
	// Get ente CLI config dir
	cliConfigDir, err := metasync.GetEnteCLIConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting CLI config dir: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("CLI Config Dir: %s\n", cliConfigDir)

	// Check for config file
	configPath := filepath.Join(cliConfigDir, "config.yaml")
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Config File: %s\n", configPath)
		// Try to load config
		cfg, err := metasync.LoadConfig()
		if err != nil {
			fmt.Printf("Warning: failed to load config: %v\n", err)
		} else {
			fmt.Printf("API Endpoint: %s\n", cfg.APIEndpoint)
		}
	} else {
		fmt.Printf("Config File: %s (not found)\n", configPath)
	}

	// Check for database
	cliDBPath := filepath.Join(cliConfigDir, "ente-cli.db")
	if _, err := os.Stat(cliDBPath); err == nil {
		fmt.Printf("Database File: %s\n", cliDBPath)
	} else {
		fmt.Printf("Database File: %s (not found)\n", cliDBPath)
	}

	// Get device key
	fmt.Println("\n--- Device Key ---")
	deviceKey, err := metasync.GetDeviceKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting device key: %v\n", err)
		fmt.Println("  You may need to set ENTE_CLI_SECRETS_PATH environment variable")
	} else {
		fmt.Printf("Device Key Length: %d bytes\n", len(deviceKey))
		fmt.Printf("Device Key (base64): %s\n", base64.StdEncoding.EncodeToString(deviceKey))
	}
}

// Export fileType for the hash command
var FileType = types.FileTypeImage
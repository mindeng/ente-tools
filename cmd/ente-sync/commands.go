package main

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"ente-hashcmp/internal/findings"
	"ente-hashcmp/internal/hash"
	"ente-hashcmp/internal/metasync"
	"ente-hashcmp/internal/upload"
)

// Accounts command
var accountsCmd = &cobra.Command{
	Use:   "accounts",
	Short: "List configured ente accounts",
	Run:   runAccounts,
}

// Collections command
var collectionsCmd = &cobra.Command{
	Use:   "collections",
	Short: "List collections for an account",
	Run:   runCollections,
}

// Sync command
var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync metadata from ente to local database",
	Run:   runSync,
}

// Debug command
var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Debug ente CLI configuration",
	Run:   runDebug,
}

// Diff command
var diffCmd = &cobra.Command{
	Use:   "diff <dir>",
	Short: "Find files not in ente library",
	Long:  "Find files that exist locally but are not in ente library. Use --copy-to option to copy missing files to a target directory.",
	Args:  cobra.ExactArgs(1),
	Run:   runDiff,
}

// Upload command
var uploadCmd = &cobra.Command{
	Use:   "upload <file|dir>",
	Short: "Upload files to ente photos library",
	Long:  "Upload files to ente photos library. If --album is not specified, the file will be uploaded to the uncategorized collection (requires sync to be run first).",
	Args:  cobra.ExactArgs(1),
	Run:   runUpload,
}

// Flags
var (
	accountFlag      string
	appFlag          string
	outputFlag       string
	verboseFlag      bool
	diffMetaDBFlag   string
	diffVerboseFlag  bool
	diffCopyToFlag   string
	diffDryRunFlag   bool
	diffWorkersFlag  int
	uploadAlbumFlag  string
)

func init() {
	rootCmd.AddCommand(accountsCmd)
	rootCmd.AddCommand(collectionsCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(debugCmd)
	rootCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(uploadCmd)

	// Collections flags
	collectionsCmd.Flags().StringVarP(&accountFlag, "account", "a", "", "Account email (required)")
	collectionsCmd.Flags().StringVar(&appFlag, "app", "photos", "App type (photos, auth, locker)")
	collectionsCmd.Flags().BoolVarP(&verboseFlag, "verbose", "v", false, "Verbose output")
	collectionsCmd.MarkFlagRequired("account")

	// Sync flags
	syncCmd.Flags().StringVarP(&accountFlag, "account", "a", "", "Account email (required)")
	syncCmd.Flags().StringVar(&appFlag, "app", "photos", "App type (photos, auth, locker)")
	syncCmd.Flags().StringVarP(&outputFlag, "output", "o", "", "Output database path (default: ~/.ente/metasync.db)")
	syncCmd.Flags().BoolVarP(&verboseFlag, "verbose", "v", false, "Verbose output")
	syncCmd.MarkFlagRequired("account")

	// Diff flags
	diffCmd.Flags().StringVar(&diffMetaDBFlag, "meta-db", "", "Path to metasync database (default: ~/.ente/metasync.db)")
	diffCmd.Flags().BoolVarP(&diffVerboseFlag, "verbose", "v", false, "Verbose output")
	diffCmd.Flags().StringVar(&diffCopyToFlag, "copy-to", "", "Copy missing files to this directory, maintaining original structure")
	diffCmd.Flags().BoolVarP(&diffDryRunFlag, "dry-run", "n", false, "Dry run - only show what would be copied")
	diffCmd.Flags().IntVarP(&diffWorkersFlag, "workers", "w", 4, "Number of parallel copy workers (default: 4)")

	// Upload flags
	uploadCmd.Flags().StringVarP(&accountFlag, "account", "a", "", "Account email (required)")
	uploadCmd.Flags().StringVar(&appFlag, "app", "photos", "App type (photos, auth, locker)")
	uploadCmd.Flags().StringVarP(&uploadAlbumFlag, "album", "", "", "Album name or ID (default: uncategorized)")
	uploadCmd.Flags().BoolVarP(&verboseFlag, "verbose", "v", false, "Verbose output")
	uploadCmd.MarkFlagRequired("account")
}

func runAccounts(cmd *cobra.Command, args []string) {
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

func runCollections(cmd *cobra.Command, args []string) {
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
		if accounts[i].Email == accountFlag && accounts[i].App == appFlag {
			targetAccount = &accounts[i]
			break
		}
	}
	if targetAccount == nil {
		fmt.Fprintf(os.Stderr, "Account not found: %s (app: %s)\n", accountFlag, appFlag)
		fmt.Fprintf(os.Stderr, "Run 'ente-sync accounts' to list available accounts.\n")
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
	collections, err := metasync.ListCollections(ctx, accountFlag, appFlag, deviceKey, accountSecret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing collections: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d collection(s):\n\n", len(collections))
	fmt.Printf("%-15s %-30s %-8s %-20s %-15s\n", "ID", "Name", "Shared", "UpdatedAt", "Type")
	fmt.Println("-----------------------------------------------------------------------------")
	for _, coll := range collections {
		shared := "No"
		if coll.IsShared {
			shared = "Yes"
		}
		updatedAt := coll.UpdatedTime.Format("2006-01-02 15:04:05")
		fmt.Printf("%-15d %-30s %-8s %-20s %-15s\n", coll.ID, coll.Name, shared, updatedAt, coll.Type)
	}
	fmt.Println("-----------------------------------------------------------------------------")
}

func runSync(cmd *cobra.Command, args []string) {
	// Set default output path if not specified
	if outputFlag == "" {
		cliConfigDir, err := metasync.GetEnteCLIConfigDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting CLI config dir: %v\n", err)
			os.Exit(1)
		}
		outputFlag = filepath.Join(cliConfigDir, "metasync.db")
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
		if accounts[i].Email == accountFlag && accounts[i].App == appFlag {
			targetAccount = &accounts[i]
			break
		}
	}
	if targetAccount == nil {
		fmt.Fprintf(os.Stderr, "Account not found: %s (app: %s)\n", accountFlag, appFlag)
		fmt.Fprintf(os.Stderr, "Run 'ente-sync accounts' to list available accounts.\n")
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
		AccountEmail:  accountFlag,
		App:           appFlag,
		DeviceKey:     deviceKey,
		AccountSecret: accountSecret,
		DBPath:        outputFlag,
		Verbose:       verboseFlag,
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

	fmt.Printf("\nDatabase saved to: %s\n", outputFlag)
}

func runDebug(cmd *cobra.Command, args []string) {
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

func runDiff(cmd *cobra.Command, args []string) {
	dir := args[0]

	// Resolve to absolute path
	absDir, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	// Set default meta db path if not specified
	metaDBPath := diffMetaDBFlag
	if metaDBPath == "" {
		cliConfigDir, err := metasync.GetEnteCLIConfigDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting CLI config dir: %v\n", err)
			os.Exit(1)
		}
		metaDBPath = filepath.Join(cliConfigDir, "metasync.db")
	}

	// Check if metasync database exists
	if _, err := os.Stat(metaDBPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: metasync database not found at %s\n", metaDBPath)
		fmt.Fprintf(os.Stderr, "Please run 'ente-sync sync --account <email>' first to sync your ente metadata.\n")
		os.Exit(1)
	}

	// Check if we need to copy files
	if diffCopyToFlag != "" {
		absTargetDir, err := filepath.Abs(diffCopyToFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error resolving target path: %v\n", err)
			os.Exit(1)
		}

		if diffDryRunFlag {
			fmt.Printf("DRY RUN - No files will be copied\n\n")
		}

		fmt.Printf("Analyzing %s and copying to %s...\n", absDir, absTargetDir)
		fmt.Printf("Using ente metadata from: %s\n", metaDBPath)
		if diffVerboseFlag {
			fmt.Printf("Using %d parallel workers\n\n", diffWorkersFlag)
		} else {
			fmt.Println()
		}

		// StreamCopyMissingFiles handles both analysis and copying in parallel
		copyResult, err := findings.StreamCopyMissingFiles(findings.StreamCopyOptions{
			SourceDir:  absDir,
			TargetDir:  absTargetDir,
			MetaDBPath: metaDBPath,
			Verbose:    diffVerboseFlag,
			DryRun:     diffDryRunFlag,
			Workers:    diffWorkersFlag,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error during copy: %v\n", err)
			os.Exit(1)
		}

		// Print results
		fmt.Print(findings.FormatStreamCopyResult(copyResult))
		return
	}

	// No copy option - just analyze and display results
	fmt.Printf("Analyzing %s...\n", absDir)
	fmt.Printf("Using ente metadata from: %s\n\n", metaDBPath)

	// Analyze missing files
	result, err := findings.AnalyzeMissing(findings.AnalyzeOptions{
		Dir:        absDir,
		MetaDBPath: metaDBPath,
		Verbose:    diffVerboseFlag,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during analysis: %v\n", err)
		os.Exit(1)
	}

	// Print results
	fmt.Print(findings.FormatMissingResult(result))
}

func runUpload(cmd *cobra.Command, args []string) {
	path := args[0]

	// Check if path exists
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error accessing path: %v\n", err)
		os.Exit(1)
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
		if accounts[i].Email == accountFlag && accounts[i].App == appFlag {
			targetAccount = &accounts[i]
			break
		}
	}
	if targetAccount == nil {
		fmt.Fprintf(os.Stderr, "Account not found: %s (app: %s)\n", accountFlag, appFlag)
		fmt.Fprintf(os.Stderr, "Run 'ente-sync accounts' to list available accounts.\n")
		os.Exit(1)
	}

	// Decrypt account secrets
	accountSecret, err := metasync.DecryptAccountSecrets(*targetAccount, deviceKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error decrypting account secrets: %v\n", err)
		os.Exit(1)
	}

	// Get ente CLI config
	cfg, err := metasync.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Determine collection ID
	var collectionID int64
	var collectionKey []byte

	if uploadAlbumFlag != "" {
		// User specified an album
		ctx := context.Background()
		collections, err := metasync.ListCollections(ctx, accountFlag, appFlag, deviceKey, accountSecret)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing collections: %v\n", err)
			os.Exit(1)
		}

		// Try to find collection by name or ID
		found := false
		var targetCollection *metasync.DecryptedCollection
		for _, coll := range collections {
			// Match by name
			if coll.Name == uploadAlbumFlag {
				targetCollection = &coll
				found = true
				break
			}
			// Also match by ID (convert flag to int64 for comparison)
			if idStr := fmt.Sprintf("%d", coll.ID); idStr == uploadAlbumFlag {
				targetCollection = &coll
				found = true
				break
			}
		}

		if !found {
			fmt.Fprintf(os.Stderr, "Album not found: %s\n", uploadAlbumFlag)
			fmt.Fprintf(os.Stderr, "Available albums:\n")
			for _, coll := range collections {
				fmt.Printf("  - %s (ID: %d)\n", coll.Name, coll.ID)
			}
			os.Exit(1)
		}

		collectionID = targetCollection.ID
		collectionKey, err = getCollectionKey(targetCollection, accountSecret.MasterKey, accountSecret.SecretKey, accountSecret.PublicKey, targetAccount.UserID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting collection key: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Using album: %s (ID: %d)\n", targetCollection.Name, collectionID)
	} else {
		// No album specified, try to find uncategorized collection from metasync DB
		metasyncDBPath, err := getDefaultMetaDBPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting metasync DB path: %v\n", err)
			os.Exit(1)
		}

		db, err := metasync.NewDatabase(metasyncDBPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening metasync database: %v\n", err)
			fmt.Fprintf(os.Stderr, "Please run 'ente-sync sync' first to create the database.\n")
			os.Exit(1)
		}
		defer db.Close()

		uncategorizedColl, err := db.GetUncategorizedCollection()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading collections: %v\n", err)
			os.Exit(1)
		}

		if uncategorizedColl == nil {
			fmt.Fprintf(os.Stderr, "No uncategorized collection found in metasync database.\n")
			fmt.Fprintf(os.Stderr, "Please run 'ente-sync sync' first.\n")
			fmt.Fprintf(os.Stderr, "If you've already run sync, your server may not have an uncategorized collection.\n")
			fmt.Fprintf(os.Stderr, "Please specify an album using --album flag.\n")
			os.Exit(1)
		}

		collectionID = uncategorizedColl.ID
		collectionKey, err = getCollectionKey(uncategorizedColl, accountSecret.MasterKey, accountSecret.SecretKey, accountSecret.PublicKey, targetAccount.UserID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting collection key: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Using uncategorized collection: %s (ID: %d)\n", uncategorizedColl.Name, collectionID)
	}

	// Create upload client with debug mode only if verbose flag is set
	uploader := upload.NewUploadClient(cfg.APIEndpoint, accountSecret.TokenStr())
	if verboseFlag {
		uploader.SetDebug(true)
	}

	// Handle file or directory
	if info.IsDir() {
		err = uploadDirectory(uploader, path, collectionID, collectionKey, verboseFlag)
	} else {
		err = uploadSingleFile(uploader, path, collectionID, collectionKey, verboseFlag)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Upload failed: %v\n", err)
		os.Exit(1)
	}
}

// getDefaultMetaDBPath returns the default path to the metasync database
func getDefaultMetaDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ente", "metasync.db"), nil
}

// getCollectionKey decrypts the collection's key using account credentials
func getCollectionKey(collection *metasync.DecryptedCollection, masterKey, secretKey, publicKey []byte, userID int64) ([]byte, error) {
	return metasync.GetCollectionKey(metasync.Collection{
		ID:                 collection.ID,
		Owner:              metasync.Owner{ID: collection.OwnerID},
		EncryptedKey:       collection.EncryptedKey,
		KeyDecryptionNonce: collection.KeyDecryptionNonce,
	}, masterKey, secretKey, publicKey, userID)
}

// uploadSingleFile uploads a single file to ente
func uploadSingleFile(uploader *upload.UploadClient, filePath string, collectionID int64, collectionKey []byte, verbose bool) error {
	// Check for live photo
	livePhotoComponents, err := upload.DetectLivePhoto(filePath)
	if err == nil && livePhotoComponents != nil {
		return uploadLivePhoto(uploader, livePhotoComponents, collectionID, collectionKey, verbose)
	}

	// Regular file upload
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Determine file type
	var fileType upload.FileCategory
	fileName := filepath.Base(filePath)
	if upload.IsImageFile(filePath) {
		fileType = upload.FileCategoryImage
	} else if upload.IsVideoFile(filePath) {
		fileType = upload.FileCategoryVideo
	} else {
		return fmt.Errorf("unsupported file type: %s", fileName)
	}

	// Compute hash
	fileHash, err := hash.ComputeHashFromBytes(fileData)
	if err != nil {
		return fmt.Errorf("failed to compute hash: %w", err)
	}

	// Get file info
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	// Generate file key
	fileKey, err := upload.GenerateKey()
	if err != nil {
		return fmt.Errorf("failed to generate file key: %w", err)
	}

	// Encrypt file key with collection key (not master key!)
	encryptedKey, err := upload.EncryptKey(fileKey, collectionKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt file key: %w", err)
	}

	// Encrypt file data (use chunked encryption for files)
	encryptedFile, fileHeader, err := upload.EncryptFileData(fileData, fileKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt file: %w", err)
	}

	// Generate thumbnail
	thumbnail, err := upload.GetThumbnail(filePath, fileType)
	if err != nil {
		return fmt.Errorf("failed to generate thumbnail: %w", err)
	}

	// Encrypt thumbnail data
	encryptedThumbnail, thumbnailHeader, err := upload.EncryptData(thumbnail, fileKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt thumbnail: %w", err)
	}

	// Build metadata with duration for videos
	metadata := upload.BuildFileMetadata(fileName, fileType, fileInfo.ModTime(), fileHash)

	// Add duration for videos (in encrypted metadata)
	if fileType == upload.FileCategoryVideo {
		videoMetadata, err := upload.ExtractVideoMetadata(filePath)
		if err == nil {
			// Duration is stored in seconds (ceiling)
			metadata.Duration = int64(videoMetadata.Duration)
			if verbose {
				fmt.Printf("Video duration: %.2fs (%.2fmin)\n", videoMetadata.Duration, videoMetadata.Duration/60)
				fmt.Printf("Video dimensions: %dx%d\n", videoMetadata.Width, videoMetadata.Height)
			}
		} else if verbose {
			fmt.Printf("Warning: failed to extract video metadata: %v\n", err)
		}
	}

	encryptedMetadata, err := upload.EncryptMetadata(metadata, fileKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt metadata: %w", err)
	}

	// Get upload URLs - use MD5 of encrypted data
	fileMD5 := computeMD5(encryptedFile)
	uploadURL, err := uploader.GetUploadURL(int64(len(encryptedFile)), fileMD5)
	if err != nil {
		return fmt.Errorf("failed to get upload URL: %w", err)
	}

	// Upload file - match MD5 to encrypted data
	if err := uploader.UploadFile(uploadURL.URL, encryptedFile, fileMD5); err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}

	// Upload thumbnail - use MD5 of encrypted thumbnail
	thumbMD5 := computeMD5(encryptedThumbnail)
	thumbUploadURL, err := uploader.GetUploadURL(int64(len(encryptedThumbnail)), thumbMD5)
	if err != nil {
		return fmt.Errorf("failed to get thumbnail upload URL: %w", err)
	}

	if err := uploader.UploadFile(thumbUploadURL.URL, encryptedThumbnail, thumbMD5); err != nil {
		return fmt.Errorf("failed to upload thumbnail: %w", err)
	}

	// Create file entry
	createReq := upload.CreateFileRequest{
		CollectionID:       collectionID,
		EncryptedKey:       encryptedKey.CipherText,
		KeyDecryptionNonce: encryptedKey.Nonce,
		File: upload.FileAttributes{
			ObjectKey:        uploadURL.ObjectKey,
			DecryptionHeader: base64.StdEncoding.EncodeToString(fileHeader),
			Size:             int64(len(encryptedFile)),
		},
		Thumbnail: upload.FileAttributes{
			ObjectKey:        thumbUploadURL.ObjectKey,
			DecryptionHeader: base64.StdEncoding.EncodeToString(thumbnailHeader),
			Size:             int64(len(encryptedThumbnail)),
		},
		Metadata:     encryptedMetadata,
		UpdationTime: time.Now().UnixMicro(),
	}

	if verbose {
		fmt.Printf("Creating file entry...\n")
		fmt.Printf("  CollectionID: %d\n", createReq.CollectionID)
		fmt.Printf("  File ObjectKey: %s\n", createReq.File.ObjectKey)
		fmt.Printf("  Thumbnail ObjectKey: %s\n", createReq.Thumbnail.ObjectKey)
	}

	resp, err := uploader.CreateFile(createReq)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	if verbose {
		fmt.Printf("File uploaded successfully (ID: %d)\n", resp.ID)
	}

	return nil
}

// uploadDirectory uploads all files in a directory
func uploadDirectory(uploader *upload.UploadClient, dir string, collectionID int64, collectionKey []byte, verbose bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	successCount := 0
	failCount := 0

	// Track live photos that have been uploaded to avoid duplicates
	// key: image path, value: true
	uploadedLivePhotos := make(map[string]bool)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())
		if verbose {
			fmt.Printf("Processing: %s\n", entry.Name())
		}

		// Check if this file is part of a live photo that was already uploaded
		// We upload live photos from the image side, so skip if video was already processed
		livePhotoComponents, err := upload.DetectLivePhoto(filePath)
		if err == nil && livePhotoComponents != nil {
			// This is a live photo
			imagePath := livePhotoComponents.ImagePath
			videoPath := livePhotoComponents.VideoPath

			// If we're processing a video file and its image was already uploaded, skip
			if filePath == videoPath && uploadedLivePhotos[imagePath] {
				if verbose {
					fmt.Printf("Skipping %s (already uploaded as part of live photo %s)\n", entry.Name(), filepath.Base(imagePath))
				}
				successCount++ // Count as success to avoid confusion
				continue
			}

			// Upload from the image side only
			if filePath == imagePath {
				if err := uploadSingleFile(uploader, filePath, collectionID, collectionKey, false); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to upload %s: %v\n", entry.Name(), err)
					failCount++
				} else {
					if verbose {
						fmt.Printf("Uploaded: %s (live photo)\n", entry.Name())
					}
					successCount++
					uploadedLivePhotos[imagePath] = true
				}
				continue
			}
		}

		// Regular file upload
		if err := uploadSingleFile(uploader, filePath, collectionID, collectionKey, false); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to upload %s: %v\n", entry.Name(), err)
			failCount++
		} else {
			if verbose {
				fmt.Printf("Uploaded: %s\n", entry.Name())
			}
			successCount++
		}
	}

	fmt.Printf("\nUpload complete: %d succeeded, %d failed\n", successCount, failCount)
	return nil
}

// uploadLivePhoto uploads a live photo as a ZIP file
func uploadLivePhoto(uploader *upload.UploadClient, components *upload.LivePhotoComponents, collectionID int64, collectionKey []byte, verbose bool) error {
	if verbose {
		if components.IsMotion {
			fmt.Printf("Detected Motion Photo: %s\n", components.ImagePath)
		} else {
			fmt.Printf("Detected Live Photo: %s + %s\n", components.ImagePath, components.VideoPath)
		}
	}

	// Create ZIP file containing image and video
	zipData, imageFileName, err := upload.CreateLivePhotoZip(components.ImagePath, components.VideoPath)
	if err != nil {
		return fmt.Errorf("failed to create live photo ZIP: %w", err)
	}

	// Generate file key
	fileKey, err := upload.GenerateKey()
	if err != nil {
		return fmt.Errorf("failed to generate file key: %w", err)
	}

	// Encrypt file key with collection key (not master key!)
	encryptedKey, err := upload.EncryptKey(fileKey, collectionKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt file key: %w", err)
	}

	// Encrypt ZIP data (use chunked encryption for files)
	encryptedZip, zipHeader, err := upload.EncryptFileData(zipData, fileKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt ZIP data: %w", err)
	}

	// Generate thumbnail from image
	thumbnail, err := upload.GetThumbnail(components.ImagePath, upload.FileCategoryImage)
	if err != nil {
		return fmt.Errorf("failed to generate thumbnail: %w", err)
	}

	// Encrypt thumbnail data
	encryptedThumbnail, thumbnailHeader, err := upload.EncryptData(thumbnail, fileKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt thumbnail: %w", err)
	}

	// Build metadata for live photo - use image filename (with original extension like .heic, not .zip)
	metadata := upload.BuildFileMetadata(imageFileName, upload.FileCategoryLivePhoto, time.UnixMicro(components.CreationTime), upload.GetLivePhotoHash(components))
	encryptedMetadata, err := upload.EncryptMetadata(metadata, fileKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt metadata: %w", err)
	}

	// Get upload URLs for ZIP file
	zipMD5 := computeMD5(encryptedZip)
	zipUploadURL, err := uploader.GetUploadURL(int64(len(encryptedZip)), zipMD5)
	if err != nil {
		return fmt.Errorf("failed to get ZIP upload URL: %w", err)
	}

	// Upload ZIP file
	if err := uploader.UploadFile(zipUploadURL.URL, encryptedZip, zipMD5); err != nil {
		return fmt.Errorf("failed to upload ZIP: %w", err)
	}

	// Upload thumbnail
	thumbMD5 := computeMD5(encryptedThumbnail)
	thumbUploadURL, err := uploader.GetUploadURL(int64(len(encryptedThumbnail)), thumbMD5)
	if err != nil {
		return fmt.Errorf("failed to get thumbnail upload URL: %w", err)
	}

	if err := uploader.UploadFile(thumbUploadURL.URL, encryptedThumbnail, thumbMD5); err != nil {
		return fmt.Errorf("failed to upload thumbnail: %w", err)
	}

	// Create file entry for live photo ZIP
	createReq := upload.CreateFileRequest{
		CollectionID:       collectionID,
		EncryptedKey:       encryptedKey.CipherText,
		KeyDecryptionNonce: encryptedKey.Nonce,
		File: upload.FileAttributes{
			ObjectKey:        zipUploadURL.ObjectKey,
			DecryptionHeader: base64.StdEncoding.EncodeToString(zipHeader),
			Size:             int64(len(encryptedZip)),
		},
		Thumbnail: upload.FileAttributes{
			ObjectKey:        thumbUploadURL.ObjectKey,
			DecryptionHeader: base64.StdEncoding.EncodeToString(thumbnailHeader),
			Size:             int64(len(encryptedThumbnail)),
		},
		Metadata:     encryptedMetadata,
		UpdationTime: time.Now().UnixMicro(),
	}

	if verbose {
		fmt.Printf("Creating live photo ZIP entry...\n")
		fmt.Printf("  CollectionID: %d\n", createReq.CollectionID)
		fmt.Printf("  ZIP File ObjectKey: %s\n", createReq.File.ObjectKey)
		fmt.Printf("  Thumbnail ObjectKey: %s\n", createReq.Thumbnail.ObjectKey)
	}

	resp, err := uploader.CreateFile(createReq)
	if err != nil {
		return fmt.Errorf("failed to create live photo: %w", err)
	}

	if verbose {
		fmt.Printf("Live Photo uploaded successfully (ID: %d)\n", resp.ID)
	}

	return nil
}

// computeMD5 computes MD5 hash of data
func computeMD5(data []byte) string {
	h := md5.New()
	h.Write(data)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
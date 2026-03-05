package metasync

import (
	"context"
	"fmt"
	"log"
	"time"
)

// SyncOptions holds configuration for the sync operation
type SyncOptions struct {
	AccountEmail  string
	App           string
	DeviceKey     []byte
	AccountSecret *AccSecretInfo
	DBPath        string
	Verbose       bool
}

// SyncResult contains statistics about the sync operation
type SyncResult struct {
	CollectionsPulled int
	FilesPulled       int
	Errors            []error
	Duration          time.Duration
}

// Sync performs the metadata sync operation
func Sync(ctx context.Context, opts SyncOptions) (*SyncResult, error) {
	startTime := time.Now()
	result := &SyncResult{}

	// Get ente CLI config
	config, err := LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Get ente CLI database path
	cliConfigDir, err := GetEnteCLIConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get CLI config dir: %w", err)
	}
	cliDBPath := fmt.Sprintf("%s/ente-cli.db", cliConfigDir)

	// Load accounts
	accounts, err := LoadAccounts(cliDBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load accounts: %w", err)
	}

	// Find matching account
	var targetAccount *Account
	for i := range accounts {
		if accounts[i].Email == opts.AccountEmail && accounts[i].App == opts.App {
			targetAccount = &accounts[i]
			break
		}
	}
	if targetAccount == nil {
		return nil, fmt.Errorf("account not found: %s (app: %s)", opts.AccountEmail, opts.App)
	}

	// Create API client
	apiConfig := APIConfig{
		BaseURL: config.APIEndpoint,
		Token:   opts.AccountSecret.TokenStr(),
		App:     opts.App,
	}
	apiClient := NewAPIClient(apiConfig)

	// Create database
	db, err := NewDatabase(opts.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}
	defer db.Close()

	if opts.Verbose {
		log.Printf("Starting sync for account: %s", opts.AccountEmail)
		log.Printf("API endpoint: %s", config.APIEndpoint)
	}

	// Get collections (incremental sync)
	lastSyncTime, err := db.GetLastSyncTime(targetAccount.AccountKey())
	if err != nil {
		return nil, fmt.Errorf("failed to get last sync time: %w", err)
	}

	syncType := "incremental"
	if lastSyncTime == 0 {
		syncType = "initial"
	}

	if opts.Verbose {
		log.Printf("Starting %s sync (last sync time: %d)...", syncType, lastSyncTime)
	}
	log.Printf("Fetching collections from server...")

	collections, err := apiClient.GetCollections(ctx, lastSyncTime)
	if err != nil {
		return nil, fmt.Errorf("failed to get collections: %w", err)
	}

	// Count collections from API (these are the "diff" records)
	result.CollectionsPulled = len(collections)
	log.Printf("Fetched %d collection(s) from server", result.CollectionsPulled)

	if opts.Verbose {
		log.Printf("Found %d collections from API (since time: %d)", result.CollectionsPulled, lastSyncTime)
	}

	// Process each collection
	for _, coll := range collections {
		// Skip deleted collections on first sync
		if lastSyncTime == 0 && coll.IsDeleted {
			continue
		}

		// Get collection key first (needed to decrypt name)
		collectionKey, err := GetCollectionKey(coll, opts.AccountSecret.MasterKey, opts.AccountSecret.SecretKey, opts.AccountSecret.PublicKey, targetAccount.UserID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to get collection key for collection %d: %w", coll.ID, err))
			continue
		}

		// Decrypt collection name
		collName, err := DecryptCollectionName(coll, collectionKey)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to decrypt collection %d: %w", coll.ID, err))
			continue
		}

		// Check if shared
		isShared := coll.Owner.ID != targetAccount.UserID

		decryptedColl := DecryptedCollection{
			ID:                 coll.ID,
			OwnerID:            coll.Owner.ID,
			Name:               collName,
			Type:               coll.Type,
			IsShared:           isShared,
			IsDeleted:          coll.IsDeleted,
			UpdatedTime:        time.UnixMicro(coll.UpdatedTime),
			EncryptedKey:       coll.EncryptedKey,
			KeyDecryptionNonce: coll.KeyDecryptionNonce,
		}

		// If collection is deleted, mark as deleted in DB
		if coll.IsDeleted {
			if err := db.MarkCollectionAsDeleted(coll.ID); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("failed to mark collection %d as deleted: %w", coll.ID, err))
			}
			continue
		}

		// Upsert collection
		if err := db.UpsertCollection(decryptedColl); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to upsert collection %s: %w", collName, err))
			continue
		}

		if opts.Verbose {
			log.Printf("Syncing collection: %s (%d)", collName, coll.ID)
		}

		// Get files for this collection (incremental sync)
		fileLastSyncTime := lastSyncTime
		collectionFilesCount := 0
		for {
			files, hasMore, err := apiClient.GetFiles(ctx, coll.ID, fileLastSyncTime)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("failed to get files for collection %s: %w", collName, err))
				break
			}

			// Count files from API (these are the "diff" records)
			result.FilesPulled += len(files)
			collectionFilesCount += len(files)

			// Process files
			for _, file := range files {
				// Update last sync time
				if file.UpdationTime > fileLastSyncTime {
					fileLastSyncTime = file.UpdationTime
				}

				// Check if file is removed from album (deleted)
				// ente uses either IsDeleted=true or File.EncryptedData="-" to mark deleted files
				isRemoved := file.IsDeleted || file.File.EncryptedData == "-"

				// On first sync, skip deleted files (no need to sync delete markers)
				if lastSyncTime == 0 && isRemoved {
					continue
				}

				// For deleted files, mark as deleted in DB
				if isRemoved {
					if err := db.MarkFileAsDeleted(file.ID); err != nil {
						result.Errors = append(result.Errors, fmt.Errorf("failed to mark file %d as deleted: %w", file.ID, err))
					}
					continue
				}

				// Decrypt file metadata
				decryptedFile, err := DecryptFile(file, collectionKey)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Errorf("failed to decrypt file %d: %w", file.ID, err))
					continue
				}

				decryptedFile.CollectionID = coll.ID

				// Upsert file
				if err := db.UpsertFile(coll.ID, *decryptedFile); err != nil {
					result.Errors = append(result.Errors, fmt.Errorf("failed to upsert file %d: %w", file.ID, err))
					continue
				}
			}

			if !hasMore {
				if collectionFilesCount > 0 {
					log.Printf("Fetched %d file(s) from collection \"%s\"", collectionFilesCount, collName)
				}
				break
			}
		}
	}

	// Update sync state
	if err := db.UpdateSyncState(targetAccount.AccountKey(), time.Now().UnixMicro()); err != nil {
		log.Printf("Warning: failed to update sync state: %v", err)
	}

	result.Duration = time.Since(startTime)

	log.Printf("Sync completed: %d collection(s), %d file(s), took %s", result.CollectionsPulled, result.FilesPulled, result.Duration.Round(time.Millisecond))

	return result, nil
}

// ListCollections lists collections for an account
func ListCollections(ctx context.Context, accountEmail, app string, deviceKey []byte, accountSecret *AccSecretInfo) ([]DecryptedCollection, error) {
	// Get ente CLI config
	config, err := LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Get ente CLI database path
	cliConfigDir, err := GetEnteCLIConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get CLI config dir: %w", err)
	}
	cliDBPath := fmt.Sprintf("%s/ente-cli.db", cliConfigDir)

	// Load accounts
	accounts, err := LoadAccounts(cliDBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load accounts: %w", err)
	}

	// Find matching account
	var targetAccount *Account
	for i := range accounts {
		if accounts[i].Email == accountEmail && accounts[i].App == app {
			targetAccount = &accounts[i]
			break
		}
	}
	if targetAccount == nil {
		return nil, fmt.Errorf("account not found: %s (app: %s)", accountEmail, app)
	}

	// Create API client
	apiConfig := APIConfig{
		BaseURL: config.APIEndpoint,
		Token:   accountSecret.TokenStr(),
		App:     app,
	}
	apiClient := NewAPIClient(apiConfig)

	// Get collections
	collections, err := apiClient.GetCollections(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get collections: %w", err)
	}

	var result []DecryptedCollection
	for _, coll := range collections {
		if coll.IsDeleted {
			continue
		}

		// Get collection key first (needed to decrypt name)
		collectionKey, err := GetCollectionKey(coll, accountSecret.MasterKey, accountSecret.SecretKey, accountSecret.PublicKey, targetAccount.UserID)
		if err != nil {
			log.Printf("Warning: failed to get collection key for %d: %v", coll.ID, err)
			continue
		}

		// Decrypt collection name
		collName, err := DecryptCollectionName(coll, collectionKey)
		if err != nil {
			log.Printf("Warning: failed to decrypt collection %d: %v", coll.ID, err)
			collName = fmt.Sprintf("Collection %d", coll.ID)
		}

		// Check if shared
		isShared := coll.Owner.ID != targetAccount.UserID

		result = append(result, DecryptedCollection{
			ID:                 coll.ID,
			OwnerID:            coll.Owner.ID,
			Name:               collName,
			Type:               coll.Type,
			IsShared:           isShared,
			IsDeleted:          coll.IsDeleted,
			UpdatedTime:        time.UnixMicro(coll.UpdatedTime),
			EncryptedKey:       coll.EncryptedKey,
			KeyDecryptionNonce: coll.KeyDecryptionNonce,
		})
	}

	return result, nil
}
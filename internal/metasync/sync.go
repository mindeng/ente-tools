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
	CollectionsSynced int
	FilesSynced       int
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
	lastSyncTime, _ := db.GetLastSyncTime(0) // Using 0 for now
	collections, err := apiClient.GetCollections(ctx, lastSyncTime)
	if err != nil {
		return nil, fmt.Errorf("failed to get collections: %w", err)
	}

	if opts.Verbose {
		log.Printf("Found %d collections from API", len(collections))
	}

	// Process each collection
	for _, coll := range collections {
		// Skip deleted collections on first sync
		if lastSyncTime == 0 && coll.IsDeleted {
			continue
		}

		// Decrypt collection name
		collName, err := DecryptCollectionName(coll, opts.AccountSecret.MasterKey)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to decrypt collection %d: %w", coll.ID, err))
			continue
		}

		decryptedColl := DecryptedCollection{
			ID:         coll.ID,
			OwnerID:    coll.Owner.ID,
			Name:       collName,
			IsShared:   coll.IsShared,
			IsDeleted:  coll.IsDeleted,
			UpdatedTime: time.UnixMicro(coll.UpdatedTime),
		}

		// If collection is deleted, mark as deleted in DB
		if coll.IsDeleted {
			if err := db.MarkCollectionAsDeleted(coll.ID); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("failed to mark collection %d as deleted: %w", coll.ID, err))
			}
			continue
		}

		// Get collection key
		collectionKey, err := GetCollectionKey(coll, opts.AccountSecret.MasterKey, opts.AccountSecret.SecretKey, opts.AccountSecret.PublicKey, targetAccount.UserID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to get collection key for %s: %w", collName, err))
			continue
		}

		// Upsert collection
		if err := db.UpsertCollection(decryptedColl); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to upsert collection %s: %w", collName, err))
			continue
		}
		result.CollectionsSynced++

		if opts.Verbose {
			log.Printf("Syncing collection: %s (%d)", collName, coll.ID)
		}

		// Get files for this collection (incremental sync)
		fileLastSyncTime := lastSyncTime
		for {
			files, hasMore, err := apiClient.GetFiles(ctx, coll.ID, fileLastSyncTime)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("failed to get files for collection %s: %w", collName, err))
				break
			}

			// Process files
			for _, file := range files {
				// Update last sync time
				if file.LastUpdateTime > fileLastSyncTime {
					fileLastSyncTime = file.LastUpdateTime
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
				result.FilesSynced++
			}

			if !hasMore {
				break
			}
		}
	}

	// Update sync state
	if err := db.UpdateSyncState(targetAccount.AccountKey(), time.Now().UnixMicro()); err != nil {
		log.Printf("Warning: failed to update sync state: %v", err)
	}

	result.Duration = time.Since(startTime)

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

		// Decrypt collection name
		collName, err := DecryptCollectionName(coll, accountSecret.MasterKey)
		if err != nil {
			log.Printf("Warning: failed to decrypt collection %d: %v", coll.ID, err)
			collName = fmt.Sprintf("Collection %d", coll.ID)
		}

		result = append(result, DecryptedCollection{
			ID:         coll.ID,
			OwnerID:    coll.Owner.ID,
			Name:       collName,
			IsShared:   coll.IsShared,
			IsDeleted:  coll.IsDeleted,
			UpdatedTime: time.UnixMicro(coll.UpdatedTime),
		})
	}

	return result, nil
}
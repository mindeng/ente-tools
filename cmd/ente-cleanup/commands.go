package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	enteHashcmp "ente-hashcmp/internal/storage"
)

var (
	datacenter string
	prefix     string
	limit      int
	outputFmt  string
)

func init() {
	rootCmd.AddCommand(listOrphanedCmd)
	rootCmd.AddCommand(listDeletedUserObjectsCmd)
	rootCmd.AddCommand(deleteOrphanedCmd)
	rootCmd.AddCommand(statsCmd)

	// Common flags
	rootCmd.PersistentFlags().StringVar(&datacenter, "datacenter", "hot", "S3 datacenter (hot, hot-b2, wasabi, scaleway)")
	rootCmd.PersistentFlags().StringVar(&prefix, "prefix", "", "S3 object key prefix to scan")
	rootCmd.PersistentFlags().IntVar(&limit, "limit", 1000, "maximum number of results")
	rootCmd.PersistentFlags().StringVar(&outputFmt, "output", "table", "output format (table, json, csv)")
}

// listOrphanedCmd lists orphan objects in S3
var listOrphanedCmd = &cobra.Command{
	Use:   "list-orphaned",
	Short: "List orphan objects in S3 storage",
	Long:  `List objects that exist in S3 storage but have no references in the database.`,
	RunE: runListOrphaned,
}

// listDeletedUserObjectsCmd lists objects belonging to deleted users
var listDeletedUserObjectsCmd = &cobra.Command{
	Use:   "list-deleted-user-objects",
	Short: "List objects belonging to deleted users",
	Long:  `List objects whose owner user has been deleted from the database.`,
	RunE: runListDeletedUserObjects,
}

// deleteOrphanedCmd deletes orphan objects
var deleteOrphanedCmd = &cobra.Command{
	Use:   "delete-orphaned",
	Short: "Delete orphan objects from S3 storage",
	Long:  `Delete objects that are either orphan (no database reference) or belong to deleted users.`,
	RunE: runDeleteOrphaned,
}

// statsCmd shows database statistics
var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show database statistics",
	Long:  `Display statistics about the Ente database including user, file, and object counts.`,
	RunE: runStats,
}

// OrphanObjectInfo contains information about an orphan object
type OrphanObjectInfo struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	SizeHuman    string `json:"size_human"`
	Datacenter   string `json:"datacenter"`
	UserID       int64  `json:"user_id"`
	UserEmail    string `json:"user_email"`
	Collection   string `json:"collection"`
	FileType     string `json:"file_type"`
	LastModified string `json:"last_modified"`
}

func runListOrphaned(cmd *cobra.Command, args []string) error {
	// Connect to database
	db, err := connectDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// Initialize S3 client
	s3Cfg, err := enteHashcmp.NewS3Config()
	if err != nil {
		return fmt.Errorf("failed to initialize S3 config: %w", err)
	}

	client := s3Cfg.GetClient(datacenter)
	if client == nil {
		availableDCs := s3Cfg.ListDatacenters()
		return fmt.Errorf("no S3 client available for datacenter '%s'. Available datacenters: %v\n\nMake sure your config file has credentials for the requested datacenter.", datacenter, availableDCs)
	}

	scanner := enteHashcmp.NewScanner(client)
	analyzer := enteHashcmp.NewAnalyzer(db)

	// Load all object keys for fast lookup
	fmt.Printf("Loading database object keys...\n")
	objectKeys, err := analyzer.LoadAllObjectKeys()
	if err != nil {
		return fmt.Errorf("failed to load object keys: %w", err)
	}
	fmt.Printf("Loaded %d object keys from database\n", len(objectKeys))

	// Scan S3 objects
	ctx := context.Background()
	orphanObjects := []OrphanObjectInfo{}
	totalCount := 0
	totalSize := int64(0)

	scanPrefix := prefix
	if scanPrefix == "" {
		scanPrefix = "" // Scan all
	}

	fmt.Printf("Scanning S3 objects with prefix '%s' in datacenter '%s'...\n", scanPrefix, datacenter)

	_, _, err = scanner.ScanPrefix(ctx, scanPrefix, func(obj enteHashcmp.ObjectInfo) error {
		// Check if object is orphan
		if !objectKeys[obj.Key] {
			userID := enteHashcmp.ParseUserIDFromKey(obj.Key)
			email := ""
			collection := ""

			if userID > 0 {
				email, _ = analyzer.GetUserEmail(userID)
			}

			orphan := OrphanObjectInfo{
				Key:        obj.Key,
				Size:       obj.Size,
				SizeHuman:  formatBytes(obj.Size),
				Datacenter: obj.Datacenter,
				UserID:     userID,
				UserEmail:  email,
				Collection: collection,
				FileType:   enteHashcmp.GetFileTypeFromKey(obj.Key),
			}

			orphanObjects = append(orphanObjects, orphan)
			totalSize += obj.Size
		}

		totalCount++
		if totalCount%1000 == 0 {
			fmt.Printf("\rScanned: %d objects, found %d orphans", totalCount, len(orphanObjects))
		}

		if len(orphanObjects) >= limit {
			return fmt.Errorf("limit reached")
		}

		return nil
	})

	fmt.Printf("\rScanned: %d objects, found %d orphans\n", totalCount, len(orphanObjects))

	if err != nil && err.Error() != "limit reached" {
		return fmt.Errorf("scan error: %w", err)
	}

	// Output results
	outputOrphanObjects(orphanObjects, totalSize, len(orphanObjects) >= limit)

	return nil
}

func runListDeletedUserObjects(cmd *cobra.Command, args []string) error {
	// Connect to database
	db, err := connectDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// Initialize S3 client
	s3Cfg, err := enteHashcmp.NewS3Config()
	if err != nil {
		return fmt.Errorf("failed to initialize S3 config: %w", err)
	}

	client := s3Cfg.GetClient(datacenter)
	if client == nil {
		availableDCs := s3Cfg.ListDatacenters()
		return fmt.Errorf("no S3 client available for datacenter '%s'. Available datacenters: %v\n\nMake sure your config file has credentials for the requested datacenter.", datacenter, availableDCs)
	}

	scanner := enteHashcmp.NewScanner(client)
	analyzer := enteHashcmp.NewAnalyzer(db)

	// Load existing user IDs
	fmt.Printf("Loading user IDs...\n")
	userIDs, err := analyzer.LoadExistingUserIDs()
	if err != nil {
		return fmt.Errorf("failed to load user IDs: %w", err)
	}
	fmt.Printf("Loaded %d user IDs from database\n", len(userIDs))

	// Scan S3 objects
	ctx := context.Background()
	deletedUserObjects := []OrphanObjectInfo{}
	totalCount := 0
	totalSize := int64(0)

	scanPrefix := prefix

	fmt.Printf("Scanning S3 objects with prefix '%s' in datacenter '%s'...\n", scanPrefix, datacenter)

	_, _, err = scanner.ScanPrefix(ctx, scanPrefix, func(obj enteHashcmp.ObjectInfo) error {
		userID := enteHashcmp.ParseUserIDFromKey(obj.Key)

		if userID > 0 && !userIDs[userID] {
			// User is deleted
			orphan := OrphanObjectInfo{
				Key:        obj.Key,
				Size:       obj.Size,
				SizeHuman:  formatBytes(obj.Size),
				Datacenter: obj.Datacenter,
				UserID:     userID,
				UserEmail:  "(deleted)",
				Collection: "(unknown)",
				FileType:   enteHashcmp.GetFileTypeFromKey(obj.Key),
			}

			deletedUserObjects = append(deletedUserObjects, orphan)
			totalSize += obj.Size
		}

		totalCount++
		if totalCount%1000 == 0 {
			fmt.Printf("\rScanned: %d objects, found %d deleted user objects", totalCount, len(deletedUserObjects))
		}

		if len(deletedUserObjects) >= limit {
			return fmt.Errorf("limit reached")
		}

		return nil
	})

	fmt.Printf("\rScanned: %d objects, found %d deleted user objects\n", totalCount, len(deletedUserObjects))

	if err != nil && err.Error() != "limit reached" {
		return fmt.Errorf("scan error: %w", err)
	}

	// Output results
	outputOrphanObjects(deletedUserObjects, totalSize, len(deletedUserObjects) >= limit)

	return nil
}

func runDeleteOrphaned(cmd *cobra.Command, args []string) error {
	// Connect to database
	db, err := connectDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// Initialize S3 client
	s3Cfg, err := enteHashcmp.NewS3Config()
	if err != nil {
		return fmt.Errorf("failed to initialize S3 config: %w", err)
	}

	client := s3Cfg.GetClient(datacenter)
	if client == nil {
		availableDCs := s3Cfg.ListDatacenters()
		return fmt.Errorf("no S3 client available for datacenter '%s'. Available datacenters: %v\n\nMake sure your config file has credentials for the requested datacenter.", datacenter, availableDCs)
	}

	scanner := enteHashcmp.NewScanner(client)
	analyzer := enteHashcmp.NewAnalyzer(db)

	// Load data
	fmt.Printf("Loading database data...\n")
	objectKeys, err := analyzer.LoadAllObjectKeys()
	if err != nil {
		return fmt.Errorf("failed to load object keys: %w", err)
	}

	userIDs, err := analyzer.LoadExistingUserIDs()
	if err != nil {
		return fmt.Errorf("failed to load user IDs: %w", err)
	}
	fmt.Printf("Loaded %d object keys and %d user IDs\n", len(objectKeys), len(userIDs))

	// Scan and identify orphan objects
	ctx := context.Background()
	orphanKeys := []string{}
	totalSize := int64(0)

	fmt.Printf("Scanning S3 objects...\n")

	_, _, err = scanner.ScanPrefix(ctx, prefix, func(obj enteHashcmp.ObjectInfo) error {
		userID := enteHashcmp.ParseUserIDFromKey(obj.Key)
		isOrphan := !objectKeys[obj.Key]
		userDeleted := userID > 0 && !userIDs[userID]

		if isOrphan || userDeleted {
			orphanKeys = append(orphanKeys, obj.Key)
			totalSize += obj.Size
		}

		fmt.Printf("\rFound %d orphan objects (size: %s)", len(orphanKeys), formatBytes(totalSize))

		return nil
	})

	fmt.Printf("\nFound %d orphan objects (total size: %s)\n", len(orphanKeys), formatBytes(totalSize))

	if err != nil {
		return fmt.Errorf("scan error: %w", err)
	}

	if len(orphanKeys) == 0 {
		fmt.Println("No orphan objects found.")
		return nil
	}

	// Show preview
	fmt.Println("\nPreview (first 10):")
	for i, key := range orphanKeys {
		if i >= 10 {
			break
		}
		fmt.Printf("  %s\n", key)
	}

	// Dry run mode
	if dryRun {
		fmt.Println("\n[DRY RUN] Skipping deletion. Use without --dry-run to actually delete.")
		return nil
	}

	// Confirm deletion
	fmt.Printf("\nConfirm deletion of %d objects? Type 'yes' to confirm: ", len(orphanKeys))
	var confirmation string
	fmt.Scanln(&confirmation)

	if strings.ToLower(confirmation) != "yes" {
		fmt.Println("Deletion cancelled.")
		return nil
	}

	// Delete objects
	fmt.Println("\nDeleting objects...")
	failed, err := scanner.DeleteObjectsBatch(ctx, orphanKeys)
	if err != nil {
		return fmt.Errorf("failed to delete objects: %w", err)
	}

	successCount := len(orphanKeys) - len(failed)
	fmt.Printf("\nDeleted: %d objects\n", successCount)
	if len(failed) > 0 {
		fmt.Printf("Failed: %d objects\n", len(failed))
		for _, key := range failed {
			fmt.Printf("  - %s\n", key)
		}
	}
	fmt.Printf("Space freed: %s\n", formatBytes(totalSize))

	return nil
}

func runStats(cmd *cobra.Command, args []string) error {
	// Connect to database
	db, err := connectDB()
	if err != nil {
		return err
	}
	defer db.Close()

	analyzer := enteHashcmp.NewAnalyzer(db)

	stats, err := analyzer.GetDatabaseStats()
	if err != nil {
		return fmt.Errorf("failed to get database stats: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Metric\tCount\n")
	fmt.Fprintf(w, "-------\t-------\n")
	fmt.Fprintf(w, "Users\t%d\n", stats.TotalUsers)
	fmt.Fprintf(w, "Files\t%d\n", stats.TotalFiles)
	fmt.Fprintf(w, "Object Keys\t%d\n", stats.TotalObjectKeys)
	fmt.Fprintf(w, "File Data\t%d\n", stats.TotalFileData)
	w.Flush()

	return nil
}

func outputOrphanObjects(objects []OrphanObjectInfo, totalSize int64, limitReached bool) {
	if len(objects) == 0 {
		fmt.Println("\nNo orphan objects found.")
		return
	}

	totalSizeHuman := formatBytes(totalSize)
	fmt.Printf("\n=== Orphan Objects (%d) ===\n", len(objects))
	fmt.Printf("Total size: %s\n", totalSizeHuman)

	if limitReached {
		fmt.Printf("(showing first %d, use --limit to see more)\n", limit)
	}

	fmt.Println()

	switch outputFmt {
	case "json":
		data, err := json.MarshalIndent(struct {
			Count  int64            `json:"count"`
			Total  string           `json:"total_size"`
			Limit  bool             `json:"limit_reached"`
			Objects []OrphanObjectInfo `json:"objects"`
		}{
			Count:  int64(len(objects)),
			Total:  totalSizeHuman,
			Limit:  limitReached,
			Objects: objects,
		}, "", "  ")
		if err != nil {
			fmt.Printf("Error encoding JSON: %v\n", err)
			return
		}
		fmt.Println(string(data))

	case "csv":
		w := csv.NewWriter(os.Stdout)
		w.Write([]string{"Key", "Size", "Datacenter", "User ID", "User Email", "Collection", "File Type"})
		for _, obj := range objects {
			w.Write([]string{obj.Key, obj.SizeHuman, obj.Datacenter, fmt.Sprintf("%d", obj.UserID), obj.UserEmail, obj.Collection, obj.FileType})
		}
		w.Flush()

	case "table":
		fallthrough
	default:
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "Key\tSize\tDatacenter\tUser ID\tUser Email\tCollection\tFile Type\n")
		fmt.Fprintf(w, "----\t----\t----------\t--------\t----------\t-----------\t----------\n")
		for _, obj := range objects {
			// Truncate key if too long
			key := obj.Key
			if len(key) > 50 {
				key = key[:47] + "..."
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
				key, obj.SizeHuman, obj.Datacenter, obj.UserID, obj.UserEmail, obj.Collection, obj.FileType)
		}
		w.Flush()
	}
}

// connectDB connects to the PostgreSQL database
func connectDB() (*sql.DB, error) {
	host := viper.GetString("database.host")
	port := viper.GetInt("database.port")
	dbname := viper.GetString("database.database")
	user := viper.GetString("database.user")
	password := viper.GetString("database.password")
	sslmode := viper.GetString("database.sslmode")

	if sslmode == "" {
		sslmode = "disable"
	}

	connStr := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		host, port, dbname, user, password, sslmode)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

// formatBytes formats a byte size into human-readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB",
		float64(bytes)/float64(div), "KMGTPE"[exp])
}
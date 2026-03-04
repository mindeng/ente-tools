package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"ente-hashcmp/internal/comparator"
	"ente-hashcmp/internal/database"
	"ente-hashcmp/internal/hash"
	"ente-hashcmp/internal/livephoto"
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

var diffCmd = &cobra.Command{
	Use:   "diff <dir1> <dir2>",
	Short: "Compare two directories' hash sets",
	Args:  cobra.ExactArgs(2),
	Run:   runDiff,
}

var hashCmd = &cobra.Command{
	Use:   "hash <file>",
	Short: "Compute hash of a single file",
	Args:  cobra.ExactArgs(1),
	Run:   runHash,
}

var dbpathCmd = &cobra.Command{
	Use:   "dbpath <dir>",
	Short: "Get the database file path for a directory",
	Args:  cobra.ExactArgs(1),
	Run:   runDBPath,
}

func init() {
	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(hashCmd)
	rootCmd.AddCommand(dbpathCmd)
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

	// Set up progress callback - use stderr and overwrite the same line
	scanner.SetProgressCallback(func(stats *types.ScanStats, currentPath string) {
		// Use \r to go to start of line, \033[K to clear to end of line
		fmt.Fprintf(os.Stderr, "\r\033[KProcessing: %s | Files: %d | Updated: %d | Skipped: %d | Live Photos: %d",
			currentPath, stats.TotalFiles, stats.UpdatedFiles, stats.SkippedFiles, stats.LivePhotos)
	})

	// Scan directory
	stats, err := scanner.Scan()
	if err != nil {
		// Clear the progress line before printing error
		fmt.Printf("\r\033[K")
		fmt.Fprintf(os.Stderr, "Error scanning directory: %v\n", err)
		os.Exit(1)
	}

	// Clear the progress line before printing final results
	fmt.Printf("\r\033[K")

	// Print results
	fmt.Print(comparator.FormatScanStats(stats, scanner.GetDBPath()))
}

func runDiff(cmd *cobra.Command, args []string) {
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

// Export fileType for the hash command
var FileType = types.FileTypeImage
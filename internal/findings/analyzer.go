package findings

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mindeng/ente-tools/internal/database"
	"github.com/mindeng/ente-tools/internal/metasync"
)

// AnalyzeOptions holds configuration for the analysis
type AnalyzeOptions struct {
	Dir        string
	MetaDBPath string
	Verbose    bool
}

// AnalyzeMissing analyzes a directory to find files not in the ente library
// Uses database comparison instead of walking the directory
func AnalyzeMissing(opts AnalyzeOptions) (*MissingResult, error) {
	startTime := time.Now()

	// Open local database to get cached hashes
	localDB, err := database.Open(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open local database: %w", err)
	}
	defer localDB.Close()

	// Get all file entries from local database
	localFiles, err := localDB.GetAllFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to get local file hashes: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("Found %d files in local database\n", len(localFiles))
	}

	// Open metasync database to get ente file hashes
	metaDB, err := metasync.NewDatabase(opts.MetaDBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open metasync database: %w", err)
	}
	defer metaDB.Close()

	// Get all hashes from ente library
	enteHashes, err := metaDB.GetAllHashes()
	if err != nil {
		return nil, fmt.Errorf("failed to get ente hashes: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("Found %d files in ente library\n", len(enteHashes))
	}

	// Build a map of Live Photo component paths -> combined hash
	// This helps us check if a component's Live Photo is in ente
	componentToLivePhotoHash := make(map[string]string)
	for relPath, record := range localFiles {
		if record.LivePhotoParts != nil {
			// This record represents a Live Photo
			// Map both components to the combined hash
			componentToLivePhotoHash[relPath] = record.Hash
			if record.LivePhotoParts.Video != "" {
				componentToLivePhotoHash[record.LivePhotoParts.Video] = record.Hash
			}
		}
	}

	// Compare and find missing files
	result := &MissingResult{
		MissingFiles: []MissingFile{},
	}

	for relPath, record := range localFiles {
		// First, check if the file's hash is in ente
		if enteHashes[record.Hash] {
			continue // File exists in Ente
		}

		// Check if this file is part of a Live Photo that exists in ente
		if livePhotoHash, ok := componentToLivePhotoHash[relPath]; ok {
			if enteHashes[livePhotoHash] {
				continue // Live Photo exists in Ente, skip this component
			}
		}

		// File is missing from ente
		missingFile := MissingFile{
			Path:    relPath,
			Hash:    record.Hash,
			Size:    record.Size,
			ModTime: record.ModTime,
		}

		// If this is a Live Photo image and the video is also missing, include both
		if record.LivePhotoParts != nil && record.LivePhotoParts.Video != "" {
			videoRecord, ok := localFiles[record.LivePhotoParts.Video]
			if ok {
				// Check if video is also missing
				if !enteHashes[videoRecord.Hash] {
					missingFile.AdditionalPaths = []string{record.LivePhotoParts.Video}
					missingFile.AdditionalInfo = []MissingFileInfo{
						{
							Path:    record.LivePhotoParts.Video,
							Hash:    videoRecord.Hash,
							Size:    videoRecord.Size,
							ModTime: videoRecord.ModTime,
						},
					}
				}
			}
		}

		result.MissingFiles = append(result.MissingFiles, missingFile)
	}

	result.TotalFiles = len(localFiles)
	result.FoundInEnte = result.TotalFiles - len(result.MissingFiles)
	result.Duration = time.Since(startTime)

	return result, nil
}

// StreamCopyOptions holds configuration for streaming copy operation
type StreamCopyOptions struct {
	SourceDir  string
	TargetDir  string
	MetaDBPath string
	Verbose    bool
	DryRun     bool
	Workers    int // Number of parallel workers
}

// StreamCopyResult contains statistics about the streaming copy operation
type StreamCopyResult struct {
	TotalFiles   int
	ScannedFiles int
	CopiedFiles  int
	SkippedFiles int
	FailedFiles  []FailedCopy
	Duration     time.Duration
}

// StreamCopyMissingFiles analyzes and copies missing files using database comparison
func StreamCopyMissingFiles(opts StreamCopyOptions) (*StreamCopyResult, error) {
	startTime := time.Now()

	if opts.Workers <= 0 {
		opts.Workers = 4 // Default to 4 workers
	}

	// Open local database to get cached hashes
	localDB, err := database.Open(opts.SourceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open local database: %w", err)
	}
	defer localDB.Close()

	// Get all file entries from local database
	localFiles, err := localDB.GetAllFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to get local file hashes: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("Found %d files in local database\n", len(localFiles))
	}

	// Open metasync database to get ente file hashes
	metaDB, err := metasync.NewDatabase(opts.MetaDBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open metasync database: %w", err)
	}
	defer metaDB.Close()

	// Get all hashes from ente library
	enteHashes, err := metaDB.GetAllHashes()
	if err != nil {
		return nil, fmt.Errorf("failed to get ente hashes: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("Found %d files in ente library\n", len(enteHashes))
	}

	// Build a map of Live Photo component paths -> combined hash
	componentToLivePhotoHash := make(map[string]string)
	for relPath, record := range localFiles {
		if record.LivePhotoParts != nil {
			componentToLivePhotoHash[relPath] = record.Hash
			if record.LivePhotoParts.Video != "" {
				componentToLivePhotoHash[record.LivePhotoParts.Video] = record.Hash
			}
		}
	}

	// Find missing files
	var missingFiles []MissingFile
	for relPath, record := range localFiles {
		// Check if the file's hash is in ente
		if enteHashes[record.Hash] {
			continue
		}

		// Check if this file is part of a Live Photo that exists in ente
		if livePhotoHash, ok := componentToLivePhotoHash[relPath]; ok {
			if enteHashes[livePhotoHash] {
				continue
			}
		}

		// File is missing from ente
		missingFile := MissingFile{
			Path:    relPath,
			Hash:    record.Hash,
			Size:    record.Size,
			ModTime: record.ModTime,
		}

		// If this is a Live Photo image and the video is also missing, include both
		if record.LivePhotoParts != nil && record.LivePhotoParts.Video != "" {
			videoRecord, ok := localFiles[record.LivePhotoParts.Video]
			if ok {
				if !enteHashes[videoRecord.Hash] {
					missingFile.AdditionalPaths = []string{record.LivePhotoParts.Video}
					missingFile.AdditionalInfo = []MissingFileInfo{
						{
							Path:    record.LivePhotoParts.Video,
							Hash:    videoRecord.Hash,
							Size:    videoRecord.Size,
							ModTime: videoRecord.ModTime,
						},
					}
				}
			}
		}

		missingFiles = append(missingFiles, missingFile)
	}

	// Channels for streaming
	missingFilesCh := make(chan MissingFile, 100)
	copyResultsCh := make(chan CopyOperation, 100)

	// Use wait groups to coordinate goroutines
	var copyWG sync.WaitGroup

	result := &StreamCopyResult{
		FailedFiles: []FailedCopy{},
	}

	// Progress tracking
	var totalCopied int64
	var totalSkipped int64
	var processedCount int64
	var mu sync.Mutex

	// Start copy workers
	for i := 0; i < opts.Workers; i++ {
		copyWG.Add(1)
		go func() {
			defer copyWG.Done()
			copyWorker(opts, missingFilesCh, copyResultsCh)
		}()
	}

	// Start result collector
	var collectorWG sync.WaitGroup
	collectorWG.Add(1)
	go func() {
		defer collectorWG.Done()
		for op := range copyResultsCh {
			mu.Lock()
			result.TotalFiles++
			switch op.Status {
			case CopyStatusCopied:
				result.CopiedFiles++
				totalCopied++
				if opts.Verbose {
					fmt.Printf("\rProcessed: %d | Copied: %d | Skipped: %d | Failed: %d | File: %s",
						processedCount, totalCopied, totalSkipped, len(result.FailedFiles), op.Path)
				}
			case CopyStatusSkipped:
				result.SkippedFiles++
				totalSkipped++
			case CopyStatusFailed:
				result.FailedFiles = append(result.FailedFiles, FailedCopy{
					Path: op.Path,
					Err:  op.Err,
				})
			}
			mu.Unlock()
		}
	}()

	// Send missing files to channel
	for _, mf := range missingFiles {
		missingFilesCh <- mf
		atomic.AddInt64(&processedCount, 1)
	}
	close(missingFilesCh)

	// Wait for copy workers to finish
	copyWG.Wait()

	// Close results channel
	close(copyResultsCh)

	// Wait for collector to finish
	collectorWG.Wait()

	if opts.Verbose {
		fmt.Println() // New line after progress
	}

	result.ScannedFiles = len(localFiles)
	result.Duration = time.Since(startTime)
	return result, nil
}

// CopyOperation represents a single copy operation result
type CopyOperation struct {
	Path   string
	Status CopyStatus
	Err    error
}

// CopyStatus represents the status of a copy operation
type CopyStatus int

const (
	CopyStatusCopied CopyStatus = iota
	CopyStatusSkipped
	CopyStatusFailed
)

// copyWorker processes missing files from the channel and copies them
func copyWorker(opts StreamCopyOptions, ch <-chan MissingFile, results chan<- CopyOperation) {
	for mf := range ch {
		// Copy main file
		op := copySingleFile(opts, mf.Path, mf)
		results <- op

		// Copy additional files (e.g., video component of Live Photo)
		for i, addPath := range mf.AdditionalPaths {
			if i < len(mf.AdditionalInfo) {
				// Use the additional info for metadata
				addInfo := MissingFile{
					Path:    addPath,
					Hash:    mf.AdditionalInfo[i].Hash,
					Size:    mf.AdditionalInfo[i].Size,
					ModTime: mf.AdditionalInfo[i].ModTime,
				}
				results <- copySingleFile(opts, addPath, addInfo)
			}
		}
	}
}

// copySingleFile copies a single file with atomic write semantics
func copySingleFile(opts StreamCopyOptions, mfPath string, mf MissingFile) CopyOperation {
	srcPath := filepath.Join(opts.SourceDir, mfPath)
	dstPath := filepath.Join(opts.TargetDir, mfPath)

	// Check if source file exists and get info
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return CopyOperation{
			Path:   mfPath,
			Status: CopyStatusFailed,
			Err:    fmt.Errorf("source file not accessible: %w", err),
		}
	}

	// Ensure target directory exists
	dstDir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return CopyOperation{
			Path:   mfPath,
			Status: CopyStatusFailed,
			Err:    fmt.Errorf("failed to create target directory: %w", err),
		}
	}

	// Check if destination already exists and can be skipped
	if dstInfo, err := os.Stat(dstPath); err == nil {
		// Skip if file exists with same size and mtime >= source
		// This indicates the file was previously copied successfully
		if dstInfo.Size() == srcInfo.Size() && !dstInfo.ModTime().Before(srcInfo.ModTime()) {
			return CopyOperation{
				Path:   mfPath,
				Status: CopyStatusSkipped,
			}
		}
		// File exists but differs - will be overwritten
	}

	if opts.DryRun {
		return CopyOperation{
			Path:   mfPath,
			Status: CopyStatusCopied,
		}
	}

	// Copy file with atomic write
	if err := copyFileContent(srcPath, dstPath); err != nil {
		return CopyOperation{
			Path:   mfPath,
			Status: CopyStatusFailed,
			Err:    fmt.Errorf("copy failed: %w", err),
		}
	}

	// Verify the copied file size matches source
	dstInfo, err := os.Stat(dstPath)
	if err != nil {
		return CopyOperation{
			Path:   mfPath,
			Status: CopyStatusFailed,
			Err:    fmt.Errorf("failed to verify destination file: %w", err),
		}
	}
	if dstInfo.Size() != srcInfo.Size() {
		os.Remove(dstPath) // Clean up incomplete file
		return CopyOperation{
			Path:   mfPath,
			Status: CopyStatusFailed,
			Err:    fmt.Errorf("size mismatch after copy: source=%d, destination=%d", srcInfo.Size(), dstInfo.Size()),
		}
	}

	// Set modification time to match source file
	// This ensures the skip check works correctly on subsequent runs
	if err := os.Chtimes(dstPath, srcInfo.ModTime(), srcInfo.ModTime()); err != nil {
		// Log warning but don't fail the copy
		// Modification time is less critical than file content
	}

	return CopyOperation{
		Path:   mfPath,
		Status: CopyStatusCopied,
	}
}

// copyFileContent copies a file from src to dst using atomic write pattern
// It writes to a temporary file first, then renames to ensure atomicity
func copyFileContent(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	// Get file info for mode
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to get source file info: %w", err)
	}

	// Create temporary file in the same directory as destination
	// This ensures the temp file is on the same filesystem for atomic rename
	dstDir := filepath.Dir(dst)
	tmpFile, err := os.CreateTemp(dstDir, ".copy-tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Cleanup function to remove temp file on failure
	cleanupTemp := func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}

	// Copy content
	_, err = tmpFile.ReadFrom(srcFile)
	if err != nil {
		cleanupTemp()
		return fmt.Errorf("failed to copy content: %w", err)
	}

	// Sync to ensure data is written to disk
	if err := tmpFile.Sync(); err != nil {
		cleanupTemp()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	// Set file mode before closing
	if err := tmpFile.Chmod(srcInfo.Mode()); err != nil {
		cleanupTemp()
		return fmt.Errorf("failed to set file mode: %w", err)
	}

	// Close the temp file before rename
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename - this is the critical atomic operation
	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file to destination: %w", err)
	}

	return nil
}
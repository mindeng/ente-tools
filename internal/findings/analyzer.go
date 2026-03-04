package findings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ente-hashcmp/internal/database"
	"ente-hashcmp/internal/hash"
	"ente-hashcmp/internal/livephoto"
	"ente-hashcmp/internal/metasync"
	"ente-hashcmp/internal/types"
)

// AnalyzeOptions holds configuration for the analysis
type AnalyzeOptions struct {
	Dir        string
	MetaDBPath string
	Verbose    bool
}

// AnalyzeMissing analyzes a directory to find files not in the ente library
func AnalyzeMissing(opts AnalyzeOptions) (*MissingResult, error) {
	return AnalyzeMissingWithCallback(opts, nil)
}

// AnalyzeMissingWithCallback analyzes a directory and calls the callback for each missing file
// When callback is nil, results are collected into MissingFiles slice
func AnalyzeMissingWithCallback(opts AnalyzeOptions, callback func(MissingFile)) (*MissingResult, error) {
	startTime := time.Now()

	// Open metasync database to get ente file hashes
	metaDB, err := metasync.NewDatabase(opts.MetaDBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open metasync database: %w", err)
	}
	defer metaDB.Close()

	// Get all hashes from ente library (only hash comparison, ignoring filenames)
	enteHashes, err := metaDB.GetAllHashes()
	if err != nil {
		return nil, fmt.Errorf("failed to get ente hashes: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("Found %d files in ente library\n", len(enteHashes))
	}

	// Use the unified streaming scan function
	result := &MissingResult{
		MissingFiles: []MissingFile{},
	}

	var scannedCount int64
	var missingCount int64

	// Progress callback for streaming output
	var lastProgress int64
	progressCallback := func(relPath string, isMissing bool) {
		if !opts.Verbose {
			return
		}
		// Only update progress periodically to avoid flickering
		if atomic.LoadInt64(&scannedCount)-lastProgress >= 10 || isMissing {
			lastProgress = atomic.LoadInt64(&scannedCount)
			if isMissing {
				fmt.Printf("\rScanned: %d | Missing: %d | File: %s", lastProgress, missingCount, relPath)
			} else {
				fmt.Printf("\rScanned: %d | Missing: %d", lastProgress, missingCount)
			}
		}
	}

	err = streamScanAndCompare(opts.Dir, enteHashes, func(mf MissingFile) {
		atomic.AddInt64(&missingCount, 1)
		progressCallback(mf.Path, true)

		// Call user callback if provided
		if callback != nil {
			callback(mf)
		} else {
			// Collect into slice
			result.MissingFiles = append(result.MissingFiles, mf)
		}
	}, &scannedCount, progressCallback, opts.Verbose)

	result.TotalFiles = int(scannedCount)
	result.FoundInEnte = result.TotalFiles - len(result.MissingFiles)
	result.Duration = time.Since(startTime)

	if opts.Verbose {
		fmt.Println() // New line after progress
	}

	return result, err
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

// StreamCopyMissingFiles analyzes and copies missing files in a streaming fashion
// This uses channels to parallelize the scan and copy operations
func StreamCopyMissingFiles(opts StreamCopyOptions) (*StreamCopyResult, error) {
	startTime := time.Now()

	if opts.Workers <= 0 {
		opts.Workers = 4 // Default to 4 workers
	}

	// Open metasync database to get ente file hashes
	metaDB, err := metasync.NewDatabase(opts.MetaDBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open metasync database: %w", err)
	}
	defer metaDB.Close()

	// Get all hashes from ente library (only hash comparison, ignoring filenames)
	enteHashes, err := metaDB.GetAllHashes()
	if err != nil {
		return nil, fmt.Errorf("failed to get ente hashes: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("Found %d files in ente library\n", len(enteHashes))
	}

	// Channels for streaming
	missingFilesCh := make(chan MissingFile, 100) // Buffered channel for missing files
	copyResultsCh := make(chan CopyOperation, 100)
	errorCh := make(chan error, 1)

	// Use wait groups to coordinate goroutines
	var scanWG, copyWG sync.WaitGroup

	result := &StreamCopyResult{
		FailedFiles: []FailedCopy{},
	}

	// Progress tracking
	var totalCopied int64
	var totalSkipped int64
	var scannedCount int64
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
					fmt.Printf("\rScanned: %d | Copied: %d | Skipped: %d | Failed: %d | File: %s",
						scannedCount, totalCopied, totalSkipped, len(result.FailedFiles), op.Path)
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

	// Start scan with callback - this is where true streaming happens
	scanWG.Add(1)
	go func() {
		defer scanWG.Done()
		defer close(missingFilesCh)

		err := streamScanAndCompare(opts.SourceDir, enteHashes, func(mf MissingFile) {
			missingFilesCh <- mf
		}, &scannedCount, nil, opts.Verbose)
		if err != nil {
			select {
			case errorCh <- err:
			default:
			}
		}
	}()

	// Wait for scan to complete (this closes missingFilesCh)
	scanWG.Wait()

	// Wait for copy workers to finish processing all files
	copyWG.Wait()

	// Close results channel (all copies done, no more results will come)
	close(copyResultsCh)

	// Wait for collector to finish (results channel closed)
	collectorWG.Wait()

	// Check for errors
	select {
	case err := <-errorCh:
		return nil, err
	default:
	}

	if opts.Verbose {
		fmt.Println() // New line after progress
	}

	result.ScannedFiles = int(scannedCount)
	result.Duration = time.Since(startTime)
	return result, nil
}

// streamScanAndCompare scans files and immediately compares against ente hashes
// This enables true streaming where files are sent to the copy channel as soon as they're identified as missing
func streamScanAndCompare(dir string, enteHashes map[string]bool, missingCallback func(MissingFile), scannedCount *int64, progressCallback func(relPath string, isMissing bool), verbose bool) error {
	// Open the scanner's database to get cached hashes
	db, err := database.Open(dir)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Track processed Live Photo base names to avoid duplicates
	processedLivePhotos := make(map[string]bool)

	// Scan directory recursively
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Skip hidden files
		if strings.HasPrefix(filepath.Base(path), ".") {
			return nil
		}

		// Check if this is a supported file type
		ft := livephoto.GetFileType(filepath.Base(path))
		if ft == types.FileTypeUnknown {
			return nil
		}

		// Increment scanned count
		atomic.AddInt64(scannedCount, 1)

		// Skip video components of Live Photos - they'll be handled with the image
		baseName := filepath.Base(path)
		baseWithoutExt := baseName[:len(baseName)-len(filepath.Ext(baseName))]
		if processedLivePhotos[baseWithoutExt] {
			// Skip this file as it was already processed as part of a Live Photo
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		// Get or compute hash for this file
		fileHash, _, err := getOrComputeHash(db, path, info, relPath)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: failed to compute hash for %s: %v\n", relPath, err)
			}
			return nil // Continue scanning other files
		}

		// Check if hash exists in ente library
		if enteHashes[fileHash] {
			return nil // File exists in Ente, not missing
		}

		// File is missing - check if it's part of a Live Photo
		missingFile := MissingFile{
			Path:    relPath,
			Hash:    fileHash,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}

		// Check if this is a Live Photo
		if ft == types.FileTypeImage {
			// Check for matching video file
			videoPath := findMatchingVideo(dir, path)
			if videoPath != "" {
				// Get video info
				videoInfo, err := os.Stat(videoPath)
				if err == nil {
					videoRelPath, ok := getRelativePath(videoPath, dir)
					if ok {
						// Get or compute video hash
						videoHash, _, err := getOrComputeHash(db, videoPath, videoInfo, videoRelPath)
						if err == nil {
							// Check if video is also missing
							if !enteHashes[videoHash] {
								// This is a Live Photo with both files missing
								missingFile.AdditionalPaths = []string{videoRelPath}
								missingFile.AdditionalInfo = []MissingFileInfo{
									{
										Path:    videoRelPath,
										Hash:    videoHash,
										Size:    videoInfo.Size(),
										ModTime: videoInfo.ModTime(),
									},
								}
								// Mark base name as processed to skip video iteration
								processedLivePhotos[baseWithoutExt] = true
								if verbose {
									fmt.Printf("Found missing Live Photo: %s + %s\n", relPath, videoRelPath)
								}
							}
						}
					}
				}
			}
		}

		// Send missing file to callback immediately
		if missingCallback != nil {
			missingCallback(missingFile)
		}

		return nil
	})

	return err
}

// findMatchingVideo finds a matching video file for a Live Photo
func findMatchingVideo(dir, imagePath string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		ft := livephoto.GetFileType(filename)
		if ft == types.FileTypeVideo {
			// Check if this video matches the image (Live Photo)
			if livephoto.MatchLivePhoto(filepath.Join(dir, imagePath), filepath.Join(dir, filename)) {
				return filepath.Join(dir, filename)
			}
		}
	}

	return ""
}

// getRelativePath converts an absolute path to a relative path from baseDir
func getRelativePath(absPath, baseDir string) (string, bool) {
	rel, err := filepath.Rel(baseDir, absPath)
	if err != nil {
		return "", false
	}
	return rel, true
}

// getOrComputeHash gets cached hash or computes a new one
func getOrComputeHash(db *database.DB, absPath string, info os.FileInfo, relPath string) (string, bool, error) {
	// Check if we need to recalculate
	needsRecalc, err := db.NeedsRecalc(relPath, info.Size(), info.ModTime())
	if err != nil {
		return "", false, err
	}

	if !needsRecalc {
		// Try to get cached hash
		record, err := db.GetFile(relPath)
		if err == nil && record != nil {
			return record.Hash, false, nil
		}
	}

	// Compute new hash
	file, err := os.Open(absPath)
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	fileHash, err := hash.ComputeHash(file)
	if err != nil {
		return "", false, err
	}

	// Store in database
	record := &types.FileRecord{
		Hash:    fileHash,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}

	if err := db.PutFile(relPath, record); err != nil {
		return fileHash, true, err // Return computed hash even if storage fails
	}

	return fileHash, true, nil
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

// copySingleFile copies a single file
func copySingleFile(opts StreamCopyOptions, relPath string, mf MissingFile) CopyOperation {
	srcPath := filepath.Join(opts.SourceDir, mf.Path)
	dstPath := filepath.Join(opts.TargetDir, mf.Path)

	// Ensure target directory exists
	dstDir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return CopyOperation{
			Path:   mf.Path,
			Status: CopyStatusFailed,
			Err:    fmt.Errorf("failed to create directory: %w", err),
		}
	}

	// Check if source file exists
	if _, err := os.Stat(srcPath); err != nil {
		return CopyOperation{
			Path:   mf.Path,
			Status: CopyStatusFailed,
			Err:    fmt.Errorf("source file not found: %w", err),
		}
	}

	// Check if destination already exists
	if _, err := os.Stat(dstPath); err == nil {
		return CopyOperation{
			Path:   mf.Path,
			Status: CopyStatusSkipped,
		}
	}

	if opts.DryRun {
		return CopyOperation{
			Path:   mf.Path,
			Status: CopyStatusCopied,
		}
	}

	// Copy file
	if err := copyFileContent(srcPath, dstPath); err != nil {
		return CopyOperation{
			Path:   mf.Path,
			Status: CopyStatusFailed,
			Err:    fmt.Errorf("copy failed: %w", err),
		}
	}

	// Set modification time
	if err := os.Chtimes(dstPath, mf.ModTime, mf.ModTime); err != nil && opts.Verbose {
		// Just log, don't fail
	}

	return CopyOperation{
		Path:   mf.Path,
		Status: CopyStatusCopied,
	}
}

// copyFileContent copies a file from src to dst
func copyFileContent(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Get file info for mode
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = dstFile.ReadFrom(srcFile)
	if err != nil {
		os.Remove(dst) // Clean up on error
		return err
	}

	return nil
}
package findings

import (
	"fmt"
	"time"

	"ente-hashcmp/internal/metasync"
	"ente-hashcmp/internal/scanner"
)

// AnalyzeOptions holds configuration for the analysis
type AnalyzeOptions struct {
	Dir        string
	MetaDBPath string
	Verbose    bool
}

// AnalyzeMissing analyzes a directory to find files not in the ente library
// It reuses the scanner module which:
// 1. Checks cached hashes from previous scans
// 2. Only recalculates hashes for changed files
// 3. Stores results in the local database
func AnalyzeMissing(opts AnalyzeOptions) (*MissingResult, error) {
	startTime := time.Now()
	result := &MissingResult{
		MissingFiles: []MissingFile{},
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

	// Create scanner to ensure all files have up-to-date hashes
	// The scanner will reuse cached hashes and only recalculate changed files
	s, err := scanner.New(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("failed to create scanner: %w", err)
	}
	defer s.Close()

	// Scan the directory - this updates hashes for changed files
	stats, err := s.Scan()
	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("Scanned %d files, updated %d, skipped %d\n",
			stats.TotalFiles, stats.UpdatedFiles, stats.SkippedFiles)
	}

	// Get all file entries from the local database (now all have up-to-date hashes)
	entries, err := s.DB().GetAllFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to get file entries: %w", err)
	}

	// Compare each file's hash against ente library (hash-only comparison)
	for relPath, entry := range entries {
		result.TotalFiles++

		// Check if hash exists in ente library
		if enteHashes[entry.Hash] {
			result.FoundInEnte++
		} else {
			result.MissingFiles = append(result.MissingFiles, MissingFile{
				Path:    relPath,
				Hash:    entry.Hash,
				Size:    entry.Size,
				ModTime: entry.ModTime,
			})
		}
	}

	result.Duration = time.Since(startTime)
	return result, nil
}
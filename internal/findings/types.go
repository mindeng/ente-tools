package findings

import "time"

// MissingResult represents the result of a missing files analysis
type MissingResult struct {
	// Total files scanned in the local directory
	TotalFiles int
	// Files found in ente library (by hash match)
	FoundInEnte int
	// Files not found in ente library
	MissingFiles []MissingFile
	// Duration of the analysis
	Duration time.Duration
}

// MissingFile represents a local file that is not in ente library
// It can be a regular file or a Live Photo (which includes both image and video)
type MissingFile struct {
	// Relative path from the scanned directory
	Path string
	// File hash
	Hash string
	// File size in bytes
	Size int64
	// File modification time
	ModTime time.Time
	// For Live Photos: additional file paths (video component)
	AdditionalPaths []string // Additional files to copy (e.g., video for Live Photos)
	// For Live Photos: additional file info
	AdditionalInfo []MissingFileInfo
}

// MissingFileInfo contains info about additional files in a group (e.g., Live Photo components)
type MissingFileInfo struct {
	Path    string
	Hash    string
	Size    int64
	ModTime time.Time
}
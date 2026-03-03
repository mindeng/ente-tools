package types

import "time"

// FileRecord represents a file's metadata stored in the database
type FileRecord struct {
	Hash          string            `json:"hash"`
	Size          int64             `json:"size"`
	ModTime       time.Time         `json:"modTime"`
	LivePhotoParts *LivePhotoParts `json:"livePhotoParts,omitempty"`
}

// LivePhotoParts contains information about the components of a Live Photo
type LivePhotoParts struct {
	Image string `json:"image"`
	Video string `json:"video"`
}

// FileType represents the type of a media file
type FileType int

const (
	FileTypeUnknown FileType = iota
	FileTypeImage
	FileTypeVideo
)

// String returns a string representation of the file type
func (ft FileType) String() string {
	switch ft {
	case FileTypeImage:
		return "image"
	case FileTypeVideo:
		return "video"
	default:
		return "unknown"
	}
}

// CompareResult represents the comparison result between two directories
type CompareResult struct {
	// Files that exist in both directories with the same hash
	Common int
	// Files that exist in both directories but with different hashes
	DifferentHash []DiffEntry
	// Files that exist only in directory A
	OnlyInA []string
	// Files that exist only in directory B
	OnlyInB []string
}

// DiffEntry represents a file with different hashes in two directories
type DiffEntry struct {
	Path       string
	HashA      string
	HashB      string
	SizeA      int64
	SizeB      int64
}

// ScanStats represents statistics from a directory scan
type ScanStats struct {
	TotalFiles    int
	UpdatedFiles  int
	SkippedFiles  int
	LivePhotos    int
	Duplicates    []DuplicateEntry
	DBPath        string
}

// DuplicateEntry represents a file with duplicates
type DuplicateEntry struct {
	Hash       string
	PrimaryPath string
	Duplicates []string
}

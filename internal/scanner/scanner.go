package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ente-hashcmp/internal/database"
	"ente-hashcmp/internal/hash"
	"ente-hashcmp/internal/livephoto"
	"ente-hashcmp/internal/types"
)

// Scanner handles directory scanning and hash calculation
type Scanner struct {
	dir        string
	db         *database.DB
	stats      types.ScanStats
	duplicates map[string]*types.DuplicateEntry
}

// New creates a new scanner for the given directory
func New(dir string) (*Scanner, error) {
	db, err := database.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return &Scanner{
		dir:       dir,
		db:        db,
		duplicates: make(map[string]*types.DuplicateEntry),
	}, nil
}

// Close closes the scanner and its database
func (s *Scanner) Close() error {
	return s.db.Close()
}

// DB returns the database instance
func (s *Scanner) DB() *database.DB {
	return s.db
}

// Scan scans the directory recursively, computing hashes for files
func (s *Scanner) Scan() (*types.ScanStats, error) {
	s.stats = types.ScanStats{
		Duplicates: []types.DuplicateEntry{},
	}

	// First pass: find all Live Photo pairs in subdirectories
	livePhotoPairs := make(map[string]livephoto.LivePhotoPair)
	err := filepath.Walk(s.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}

		// Skip hidden directories and the .ente-hashcmp directory
		if strings.HasPrefix(filepath.Base(path), ".") {
			if filepath.Base(path) != ".ente-hashcmp" {
				return filepath.SkipDir
			}
			return filepath.SkipDir
		}

		// Find Live Photo pairs in this directory
		pairs, err := livephoto.FindLivePhotoPairs(path)
		if err != nil {
			return fmt.Errorf("failed to find Live Photo pairs in %s: %w", path, err)
		}
		for baseName, pair := range pairs {
			livePhotoPairs[baseName] = pair
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Second pass: scan all files
	err = filepath.Walk(s.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			// Skip hidden directories
			if strings.HasPrefix(filepath.Base(path), ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden files
		if strings.HasPrefix(filepath.Base(path), ".") {
			return nil
		}

		// Check if this is a supported file type
		if !livephoto.IsSupportedFile(filepath.Base(path)) {
			return nil
		}

		s.stats.TotalFiles++

		// Check if this file is part of a Live Photo
		baseName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if pair, ok := livePhotoPairs[baseName]; ok {
			// This file is part of a Live Photo
			fileType := livephoto.GetFileType(filepath.Base(path))
			if fileType == types.FileTypeVideo {
				// Process the entire Live Photo when we hit the video component
				return s.processLivePhoto(pair, path)
			}
			// Skip the image component - it will be processed as part of the Live Photo
			s.stats.SkippedFiles++
			return nil
		}

		// Regular file processing
		return s.processFile(path, info)
	})

	if err != nil {
		return nil, err
	}

	// Convert duplicates map to slice - only include entries with actual duplicates
	for _, dup := range s.duplicates {
		if len(dup.Duplicates) > 0 {
			s.stats.Duplicates = append(s.stats.Duplicates, *dup)
		}
	}

	return &s.stats, nil
}

// processFile computes and stores hash for a regular file
func (s *Scanner) processFile(path string, info os.FileInfo) error {
	// Get relative path
	relPath, err := filepath.Rel(s.dir, path)
	if err != nil {
		return err
	}

	// Check if we need to recalculate
	needsRecalc, err := s.db.NeedsRecalc(relPath, info.Size(), info.ModTime())
	if err != nil {
		return fmt.Errorf("failed to check if recalc needed: %w", err)
	}

	if !needsRecalc {
		s.stats.SkippedFiles++
		return nil
	}

	// Open file
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", path, err)
	}
	defer file.Close()

	// Compute hash
	fileHash, err := hash.ComputeHash(file)
	if err != nil {
		return fmt.Errorf("failed to compute hash for %s: %w", path, err)
	}

	// Store in database
	record := &types.FileRecord{
		Hash:    fileHash,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}

	if err := s.db.PutFile(relPath, record); err != nil {
		return fmt.Errorf("failed to store record: %w", err)
	}

	s.stats.UpdatedFiles++

	// Track duplicates
	s.trackDuplicate(relPath, fileHash)

	return nil
}

// processLivePhoto computes and stores hash for a Live Photo
func (s *Scanner) processLivePhoto(pair livephoto.LivePhotoPair, videoPath string) error {
	// Get relative path for the image (primary path)
	imageRelPath, err := filepath.Rel(s.dir, pair.ImagePath)
	if err != nil {
		return err
	}

	// Get file info for both components
	imageInfo, err := os.Stat(pair.ImagePath)
	if err != nil {
		return fmt.Errorf("failed to stat image %s: %w", pair.ImagePath, err)
	}

	videoInfo, err := os.Stat(pair.VideoPath)
	if err != nil {
		return fmt.Errorf("failed to stat video %s: %w", pair.VideoPath, err)
	}

	// Get combined size
	totalSize := imageInfo.Size() + videoInfo.Size()

	// Check if we need to recalculate
	needsRecalc, err := s.db.NeedsRecalc(imageRelPath, totalSize, imageInfo.ModTime())
	if err != nil {
		return fmt.Errorf("failed to check if recalc needed: %w", err)
	}

	if !needsRecalc {
		s.stats.SkippedFiles++
		s.stats.LivePhotos++
		return nil
	}

	// Compute Live Photo hash
	combinedHash, err := livephoto.CalculateLivePhotoHash(pair.ImagePath, pair.VideoPath)
	if err != nil {
		return fmt.Errorf("failed to compute Live Photo hash: %w", err)
	}

	// Store in database with both parts
	record := &types.FileRecord{
		Hash:    combinedHash,
		Size:    totalSize,
		ModTime: imageInfo.ModTime(),
		LivePhotoParts: &types.LivePhotoParts{
			Image: imageRelPath,
			Video: filepath.Join(filepath.Dir(imageRelPath), filepath.Base(pair.VideoPath)),
		},
	}

	if err := s.db.PutFile(imageRelPath, record); err != nil {
		return fmt.Errorf("failed to store record: %w", err)
	}

	s.stats.UpdatedFiles++
	s.stats.LivePhotos++

	// Track duplicates
	s.trackDuplicate(imageRelPath, combinedHash)

	return nil
}

// trackDuplicate tracks duplicate files by hash
func (s *Scanner) trackDuplicate(path string, fileHash string) {
	if dup, exists := s.duplicates[fileHash]; exists {
		// Add to existing duplicates
		if len(dup.Duplicates) == 0 {
			// This was previously the primary, make it a duplicate
			dup.Duplicates = append(dup.Duplicates, dup.PrimaryPath)
			dup.PrimaryPath = path
		} else {
			dup.Duplicates = append(dup.Duplicates, path)
		}
	} else {
		// New entry
		s.duplicates[fileHash] = &types.DuplicateEntry{
			Hash:       fileHash,
			PrimaryPath: path,
			Duplicates: []string{},
		}
	}
}

// GetDBPath returns the path to the database file
func (s *Scanner) GetDBPath() string {
	return database.GetPath(s.dir)
}

package scanner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ente-hashcmp/internal/database"
	"ente-hashcmp/internal/hash"
	"ente-hashcmp/internal/livephoto"
	"ente-hashcmp/internal/types"
)

// ProgressCallback is called during scanning to report progress
type ProgressCallback func(stats *types.ScanStats, currentPath string)

// Scanner handles directory scanning and hash calculation
type Scanner struct {
	dir        string
	db         *database.DB
	stats      types.ScanStats
	duplicates map[string]*types.DuplicateEntry
	progress   ProgressCallback
	// Progress tracking
	lastProgressTime time.Time
	currentDir       string
	// Track unsupported file extensions
	unsupportedExts map[string]bool
	// Track potential Live Photo components
	// dirPath -> baseName -> []potentialComponent
	potentialLivePhotos map[string]map[string][]*potentialComponent
}

// potentialComponent stores file information for Live Photo matching
type potentialComponent struct {
	path     string
	fileType types.FileType
	size     int64
	modTime  time.Time
	relPath  string
	hash     string
}

// New creates a new scanner for the given directory
func New(dir string) (*Scanner, error) {
	db, err := database.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return &Scanner{
		dir:                 dir,
		db:                  db,
		duplicates:          make(map[string]*types.DuplicateEntry),
		lastProgressTime:    time.Now(),
		unsupportedExts:     make(map[string]bool),
		potentialLivePhotos: make(map[string]map[string][]*potentialComponent),
	}, nil
}

// SetProgressCallback sets a callback function to be called during scanning
func (s *Scanner) SetProgressCallback(callback ProgressCallback) {
	s.progress = callback
}

// Close closes the scanner and its database
func (s *Scanner) Close() error {
	return s.db.Close()
}

// DB returns the database instance
func (s *Scanner) DB() *database.DB {
	return s.db
}

// Scan scans the directory recursively in a single pass
func (s *Scanner) Scan() (*types.ScanStats, error) {
	s.stats = types.ScanStats{
		Duplicates: []types.DuplicateEntry{},
	}

	var currentDirPath string
	maxAssetSize := int64(20 * 1024 * 1024) // 20MB for Live Photo size limit

	err := filepath.WalkDir(s.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			// Skip hidden directories and the .fhash directory
			if strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}

			// Detect directory change and release memory for completed directories
			if currentDirPath != "" && !strings.HasPrefix(path, currentDirPath+string(filepath.Separator)) {
				// We've moved out of currentDirPath (and its subdirs), release its potentialLivePhotos
				delete(s.potentialLivePhotos, currentDirPath)
			}

			currentDirPath = path
			s.currentDir = d.Name()
			s.reportProgress(fmt.Sprintf("scanning: %s", d.Name()))
			return nil
		}

		// Skip hidden files
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		// Check if this is a supported file type
		if !livephoto.IsSupportedFile(d.Name()) {
			// Track unsupported file extension
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if ext != "" {
				s.unsupportedExts[ext] = true
			}
			s.stats.UnsupportedFiles++
			return nil
		}

		// Get file type
		ft := livephoto.GetFileType(path)
		if ft != types.FileTypeImage && ft != types.FileTypeVideo {
			return nil
		}

		// Get file info
		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(s.dir, path)
		if err != nil {
			return err
		}

		// Compute and store hash for this file (always compute hash)
		fileHash, computed, err := s.computeAndStoreHash(path, info, relPath)
		if err != nil {
			return fmt.Errorf("failed to compute hash for %s: %w", path, err)
		}

		s.stats.TotalFiles++

		// Check if this could be part of a Live Photo
		// Skip files not supported by Live Photo format (RAW formats etc.)
		if !livephoto.IsLivePhotoSupportedFile(path) {
			return nil
		}

		// Skip files larger than Live Photo size limit
		if info.Size() > maxAssetSize {
			return nil
		}

		// Get directory path and base name for matching
		dirPath := filepath.Dir(path)
		baseName := livephoto.GetBaseName(path, ft, "")

		// Initialize potential Live Photos map for this directory if needed
		if s.potentialLivePhotos[dirPath] == nil {
			s.potentialLivePhotos[dirPath] = make(map[string][]*potentialComponent)
		}

		// Try to find a matching component in pending
		pendingList := s.potentialLivePhotos[dirPath][baseName]
		matched := false
		matchedIndex := -1

		for i, match := range pendingList {
			// Only match opposite type (image with video)
			if match.fileType == ft {
				continue
			}

			var imagePath, videoPath string
			var imageRelPath, videoRelPath string
			var imageHash, videoHash string
			var imageModTime time.Time

			if ft == types.FileTypeImage {
				imagePath = path
				videoPath = match.path
				imageRelPath = relPath
				videoRelPath = match.relPath
				imageHash = fileHash
				videoHash = match.hash
				imageModTime = info.ModTime()
			} else {
				imagePath = match.path
				videoPath = path
				imageRelPath = match.relPath
				videoRelPath = relPath
				imageHash = match.hash
				videoHash = fileHash
				imageModTime = match.modTime
			}

			// Verify with full AreLivePhotoAssets check
			if livephoto.AreLivePhotoAssets(imagePath, videoPath) {
				// Valid Live Photo pair found - compute and store combined hash
				combinedHash := livephoto.CombineHashes(imageHash, videoHash)
				totalSize := info.Size() + match.size

				record := &types.FileRecord{
					Hash:    combinedHash,
					Size:    totalSize,
					ModTime: imageModTime,
					LivePhotoParts: &types.LivePhotoParts{
						Image: imageRelPath,
						Video: videoRelPath,
					},
				}

				// Store or update the Live Photo record
				if err := s.db.PutFile(imageRelPath, record); err != nil {
					return fmt.Errorf("failed to store Live Photo record: %w", err)
				}

				if computed || match.hash == "" {
					// If we computed a new hash or this is a new Live Photo, count as updated
					s.stats.UpdatedFiles++
				} else {
					s.stats.SkippedFiles++
				}

				s.stats.LivePhotos++

				// Track duplicates for the combined hash
				s.trackDuplicate(imageRelPath, combinedHash)

				matched = true
				matchedIndex = i
				s.reportProgress(fmt.Sprintf("Live Photo (%d): %s", s.stats.LivePhotos, d.Name()))
				break
			}
		}

		if matched && matchedIndex >= 0 {
			// Remove the matched component from pending list
			pendingList = append(pendingList[:matchedIndex], pendingList[matchedIndex+1:]...)
			s.potentialLivePhotos[dirPath][baseName] = pendingList
		} else {
			// Add to potential Live Photos list
			s.potentialLivePhotos[dirPath][baseName] = append(pendingList, &potentialComponent{
				path:     path,
				fileType: ft,
				size:     info.Size(),
				modTime:  info.ModTime(),
				relPath:  relPath,
				hash:     fileHash,
			})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Report final progress
	s.reportProgress("complete")

	// Convert duplicates map to slice - only include entries with actual duplicates
	for _, dup := range s.duplicates {
		if len(dup.Duplicates) > 0 {
			s.stats.Duplicates = append(s.stats.Duplicates, *dup)
		}
	}

	// Convert unsupported extensions map to sorted slice
	for ext := range s.unsupportedExts {
		s.stats.UnsupportedExts = append(s.stats.UnsupportedExts, ext)
	}

	return &s.stats, nil
}

// computeAndStoreHash computes and stores hash for a file
func (s *Scanner) computeAndStoreHash(path string, info os.FileInfo, relPath string) (string, bool, error) {
	// Check if we need to recalculate
	needsRecalc, err := s.db.NeedsRecalc(relPath, info.Size(), info.ModTime())
	if err != nil {
		return "", false, fmt.Errorf("failed to check if recalc needed: %w", err)
	}

	if !needsRecalc {
		// Try to get cached hash
		record, err := s.db.GetFile(relPath)
		if err == nil && record != nil {
			s.stats.SkippedFiles++
			return record.Hash, false, nil
		}
	}

	// Open file
	file, err := os.Open(path)
	if err != nil {
		return "", false, fmt.Errorf("failed to open file %s: %w", path, err)
	}
	defer file.Close()

	// Compute hash
	fileHash, err := hash.ComputeHash(file)
	if err != nil {
		return "", false, fmt.Errorf("failed to compute hash for %s: %w", path, err)
	}

	// Store in database (as individual file, will be updated if part of Live Photo)
	record := &types.FileRecord{
		Hash:    fileHash,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}

	if err := s.db.PutFile(relPath, record); err != nil {
		return fileHash, true, fmt.Errorf("failed to store record: %w", err)
	}

	s.stats.UpdatedFiles++

	// Track duplicates for the individual file hash
	s.trackDuplicate(relPath, fileHash)

	return fileHash, true, nil
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
			Hash:        fileHash,
			PrimaryPath: path,
			Duplicates:  []string{},
		}
	}
}

// GetDBPath returns the path to the database file
func (s *Scanner) GetDBPath() string {
	return database.GetPath(s.dir)
}

// reportProgress reports progress if enough time has passed since last report
func (s *Scanner) reportProgress(currentPath string) {
	if s.progress == nil {
		return
	}

	// Only report progress every 500ms to avoid spamming output
	now := time.Now()
	if now.Sub(s.lastProgressTime) < 500*time.Millisecond {
		return
	}

	s.lastProgressTime = now
	s.progress(&s.stats, currentPath)
}
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
}

// New creates a new scanner for the given directory
func New(dir string) (*Scanner, error) {
	db, err := database.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return &Scanner{
		dir:              dir,
		db:               db,
		duplicates:       make(map[string]*types.DuplicateEntry),
		lastProgressTime: time.Now(),
		unsupportedExts:  make(map[string]bool),
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

// fileInfo stores file information for Live Photo matching
type fileInfo struct {
	path     string
	fileType types.FileType
	size     int64
	modTime  time.Time
}

// Scan scans the directory recursively, computing hashes for files
func (s *Scanner) Scan() (*types.ScanStats, error) {
	s.stats = types.ScanStats{
		Duplicates: []types.DuplicateEntry{},
	}

	// First pass: find all Live Photo pairs in a single WalkDir
	livePhotoPairs := make(map[string]livephoto.LivePhotoPair)
	// pendingFiles: dirPath -> baseName -> []fileInfo (files waiting for match)
	pendingFiles := make(map[string]map[string][]*fileInfo)
	maxAssetSize := int64(20 * 1024 * 1024) // 20MB

	var currentDirPath string

	err := filepath.WalkDir(s.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			// Skip hidden directories and the .fhash directory
			if strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}

			// Detect directory change and release memory
			if currentDirPath != "" && !strings.HasPrefix(path, currentDirPath+string(filepath.Separator)) {
				// We've moved out of currentDirPath (and its subdirs), release its pendingFiles
				delete(pendingFiles, currentDirPath)
			}

			currentDirPath = path
			s.currentDir = d.Name()
			s.reportProgress(fmt.Sprintf("finding Live Photos (%d): %s", len(livePhotoPairs), d.Name()))
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

		// Skip files not supported by Live Photo format (RAW formats etc.)
		if !livephoto.IsLivePhotoSupportedFile(path) {
			return nil
		}

		// Quick size check (get size without Stat for performance)
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() > maxAssetSize {
			return nil
		}

		// Get directory path for grouping
		dirPath := filepath.Dir(path)
		baseName := livephoto.GetBaseName(path, ft, "")

		// Initialize pending files map for this directory if needed
		if pendingFiles[dirPath] == nil {
			pendingFiles[dirPath] = make(map[string][]*fileInfo)
		}

		// Try to find a matching file in pending
		pendingList := pendingFiles[dirPath][baseName]
		matched := false
		matchedIndex := -1

		for i, match := range pendingList {
			// Only match opposite type (image with video)
			if match.fileType == ft {
				continue
			}

			var imagePath, videoPath string
			if ft == types.FileTypeImage {
				imagePath = path
				videoPath = match.path
			} else {
				imagePath = match.path
				videoPath = path
			}

			// Verify with full AreLivePhotoAssets check
			if livephoto.AreLivePhotoAssets(imagePath, videoPath) {
				// Valid Live Photo pair found
				livePhotoPairs[imagePath] = livephoto.LivePhotoPair{
					ImagePath: imagePath,
					VideoPath: videoPath,
					BaseName:  baseName,
				}
				matched = true
				matchedIndex = i
				s.reportProgress(fmt.Sprintf("finding Live Photos (%d): %s", len(livePhotoPairs), d.Name()))
				break
			}
		}

		if matched && matchedIndex >= 0 {
			// Remove the matched file from pending list
			pendingList = append(pendingList[:matchedIndex], pendingList[matchedIndex+1:]...)
			pendingFiles[dirPath][baseName] = pendingList
		} else {
			// Add to pending list
			pendingFiles[dirPath][baseName] = append(pendingList, &fileInfo{
				path:     path,
				fileType: ft,
				size:     info.Size(),
				modTime:  info.ModTime(),
			})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Second pass: scan all files for hash computation
	err = filepath.WalkDir(s.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if d.IsDir() {
			// Skip hidden directories
			if strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			// Update current directory for progress reporting
			s.currentDir = d.Name()
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

		// Check if this file is part of a Live Photo
		if pair, ok := livePhotoPairs[path]; ok {
			// This file is the image component of a Live Photo
			// Process the entire Live Photo (we need the video for the hash)
			currentPath := d.Name()
			err = s.processLivePhoto(pair)
			s.reportProgress(currentPath)
			return err
		}

		// Check if this file is the video component of a Live Photo
		for _, pair := range livePhotoPairs {
			if pair.VideoPath == path {
				// This is the video, but we process Live Photo when we hit the image
				// Skip this file as it will be processed with the image
				return nil
			}
		}

		// Regular file processing
		currentPath := d.Name()
		fileInfo, err := d.Info()
		if err != nil {
			return err
		}
		err = s.processFile(path, fileInfo)
		s.reportProgress(currentPath)
		return err
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

// processFile computes and stores hash for a regular file
func (s *Scanner) processFile(path string, info os.FileInfo) error {
	s.stats.TotalFiles++ // Count this file

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
func (s *Scanner) processLivePhoto(pair livephoto.LivePhotoPair) error {
	s.stats.TotalFiles++ // Count this Live Photo as one unit

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
		// Check if the database record matches the current video path
		// This is important because multiple videos might share the same base name
		// but match to different images due to Live Photo suffix removal
		record, err := s.db.GetFile(imageRelPath)
		if err == nil && record != nil && record.LivePhotoParts != nil {
			videoRelPath := filepath.Join(filepath.Dir(imageRelPath), filepath.Base(pair.VideoPath))
			if record.LivePhotoParts.Video == videoRelPath {
				// Same video, can skip
				s.stats.SkippedFiles++
				s.stats.LivePhotos++
				return nil
			}
			// Different video with same base name, need to recalculate
		} else {
			// No LivePhotoParts or no record, can skip
			s.stats.SkippedFiles++
			s.stats.LivePhotos++
			return nil
		}
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

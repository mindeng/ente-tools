package livephoto

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"ente-hashcmp/internal/hash"
	"ente-hashcmp/internal/types"
)

// LivePhotoPair represents an image and video file that form a Live Photo
type LivePhotoPair struct {
	ImagePath string
	VideoPath string
	BaseName  string
}

// CalculateLivePhotoHash computes the combined hash for a Live Photo
// Format: "imageHash:videoHash"
func CalculateLivePhotoHash(imagePath, videoPath string) (string, error) {
	imageFile, err := os.Open(imagePath)
	if err != nil {
		return "", err
	}
	defer imageFile.Close()

	videoFile, err := os.Open(videoPath)
	if err != nil {
		return "", err
	}
	defer videoFile.Close()

	imageHash, err := hash.ComputeHash(imageFile)
	if err != nil {
		return "", err
	}

	videoHash, err := hash.ComputeHash(videoFile)
	if err != nil {
		return "", err
	}

	return imageHash + ":" + videoHash, nil
}

// LivePhotoInfo contains information about a Live Photo's components
type LivePhotoInfo struct {
	CombinedHash string
	TotalSize    int64
	ImageSize    int64
	VideoSize    int64
	ModTime      time.Time
	ImageModTime time.Time
	VideoModTime time.Time
}

// GetLivePhotoInfo calculates hash and metadata for a Live Photo
func GetLivePhotoInfo(pair LivePhotoPair) (*LivePhotoInfo, error) {
	imageInfo, err := os.Stat(pair.ImagePath)
	if err != nil {
		return nil, err
	}

	videoInfo, err := os.Stat(pair.VideoPath)
	if err != nil {
		return nil, err
	}

	combinedHash, err := CalculateLivePhotoHash(pair.ImagePath, pair.VideoPath)
	if err != nil {
		return nil, err
	}

	// Use the more recent modification time as the Live Photo's mod time
	modTime := imageInfo.ModTime()
	if videoInfo.ModTime().After(modTime) {
		modTime = videoInfo.ModTime()
	}

	return &LivePhotoInfo{
		CombinedHash: combinedHash,
		TotalSize:    imageInfo.Size() + videoInfo.Size(),
		ImageSize:    imageInfo.Size(),
		VideoSize:    videoInfo.Size(),
		ModTime:      modTime,
		ImageModTime: imageInfo.ModTime(),
		VideoModTime: videoInfo.ModTime(),
	}, nil
}

// FindLivePhotoPairs finds all Live Photo pairs in a directory
// Returns a map of base names to their image and video paths
func FindLivePhotoPairs(dir string) (map[string]LivePhotoPair, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	// Group files by their base name
	filesByBaseName := make(map[string][]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		if !IsSupportedFile(filename) {
			continue
		}

		// Get the base name without extension
		baseName := strings.TrimSuffix(filename, filepath.Ext(filename))
		filesByBaseName[baseName] = append(filesByBaseName[baseName], filepath.Join(dir, filename))
	}

	// Find pairs: one image and one video with the same base name
	pairs := make(map[string]LivePhotoPair)
	for baseName, files := range filesByBaseName {
		var imagePath, videoPath string
		hasImage := false
		hasVideo := false

		for _, file := range files {
			ft := GetFileType(file)
			if ft == types.FileTypeImage && !hasImage {
				imagePath = file
				hasImage = true
			} else if ft == types.FileTypeVideo && !hasVideo {
				videoPath = file
				hasVideo = true
			}
		}

		if hasImage && hasVideo {
			pairs[baseName] = LivePhotoPair{
				ImagePath: imagePath,
				VideoPath: videoPath,
				BaseName:  baseName,
			}
		}
	}

	return pairs, nil
}

// ShouldSkipLivePhoto returns true if a file is part of a Live Photo and should be skipped
// This is used during directory scanning to avoid processing the individual components
func ShouldSkipLivePhoto(filename string, pairs map[string]LivePhotoPair) (string, bool) {
	for baseName := range pairs {
		if strings.Contains(filename, baseName) {
			ft := GetFileType(filename)
			if ft == types.FileTypeVideo {
				// Skip the video component, process as Live Photo
				return baseName, true
			}
		}
	}
	return "", false
}

package livephoto

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ente-hashcmp/internal/exif"
	"ente-hashcmp/internal/types"
	"ente-hashcmp/internal/video"
)

var (
	// Live Photo supported image extensions (limited to common formats)
	livePhotoImageExts = map[string]bool{
		"heic": true, "heif": true, "jpeg": true,
		"jpg":  true, "png":  true,  "gif":  true,
		"bmp":  true, "tiff": true, "webp": true,
	}

	// Live Photo supported video extensions
	livePhotoVideoExts = map[string]bool{
		"mov": true, "mp4": true, "m4v": true,
	}

	// General image extensions (includes RAW formats)
	imageExts = map[string]bool{
		"heic": true, "heif": true, "jpeg": true,
		"jpg":  true, "png":  true,  "gif":  true,
		"bmp":  true, "tiff": true, "webp": true,
		"arw":  true, "dng":  true,  "nef":  true,
	}

	// General video extensions
	videoExts = map[string]bool{
		"mov": true, "mp4": true, "m4v": true,
		"avi": true, "wmv": true, "flv": true,
		"mkv": true, "webm": true, "3gp": true,
		"3g2": true, "ogv": true, "mpg": true,
		"qt":  true,
	}

	// Suffixes to remove from Live Photo file names
	suffixes = []string{"_3", "_HVEC", "_hvec"}
)

// AreLivePhotoAssets checks if two files form a Live Photo pair
// Based on the logic from ente web's areLivePhotoAssets function
func AreLivePhotoAssets(filePath1, filePath2 string) bool {
	ft1 := GetFileType(filePath1)
	ft2 := GetFileType(filePath2)

	// Must be one image and one video
	if ft1 == types.FileTypeImage && ft2 == types.FileTypeVideo {
		return checkLivePhotoPair(filePath1, filePath2)
	}
	if ft1 == types.FileTypeVideo && ft2 == types.FileTypeImage {
		return checkLivePhotoPair(filePath2, filePath1)
	}
	return false
}

// checkLivePhotoPair performs the full check for a Live Photo pair
func checkLivePhotoPair(imagePath, videoPath string) bool {
	// Get file names
	imageName := filepath.Base(imagePath)
	videoName := filepath.Base(videoPath)
	imageExt := strings.ToLower(filepath.Ext(imagePath))
	videoExt := strings.ToLower(filepath.Ext(videoPath))

	// Remove potential Live Photo suffixes
	imagePrunedName := removePotentialLivePhotoSuffix(
		strings.TrimSuffix(imageName, imageExt),
		videoExt,
	)
	videoPrunedName := removePotentialLivePhotoSuffix(
		strings.TrimSuffix(videoName, videoExt),
		"", // videos don't need extra suffix removal
	)

	// File names must match after pruning
	if imagePrunedName != videoPrunedName {
		return false
	}

	// Check file sizes (max 20MB per asset)
	maxAssetSize := int64(20 * 1024 * 1024) // 20MB
	imageInfo, err := os.Stat(imagePath)
	if err != nil {
		return false
	}
	videoInfo, err := os.Stat(videoPath)
	if err != nil {
		return false
	}

	if imageInfo.Size() > maxAssetSize || videoInfo.Size() > maxAssetSize {
		return false
	}

	// Get creation times from EXIF (for image) and video metadata (for video)
	imageCreationTime, err := exif.GetCreationTime(imagePath)
	if err != nil {
		// Fall back to modTime if EXIF fails
		imageCreationTime = imageInfo.ModTime()
	}

	// Get video creation time from metadata
	videoCreationTime, err := video.GetCreationTime(videoPath)
	if err != nil {
		// Fall back to modTime if video metadata extraction fails
		videoCreationTime = videoInfo.ModTime()
	}

	// Check that creation times are within 1 day
	threshold := 24 * time.Hour
	timeDiff := imageCreationTime.Sub(videoCreationTime)
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}

	// Only check time difference if both times are non-zero
	if !imageCreationTime.IsZero() && !videoCreationTime.IsZero() {
		if timeDiff > threshold {
			return false
		}
	}

	return true
}

// RemovePotentialLivePhotoSuffix removes Live Photo suffixes from a file name
// Based on ente web's removePotentialLivePhotoSuffix function
// Exported for use in other packages
func RemovePotentialLivePhotoSuffix(name, suffix string) string {
	// Check for _3 suffix
	if strings.HasSuffix(name, "_3") {
		return strings.TrimSuffix(name, "_3")
	}

	// Check for _HVEC suffix (case-insensitive)
	if strings.HasSuffix(strings.ToUpper(name), "_HVEC") {
		return strings.TrimSuffix(strings.ToUpper(name), "_HVEC")
	}

	// Check for custom suffix (e.g., .mp4 for Google Live Photos)
	if suffix != "" {
		if strings.HasSuffix(strings.ToLower(name), strings.ToLower(suffix)) {
			return strings.TrimSuffix(strings.ToLower(name), strings.ToLower(suffix))
		}
	}

	return name
}

// removePotentialLivePhotoSuffix is the private version
func removePotentialLivePhotoSuffix(name, suffix string) string {
	return RemovePotentialLivePhotoSuffix(name, suffix)
}

// GetFileType determines the type of file based on its extension
func GetFileType(filename string) types.FileType {
	ext := strings.ToLower(path.Ext(filename))
	if len(ext) > 0 {
		ext = ext[1:] // Remove the dot
	}
	if imageExts[ext] {
		return types.FileTypeImage
	}
	if videoExts[ext] {
		return types.FileTypeVideo
	}
	return types.FileTypeUnknown
}

// IsSupportedFile returns true if the file is a supported media type
func IsSupportedFile(filename string) bool {
	ft := GetFileType(filename)
	return ft == types.FileTypeImage || ft == types.FileTypeVideo
}

// IsLivePhotoSupportedFile returns true if the file could be part of a Live Photo
// (more restrictive than IsSupportedFile, as Live Photos have limited format support)
func IsLivePhotoSupportedFile(filename string) bool {
	ft := GetFileType(filename)
	if ft == types.FileTypeUnknown {
		return false
	}

	ext := strings.ToLower(path.Ext(filename))
	if len(ext) > 0 {
		ext = ext[1:] // Remove the dot
	}

	// Only check supported Live Photo extensions
	if ft == types.FileTypeImage {
		return livePhotoImageExts[ext]
	}
	if ft == types.FileTypeVideo {
		return livePhotoVideoExts[ext]
	}
	return false
}

// GetBaseName returns the base name of a file after removing Live Photo suffixes
// For example:
//   - IMG_0001.HEIC -> IMG_0001
//   - IMG_0001_3.MOV -> IMG_0001
//   - IMG_0001_HVEC.MOV -> IMG_0001
//   - IMG_0001.mp4.jpg -> IMG_0001 (Google Live Photo format)
func GetBaseName(filename string, fileType types.FileType, otherFilename string) string {
	name := path.Base(filename)
	name = strings.TrimSuffix(name, path.Ext(name))

	// For Google Live Photos, the image file might have the video extension as a suffix
	// Example: IMG_20210630_0001.mp4.jpg
	if fileType == types.FileTypeImage {
		otherExt := strings.ToLower(path.Ext(otherFilename))
		if len(otherExt) > 0 {
			otherExt = otherExt[1:] // Remove the dot
			name = strings.TrimSuffix(strings.ToLower(name), "."+otherExt)
		}
	}

	// Remove known Live Photo suffixes
	for _, suffix := range suffixes {
		name = strings.TrimSuffix(strings.ToLower(name), strings.ToLower(suffix))
	}

	// Restore original case for the remaining part
	// We need to be careful here since we've been lowercasing
	// Let's use a regex approach for better case handling
	return getBaseNameRegex(path.Base(filename), fileType, otherFilename)
}

// getBaseNameRegex uses regex to remove Live Photo suffixes while preserving case
func getBaseNameRegex(filename string, fileType types.FileType, otherFilename string) string {
	name := filename
	name = strings.TrimSuffix(name, path.Ext(name))

	// For Google Live Photos
	if fileType == types.FileTypeImage {
		otherExt := path.Ext(otherFilename)
		if otherExt != "" {
			// Remove video extension suffix (case-insensitive)
			re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(otherExt) + `$`)
			name = re.ReplaceAllString(name, "")
		}
	}

	// Remove known Live Photo suffixes (case-insensitive)
	for _, suffix := range suffixes {
		re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(suffix) + `$`)
		name = re.ReplaceAllString(name, "")
	}

	return name
}

// MatchLivePhoto checks if two files form a Live Photo pair
// Returns true if one is an image, one is a video, and they have the same base name
func MatchLivePhoto(file1, file2 string) bool {
	ft1 := GetFileType(file1)
	ft2 := GetFileType(file2)

	// Must be one image and one video
	if ft1 == types.FileTypeImage && ft2 == types.FileTypeVideo {
		return GetBaseName(file1, ft1, file2) == GetBaseName(file2, ft2, file1)
	}
	if ft1 == types.FileTypeVideo && ft2 == types.FileTypeImage {
		return GetBaseName(file1, ft1, file2) == GetBaseName(file2, ft2, file1)
	}
	return false
}

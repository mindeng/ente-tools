package livephoto

import (
	"path"
	"regexp"
	"strings"

	"ente-hashcmp/internal/types"
)

var (
	// Supported image extensions
	imageExts = map[string]bool{
		"heic": true, "heif": true, "jpeg": true,
		"jpg": true, "png": true, "gif": true,
		"bmp": true, "tiff": true, "webp": true,
	}

	// Supported video extensions
	videoExts = map[string]bool{
		"mov": true, "mp4": true, "m4v": true,
		"avi": true, "wmv": true, "flv": true,
		"mkv": true, "webm": true, "3gp": true,
		"3g2": true, "ogv": true, "mpg": true,
	}

	// Suffixes to remove from Live Photo file names
	suffixes = []string{"_3", "_HVEC", "_hvec"}
)

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

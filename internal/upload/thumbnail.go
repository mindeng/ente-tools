package upload

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
)

// ThumbnailSize is the target size for thumbnails
const ThumbnailSize = 300

// GenerateThumbnail generates a thumbnail from an image file
func GenerateThumbnail(imagePath string) ([]byte, error) {
	// Check if it's a HEIC file and needs conversion
	ext := strings.ToLower(filepath.Ext(imagePath))
	if ext == ".heic" || ext == ".heif" {
		// Try to convert HEIC to JPEG using ffmpeg first
		if jpegData, err := convertHEICToJPEG(imagePath); err == nil {
			return GenerateThumbnailFromBytes(jpegData)
		}
		// If conversion fails, continue with regular decoding
	}

	// Open the image file
	file, err := os.Open(imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open image: %w", err)
	}
	defer file.Close()

	// Read all data
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read image data: %w", err)
	}

	return GenerateThumbnailFromBytes(data)
}

// convertHEICToJPEG converts HEIC/HEIF to JPEG using ffmpeg
func convertHEICToJPEG(imagePath string) ([]byte, error) {
	cmd := exec.Command("ffmpeg",
		"-i", imagePath,
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"-q:v", "2",
		"-")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %w", err)
	}

	return output, nil
}

// GenerateThumbnailFromBytes generates a thumbnail from image data
// Uses imaging library with AutoOrientation to handle EXIF rotation
func GenerateThumbnailFromBytes(data []byte) ([]byte, error) {
	// Decode the image with auto-orientation enabled
	src, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	// Resize to fit within ThumbnailSize while maintaining aspect ratio
	dst := imaging.Fit(src, ThumbnailSize, ThumbnailSize, imaging.Lanczos)

	// Encode to JPEG
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, dst, imaging.JPEG); err != nil {
		return nil, fmt.Errorf("failed to encode thumbnail: %w", err)
	}

	return buf.Bytes(), nil
}

// GeneratePlaceholderThumbnail generates a placeholder thumbnail
func GeneratePlaceholderThumbnail() ([]byte, error) {
	// Create a simple gray image as placeholder
	img := image.NewRGBA(image.Rect(0, 0, ThumbnailSize, ThumbnailSize))
	gray := color.Gray{Y: 128}
	for y := 0; y < ThumbnailSize; y++ {
		for x := 0; x < ThumbnailSize; x++ {
			img.Set(x, y, gray)
		}
	}

	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG); err != nil {
		return nil, fmt.Errorf("failed to encode placeholder: %w", err)
	}

	return buf.Bytes(), nil
}

// GetThumbnail generates appropriate thumbnail based on file type
func GetThumbnail(filePath string, fileType FileCategory) ([]byte, error) {
	switch fileType {
	case FileCategoryImage:
		return GenerateThumbnail(filePath)
	case FileCategoryVideo:
		thumb, err := GenerateVideoThumbnail(filePath)
		if err != nil {
			// Fall back to placeholder if video thumbnail generation fails
			return GeneratePlaceholderThumbnail()
		}
		return thumb, nil
	case FileCategoryLivePhoto:
		// For live photos, use the image as thumbnail
		return GenerateThumbnail(filePath)
	default:
		return GeneratePlaceholderThumbnail()
	}
}

// GetThumbnailFromBytes generates thumbnail from byte data
func GetThumbnailFromBytes(data []byte, fileType FileCategory) ([]byte, error) {
	switch fileType {
	case FileCategoryImage:
		return GenerateThumbnailFromBytes(data)
	case FileCategoryVideo:
		return GeneratePlaceholderThumbnail()
	case FileCategoryLivePhoto:
		return GenerateThumbnailFromBytes(data)
	default:
		return GeneratePlaceholderThumbnail()
	}
}

// IsImageFile checks if a file is an image based on extension
func IsImageFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	imageExts := map[string]bool{
		".jpg":  true, ".jpeg": true, ".jpe": true,
		".png":  true, ".gif":  true, ".bmp":  true,
		".webp": true, ".tiff": true, ".tif":  true,
		".heic": true, ".heif": true, ".avif": true,
	}
	return imageExts[ext]
}

// IsVideoFile checks if a file is a video based on extension
func IsVideoFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	videoExts := map[string]bool{
		".mp4":  true, ".mov": true, ".avi":  true,
		".mkv":  true, ".webm": true, ".flv":  true,
		".wmv":  true, ".3gp": true, ".3g2":  true,
		".m4v":  true, ".ogv": true, ".ts":   true,
		".mpg":  true, ".mpeg": true, ".qt":  true,
	}
	return videoExts[ext]
}
package upload

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ente-hashcmp/internal/hash"
	"ente-hashcmp/internal/livephoto"
	"ente-hashcmp/internal/types"
)

// LivePhotoComponents represents the components of a live photo
type LivePhotoComponents struct {
	ImagePath    string
	VideoPath    string
	ImageData    []byte
	VideoData    []byte
	ImageHash    string
	VideoHash    string
	CreationTime int64
	IsMotion     bool
}

// MotionPhotoMarker is the marker for Google Motion Photos
const MotionPhotoMarker = "MotionPhoto"

// DetectLivePhoto detects if a file is part of a live photo and finds its pair
func DetectLivePhoto(filePath string) (*LivePhotoComponents, error) {
	fileType := livephoto.GetFileType(filePath)

	if fileType == types.FileTypeUnknown {
		return nil, errors.New("unsupported file type")
	}

	// Check for motion photo (Google's format) - video embedded in image
	if fileType == types.FileTypeImage {
		motionVideo, err := ExtractMotionPhotoVideo(filePath)
		if err == nil && len(motionVideo) > 0 {
			// This is a motion photo
			imageData, err := os.ReadFile(filePath)
			if err != nil {
				return nil, fmt.Errorf("failed to read image: %w", err)
			}

			imageHash, err := computeHashFromBytes(imageData)
			if err != nil {
				return nil, fmt.Errorf("failed to compute image hash: %w", err)
			}

			videoHash, err := computeHashFromBytes(motionVideo)
			if err != nil {
				return nil, fmt.Errorf("failed to compute video hash: %w", err)
			}

			return &LivePhotoComponents{
				ImagePath:    filePath,
				VideoPath:    "", // Embedded video
				ImageData:    imageData,
				VideoData:    motionVideo,
				ImageHash:    imageHash,
				VideoHash:    videoHash,
				CreationTime: getFileModTime(filePath),
				IsMotion:     true,
			}, nil
		}
	}

	// Check for Apple Live Photo (separate image and video files)
	dir := filepath.Dir(filePath)
	baseName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))

	// Look for matching image/video pair
	var imagePath, videoPath string
	var imageData, videoData []byte
	var imageHash, videoHash string

	if fileType == types.FileTypeImage {
		imagePath = filePath
		videoPath = findMatchingVideo(dir, baseName)
	} else {
		videoPath = filePath
		imagePath = findMatchingImage(dir, baseName)
	}

	if imagePath == "" || videoPath == "" {
		// Not a live photo
		return nil, errors.New("not a live photo")
	}

	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read image: %w", err)
	}

	videoData, err = os.ReadFile(videoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read video: %w", err)
	}

	imageHash, err = computeHashFromBytes(imageData)
	if err != nil {
		return nil, fmt.Errorf("failed to compute image hash: %w", err)
	}

	videoHash, err = computeHashFromBytes(videoData)
	if err != nil {
		return nil, fmt.Errorf("failed to compute video hash: %w", err)
	}

	return &LivePhotoComponents{
		ImagePath:    imagePath,
		VideoPath:    videoPath,
		ImageData:    imageData,
		VideoData:    videoData,
		ImageHash:    imageHash,
		VideoHash:    videoHash,
		CreationTime: getFileModTime(imagePath),
		IsMotion:     false,
	}, nil
}

// IsLivePhoto checks if a file is part of a live photo
func IsLivePhoto(filePath string) bool {
	_, err := DetectLivePhoto(filePath)
	return err == nil
}

// CreateLivePhotoZip creates a ZIP file containing image and video for live photo upload
// Returns the ZIP data and the base filename with image extension (e.g., "IMG_9914.heic")
func CreateLivePhotoZip(imagePath, videoPath string) ([]byte, string, error) {
	// Use image filename with its original extension (not .zip)
	imageFileName := filepath.Base(imagePath)

	// Create ZIP in memory
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	// Add image file to ZIP
	zipImageName := fmt.Sprintf("%s.image%s", strings.TrimSuffix(imageFileName, filepath.Ext(imageFileName)), filepath.Ext(imagePath))
	if err := addFileToZip(zipWriter, zipImageName, imagePath); err != nil {
		return nil, "", fmt.Errorf("failed to add image to ZIP: %w", err)
	}

	// Add video file to ZIP
	zipVideoName := fmt.Sprintf("%s.video%s", strings.TrimSuffix(imageFileName, filepath.Ext(imageFileName)), filepath.Ext(videoPath))
	if err := addFileToZip(zipWriter, zipVideoName, videoPath); err != nil {
		return nil, "", fmt.Errorf("failed to add video to ZIP: %w", err)
	}

	if err := zipWriter.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close ZIP writer: %w", err)
	}

	return buf.Bytes(), imageFileName, nil
}

// addFileToZip adds a file to the ZIP archive without compression (store mode)
func addFileToZip(zipWriter *zip.Writer, entryName, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	// Create ZIP entry with Store mode (no compression)
	// This is important since image/video files are already compressed
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return fmt.Errorf("failed to create ZIP header: %w", err)
	}
	header.Name = entryName
	header.Method = zip.Store // No compression

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("failed to create ZIP entry: %w", err)
	}

	if _, err := io.Copy(writer, file); err != nil {
		return fmt.Errorf("failed to write file to ZIP: %w", err)
	}

	return nil
}

// ExtractMotionPhotoVideo extracts embedded video from Google Motion Photo
// Google Motion Photos store video data in the XMP metadata of JPEG/HEIC files
func ExtractMotionPhotoVideo(filePath string) ([]byte, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Look for XMP metadata in JPEG files
	if isJPEGFile(filePath) {
		return extractVideoFromJPEG(data)
	}

	// For HEIC files, would need a more complex parser
	// For now, return empty to indicate not a motion photo
	return nil, nil
}

// extractVideoFromJPEG extracts video from XMP metadata in JPEG files
func extractVideoFromJPEG(data []byte) ([]byte, error) {
	// JPEG markers: 0xFFE1 = APP1, 0xFFE2 = APP2, etc.
	// XMP is usually in APP1 (0xFFE1)

	i := 0
	for i < len(data)-1 {
		// Look for JPEG marker
		if data[i] == 0xFF && i+1 < len(data) {
			marker := data[i+1]

			// Check for APP1 (0xE1) marker
			if marker == 0xE1 && i+3 < len(data) {
				length := int(binary.BigEndian.Uint16(data[i+2 : i+4]))

				// Check if this segment contains XMP
				if i+4+length <= len(data) {
					segment := data[i+4 : i+4+length]

					// Look for XMP header
					xmpHeader := []byte("http://ns.adobe.com/xap/1.0/")
					if bytes.Contains(segment, xmpHeader) {
						// Look for MotionPhoto marker
						if bytes.Contains(segment, []byte(MotionPhotoMarker)) {
							// Try to find embedded video
							// This is a simplified implementation
							// Real implementation would parse the XMP structure
							return nil, nil
						}
					}
				}
			}
		}
		i++
	}

	return nil, nil
}

// isJPEGFile checks if a file is a JPEG
func isJPEGFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".jpg" || ext == ".jpeg" || ext == ".jpe" || ext == ".jfif"
}

// findMatchingVideo finds a video file matching the given base name
func findMatchingVideo(dir, baseName string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		if livephoto.GetFileType(filename) == types.FileTypeVideo {
			// Check for live photo suffixes
			if livephoto.MatchLivePhoto(filepath.Join(dir, baseName+".jpg"), filepath.Join(dir, filename)) {
				return filepath.Join(dir, filename)
			}
		}
	}

	return ""
}

// findMatchingImage finds an image file matching the given base name
func findMatchingImage(dir, baseName string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		if livephoto.GetFileType(filename) == types.FileTypeImage {
			// Check for live photo suffixes
			if livephoto.MatchLivePhoto(filepath.Join(dir, filename), filepath.Join(dir, baseName+".mov")) {
				return filepath.Join(dir, filename)
			}
		}
	}

	return ""
}

// computeHashFromBytes computes the blake2b hash of data
func computeHashFromBytes(data []byte) (string, error) {
	return hash.ComputeHashFromBytes(data)
}

// getFileModTime returns the modification time of a file as Unix microseconds
func getFileModTime(filePath string) int64 {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0
	}
	return info.ModTime().UnixMicro()
}

// GetLivePhotoHash returns the combined hash for a live photo
func GetLivePhotoHash(components *LivePhotoComponents) string {
	return components.ImageHash + ":" + components.VideoHash
}

// LivePhotoUploadData represents the data needed to upload a live photo
type LivePhotoUploadData struct {
	ImageEncrypted    []byte
	ImageObjectKey    string
	ImageHeader       []byte
	VideoEncrypted    []byte
	VideoObjectKey    string
	VideoHeader       []byte
	ThumbnailEncrypted []byte
	ThumbnailObjectKey string
	ThumbnailHeader   []byte
}

// PrepareLivePhotoUpload prepares a live photo for upload
func PrepareLivePhotoUpload(components *LivePhotoComponents, fileKey []byte) (*LivePhotoUploadData, error) {
	// Encrypt image
	imageEncrypted, imageHeader, err := EncryptData(components.ImageData, fileKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt image: %w", err)
	}

	// Encrypt video
	videoEncrypted, videoHeader, err := EncryptData(components.VideoData, fileKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt video: %w", err)
	}

	// For live photos, use image as thumbnail
	thumbnailEncrypted := imageEncrypted
	thumbnailHeader := imageHeader

	return &LivePhotoUploadData{
		ImageEncrypted:     imageEncrypted,
		ImageHeader:        imageHeader,
		VideoEncrypted:     videoEncrypted,
		VideoHeader:        videoHeader,
		ThumbnailEncrypted: thumbnailEncrypted,
		ThumbnailHeader:    thumbnailHeader,
	}, nil
}
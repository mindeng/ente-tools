package upload

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// FileCategory represents the file type category
type FileCategory int

const (
	FileCategoryImage FileCategory = 0
	FileCategoryVideo FileCategory = 1
	FileCategoryLivePhoto FileCategory = 2
)

// FileMetadata represents encrypted metadata for a file
type FileMetadata struct {
	Title            string       `json:"title"`
	CreationTime     int64        `json:"creationTime"`
	ModificationTime int64        `json:"modificationTime"`
	FileType         FileCategory `json:"fileType"`
	Hash             string       `json:"hash,omitempty"`
	Duration         int64        `json:"duration,omitempty"` // Video duration in seconds (ceil)
	Latitude         float64      `json:"latitude,omitempty"`
	Longitude        float64      `json:"longitude,omitempty"`
}

// Metadata represents the metadata structure for upload
type Metadata struct {
	EncryptedData    string `json:"encryptedData"`
	DecryptionHeader string `json:"decryptionHeader"`
}

// PublicMagicMetadata represents public magic metadata
type PublicMagicMetadata struct {
	EditedName    string  `json:"editedName"`
	EditedTime    int64   `json:"editedTime"`
	Caption       string  `json:"caption,omitempty"`
	Lat           float64 `json:"lat,omitempty"`
	Long          float64 `json:"long,omitempty"`
	Width         int64   `json:"width,omitempty"`
	Height        int64   `json:"height,omitempty"`
	Duration      int64   `json:"duration,omitempty"`
	EXIFMake      string  `json:"exifMake,omitempty"`
	EXIFModel     string  `json:"exifModel,omitempty"`
}

// LivePhotoMetadata represents metadata specific to live photos
type LivePhotoMetadata struct {
	ImageHash string `json:"imageHash"`
	VideoHash string `json:"videoHash"`
}

// BuildFileMetadata builds file metadata from file info
func BuildFileMetadata(title string, fileType FileCategory, creationTime time.Time, hash string) *FileMetadata {
	return &FileMetadata{
		Title:            title,
		CreationTime:     creationTime.UnixMicro(),
		ModificationTime: time.Now().UnixMicro(),
		FileType:         fileType,
		Hash:             hash,
	}
}

// BuildLivePhotoMetadata builds metadata for a live photo
func BuildLivePhotoMetadata(title string, creationTime time.Time, imageHash, videoHash string) *FileMetadata {
	combinedHash := fmt.Sprintf("%s:%s", imageHash, videoHash)
	return &FileMetadata{
		Title:            title,
		CreationTime:     creationTime.UnixMicro(),
		ModificationTime: time.Now().UnixMicro(),
		FileType:         FileCategoryLivePhoto,
		Hash:             combinedHash,
	}
}

// EncryptMetadata encrypts file metadata
func EncryptMetadata(metadata *FileMetadata, key []byte) (Metadata, error) {
	// Marshal metadata to JSON
	data, err := json.Marshal(metadata)
	if err != nil {
		return Metadata{}, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Encrypt using secretstream (XChaCha20-Poly1305)
	ciphertext, header, err := EncryptData(data, key)
	if err != nil {
		return Metadata{}, fmt.Errorf("failed to encrypt metadata: %w", err)
	}

	return Metadata{
		EncryptedData:    base64.StdEncoding.EncodeToString(ciphertext),
		DecryptionHeader: base64.StdEncoding.EncodeToString(header),
	}, nil
}

// EncryptPublicMagicMetadata encrypts public magic metadata
func EncryptPublicMagicMetadata(pubMeta *PublicMagicMetadata, key []byte) (Metadata, error) {
	// Marshal to JSON
	data, err := json.Marshal(pubMeta)
	if err != nil {
		return Metadata{}, fmt.Errorf("failed to marshal public metadata: %w", err)
	}

	// Encrypt using secretstream (XChaCha20-Poly1305)
	ciphertext, header, err := EncryptData(data, key)
	if err != nil {
		return Metadata{}, fmt.Errorf("failed to encrypt public metadata: %w", err)
	}

	return Metadata{
		EncryptedData:    base64.StdEncoding.EncodeToString(ciphertext),
		DecryptionHeader: base64.StdEncoding.EncodeToString(header),
	}, nil
}

// CreatePublicMagicMetadata creates public magic metadata from file info
func CreatePublicMagicMetadata(width, height int64, duration int64) *PublicMagicMetadata {
	return &PublicMagicMetadata{
		EditedName: "",
		EditedTime: 0,
		Width:      width,
		Height:     height,
		Duration:   duration,
	}
}

// GetDefaultCreationTime returns the current time as creation time if not available
func GetDefaultCreationTime() time.Time {
	return time.Now()
}

// GetFileCategory returns the file category based on type
func GetFileCategory(isVideo bool, isLivePhoto bool) FileCategory {
	if isLivePhoto {
		return FileCategoryLivePhoto
	}
	if isVideo {
		return FileCategoryVideo
	}
	return FileCategoryImage
}

// ParseLivePhotoHashes parses the combined hash format "imageHash:videoHash"
func ParseLivePhotoHashes(combinedHash string) (imageHash, videoHash string, err error) {
	parts := splitHash(combinedHash)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid live photo hash format: %s", combinedHash)
	}
	return parts[0], parts[1], nil
}

func splitHash(hash string) []string {
	var result []string
	var current bytes.Buffer
	for i := 0; i < len(hash); i++ {
		if hash[i] == ':' {
			result = append(result, current.String())
			current.Reset()
		} else {
			current.WriteByte(hash[i])
		}
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}
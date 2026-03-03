package metasync

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	TokenHeader     = "X-Auth-Token"
	ClientPkgHeader = "X-Client-Package"
)

// APIConfig holds configuration for the API client
type APIConfig struct {
	BaseURL string
	Token   string
	App     string
}

// APIClient handles communication with the ente API
type APIClient struct {
	client *resty.Client
	config APIConfig
}

// NewAPIClient creates a new API client
func NewAPIClient(config APIConfig) *APIClient {
	client := resty.New().
		SetBaseURL(config.BaseURL).
		SetHeader(ClientPkgHeader, getClientPackage(config.App))

	if config.Token != "" {
		client.SetHeader(TokenHeader, config.Token)
	}

	return &APIClient{
		client: client,
		config: config,
	}
}

func getClientPackage(app string) string {
	switch app {
	case "photos":
		return "io.ente.photos"
	case "auth":
		return "io.ente.auth"
	case "locker":
		return "io.ente.locker"
	default:
		return "io.ente.photos"
	}
}

// Collection represents a photo album/collection
type Collection struct {
	ID                  int64                  `json:"id"`
	Owner               Owner                  `json:"owner"`
	EncryptedKey        string                 `json:"encryptedKey"`
	KeyDecryptionNonce  string                 `json:"keyDecryptionNonce"`
	Name                string                 `json:"name"`
	EncryptedName       string                 `json:"encryptedName"`
	NameDecryptionNonce string                 `json:"nameDecryptionNonce"`
	Type                string                 `json:"type"`
	MagicMetadata       map[string]interface{} `json:"magicMetadata"`
	IsDeleted           bool                   `json:"isDeleted"`
	UpdatedTime         int64                  `json:"updationTime"`
	IsShared            bool                   `json:"isShared"`
}

// Owner represents a user who owns a collection
type Owner struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
}

// FileAttributes represents an encrypted file item
type FileAttributes struct {
	EncryptedData    string `json:"encryptedData,omitempty"`
	DecryptionHeader string `json:"decryptionHeader" binding:"required"`
}

// MagicMetadata represents encrypted magic metadata
type MagicMetadata struct {
	Data   string `json:"data"`
	Header string `json:"header"`
}

// File represents a photo or video file
type File struct {
	ID                 int64           `json:"id"`
	OwnerID            int64           `json:"ownerID"`
	CollectionID       int64           `json:"collectionID"`
	CollectionOwnerID  *int64          `json:"collectionOwnerID"`
	EncryptedKey       string          `json:"encryptedKey"`
	KeyDecryptionNonce string          `json:"keyDecryptionNonce"`
	File               FileAttributes  `json:"file" binding:"required"`
	Thumbnail          FileAttributes  `json:"thumbnail" binding:"required"`
	Metadata           FileAttributes  `json:"metadata" binding:"required"`
	IsDeleted          bool            `json:"isDeleted"`
	UpdationTime       int64           `json:"updationTime"`
	MagicMetadata      *MagicMetadata  `json:"magicMetadata,omitempty"`
	PubicMagicMetadata *MagicMetadata  `json:"pubMagicMetadata,omitempty"`
	Info               *FileInfo       `json:"info,omitempty"`
}

// FileInfo contains file size information
type FileInfo struct {
	FileSize      int64 `json:"fileSize,omitempty"`
	ThumbnailSize int64 `json:"thumbSize,omitempty"`
}

// GetCollections retrieves collections from the API
func (c *APIClient) GetCollections(ctx context.Context, sinceTime int64) ([]Collection, error) {
	var res struct {
		Collections []Collection `json:"collections"`
	}

	resp, err := c.client.R().
		SetContext(ctx).
		SetQueryParam("sinceTime", strconv.FormatInt(sinceTime, 10)).
		SetResult(&res).
		Get("/collections/v2")
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode(), resp.String())
	}

	return res.Collections, nil
}

// GetFiles retrieves files from a collection
func (c *APIClient) GetFiles(ctx context.Context, collectionID, sinceTime int64) ([]File, bool, error) {
	var res struct {
		Files   []File `json:"diff"`
		HasMore bool   `json:"hasMore"`
	}

	resp, err := c.client.R().
		SetContext(ctx).
		SetQueryParam("sinceTime", strconv.FormatInt(sinceTime, 10)).
		SetQueryParam("collectionID", strconv.FormatInt(collectionID, 10)).
		SetResult(&res).
		Get("/collections/v2/diff")
	if err != nil {
		return nil, false, fmt.Errorf("request failed: %w", err)
	}

	if resp.IsError() {
		return nil, false, fmt.Errorf("API error (status %d): %s", resp.StatusCode(), resp.String())
	}

	return res.Files, res.HasMore, nil
}

// DecryptedFile represents a file with decrypted metadata
type DecryptedFile struct {
	ID               int64
	CollectionID     int64
	OwnerID          int64
	Title            string
	Description      *string
	CreationTime     time.Time
	ModificationTime time.Time
	Latitude         *float64
	Longitude        *float64
	FileType         string
	FileSize         int64
	Hash             *string
	EXIFMake         *string
	EXIFModel        *string
	IsDeleted        bool
}

// DecryptedCollection represents a collection with decrypted name
type DecryptedCollection struct {
	ID                 int64
	OwnerID            int64
	Name               string
	Type               string
	IsShared           bool
	IsDeleted          bool
	UpdatedTime        time.Time
	EncryptedKey       string
	KeyDecryptionNonce string
}

// DecryptFile decrypts a file's metadata using the collection key
func DecryptFile(file File, collectionKey []byte) (*DecryptedFile, error) {
	// First, decrypt the file key using the collection key
	fileKey, err := SecretBoxOpen(
		decodeBase64(file.EncryptedKey),
		decodeBase64(file.KeyDecryptionNonce),
		collectionKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt file key: %w", err)
	}

	// Decrypt metadata
	var metadata map[string]interface{}
	if file.Metadata.DecryptionHeader != "" && file.Metadata.EncryptedData != "" {
		_, metadataBytes, err := DecryptChaChaBase64(file.Metadata.EncryptedData, fileKey, file.Metadata.DecryptionHeader)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt metadata: %w", err)
		}
		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	// Decrypt magic metadata (private)
	var privateMetadata map[string]interface{}
	if file.MagicMetadata != nil && file.MagicMetadata.Header != "" {
		_, magicBytes, err := DecryptChaChaBase64(file.MagicMetadata.Data, fileKey, file.MagicMetadata.Header)
		if err != nil {
			// Magic metadata may not exist for all files, don't fail
			privateMetadata = make(map[string]interface{})
		} else {
			if err := json.Unmarshal(magicBytes, &privateMetadata); err != nil {
				privateMetadata = make(map[string]interface{})
			}
		}
	}

	// Decrypt public magic metadata
	var publicMetadata map[string]interface{}
	if file.PubicMagicMetadata != nil && file.PubicMagicMetadata.Header != "" {
		_, pubMagicBytes, err := DecryptChaChaBase64(file.PubicMagicMetadata.Data, fileKey, file.PubicMagicMetadata.Header)
		if err != nil {
			// Public magic metadata may not exist for all files, don't fail
			publicMetadata = make(map[string]interface{})
		} else {
			if err := json.Unmarshal(pubMagicBytes, &publicMetadata); err != nil {
				publicMetadata = make(map[string]interface{})
			}
		}
	}

	// Decrypt file title from metadata
	title := ""
	if metadata != nil {
		if t, ok := metadata["title"].(string); ok {
			title = t
		}
	}

	// Check for edited name in public metadata
	if publicMetadata != nil {
		if editedName, ok := publicMetadata["editedName"].(string); ok {
			title = editedName
		}
	}

	// Get description (caption)
	var description *string
	// First check metadata for caption
	if metadata != nil {
		if caption, ok := metadata["caption"].(string); ok && caption != "" {
			description = &caption
		}
	}
	// Also check publicMetadata for caption
	if description == nil && publicMetadata != nil {
		if caption, ok := publicMetadata["caption"].(string); ok && caption != "" {
			description = &caption
		}
	}

	// Get creation time
	creationTime := time.Now()
	if publicMetadata != nil {
		if editedTime, ok := publicMetadata["editedTime"].(float64); ok && editedTime != 0 {
			creationTime = time.UnixMicro(int64(editedTime))
		}
	}
	if metadata != nil {
		if ct, ok := metadata["creationTime"].(float64); ok {
			creationTime = time.UnixMicro(int64(ct))
		}
	}

	// Get modification time
	modificationTime := time.Now()
	if metadata != nil {
		if mt, ok := metadata["modificationTime"].(float64); ok {
			modificationTime = time.UnixMicro(int64(mt))
		}
	}

	// Get location
	var lat, long *float64
	// First check metadata (where ente stores lat/long)
	if metadata != nil {
		if latitude, ok := metadata["latitude"].(float64); ok {
			if longitude, ok := metadata["longitude"].(float64); ok {
				if latitude != 0 || longitude != 0 {
					lat = &latitude
					long = &longitude
				}
			}
		}
	}
	// Also check publicMetadata for lat/long (alternative location)
	if lat == nil && publicMetadata != nil {
		if latitude, ok := publicMetadata["lat"].(float64); ok {
			if longitude, ok := publicMetadata["long"].(float64); ok {
				if latitude != 0 || longitude != 0 {
					lat = &latitude
					long = &longitude
				}
			}
		}
	}

	// Get file type
	fileType := "image"
	if metadata != nil {
		if ft, ok := metadata["fileType"].(float64); ok {
			switch int8(ft) {
			case 0:
				fileType = "image"
			case 1:
				fileType = "video"
			case 2:
				fileType = "livephoto"
			}
		}
	}

	// Get file size
	fileSize := int64(0)
	if file.Info != nil && file.Info.FileSize > 0 {
		fileSize = file.Info.FileSize
	}

	// Get hash
	var hash *string
	if metadata != nil {
		if h, ok := metadata["hash"].(string); ok && h != "" {
			hash = &h
		} else {
			// Check for live photo hash
			if imgHash, ok := metadata["imageHash"].(string); ok {
				if vidHash, ok := metadata["videoHash"].(string); ok {
					combinedHash := fmt.Sprintf("%s:%s", imgHash, vidHash)
					hash = &combinedHash
				}
			}
		}
	}

	// EXIF data - if available in metadata, store it
	var exifMake, exifModel *string
	if publicMetadata != nil {
		// Try ente's field names first
		if make, ok := publicMetadata["cameraMake"].(string); ok && make != "" {
			exifMake = &make
		} else if make, ok := publicMetadata["exifMake"].(string); ok && make != "" {
			exifMake = &make
		}
		if model, ok := publicMetadata["cameraModel"].(string); ok && model != "" {
			exifModel = &model
		} else if model, ok := publicMetadata["exifModel"].(string); ok && model != "" {
			exifModel = &model
		}
	}

	return &DecryptedFile{
		ID:               file.ID,
		OwnerID:          file.OwnerID,
		Title:            title,
		Description:      description,
		CreationTime:     creationTime,
		ModificationTime: modificationTime,
		Latitude:         lat,
		Longitude:        long,
		FileType:         fileType,
		FileSize:         fileSize,
		Hash:             hash,
		EXIFMake:         exifMake,
		EXIFModel:        exifModel,
		IsDeleted:        false,
	}, nil
}

// DecryptCollectionName decrypts a collection's name
func DecryptCollectionName(collection Collection, collectionKey []byte) (string, error) {
	// If there's an encrypted name, decrypt it using SecretBox
	if collection.EncryptedName != "" {
		decrypted, err := SecretBoxOpenBase64(collection.EncryptedName, collection.NameDecryptionNonce, collectionKey)
		if err != nil {
			return "", err
		}
		return string(decrypted), nil
	}

	// Fallback: use the plain name (early beta users might have collections without encrypted names)
	if collection.Name != "" {
		return collection.Name, nil
	}

	// Last resort: use ID as name
	return fmt.Sprintf("Collection %d", collection.ID), nil
}

// GetCollectionKey decrypts a collection's key
func GetCollectionKey(collection Collection, masterKey, secretKey, publicKey []byte, userID int64) ([]byte, error) {
	if collection.Owner.ID == userID {
		// Own collection: use master key
		cipherBytes, _ := base64.StdEncoding.DecodeString(collection.EncryptedKey)
		nonce, _ := base64.StdEncoding.DecodeString(collection.KeyDecryptionNonce)
		return SecretBoxOpen(cipherBytes, nonce, masterKey)
	}

	// Shared collection: use sealed box with public/secret key
	cipherBytes, _ := base64.StdEncoding.DecodeString(collection.EncryptedKey)
	return SealedBoxOpen(cipherBytes, publicKey, secretKey)
}

// decodeBase64 decodes a base64 string
func decodeBase64(s string) []byte {
	b, _ := base64.StdEncoding.DecodeString(s)
	return b
}
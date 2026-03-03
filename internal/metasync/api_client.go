package metasync

import (
	"context"
	"encoding/base64"
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
	EncryptedName       string                 `json:"encryptedName"`
	NameDecryptionNonce string                 `json:"nameDecryptionNonce"`
	MagicMetadata       map[string]interface{} `json:"magicMetadata"`
	IsShared            bool                   `json:"isShared"`
	IsDeleted           bool                   `json:"isDeleted"`
	UpdatedTime         int64                  `json:"updatedTime"`
}

// Owner represents a user who owns a collection
type Owner struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
}

// File represents a photo or video file
type File struct {
	ID              int64                  `json:"id"`
	OwnerID         int64                  `json:"ownerID"`
	Key             EncString              `json:"key"`
	LastUpdateTime  int64                  `json:"lastUpdateTime"`
	FileNonce       string                 `json:"fileNonce"`
	ThumbnailNonce  string                 `json:"thumbnailNonce"`
	Metadata        map[string]interface{} `json:"metadata"`
	PrivateMetadata map[string]interface{} `json:"privateMetadata"`
	PublicMetadata  map[string]interface{} `json:"publicMetadata"`
	Info            FileInfo               `json:"info"`
}

// FileInfo contains file size information
type FileInfo struct {
	FileSize      int64 `json:"fileSize"`
	ThumbnailSize int64 `json:"thumbSize"`
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
	ID              int64
	CollectionID    int64
	OwnerID         int64
	Title           string
	Description     *string
	CreationTime    time.Time
	ModificationTime time.Time
	Latitude        *float64
	Longitude       *float64
	FileType        string
	FileSize        int64
	Hash            *string
	EXIFMake        *string
	EXIFModel       *string
	IsDeleted       bool
}

// DecryptedCollection represents a collection with decrypted name
type DecryptedCollection struct {
	ID         int64
	OwnerID    int64
	Name       string
	IsShared   bool
	IsDeleted  bool
	UpdatedTime time.Time
}

// DecryptFile decrypts a file's metadata using the collection key
func DecryptFile(file File, collectionKey []byte) (*DecryptedFile, error) {
	// Decrypt file title from metadata
	title := ""
	if t, ok := file.Metadata["title"].(string); ok {
		title = t
	}

	// Check for edited name in public metadata
	if pubMeta := file.PublicMetadata; pubMeta != nil {
		if editedName, ok := pubMeta["editedName"].(string); ok {
			title = editedName
		}
	}

	// Get description
	var description *string
	if pubMeta := file.PublicMetadata; pubMeta != nil {
		if caption, ok := pubMeta["caption"].(string); ok && caption != "" {
			description = &caption
		}
	}

	// Get creation time
	creationTime := time.Now()
	if pubMeta := file.PublicMetadata; pubMeta != nil {
		if editedTime, ok := pubMeta["editedTime"].(float64); ok && editedTime != 0 {
			creationTime = time.UnixMicro(int64(editedTime))
		}
	}
	if ct, ok := file.Metadata["creationTime"].(float64); ok {
		creationTime = time.UnixMicro(int64(ct))
	}

	// Get modification time
	modificationTime := time.Now()
	if mt, ok := file.Metadata["modificationTime"].(float64); ok {
		modificationTime = time.UnixMicro(int64(mt))
	}

	// Get location
	var lat, long *float64
	if pubMeta := file.PublicMetadata; pubMeta != nil {
		if latitude, ok := pubMeta["lat"].(float64); ok {
			if longitude, ok := pubMeta["long"].(float64); ok {
				if latitude != 0 || longitude != 0 {
					lat = &latitude
					long = &longitude
				}
			}
		}
	}

	// Get file type
	fileType := "image"
	if ft, ok := file.Metadata["fileType"].(float64); ok {
		switch int8(ft) {
		case 0:
			fileType = "image"
		case 1:
			fileType = "video"
		case 2:
			fileType = "livephoto"
		}
	}

	// Get file size
	fileSize := int64(0)
	if file.Info.FileSize > 0 {
		fileSize = file.Info.FileSize
	}

	// Get hash
	var hash *string
	if h, ok := file.Metadata["hash"].(string); ok {
		hash = &h
	} else {
		// Check for live photo hash
		if imgHash, ok := file.Metadata["imageHash"].(string); ok {
			if vidHash, ok := file.Metadata["videoHash"].(string); ok {
				combinedHash := fmt.Sprintf("%s:%s", imgHash, vidHash)
				hash = &combinedHash
			}
		}
	}

	// EXIF data - if available in metadata, store it
	var exifMake, exifModel *string
	if pubMeta := file.PublicMetadata; pubMeta != nil {
		if make, ok := pubMeta["exifMake"].(string); ok && make != "" {
			exifMake = &make
		}
		if model, ok := pubMeta["exifModel"].(string); ok && model != "" {
			exifModel = &model
		}
	}

	return &DecryptedFile{
		ID:              file.ID,
		OwnerID:         file.OwnerID,
		Title:           title,
		Description:     description,
		CreationTime:    creationTime,
		ModificationTime: modificationTime,
		Latitude:        lat,
		Longitude:       long,
		FileType:        fileType,
		FileSize:        fileSize,
		Hash:            hash,
		EXIFMake:        exifMake,
		EXIFModel:       exifModel,
		IsDeleted:       false,
	}, nil
}

// DecryptCollectionName decrypts a collection's name
func DecryptCollectionName(collection Collection, key []byte) (string, error) {
	// Try to decrypt the encrypted name
	decrypted, err := decryptBase64Data(collection.EncryptedName, collection.NameDecryptionNonce, key)
	if err == nil {
		return string(decrypted), nil
	}

	// Fallback: try magic metadata
	if collection.MagicMetadata != nil {
		if name, ok := collection.MagicMetadata["name"].(string); ok {
			return name, nil
		}
	}

	// Last resort: use ID as name
	return fmt.Sprintf("Collection %d", collection.ID), nil
}

// decryptBase64Data decrypts base64-encoded data with nonce
func decryptBase64Data(cipherText, nonceBase64 string, key []byte) ([]byte, error) {
	cipherBytes, err := base64.StdEncoding.DecodeString(cipherText)
	if err != nil {
		return nil, err
	}

	nonce, err := base64.StdEncoding.DecodeString(nonceBase64)
	if err != nil {
		return nil, err
	}

	return decryptChaCha20Poly1305(cipherBytes, key, nonce)
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
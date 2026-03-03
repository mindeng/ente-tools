package upload

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"
	"golang.org/x/crypto/chacha20"
	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/crypto/poly1305"
)

const (
	// Stream encryption constants from libsodium
	TagMessage = 0
	TagPush    = 0x01
	TagRekey   = 0x02
	TagFinal   = TagPush | TagRekey

	StreamKeyBytes    = 32
	StreamHeaderBytes = 24
	ABYTES            = 17  // 1 tag + 16 poly1305 MAC
	ChunkSize         = 4 * 1024 * 1024 // 4MB chunks for file encryption (must match web)

	cryptoCoreHchacha20InputBytes       = 16
	cryptoSecretStreamXchacha20poly1305Counterbytes = 4
)

var pad0 [16]byte

type streamState struct {
	k     [StreamKeyBytes]byte
	nonce [12]byte
	pad   [8]byte
}

type encryptor struct {
	streamState
}

func (s *streamState) reset() {
	for i := range s.nonce {
		s.nonce[i] = 0
	}
	s.nonce[0] = 1
}

func xorBuf(out, in []byte) {
	for i := range out {
		out[i] ^= in[i]
	}
}

func bufInc(n []byte) {
	c := 1
	for i := range n {
		c += int(n[i])
		n[i] = byte(c)
		c >>= 8
	}
}

func memZero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// newEncryptor creates a new stream encryptor
func newEncryptor(key []byte) (*encryptor, []byte, error) {
	if len(key) != StreamKeyBytes {
		return nil, nil, fmt.Errorf("invalid key length: expected 32, got %d", len(key))
	}

	header := make([]byte, StreamHeaderBytes)
	if _, err := rand.Read(header); err != nil {
		return nil, nil, fmt.Errorf("failed to generate header: %w", err)
	}

	stream := &encryptor{}

	// Derive stream key using HChaCha20
	streamKey, err := chacha20.HChaCha20(key, header[:16])
	if err != nil {
		return nil, nil, fmt.Errorf("HChaCha20 error: %w", err)
	}
	copy(stream.k[:], streamKey)
	stream.reset()

	// Copy inonce from header (8 bytes)
	for i, b := range header[cryptoCoreHchacha20InputBytes:] {
		stream.nonce[i+cryptoSecretStreamXchacha20poly1305Counterbytes] = b
	}

	return stream, header, nil
}

// push encrypts a chunk of data with the given tag
func (s *encryptor) push(plain []byte, tag byte) ([]byte, error) {
	var block [64]byte
	var slen [8]byte

	mlen := len(plain)
	out := make([]byte, mlen+ABYTES)

	// Create cipher for this chunk
	chachaCipher, err := chacha20.NewUnauthenticatedCipher(s.k[:], s.nonce[:])
	if err != nil {
		return nil, err
	}

	// Generate Poly1305 key
	chachaCipher.XORKeyStream(block[:], block[:])
	var polyInit [32]byte
	copy(polyInit[:], block[:])
	poly := poly1305.New(&polyInit)

	// Encrypt and authenticate
	memZero(block[:])
	block[0] = tag
	chachaCipher.XORKeyStream(block[:], block[:])
	out[0] = block[0]
	poly.Write(block[:])

	// Encrypt data
	c := out[1:]
	chachaCipher.XORKeyStream(c, plain)
	poly.Write(c[:mlen])

	// Padding
	padLen := (0x10 - len(block) + mlen) & 0xf
	poly.Write(pad0[:padLen])

	// Write lengths
	binary.LittleEndian.PutUint64(slen[:], 0)
	poly.Write(slen[:])

	binary.LittleEndian.PutUint64(slen[:], uint64(len(block)+mlen))
	poly.Write(slen[:])

	// Copy MAC
	mac := c[mlen:]
	copy(mac, poly.Sum(nil))

	// Update nonce
	xorBuf(s.nonce[cryptoSecretStreamXchacha20poly1305Counterbytes:], mac)
	bufInc(s.nonce[:cryptoSecretStreamXchacha20poly1305Counterbytes])

	return out, nil
}

// EncryptData encrypts data using secretstream format
// For small data (metadata, thumbnails), uses single chunk with TAG_FINAL
// Returns ciphertext and header
func EncryptData(data, key []byte) ([]byte, []byte, error) {
	encryptor, header, err := newEncryptor(key)
	if err != nil {
		return nil, nil, err
	}

	out, err := encryptor.push(data, TagFinal)
	if err != nil {
		return nil, nil, err
	}

	return out, header, nil
}

// EncryptFileData encrypts large file data using chunked secretstream
// Returns ciphertext and header
func EncryptFileData(data []byte, key []byte) ([]byte, []byte, error) {
	encryptor, header, err := newEncryptor(key)
	if err != nil {
		return nil, nil, err
	}

	var buf bytes.Buffer
	bytesRead := 0
	totalLen := len(data)

	for bytesRead < totalLen {
		chunkSize := ChunkSize
		tag := TagMessage

		if bytesRead+chunkSize >= totalLen {
			chunkSize = totalLen - bytesRead
			tag = TagFinal
		}

		chunk := data[bytesRead : bytesRead+chunkSize]
		bytesRead += chunkSize

		encrypted, err := encryptor.push(chunk, byte(tag))
		if err != nil {
			return nil, nil, err
		}
		buf.Write(encrypted)
	}

	return buf.Bytes(), header, nil
}

const (
	// UploadURLPath endpoint for getting upload URLs
	UploadURLPath = "/files/upload-url"
	// FilesPath endpoint for creating files
	FilesPath = "/files"
	// CollectionsAddFilesPath alternative endpoint for adding files
	CollectionsAddFilesPath = "/collections/add-files"
	// MultipartUploadURLPath endpoint for multipart uploads
	MultipartUploadURLPath = "/files/multipart-upload-url"
)

// UploadURLResponse represents the response from upload URL endpoint
type UploadURLResponse struct {
	ObjectKey string `json:"objectKey"`
	URL       string `json:"url"`
}

// MultipartUploadURLResponse represents the response for multipart upload URL
type MultipartUploadURLResponse struct {
	ObjectKey   string   `json:"objectKey"`
	PartURLs    []string `json:"partURLs"`
	CompleteURL string   `json:"completeURL"`
}

// FileAttributes represents encrypted file attributes for upload
type FileAttributes struct {
	ObjectKey        string `json:"objectKey"`
	DecryptionHeader string `json:"decryptionHeader"`
	Size             int64  `json:"size"`
}

// CreateFileRequest represents the request to create a file on the server
type CreateFileRequest struct {
	CollectionID       int64                  `json:"collectionID"`
	EncryptedKey       string                 `json:"encryptedKey"`
	KeyDecryptionNonce string                 `json:"keyDecryptionNonce"`
	File               FileAttributes         `json:"file"`
	Thumbnail          FileAttributes         `json:"thumbnail"`
	Metadata           Metadata               `json:"metadata"`
	UpdationTime       int64                  `json:"updationTime"`
	PubMagicMetadata   map[string]interface{} `json:"pubMagicMetadata,omitempty"`
}

// CreateFileResponse represents the response from creating a file
type CreateFileResponse struct {
	ID             int64 `json:"id"`
	OwnerID        int64 `json:"ownerID"`
	LastUpdateTime int64 `json:"lastUpdateTime"`
}

// UploadClient handles file upload operations
type UploadClient struct {
	client *resty.Client
	token  string
}

// NewUploadClient creates a new upload client
func NewUploadClient(baseURL, token string) *UploadClient {
	client := resty.New().
		SetBaseURL(baseURL).
		SetHeader("Content-Type", "application/json").
		SetHeader("X-Auth-Token", token).
		SetHeader("X-Client-Package", "io.ente.photos")

	return &UploadClient{
		client: client,
		token:  token,
	}
}

// SetDebug enables debug logging for the client
func (c *UploadClient) SetDebug(enabled bool) *UploadClient {
	c.client.SetDebug(enabled)
	return c
}

// GetUploadURL fetches an upload URL for a single file
func (c *UploadClient) GetUploadURL(contentLength int64, contentMD5 string) (*UploadURLResponse, error) {
	var reqBody struct {
		ContentLength int64  `json:"contentLength"`
		ContentMD5    string `json:"contentMD5"`
	}
	reqBody.ContentLength = contentLength
	reqBody.ContentMD5 = contentMD5

	var resp UploadURLResponse
	res, err := c.client.R().
		SetBody(reqBody).
		SetResult(&resp).
		Post(UploadURLPath)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if res.IsError() {
		return nil, fmt.Errorf("API error (status %d): %s", res.StatusCode(), res.String())
	}

	return &resp, nil
}

// UploadFile uploads file data to the given URL
func (c *UploadClient) UploadFile(uploadURL string, data []byte, contentMD5 string) error {
	req, err := http.NewRequest("PUT", uploadURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	if contentMD5 != "" {
		req.Header.Set("Content-MD5", contentMD5)
	}

	client := &http.Client{
		Timeout: 30 * time.Minute,
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("upload failed with status %d: %s", res.StatusCode, string(body))
	}

	return nil
}

// CreateFile creates a file entry on the server after upload
func (c *UploadClient) CreateFile(req CreateFileRequest) (*CreateFileResponse, error) {
	var resp CreateFileResponse
	res, err := c.client.R().
		SetBody(req).
		SetResult(&resp).
		Post(FilesPath)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if res.IsError() {
		return nil, fmt.Errorf("API error (status %d): %s", res.StatusCode(), res.String())
	}

	return &resp, nil
}

// CreateFileWithDebug creates a file entry on the server after upload with debug output
func (c *UploadClient) CreateFileWithDebug(req CreateFileRequest) (*CreateFileResponse, error) {
	var resp CreateFileResponse
	res, err := c.client.R().
		SetBody(req).
		SetResult(&resp).
		SetDebug(true).
		Post(FilesPath)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if res.IsError() {
		return nil, fmt.Errorf("API error (status %d): %s", res.StatusCode(), res.String())
	}

	return &resp, nil
}

// GetMultipartUploadURL fetches URLs for multipart upload
func (c *UploadClient) GetMultipartUploadURL(contentLength, partLength int64, partMd5s []string) (*MultipartUploadURLResponse, error) {
	var reqBody struct {
		ContentLength int64    `json:"contentLength"`
		PartLength    int64    `json:"partLength"`
		PartMd5s      []string `json:"partMd5s"`
	}
	reqBody.ContentLength = contentLength
	reqBody.PartLength = partLength
	reqBody.PartMd5s = partMd5s

	var resp MultipartUploadURLResponse
	res, err := c.client.R().
		SetBody(reqBody).
		SetResult(&resp).
		Post(MultipartUploadURLPath)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if res.IsError() {
		return nil, fmt.Errorf("API error (status %d): %s", res.StatusCode(), res.String())
	}

	return &resp, nil
}

// UploadPart uploads a part of a multipart upload
func (c *UploadClient) UploadPart(partURL string, data []byte, partNum int, contentMD5 string) (string, error) {
	req, err := http.NewRequest("PUT", partURL, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	if contentMD5 != "" {
		req.Header.Set("Content-MD5", contentMD5)
	}

	client := &http.Client{
		Timeout: 10 * time.Minute,
	}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("upload failed with status %d: %s", res.StatusCode, string(body))
	}

	// Return ETag
	etag := res.Header.Get("ETag")
	return etag, nil
}

// CompleteMultipartUpload completes a multipart upload
func (c *UploadClient) CompleteMultipartUpload(completeURL string, parts []MultipartPart) error {
	body, err := buildMultipartCompleteXML(parts)
	if err != nil {
		return fmt.Errorf("failed to build XML: %w", err)
	}

	req, err := http.NewRequest("POST", completeURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "text/xml")

	client := &http.Client{
		Timeout: 5 * time.Minute,
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("completion failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("completion failed with status %d: %s", res.StatusCode, string(body))
	}

	return nil
}

// MultipartPart represents a completed part in multipart upload
type MultipartPart struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"eTag"`
}

// buildMultipartCompleteXML builds the XML body for completing multipart upload
func buildMultipartCompleteXML(parts []MultipartPart) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`<CompleteMultipartUpload>`)
	for _, part := range parts {
		fmt.Fprintf(&buf, `<Part><PartNumber>%d</PartNumber><ETag>%s</ETag></Part>`, part.PartNumber, part.ETag)
	}
	buf.WriteString(`</CompleteMultipartUpload>`)
	return buf.Bytes(), nil
}

// EncryptDataSecretBox encrypts data using NaCl secretbox
// Returns ciphertext and nonce
func EncryptDataSecretBox(data, key []byte) ([]byte, []byte, error) {
	if len(key) != 32 {
		return nil, nil, fmt.Errorf("invalid key length: expected 32, got %d", len(key))
	}

	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	var nonceArr [24]byte
	var keyArr [32]byte
	copy(nonceArr[:], nonce)
	copy(keyArr[:], key)

	ciphertext := secretbox.Seal(nil, data, &nonceArr, &keyArr)

	return ciphertext, nonce, nil
}

// GenerateKey generates a random 32-byte key
func GenerateKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}
	return key, nil
}

// EncryptKey encrypts a key using the master key
func EncryptKey(key, masterKey []byte) (EncryptedKey, error) {
	ciphertext, nonce, err := EncryptDataSecretBox(key, masterKey)
	if err != nil {
		return EncryptedKey{}, err
	}

	return EncryptedKey{
		CipherText: base64.StdEncoding.EncodeToString(ciphertext),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
	}, nil
}

// EncryptedKey represents an encrypted key with nonce
type EncryptedKey struct {
	CipherText string `json:"cipherText"`
	Nonce      string `json:"nonce"`
}

// UploadResult represents the result of an upload operation
type UploadResult struct {
	FileID   int64  `json:"fileId"`
	ObjectID string `json:"objectId"`
}

// UploadToS3 uploads data directly to S3 using a presigned URL
func UploadToS3(uploadURL string, data []byte, contentType string) error {
	req, err := http.NewRequest("PUT", uploadURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", contentType)

	client := &http.Client{
		Timeout: 30 * time.Minute,
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("upload failed with status %d: %s", res.StatusCode, string(body))
	}

	return nil
}

// UploadMultipartFile uploads a file using multipart upload
func (c *UploadClient) UploadMultipartFile(data []byte, partSize int64) (string, error) {
	// Calculate number of parts
	partCount := int((len(data)-1)/int(partSize)) + 1

	// Calculate MD5 for each part
	partMd5s := make([]string, 0, partCount)
	parts := make([]MultipartPart, 0, partCount)

	for i := 0; i < partCount; i++ {
		start := i * int(partSize)
		end := start + int(partSize)
		if end > len(data) {
			end = len(data)
		}
		partData := data[start:end]
		md5 := computeMD5(partData)
		partMd5s = append(partMd5s, md5)
	}

	// Get multipart upload URLs
	multipartURL, err := c.GetMultipartUploadURL(int64(len(data)), partSize, partMd5s)
	if err != nil {
		return "", fmt.Errorf("failed to get multipart upload URLs: %w", err)
	}

	// Upload each part
	for i := 0; i < partCount; i++ {
		start := i * int(partSize)
		end := start + int(partSize)
		if end > len(data) {
			end = len(data)
		}
		partData := data[start:end]

		etag, err := c.UploadPart(multipartURL.PartURLs[i], partData, i+1, partMd5s[i])
		if err != nil {
			return "", fmt.Errorf("failed to upload part %d: %w", i+1, err)
		}

		parts = append(parts, MultipartPart{
			PartNumber: i + 1,
			ETag:       etag,
		})
	}

	// Complete the multipart upload
	if err := c.CompleteMultipartUpload(multipartURL.CompleteURL, parts); err != nil {
		return "", fmt.Errorf("failed to complete multipart upload: %w", err)
	}

	return multipartURL.ObjectKey, nil
}

// computeMD5 computes MD5 hash of data
func computeMD5(data []byte) string {
	h := md5.New()
	h.Write(data)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}


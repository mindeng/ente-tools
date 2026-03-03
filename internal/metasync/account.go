package metasync

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/zalando/go-keyring"
	bolt "go.etcd.io/bbolt"
)

const (
	AccBucket = "accounts"
)

// Account represents an ente CLI account stored in the database
type Account struct {
	Email     string    `json:"email"`
	UserID    int64     `json:"userID"`
	App       string    `json:"app"`
	MasterKey EncString `json:"masterKey"`
	SecretKey EncString `json:"secretKey"`
	PublicKey string    `json:"publicKey"`
	Token     EncString `json:"token"`
	ExportDir string    `json:"exportDir"`
}

// EncString represents encrypted data with ciphertext and nonce
type EncString struct {
	CipherText string `json:"cipherText"`
	Nonce      string `json:"nonce"`
}

// AccountKey returns the key used to store the account in the database
func (a *Account) AccountKey() string {
	return fmt.Sprintf("%s-%d", a.App, a.UserID)
}

// AccSecretInfo holds decrypted account secrets
type AccSecretInfo struct {
	MasterKey []byte
	SecretKey []byte
	Token     []byte
	PublicKey []byte
}

// TokenStr returns the token as a base64 URL encoded string
func (a *AccSecretInfo) TokenStr() string {
	return base64.URLEncoding.EncodeToString(a.Token)
}

// GetDeviceKey retrieves or creates the device key from the system keyring
func GetDeviceKey() ([]byte, error) {
	// Check if custom secrets file is specified
	if secretsFile := os.Getenv("ENTE_CLI_SECRETS_PATH"); secretsFile != "" {
		fmt.Fprintf(os.Stderr, "Using secrets file from ENTE_CLI_SECRETS_PATH: %s\n", secretsFile)
		return getSecretFromFile(secretsFile)
	}

	// Try system keyring
	secret, err := keyring.Get(secretService, secretUser)
	if err != nil {
		if err == keyring.ErrNotFound {
			return nil, fmt.Errorf("ente CLI not configured. Please run 'ente account add' first, or set ENTE_CLI_SECRETS_PATH environment variable")
		}
		return nil, fmt.Errorf("failed to get device key from keyring: %w", err)
	}

	// Try to decode as base64 (new format)
	decoded, err := base64.StdEncoding.DecodeString(secret)
	if err == nil && len(decoded) == 32 {
		return decoded, nil
	}

	// Legacy format (raw bytes)
	if len(secret) == 32 {
		return []byte(secret), nil
	}

	return nil, fmt.Errorf("invalid device key format: expected 32 bytes, got %d", len(secret))
}

func getSecretFromFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read secrets file: %w", err)
	}
	if len(data) != 32 {
		return nil, fmt.Errorf("invalid secrets file: expected 32 bytes, got %d", len(data))
	}
	return data, nil
}

// LoadAccounts loads all accounts from the ente CLI database
func LoadAccounts(dbPath string) ([]Account, error) {
	var accounts []Account

	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(AccBucket))
		if b == nil {
			return fmt.Errorf("no accounts found. Please run 'ente account add' first")
		}

		return b.ForEach(func(k, v []byte) error {
			var acc Account
			if err := json.Unmarshal(v, &acc); err != nil {
				return fmt.Errorf("failed to unmarshal account: %w", err)
			}
			accounts = append(accounts, acc)
			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	return accounts, nil
}

// DecryptChaChaBase64 decrypts base64-encoded ChaCha20-Poly1305 encrypted data
// This matches the ente cli's crypto.DecryptChaChaBase64 function
func DecryptChaChaBase64(data string, key []byte, nonce string) (string, []byte, error) {
	// Decode data from base64
	dataBytes, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", nil, fmt.Errorf("invalid base64 data: %v", err)
	}
	// Decode nonce from base64
	nonceBytes, err := base64.StdEncoding.DecodeString(nonce)
	if err != nil {
		return "", nil, fmt.Errorf("invalid nonce: %v", err)
	}
	// Decrypt data
	decryptedData, err := decryptChaCha20Poly1305(dataBytes, key, nonceBytes)
	if err != nil {
		return "", nil, fmt.Errorf("failed to decrypt data: %v", err)
	}
	return base64.StdEncoding.EncodeToString(decryptedData), decryptedData, nil
}

// decryptEncString decrypts an EncString using the provided key
// This matches the behavior of ente cli's EncString.MustDecrypt()
func decryptEncString(enc EncString, key []byte) ([]byte, error) {
	// Use DecryptChaChaBase64 which is what ente cli uses for EncString
	_, plainBytes, err := DecryptChaChaBase64(enc.CipherText, key, enc.Nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt with ChaCha20: %w", err)
	}

	return plainBytes, nil
}

// DecryptAccountSecrets decrypts the account's secrets using the device key
func DecryptAccountSecrets(account Account, deviceKey []byte) (*AccSecretInfo, error) {
	// Debug: print key length
	if len(deviceKey) != 32 {
		return nil, fmt.Errorf("invalid device key length: expected 32, got %d", len(deviceKey))
	}

	masterKey, err := decryptEncString(account.MasterKey, deviceKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt master key: %w", err)
	}

	secretKey, err := decryptEncString(account.SecretKey, deviceKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt secret key: %w", err)
	}

	token, err := decryptEncString(account.Token, deviceKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt token: %w", err)
	}

	publicKey, err := base64.StdEncoding.DecodeString(account.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode public key: %w", err)
	}

	return &AccSecretInfo{
		MasterKey: masterKey,
		SecretKey: secretKey,
		Token:     token,
		PublicKey: publicKey,
	}, nil
}
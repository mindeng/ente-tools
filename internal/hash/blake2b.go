package hash

import (
	"encoding/base64"
	"io"

	"golang.org/x/crypto/blake2b"
)

const (
	// ChunkSize is the size of each chunk when streaming large files
	ChunkSize = 4 * 1024 * 1024 // 4MB
)

// ComputeHash calculates the Blake2b hash of a file, streaming it in 4MB chunks
// Returns a base64 encoded 64-byte hash (512 bits) to match Ente's implementation
// which uses libsodium's crypto_generichash_BYTES_MAX
func ComputeHash(r io.Reader) (string, error) {
	// Create a new BLAKE2b hash with 64-byte output (Blake2b-512)
	// This matches libsodium's crypto_generichash_BYTES_MAX
	hasher, err := blake2b.New512(nil)
	if err != nil {
		return "", err
	}

	// Read in 4MB chunks to avoid loading entire file into memory
	buf := make([]byte, ChunkSize)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if _, err := hasher.Write(buf[:n]); err != nil {
				return "", err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}

	hashBytes := hasher.Sum(nil)
	return base64.StdEncoding.EncodeToString(hashBytes), nil
}

// ComputeHashFromBytes calculates the Blake2b hash of byte slice
// Returns a base64 encoded 64-byte hash (512 bits) to match Ente's implementation
func ComputeHashFromBytes(data []byte) (string, error) {
	hasher, err := blake2b.New512(nil)
	if err != nil {
		return "", err
	}

	if _, err := hasher.Write(data); err != nil {
		return "", err
	}

	hashBytes := hasher.Sum(nil)
	return base64.StdEncoding.EncodeToString(hashBytes), nil
}

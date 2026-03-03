package database

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"go.etcd.io/bbolt"

	"ente-hashcmp/internal/types"
)

const (
	// filesBucket stores file hash records
	filesBucket = "files"
	// metadataBucket stores scan metadata
	metadataBucket = "metadata"
)

// DB wraps bbolt database operations
type DB struct {
	db *bbolt.DB
}

// Open opens or creates the database for a given directory
func Open(dir string) (*DB, error) {
	dbDir := filepath.Join(dir, ".ente-hashcmp")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dbDir, "db")
	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}

	// Create buckets if they don't exist
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(filesBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(metadataBucket))
		return err
	})

	if err != nil {
		db.Close()
		return nil, err
	}

	return &DB{db: db}, nil
}

// Close closes the database
func (db *DB) Close() error {
	return db.db.Close()
}

// GetPath returns the path to the database file
func GetPath(dir string) string {
	return filepath.Join(dir, ".ente-hashcmp", "db")
}

// PutFile stores or updates a file record
func (db *DB) PutFile(key string, record *types.FileRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}

	return db.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(filesBucket))
		return b.Put([]byte(key), data)
	})
}

// GetFile retrieves a file record
func (db *DB) GetFile(key string) (*types.FileRecord, error) {
	var record types.FileRecord

	err := db.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(filesBucket))
		data := b.Get([]byte(key))
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, &record)
	})

	if err != nil {
		return nil, err
	}

	return &record, nil
}

// GetAllFiles returns all file records
func (db *DB) GetAllFiles() (map[string]*types.FileRecord, error) {
	result := make(map[string]*types.FileRecord)

	err := db.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(filesBucket))
		return b.ForEach(func(k, v []byte) error {
			var record types.FileRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return err
			}
			result[string(k)] = &record
			return nil
		})
	})

	return result, err
}

// DeleteFile removes a file record
func (db *DB) DeleteFile(key string) error {
	return db.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(filesBucket))
		return b.Delete([]byte(key))
	})
}

// PutMetadata stores scan metadata
func (db *DB) PutMetadata(key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return db.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(metadataBucket))
		return b.Put([]byte(key), data)
	})
}

// GetMetadata retrieves scan metadata
func (db *DB) GetMetadata(key string, value interface{}) error {
	return db.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(metadataBucket))
		data := b.Get([]byte(key))
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, value)
	})
}

// NeedsRecalc determines if a file hash needs to be recalculated
// based on size and modification time comparison
func (db *DB) NeedsRecalc(key string, size int64, modTime time.Time) (bool, error) {
	record, err := db.GetFile(key)
	if err != nil {
		return true, err
	}
	if record == nil {
		return true, nil
	}

	// Compare size and modification time
	if record.Size != size {
		return true, nil
	}

	// Compare modification times
	return !record.ModTime.Equal(modTime), nil
}

// FileEntry represents a file entry with its path and hash
type FileEntry struct {
	Path string
	Hash string
}

// GetAllFileEntries returns all file entries (path -> hash)
func (db *DB) GetAllFileEntries() (map[string]string, error) {
	result := make(map[string]string)

	err := db.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(filesBucket))
		return b.ForEach(func(k, v []byte) error {
			var record types.FileRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return err
			}
			result[string(k)] = record.Hash
			return nil
		})
	})

	return result, err
}

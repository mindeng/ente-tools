package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	"ente-tools/internal/types"

	_ "modernc.org/sqlite"
)

// DB wraps SQLite database operations
type DB struct {
	db *sql.DB
}

// Open opens or creates the database for a given directory
func Open(dir string) (*DB, error) {
	dbDir := filepath.Join(dir, ".fhash")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dbDir, "db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Create table if it doesn't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
			key TEXT PRIMARY KEY,
			hash TEXT NOT NULL,
			size INTEGER NOT NULL,
			mod_time INTEGER NOT NULL,
			live_photo_image TEXT,
			live_photo_video TEXT
		);
	`)
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
	return filepath.Join(dir, ".fhash", "db")
}

// PutFile stores or updates a file record
func (db *DB) PutFile(key string, record *types.FileRecord) error {
	var livePhotoImage, livePhotoVideo string
	if record.LivePhotoParts != nil {
		livePhotoImage = record.LivePhotoParts.Image
		livePhotoVideo = record.LivePhotoParts.Video
	}

	_, err := db.db.Exec(`
		INSERT OR REPLACE INTO files (key, hash, size, mod_time, live_photo_image, live_photo_video)
		VALUES (?, ?, ?, ?, ?, ?)
	`, key, record.Hash, record.Size, record.ModTime.UnixNano(), livePhotoImage, livePhotoVideo)
	return err
}

// GetFile retrieves a file record
func (db *DB) GetFile(key string) (*types.FileRecord, error) {
	var hash string
	var size int64
	var modTimeNano int64
	var livePhotoImage, livePhotoVideo sql.NullString

	err := db.db.QueryRow(`
		SELECT hash, size, mod_time, live_photo_image, live_photo_video
		FROM files WHERE key = ?
	`, key).Scan(&hash, &size, &modTimeNano, &livePhotoImage, &livePhotoVideo)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	record := &types.FileRecord{
		Hash:    hash,
		Size:    size,
		ModTime: time.Unix(0, modTimeNano),
	}

	if livePhotoImage.Valid || livePhotoVideo.Valid {
		record.LivePhotoParts = &types.LivePhotoParts{
			Image: livePhotoImage.String,
			Video: livePhotoVideo.String,
		}
	}

	return record, nil
}

// GetAllFiles returns all file records
func (db *DB) GetAllFiles() (map[string]*types.FileRecord, error) {
	result := make(map[string]*types.FileRecord)

	rows, err := db.db.Query(`
		SELECT key, hash, size, mod_time, live_photo_image, live_photo_video
		FROM files
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var key, hash string
		var size int64
		var modTimeNano int64
		var livePhotoImage, livePhotoVideo sql.NullString

		if err := rows.Scan(&key, &hash, &size, &modTimeNano, &livePhotoImage, &livePhotoVideo); err != nil {
			return nil, err
		}

		record := &types.FileRecord{
			Hash:    hash,
			Size:    size,
			ModTime: time.Unix(0, modTimeNano),
		}

		if livePhotoImage.Valid || livePhotoVideo.Valid {
			record.LivePhotoParts = &types.LivePhotoParts{
				Image: livePhotoImage.String,
				Video: livePhotoVideo.String,
			}
		}

		result[key] = record
	}

	return result, rows.Err()
}

// DeleteFile removes a file record
func (db *DB) DeleteFile(key string) error {
	_, err := db.db.Exec(`DELETE FROM files WHERE key = ?`, key)
	return err
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

	rows, err := db.db.Query(`SELECT key, hash FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var key, hash string
		if err := rows.Scan(&key, &hash); err != nil {
			return nil, err
		}
		result[key] = hash
	}

	return result, rows.Err()
}

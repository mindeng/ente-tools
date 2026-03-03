package metasync

import (
	"context"
	"fmt"
	"log"
	"time"

	"database/sql"
	_ "modernc.org/sqlite"
)

// Database handles SQLite database operations for metadata storage
type Database struct {
	db *sql.DB
}

// NewDatabase creates or opens a SQLite database
func NewDatabase(path string) (*Database, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	database := &Database{db: db}

	// Initialize schema
	if err := database.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return database, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}

// initSchema creates the database tables if they don't exist
func (d *Database) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS collections (
		id INTEGER PRIMARY KEY,
		owner_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		is_shared BOOLEAN DEFAULT FALSE,
		is_deleted BOOLEAN DEFAULT FALSE,
		updated_at INTEGER,
		synced_at INTEGER DEFAULT (strftime('%s', 'now'))
	);

	CREATE TABLE IF NOT EXISTS files (
		id INTEGER PRIMARY KEY,
		collection_id INTEGER NOT NULL,
		owner_id INTEGER NOT NULL,
		title TEXT,
		description TEXT,
		creation_time INTEGER,
		modification_time INTEGER,
		latitude REAL,
		longitude REAL,
		file_type TEXT,
		file_size INTEGER,
		hash TEXT,
		exif_make TEXT,
		exif_model TEXT,
		is_deleted BOOLEAN DEFAULT FALSE,
		synced_at INTEGER DEFAULT (strftime('%s', 'now')),
		FOREIGN KEY (collection_id) REFERENCES collections(id)
	);

	CREATE INDEX IF NOT EXISTS idx_files_collection ON files(collection_id);
	CREATE INDEX IF NOT EXISTS idx_files_creation_time ON files(creation_time);
	CREATE INDEX IF NOT EXISTS idx_files_hash ON files(hash);

	CREATE TABLE IF NOT EXISTS sync_state (
		account_key TEXT PRIMARY KEY,
		last_collection_sync INTEGER DEFAULT 0,
		last_sync_time INTEGER DEFAULT (strftime('%s', 'now'))
	);
	`

	_, err := d.db.Exec(schema)
	return err
}

// UpsertCollection inserts or updates a collection
func (d *Database) UpsertCollection(coll DecryptedCollection) error {
	query := `
	INSERT INTO collections (id, owner_id, name, is_shared, is_deleted, updated_at, synced_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		owner_id = excluded.owner_id,
		name = excluded.name,
		is_shared = excluded.is_shared,
		is_deleted = excluded.is_deleted,
		updated_at = excluded.updated_at,
		synced_at = excluded.synced_at
	`

	_, err := d.db.Exec(query,
		coll.ID,
		coll.OwnerID,
		coll.Name,
		coll.IsShared,
		coll.IsDeleted,
		coll.UpdatedTime,
		time.Now().Unix(),
	)

	return err
}

// UpsertFile inserts or updates a file
func (d *Database) UpsertFile(collectionID int64, file DecryptedFile) error {
	query := `
	INSERT INTO files (
		id, collection_id, owner_id, title, description, creation_time,
		modification_time, latitude, longitude, file_type, file_size,
		hash, exif_make, exif_model, is_deleted, synced_at
	)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		collection_id = excluded.collection_id,
		owner_id = excluded.owner_id,
		title = excluded.title,
		description = excluded.description,
		creation_time = excluded.creation_time,
		modification_time = excluded.modification_time,
		latitude = excluded.latitude,
		longitude = excluded.longitude,
		file_type = excluded.file_type,
		file_size = excluded.file_size,
		hash = excluded.hash,
		exif_make = excluded.exif_make,
		exif_model = excluded.exif_model,
		is_deleted = excluded.is_deleted,
		synced_at = excluded.synced_at
	`

	var lat, long sql.NullFloat64
	if file.Latitude != nil {
		lat = sql.NullFloat64{Float64: *file.Latitude, Valid: true}
	}
	if file.Longitude != nil {
		long = sql.NullFloat64{Float64: *file.Longitude, Valid: true}
	}

	var description sql.NullString
	if file.Description != nil {
		description = sql.NullString{String: *file.Description, Valid: true}
	}

	var hash sql.NullString
	if file.Hash != nil {
		hash = sql.NullString{String: *file.Hash, Valid: true}
	}

	var exifMake, exifModel sql.NullString
	if file.EXIFMake != nil {
		exifMake = sql.NullString{String: *file.EXIFMake, Valid: true}
	}
	if file.EXIFModel != nil {
		exifModel = sql.NullString{String: *file.EXIFModel, Valid: true}
	}

	_, err := d.db.Exec(query,
		file.ID,
		collectionID,
		file.OwnerID,
		file.Title,
		description,
		file.CreationTime.UnixMicro(),
		file.ModificationTime.UnixMicro(),
		lat,
		long,
		file.FileType,
		file.FileSize,
		hash,
		exifMake,
		exifModel,
		file.IsDeleted,
		time.Now().Unix(),
	)

	return err
}

// UpdateSyncState updates the sync state for an account
func (d *Database) UpdateSyncState(accountKey string, lastCollectionSync int64) error {
	query := `
	INSERT INTO sync_state (account_key, last_collection_sync, last_sync_time)
	VALUES (?, ?, ?)
	ON CONFLICT(account_key) DO UPDATE SET
		last_collection_sync = excluded.last_collection_sync,
		last_sync_time = excluded.last_sync_time
	`

	_, err := d.db.Exec(query, accountKey, lastCollectionSync, time.Now().Unix())
	return err
}

// GetLastSyncTime retrieves the last sync time for a collection
func (d *Database) GetLastSyncTime(collectionID int64) (int64, error) {
	// For simplicity, we use 0 for first sync
	// A more advanced implementation would track per-collection sync time
	return 0, nil
}

// GetCollectionsCount returns the total number of collections
func (d *Database) GetCollectionsCount() (int, error) {
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM collections WHERE is_deleted = FALSE").Scan(&count)
	return count, err
}

// GetFilesCount returns the total number of files
func (d *Database) GetFilesCount() (int, error) {
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM files WHERE is_deleted = FALSE").Scan(&count)
	return count, err
}

// BeginTx begins a transaction
func (d *Database) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return d.db.BeginTx(ctx, nil)
}

// DeleteFilesForCollection removes all files for a collection
func (d *Database) DeleteFilesForCollection(collectionID int64) error {
	_, err := d.db.Exec("DELETE FROM files WHERE collection_id = ?", collectionID)
	return err
}

// DeleteCollection removes a collection and its files
func (d *Database) DeleteCollection(collectionID int64) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM files WHERE collection_id = ?", collectionID); err != nil {
		return err
	}

	if _, err := tx.Exec("DELETE FROM collections WHERE id = ?", collectionID); err != nil {
		return err
	}

	return tx.Commit()
}

// MarkCollectionAsDeleted marks a collection as deleted
func (d *Database) MarkCollectionAsDeleted(collectionID int64) error {
	_, err := d.db.Exec("UPDATE collections SET is_deleted = TRUE WHERE id = ?", collectionID)
	return err
}

// GetCollections retrieves all collections
func (d *Database) GetCollections() ([]DecryptedCollection, error) {
	query := `
	SELECT id, owner_id, name, is_shared, is_deleted, updated_at
	FROM collections
	WHERE is_deleted = FALSE
	ORDER BY name
	`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var collections []DecryptedCollection
	for rows.Next() {
		var coll DecryptedCollection
		err := rows.Scan(&coll.ID, &coll.OwnerID, &coll.Name, &coll.IsShared, &coll.IsDeleted, &coll.UpdatedTime)
		if err != nil {
			log.Printf("Error scanning collection: %v", err)
			continue
		}
		collections = append(collections, coll)
	}

	return collections, nil
}
package storage

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/lib/pq"
)

// Analyzer provides database analysis for orphan objects
type Analyzer struct {
	db *sql.DB
}

// NewAnalyzer creates a new database analyzer
func NewAnalyzer(db *sql.DB) *Analyzer {
	return &Analyzer{db: db}
}

// DoesObjectExist checks if an object key exists in either object_keys or file_data tables
func (a *Analyzer) DoesObjectExist(objectKey string) (bool, error) {
	// Check object_keys table
	var exists bool
	err := a.db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM object_keys WHERE object_key = $1 AND is_deleted = false)`,
		objectKey).Scan(&exists)
	if err != nil {
		return false, err
	}
	if exists {
		return true, nil
	}

	// Check if it's a file_data object
	return a.isFileDataObject(objectKey)
}

// isFileDataObject checks if an object key corresponds to a valid file_data entry
// file_data objects have format: {userID}/file-data/{fileID}/...
func (a *Analyzer) isFileDataObject(objectKey string) (bool, error) {
	parts := strings.Split(objectKey, "/")
	if len(parts) < 3 || parts[1] != "file-data" {
		return false, nil
	}

	// Parse userID and fileID
	userID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false, nil
	}

	fileID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return false, nil
	}

	// Check if file_data entry exists
	var exists bool
	err = a.db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM file_data WHERE file_id = $1 AND user_id = $2 AND is_deleted = false)`,
		fileID, userID).Scan(&exists)

	return exists, err
}

// DoesUserExist checks if a user ID exists in the users table
func (a *Analyzer) DoesUserExist(userID int64) (bool, error) {
	var exists bool
	err := a.db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM users WHERE user_id = $1)`,
		userID).Scan(&exists)
	return exists, err
}

// GetUserEmail returns the email for a user ID
func (a *Analyzer) GetUserEmail(userID int64) (string, error) {
	var email string
	err := a.db.QueryRow(
		`SELECT email FROM users WHERE user_id = $1`,
		userID).Scan(&email)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return email, err
}

// LoadExistingUserIDs loads all existing user IDs into memory
// Returns a map for fast lookups
func (a *Analyzer) LoadExistingUserIDs() (map[int64]bool, error) {
	rows, err := a.db.Query(`SELECT user_id FROM users`)
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %w", err)
	}
	defer rows.Close()

	userIDs := make(map[int64]bool)
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, fmt.Errorf("failed to scan user ID: %w", err)
		}
		userIDs[userID] = true
	}

	return userIDs, rows.Err()
}

// LoadAllObjectKeys loads all object keys from the database
// This includes both object_keys and file_data entries
// Returns a set for fast lookups
func (a *Analyzer) LoadAllObjectKeys() (map[string]bool, error) {
	objectKeys := make(map[string]bool)

	// Load object_keys
	rows, err := a.db.Query(`SELECT object_key FROM object_keys WHERE is_deleted = false`)
	if err != nil {
		return nil, fmt.Errorf("failed to query object_keys: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("failed to scan object key: %w", err)
		}
		objectKeys[key] = true
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load file_data entries - we'll need to construct possible object keys
	// file_data objects have format: {userID}/file-data/{fileID}/{type}/{obj_id}
	// or: {userID}/file-data/{fileID}/{type}_playlist
	rows2, err := a.db.Query(`
		SELECT user_id, file_id, data_type, obj_id
		FROM file_data
		WHERE is_deleted = false
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query file_data: %w", err)
	}
	defer rows2.Close()

	for rows2.Next() {
		var userID, fileID int64
		var dataType string
		var objID sql.NullString

		if err := rows2.Scan(&userID, &fileID, &dataType, &objID); err != nil {
			return nil, fmt.Errorf("failed to scan file_data: %w", err)
		}

		if objID.Valid {
			// Format: {userID}/file-data/{fileID}/{type}/{obj_id}
			basePath := fmt.Sprintf("%d/file-data/%d/%s/%s", userID, fileID, dataType, objID.String)
			objectKeys[basePath] = true

			// For video, also add playlist
			if dataType == "vid_preview" {
				playlistPath := fmt.Sprintf("%s_playlist", basePath)
				objectKeys[playlistPath] = true
			}
		} else {
			// For ML data without obj_id: {userID}/file-data/{fileID}/{type}
			basePath := fmt.Sprintf("%d/file-data/%d/%s", userID, fileID, dataType)
			objectKeys[basePath] = true
		}
	}

	return objectKeys, rows2.Err()
}

// GetFileInfo returns information about a file given its ID
func (a *Analyzer) GetFileInfo(fileID int64) (string, string, error) {
	var collectionName sql.NullString
	var ownerEmail sql.NullString

	err := a.db.QueryRow(`
		SELECT c.name, u.email
		FROM files f
		LEFT JOIN collection_files cf ON f.file_id = cf.file_id
		LEFT JOIN collections c ON cf.collection_id = c.collection_id
		LEFT JOIN users u ON f.owner_id = u.user_id
		WHERE f.file_id = $1
		LIMIT 1
	`, fileID).Scan(&collectionName, &ownerEmail)

	if err != nil {
		if err == sql.ErrNoRows {
			return "", "", nil
		}
		return "", "", err
	}

	collName := ""
	if collectionName.Valid {
		collName = collectionName.String
	}

	email := ""
	if ownerEmail.Valid {
		email = ownerEmail.String
	}

	return email, collName, nil
}

// GetFileDataInfo returns information about file_data given a file ID
func (a *Analyzer) GetFileDataInfo(fileID int64) (string, string, string, error) {
	var email, collectionName string
	var dataType sql.NullString

	err := a.db.QueryRow(`
		SELECT u.email, c.name, fd.data_type
		FROM file_data fd
		LEFT JOIN files f ON fd.file_id = f.file_id
		LEFT JOIN users u ON f.owner_id = u.user_id
		LEFT JOIN collection_files cf ON f.file_id = cf.file_id
		LEFT JOIN collections c ON cf.collection_id = c.collection_id
		WHERE fd.file_id = $1 AND fd.is_deleted = false
		LIMIT 1
	`, fileID).Scan(&email, &collectionName, &dataType)

	if err != nil {
		if err == sql.ErrNoRows {
			return "", "", "", nil
		}
		return "", "", "", err
	}

	dt := "unknown"
	if dataType.Valid {
		dt = dataType.String
	}

	return email, collectionName, dt, nil
}

// IsObjectOrphan checks if an object is orphan (not referenced in database)
func (a *Analyzer) IsObjectOrphan(objectKey string) (bool, error) {
	// Check object_keys and file_data tables
	exists, err := a.DoesObjectExist(objectKey)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}

	// Check temp_objects table (for uploads in progress)
	var tempExists bool
	err = a.db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM temp_objects WHERE object_key = $1)`,
		objectKey).Scan(&tempExists)
	if err == nil && tempExists {
		return false, nil
	}

	return true, nil
}

// GetFileTypeFromKey attempts to determine the file type from an object key
func GetFileTypeFromKey(key string) string {
	if strings.Contains(key, "/file-data/") {
		parts := strings.Split(key, "/")
		for i, part := range parts {
			if part == "file-data" && i+2 < len(parts) {
				return parts[i+2] // e.g., "MlData", "Thumbnail", etc.
			}
		}
	}
	return "original"
}

// ParseUserIDFromKey parses user ID from object key
// Returns -1 if user ID cannot be parsed
func ParseUserIDFromKey(key string) int64 {
	parts := strings.Split(key, "/")
	if len(parts) > 0 {
		userID, err := strconv.ParseInt(parts[0], 10, 64)
		if err == nil {
			return userID
		}
	}
	return -1
}

// ObjectMetadata contains metadata about an object
type ObjectMetadata struct {
	UserEmail     string
	Collection    string
	FileType      string
	IsOrphan      bool
	UserDeleted   bool
	UserID        int64
	FileID        int64
}

// GetObjectMetadata attempts to find metadata for an object
func (a *Analyzer) GetObjectMetadata(key string) (*ObjectMetadata, error) {
	meta := &ObjectMetadata{
		UserID:   ParseUserIDFromKey(key),
		FileType: GetFileTypeFromKey(key),
	}

	// Check if user exists
	if meta.UserID > 0 {
		userExists, err := a.DoesUserExist(meta.UserID)
		if err != nil {
			return nil, err
		}
		meta.UserDeleted = !userExists

		// Get user email if user exists
		if userExists {
			email, err := a.GetUserEmail(meta.UserID)
			if err != nil {
				return nil, err
			}
			meta.UserEmail = email
		}
	}

	// Check if object is orphan
	isOrphan, err := a.IsObjectOrphan(key)
	if err != nil {
		return nil, err
	}
	meta.IsOrphan = isOrphan

	// Try to find file info if not orphan
	if !isOrphan {
		// Try to find file ID from object_keys
		var fileID sql.NullInt64
		err = a.db.QueryRow(
			`SELECT file_id FROM object_keys WHERE object_key = $1 LIMIT 1`,
			key).Scan(&fileID)
		if err == nil && fileID.Valid {
			meta.FileID = fileID.Int64
			email, coll, err := a.GetFileInfo(fileID.Int64)
			if err == nil {
				meta.UserEmail = email
				meta.Collection = coll
			}
		}
	}

	// For file-data type, try to get more info
	if strings.Contains(key, "/file-data/") {
		parts := strings.Split(key, "/")
		for i, part := range parts {
			if part == "file-data" && i+1 < len(parts) {
				fileID, err := strconv.ParseInt(parts[i+1], 10, 64)
				if err == nil {
					email, coll, dt, err := a.GetFileDataInfo(fileID)
					if err == nil {
						meta.UserEmail = email
						meta.Collection = coll
						if dt != "" {
							meta.FileType = dt
						}
					}
				}
				break
			}
		}
	}

	return meta, nil
}

// Stats contains statistics about the database
type Stats struct {
	TotalUsers      int64
	TotalFiles      int64
	TotalObjectKeys int64
	TotalFileData   int64
}

// GetDatabaseStats returns statistics about the database
func (a *Analyzer) GetDatabaseStats() (*Stats, error) {
	stats := &Stats{}

	err := a.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&stats.TotalUsers)
	if err != nil {
		return nil, fmt.Errorf("failed to count users: %w", err)
	}

	err = a.db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&stats.TotalFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to count files: %w", err)
	}

	err = a.db.QueryRow(`SELECT COUNT(*) FROM object_keys WHERE is_deleted = false`).Scan(&stats.TotalObjectKeys)
	if err != nil {
		return nil, fmt.Errorf("failed to count object_keys: %w", err)
	}

	err = a.db.QueryRow(`SELECT COUNT(*) FROM file_data WHERE is_deleted = false`).Scan(&stats.TotalFileData)
	if err != nil {
		return nil, fmt.Errorf("failed to count file_data: %w", err)
	}

	return stats, nil
}

// MarkObjectKeysAsDeleted marks object keys as deleted in the database
func (a *Analyzer) MarkObjectKeysAsDeleted(keys []string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	result, err := a.db.Exec(
		`UPDATE object_keys SET is_deleted = true WHERE object_key = ANY($1)`,
		pq.Array(keys))
	if err != nil {
		return 0, fmt.Errorf("failed to mark object keys as deleted: %w", err)
	}

	return result.RowsAffected()
}
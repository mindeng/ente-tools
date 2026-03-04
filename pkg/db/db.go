package db

import (
	"fmt"
	"path/filepath"
)

// GetPath returns the path to the database file for a given directory
func GetPath(dir string) string {
	return filepath.Join(dir, ".fhash", "db")
}

// GetDirPath returns the path to the .fhash directory
func GetDirPath(dir string) string {
	return filepath.Join(dir, ".fhash")
}

// String returns a formatted string representation of the database path
func (db *DB) String() string {
	return fmt.Sprintf("database at %s", db.path)
}

// DB represents a database path
type DB struct {
	path string
}

// New creates a new DB instance for the given directory
func New(dir string) *DB {
	return &DB{path: GetPath(dir)}
}

// Path returns the database file path
func (db *DB) Path() string {
	return db.path
}

// DirPath returns the .fhash directory path
func (db *DB) DirPath() string {
	return GetDirPath(db.path)
}

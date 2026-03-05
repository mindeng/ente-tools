package exif

import (
	"fmt"
	"os"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

// GetCreationTime extracts the creation date/time from an image file
// Returns the DateTimeOriginal or DateTime from EXIF data, or file modification time if not available
func GetCreationTime(filePath string) (time.Time, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Decode EXIF data
	x, err := exif.Decode(file)
	if err != nil {
		// If EXIF decoding fails, return file modification time
		info, err := os.Stat(filePath)
		if err != nil {
			return time.Time{}, fmt.Errorf("failed to get file info: %w", err)
		}
		return info.ModTime(), nil
	}

	// Try to get DateTimeOriginal first (the actual capture time)
	dt, err := x.DateTime()
	if err == nil {
		return dt, nil
	}

	// If DateTimeOriginal fails, fall back to file modification time
	info, err := os.Stat(filePath)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get file info: %w", err)
	}
	return info.ModTime(), nil
}
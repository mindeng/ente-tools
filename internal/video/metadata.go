package video

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Metadata represents video metadata
type Metadata struct {
	CreationDate *time.Time
	Duration     *time.Duration
	Width        *int
	Height       *int
}

// GetMetadata extracts metadata from a video file using ffprobe
// Returns nil if ffprobe is not available or extraction fails
func GetMetadata(filePath string) (*Metadata, error) {
	// Check if ffprobe is available
	_, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe not available")
	}

	// Run ffprobe to get video metadata
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-show_entries", "format=duration,creation_time",
		"-of", "json",
		filePath,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	return parseMetadata(string(output)), nil
}

// GetCreationTime extracts the creation time from a video file
// Falls back to file modification time if not available
func GetCreationTime(filePath string) (time.Time, error) {
	metadata, err := GetMetadata(filePath)
	if err != nil {
		// Fall back to file modification time
		info, err := os.Stat(filePath)
		if err != nil {
			return time.Time{}, fmt.Errorf("failed to get file info: %w", err)
		}
		return info.ModTime(), nil
	}

	if metadata.CreationDate != nil {
		return *metadata.CreationDate, nil
	}

	// Fall back to file modification time
	info, err := os.Stat(filePath)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get file info: %w", err)
	}
	return info.ModTime(), nil
}

// parseMetadata parses ffprobe JSON output
func parseMetadata(jsonStr string) *Metadata {
	// Simple parsing without JSON dependency for now
	// In a production environment, use a proper JSON parser
	var metadata Metadata

	// Try to extract creation_time
	creationTime := extractValue(jsonStr, "creation_time")
	if creationTime != "" {
		if t, err := time.Parse(time.RFC3339, creationTime); err == nil {
			metadata.CreationDate = &t
		}
	}

	return &metadata
}

// extractValue extracts a key value from JSON-like string
func extractValue(jsonStr, key string) string {
	searchStr := fmt.Sprintf(`"%s"`, key)
	idx := strings.Index(jsonStr, searchStr)
	if idx == -1 {
		return ""
	}

	// Find the value after the key
	start := strings.Index(jsonStr[idx:], ":")
	if start == -1 {
		return ""
	}
	start += idx + 1

	// Skip whitespace
	for start < len(jsonStr) && (jsonStr[start] == ' ' || jsonStr[start] == '\t' || jsonStr[start] == '\n') {
		start++
	}

	// If value is quoted string
	if jsonStr[start] == '"' {
		end := strings.Index(jsonStr[start+1:], `"`)
		if end == -1 {
			return ""
		}
		return jsonStr[start+1 : start+1+end]
	}

	return ""
}
package comparator

import (
	"fmt"
	"sort"
	"strings"

	"ente-tools/internal/types"
)

// Compare compares two directories based on their hash sets (not paths)
func Compare(dirA, dirB string) (*types.CompareResult, error) {
	// Get all files from both directories
	// This is a simplified version - in production, you'd load from the actual databases

	// For now, this is a placeholder that would be filled by actual database queries
	// The actual implementation would use database.GetAllFileEntries() for both directories

	return &types.CompareResult{
		Common:   0,
		OnlyInA:  []string{},
		OnlyInB:  []string{},
	}, nil
}

// CompareMaps compares two directories based on their hash sets only
// This ignores paths and only compares the sets of unique hashes
func CompareMaps(mapA, mapB map[string]string) *types.CompareResult {
	result := &types.CompareResult{
		OnlyInA: []string{},
		OnlyInB: []string{},
	}

	// Build hash sets (unique hashes)
	hashesA := make(map[string]string)   // hash -> path (one example path)
	hashesB := make(map[string]string)   // hash -> path (one example path)
	pathsByHashA := make(map[string][]string) // hash -> all paths
	pathsByHashB := make(map[string][]string) // hash -> all paths

	// Build sets for A
	for path, hash := range mapA {
		hashesA[hash] = path
		pathsByHashA[hash] = append(pathsByHashA[hash], path)
	}

	// Build sets for B
	for path, hash := range mapB {
		hashesB[hash] = path
		pathsByHashB[hash] = append(pathsByHashB[hash], path)
	}

	// Find common hashes
	for hash := range hashesA {
		if _, exists := hashesB[hash]; exists {
			result.Common++
		}
	}

	// Find hashes only in A
	for hash := range hashesA {
		if _, exists := hashesB[hash]; !exists {
			// Add all paths with this hash
			for _, p := range pathsByHashA[hash] {
				result.OnlyInA = append(result.OnlyInA, p)
			}
		}
	}

	// Find hashes only in B
	for hash := range hashesB {
		if _, exists := hashesA[hash]; !exists {
			// Add all paths with this hash
			for _, p := range pathsByHashB[hash] {
				result.OnlyInB = append(result.OnlyInB, p)
			}
		}
	}

	return result
}

// FormatCompareResult formats a comparison result for display
func FormatCompareResult(result *types.CompareResult) string {
	var sb strings.Builder

	sb.WriteString("\nSummary:\n")
	sb.WriteString(fmt.Sprintf("  Common hashes: %d\n", result.Common))
	sb.WriteString(fmt.Sprintf("  Files only in A: %d\n", len(result.OnlyInA)))
	sb.WriteString(fmt.Sprintf("  Files only in B: %d\n", len(result.OnlyInB)))

	// Show files only in A
	if len(result.OnlyInA) > 0 {
		sb.WriteString("\nFiles only in A:\n")
		sort.Strings(result.OnlyInA)
		for _, path := range result.OnlyInA {
			sb.WriteString(fmt.Sprintf("  - %s\n", path))
		}
	}

	// Show files only in B
	if len(result.OnlyInB) > 0 {
		sb.WriteString("\nFiles only in B:\n")
		sort.Strings(result.OnlyInB)
		for _, path := range result.OnlyInB {
			sb.WriteString(fmt.Sprintf("  - %s\n", path))
		}
	}

	return sb.String()
}

// FormatScanStats formats scan statistics for display
func FormatScanStats(stats *types.ScanStats, dbPath string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("\nProcessed %d files (%d updated, %d skipped)\n",
		stats.TotalFiles, stats.UpdatedFiles, stats.SkippedFiles))
	sb.WriteString(fmt.Sprintf("Found %d Live Photos\n", stats.LivePhotos))

	if stats.UnsupportedFiles > 0 {
		// Sort extensions alphabetically
		exts := make([]string, len(stats.UnsupportedExts))
		copy(exts, stats.UnsupportedExts)
		sort.Strings(exts)
		sb.WriteString(fmt.Sprintf("Skipped %d unsupported files (extensions: %s)\n",
			stats.UnsupportedFiles, strings.Join(exts, ", ")))
	}

	if len(stats.Duplicates) > 0 {
		actualDuplicates := 0
		for _, dup := range stats.Duplicates {
			if len(dup.Duplicates) > 0 {
				actualDuplicates++
			}
		}
		if actualDuplicates > 0 {
			sb.WriteString(fmt.Sprintf("Found %d duplicates:\n", actualDuplicates))
			for _, dup := range stats.Duplicates {
				if len(dup.Duplicates) > 0 {
					sb.WriteString(fmt.Sprintf("  - %s (hash: %s...)\n", dup.PrimaryPath, dup.Hash[:16]))
					for _, d := range dup.Duplicates {
						sb.WriteString(fmt.Sprintf("    duplicates: %s\n", d))
					}
				}
			}
		}
	}

	sb.WriteString(fmt.Sprintf("\nDatabase written to: %s\n", dbPath))

	return sb.String()
}

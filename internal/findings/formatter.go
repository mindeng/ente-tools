package findings

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FormatMissingResult formats the missing files analysis result
func FormatMissingResult(result *MissingResult) string {
	var sb strings.Builder

	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n")
	sb.WriteString("MISSING FILES ANALYSIS\n")
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")

	sb.WriteString(fmt.Sprintf("Total files scanned:  %d\n", result.TotalFiles))
	sb.WriteString(fmt.Sprintf("Files in ente:        %d\n", result.FoundInEnte))
	sb.WriteString(fmt.Sprintf("Files NOT in ente:    %d\n", len(result.MissingFiles)))
	sb.WriteString(fmt.Sprintf("Analysis duration:    %v\n", result.Duration))
	sb.WriteString("\n")

	if len(result.MissingFiles) > 0 {
		sb.WriteString(strings.Repeat("-", 60))
		sb.WriteString("\n")
		sb.WriteString("MISSING FILES:\n")
		sb.WriteString(strings.Repeat("-", 60))
		sb.WriteString("\n")

		for _, f := range result.MissingFiles {
			sb.WriteString(fmt.Sprintf("%s\n", f.Path))
			// Show additional files (e.g., video component of Live Photo)
			for _, addPath := range f.AdditionalPaths {
				sb.WriteString(fmt.Sprintf("  + %s (Live Photo component)\n", addPath))
			}
		}
	} else {
		sb.WriteString("All files are already in your ente library!\n")
	}

	sb.WriteString("\n")
	return sb.String()
}

// CopyOptions holds configuration for copying missing files
type CopyOptions struct {
	SourceDir    string // Source directory where files are located
	TargetDir    string // Target directory to copy files to
	MissingFiles []MissingFile
	Verbose      bool
	DryRun       bool // If true, only print what would be copied
}

// CopyMissingFiles copies missing files from source to target directory
// maintaining the original directory structure
func CopyMissingFiles(opts CopyOptions) (*CopyResult, error) {
	result := &CopyResult{
		TotalFiles:   len(opts.MissingFiles),
		CopiedFiles:  0,
		SkippedFiles: 0,
		FailedFiles:  []FailedCopy{},
	}

	for _, missing := range opts.MissingFiles {
		// Copy main file
		if err := copyMissingFile(opts.SourceDir, opts.TargetDir, missing, opts.Verbose, opts.DryRun, result); err != nil {
			// Error handled by copyMissingFile
			continue
		}

		// Copy additional files (e.g., video component of Live Photo)
		for i, addPath := range missing.AdditionalPaths {
			if i < len(missing.AdditionalInfo) {
				addInfo := MissingFile{
					Path:    addPath,
					Hash:    missing.AdditionalInfo[i].Hash,
					Size:    missing.AdditionalInfo[i].Size,
					ModTime: missing.AdditionalInfo[i].ModTime,
				}
				copyMissingFile(opts.SourceDir, opts.TargetDir, addInfo, opts.Verbose, opts.DryRun, result)
			}
		}
	}

	return result, nil
}

// copyMissingFile copies a single missing file
func copyMissingFile(sourceDir, targetDir string, missing MissingFile, verbose, dryRun bool, result *CopyResult) error {
	srcPath := filepath.Join(sourceDir, missing.Path)
	dstPath := filepath.Join(targetDir, missing.Path)

	// Ensure target directory exists
	dstDir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		result.FailedFiles = append(result.FailedFiles, FailedCopy{
			Path: missing.Path,
			Err:  fmt.Errorf("failed to create directory: %w", err),
		})
		result.SkippedFiles++
		return fmt.Errorf("directory creation failed")
	}

	// Check if source file exists
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		result.FailedFiles = append(result.FailedFiles, FailedCopy{
			Path: missing.Path,
			Err:  fmt.Errorf("source file not found: %w", err),
		})
		result.SkippedFiles++
		return fmt.Errorf("source not found")
	}

	// Check if destination already exists
	if _, err := os.Stat(dstPath); err == nil {
		if verbose {
			fmt.Printf("Skipping (exists): %s\n", missing.Path)
		}
		result.SkippedFiles++
		return nil
	}

	if dryRun {
		if verbose {
			fmt.Printf("Would copy: %s -> %s\n", srcPath, dstPath)
		}
		result.CopiedFiles++
		return nil
	}

	// Copy file
	if err := copyFile(srcPath, dstPath, srcInfo.Mode()); err != nil {
		result.FailedFiles = append(result.FailedFiles, FailedCopy{
			Path: missing.Path,
			Err:  fmt.Errorf("copy failed: %w", err),
		})
		result.SkippedFiles++
		return fmt.Errorf("copy failed")
	}

	// Set modification time
	if err := os.Chtimes(dstPath, missing.ModTime, missing.ModTime); err != nil && verbose {
		fmt.Printf("Warning: failed to set mod time for %s: %v\n", missing.Path, err)
	}

	if verbose {
		fmt.Printf("Copied: %s\n", missing.Path)
	}
	result.CopiedFiles++
	return nil
}

// copyFile copies a file from src to dst with the given mode
func copyFile(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		os.Remove(dst) // Clean up on error
		return err
	}

	return nil
}

// FormatCopyResult formats the copy result
func FormatCopyResult(result *CopyResult) string {
	var sb strings.Builder

	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n")
	sb.WriteString("COPY RESULT\n")
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")

	sb.WriteString(fmt.Sprintf("Total files to copy:  %d\n", result.TotalFiles))
	sb.WriteString(fmt.Sprintf("Copied:              %d\n", result.CopiedFiles))
	sb.WriteString(fmt.Sprintf("Skipped (exists):    %d\n", result.SkippedFiles))
	sb.WriteString(fmt.Sprintf("Failed:              %d\n", len(result.FailedFiles)))
	sb.WriteString("\n")

	if len(result.FailedFiles) > 0 {
		sb.WriteString(strings.Repeat("-", 60))
		sb.WriteString("\n")
		sb.WriteString("FAILED FILES:\n")
		sb.WriteString(strings.Repeat("-", 60))
		sb.WriteString("\n")

		for _, f := range result.FailedFiles {
			sb.WriteString(fmt.Sprintf("  %s: %v\n", f.Path, f.Err))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// CopyResult contains statistics about the copy operation
type CopyResult struct {
	TotalFiles   int
	CopiedFiles  int
	SkippedFiles int
	FailedFiles  []FailedCopy
}

// FailedCopy represents a file that failed to copy
type FailedCopy struct {
	Path string
	Err  error
}

// FormatStreamCopyResult formats the streaming copy result
func FormatStreamCopyResult(result *StreamCopyResult) string {
	var sb strings.Builder

	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n")
	sb.WriteString("COPY RESULT\n")
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")

	sb.WriteString(fmt.Sprintf("Total files processed: %d\n", result.TotalFiles))
	sb.WriteString(fmt.Sprintf("Copied:               %d\n", result.CopiedFiles))
	sb.WriteString(fmt.Sprintf("Skipped (exists):     %d\n", result.SkippedFiles))
	sb.WriteString(fmt.Sprintf("Failed:               %d\n", len(result.FailedFiles)))
	sb.WriteString(fmt.Sprintf("Duration:             %v\n", result.Duration))
	sb.WriteString("\n")

	if len(result.FailedFiles) > 0 {
		sb.WriteString(strings.Repeat("-", 60))
		sb.WriteString("\n")
		sb.WriteString("FAILED FILES:\n")
		sb.WriteString(strings.Repeat("-", 60))
		sb.WriteString("\n")

		for _, f := range result.FailedFiles {
			sb.WriteString(fmt.Sprintf("  %s: %v\n", f.Path, f.Err))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
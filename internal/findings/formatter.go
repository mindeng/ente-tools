package findings

import (
	"fmt"
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
		}
	} else {
		sb.WriteString("All files are already in your ente library!\n")
	}

	sb.WriteString("\n")
	return sb.String()
}
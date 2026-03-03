package upload

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// VideoMetadata represents metadata extracted from a video file
type VideoMetadata struct {
	Duration float64 // Duration in seconds
	Width    int64
	Height   int64
}

// GetVideoDuration extracts the duration of a video file using ffmpeg
func GetVideoDuration(videoPath string) (float64, error) {
	// Run ffprobe or ffmpeg to get video duration
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: try ffmpeg and parse stderr
		return extractDurationFromFFmpeg(videoPath)
	}

	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration: %w", err)
	}

	return duration, nil
}

// extractDurationFromFFmpeg tries to extract duration from ffmpeg output
func extractDurationFromFFmpeg(videoPath string) (float64, error) {
	cmd := exec.Command("ffmpeg", "-i", videoPath)
	output, _ := cmd.CombinedOutput()

	// Look for Duration in the output
	re := regexp.MustCompile(`Duration: (\d{2}):(\d{2}):(\d{2}\.\d{2})`)
	matches := re.FindStringSubmatch(string(output))
	if len(matches) < 4 {
		return 0, fmt.Errorf("could not find duration in ffmpeg output")
	}

	hours, _ := strconv.ParseFloat(matches[1], 64)
	minutes, _ := strconv.ParseFloat(matches[2], 64)
	seconds, _ := strconv.ParseFloat(matches[3], 64)

	duration := hours*3600 + minutes*60 + seconds
	return duration, nil
}

// GetVideoDimensions extracts width and height from a video file
func GetVideoDimensions(videoPath string) (int64, int64, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=s=x:p=0",
		videoPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("ffprobe failed: %w", err)
	}

	dims := strings.Split(strings.TrimSpace(string(output)), "x")
	if len(dims) < 2 {
		return 0, 0, fmt.Errorf("invalid dimensions format: %s", string(output))
	}

	width, err := strconv.ParseInt(strings.TrimSpace(dims[0]), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid width: %w", err)
	}

	height, err := strconv.ParseInt(strings.TrimSpace(dims[1]), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid height: %w", err)
	}

	return width, height, nil
}

// ExtractVideoMetadata extracts all video metadata
func ExtractVideoMetadata(videoPath string) (*VideoMetadata, error) {
	duration, err := GetVideoDuration(videoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get duration: %w", err)
	}

	width, height, err := GetVideoDimensions(videoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get dimensions: %w", err)
	}

	return &VideoMetadata{
		Duration: duration,
		Width:    width,
		Height:   height,
	}, nil
}

// GenerateVideoThumbnail generates a thumbnail from a video using ffmpeg
func GenerateVideoThumbnail(videoPath string) ([]byte, error) {
	// Use ffmpeg to extract a frame from the middle of the video
	duration, err := GetVideoDuration(videoPath)
	if err != nil {
		duration = 1 // Default to 1 second if we can't get duration
	}

	seekTime := duration / 2 // Seek to middle

	cmd := exec.Command("ffmpeg",
		"-ss", fmt.Sprintf("%.2f", seekTime),
		"-i", videoPath,
		"-vframes", "1",
		"-vf", fmt.Sprintf("scale=%d:%d", ThumbnailSize, ThumbnailSize),
		"-q:v", "2",
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"-")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %w, output: %s", err, string(output))
	}

	return output, nil
}
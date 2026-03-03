package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ente-hashcmp/internal/hash"
	"ente-hashcmp/internal/livephoto"
)

// MetaFile represents the structure of Ente metadata JSON files
type MetaFile struct {
	Title       string      `json:"title"`
	Description interface{} `json:"description"`
	Info        struct {
		ID        int64    `json:"id"`
		Hash      string   `json:"hash"`
		OwnerID   int64    `json:"ownerID"`
		FileNames []string `json:"fileNames"`
	} `json:"info"`
}

// TestEnteHashCompatibility verifies that our hash calculation matches Ente's hash
// from the metadata JSON files in testdata/.meta
func TestEnteHashCompatibility(t *testing.T) {
	testdataDir := "../../testdata"
	metaDir := filepath.Join(testdataDir, ".meta")

	// Check if testdata exists
	if _, err := os.Stat(testdataDir); os.IsNotExist(err) {
		t.Skip("testdata directory not found, skipping integration test")
	}

	// Read all meta files
	metaFiles, err := os.ReadDir(metaDir)
	if err != nil {
		t.Fatalf("Failed to read meta directory: %v", err)
	}

	for _, metaFile := range metaFiles {
		if metaFile.IsDir() || !strings.HasSuffix(metaFile.Name(), ".json") {
			continue
		}

		metaPath := filepath.Join(metaDir, metaFile.Name())
		data, err := os.ReadFile(metaPath)
		if err != nil {
			t.Errorf("Failed to read meta file %s: %v", metaPath, err)
			continue
		}

		var meta MetaFile
		if err := json.Unmarshal(data, &meta); err != nil {
			t.Errorf("Failed to parse meta file %s: %v", metaPath, err)
			continue
		}

		// Check if this is a Live Photo (has both image and video in fileNames)
		if len(meta.Info.FileNames) == 2 {
			// Live Photo: fileNames[0] is image, fileNames[1] is video
			imageName := meta.Info.FileNames[0]
			videoName := meta.Info.FileNames[1]

			imagePath := filepath.Join(testdataDir, imageName)
			videoPath := filepath.Join(testdataDir, videoName)

			// Verify files exist
			if _, err := os.Stat(imagePath); os.IsNotExist(err) {
				t.Errorf("Image file not found: %s", imagePath)
				continue
			}
			if _, err := os.Stat(videoPath); os.IsNotExist(err) {
				t.Errorf("Video file not found: %s", videoPath)
				continue
			}

			// Calculate Live Photo hash using our implementation
			calculatedHash, err := livephoto.CalculateLivePhotoHash(imagePath, videoPath)
			if err != nil {
				t.Errorf("Failed to calculate Live Photo hash for %s: %v", imageName, err)
				continue
			}

			// Compare with Ente's hash
			if calculatedHash != meta.Info.Hash {
				t.Errorf("Hash mismatch for %s:\n  Expected: %s\n  Got:      %s",
					imageName, meta.Info.Hash, calculatedHash)
			} else {
				t.Logf("✓ Hash matches for %s", imageName)
			}
		} else if len(meta.Info.FileNames) == 1 {
			// Single file (regular photo)
			fileName := meta.Info.FileNames[0]
			filePath := filepath.Join(testdataDir, fileName)

			// Verify file exists
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				t.Errorf("File not found: %s", filePath)
				continue
			}

			// Calculate hash
			file, err := os.Open(filePath)
			if err != nil {
				t.Errorf("Failed to open file %s: %v", filePath, err)
				continue
			}
			defer file.Close()

			calculatedHash, err := hash.ComputeHash(file)
			if err != nil {
				t.Errorf("Failed to calculate hash for %s: %v", fileName, err)
				continue
			}

			// Compare with Ente's hash
			if calculatedHash != meta.Info.Hash {
				t.Errorf("Hash mismatch for %s:\n  Expected: %s\n  Got:      %s",
					fileName, meta.Info.Hash, calculatedHash)
			} else {
				t.Logf("✓ Hash matches for %s", fileName)
			}
		}
	}
}

// TestIndividualLivePhotoComponents verifies that we can correctly calculate
// individual image and video hashes that match Ente's stored values
func TestIndividualLivePhotoComponents(t *testing.T) {
	testdataDir := "../../testdata"
	metaDir := filepath.Join(testdataDir, ".meta")

	// Check if testdata exists
	if _, err := os.Stat(testdataDir); os.IsNotExist(err) {
		t.Skip("testdata directory not found, skipping integration test")
	}

	// Read all meta files
	metaFiles, err := os.ReadDir(metaDir)
	if err != nil {
		t.Fatalf("Failed to read meta directory: %v", err)
	}

	for _, metaFile := range metaFiles {
		if metaFile.IsDir() || !strings.HasSuffix(metaFile.Name(), ".json") {
			continue
		}

		metaPath := filepath.Join(metaDir, metaFile.Name())
		data, err := os.ReadFile(metaPath)
		if err != nil {
			t.Errorf("Failed to read meta file %s: %v", metaPath, err)
			continue
		}

		var meta MetaFile
		if err := json.Unmarshal(data, &meta); err != nil {
			t.Errorf("Failed to parse meta file %s: %v", metaPath, err)
			continue
		}

		// Only test Live Photos
		if len(meta.Info.FileNames) != 2 {
			continue
		}

		imageName := meta.Info.FileNames[0]
		videoName := meta.Info.FileNames[1]

		imagePath := filepath.Join(testdataDir, imageName)
		videoPath := filepath.Join(testdataDir, videoName)

		// Verify files exist
		if _, err := os.Stat(imagePath); os.IsNotExist(err) {
			continue
		}
		if _, err := os.Stat(videoPath); os.IsNotExist(err) {
			continue
		}

		// Calculate individual hashes
		imageFile, err := os.Open(imagePath)
		if err != nil {
			t.Errorf("Failed to open image %s: %v", imageName, err)
			continue
		}

		imageHash, err := hash.ComputeHash(imageFile)
		imageFile.Close()
		if err != nil {
			t.Errorf("Failed to calculate image hash for %s: %v", imageName, err)
			continue
		}

		videoFile, err := os.Open(videoPath)
		if err != nil {
			t.Errorf("Failed to open video %s: %v", videoName, err)
			continue
		}

		videoHash, err := hash.ComputeHash(videoFile)
		videoFile.Close()
		if err != nil {
			t.Errorf("Failed to calculate video hash for %s: %v", videoName, err)
			continue
		}

		// Combine hashes
		combinedHash := imageHash + ":" + videoHash

		// Parse Ente's hash to get components
		enteHashParts := strings.Split(meta.Info.Hash, ":")
		if len(enteHashParts) != 2 {
			t.Errorf("Invalid Ente hash format for %s: %s", imageName, meta.Info.Hash)
			continue
		}
		enteImageHash := enteHashParts[0]
		enteVideoHash := enteHashParts[1]

		// Compare individual components
		if imageHash != enteImageHash {
			t.Errorf("Image hash mismatch for %s:\n  Expected: %s\n  Got:      %s",
				imageName, enteImageHash, imageHash)
		} else {
			t.Logf("✓ Image hash matches for %s", imageName)
		}

		if videoHash != enteVideoHash {
			t.Errorf("Video hash mismatch for %s:\n  Expected: %s\n  Got:      %s",
				videoName, enteVideoHash, videoHash)
		} else {
			t.Logf("✓ Video hash matches for %s", videoName)
		}

		// Compare combined hash
		if combinedHash != meta.Info.Hash {
			t.Errorf("Combined hash mismatch for %s:\n  Expected: %s\n  Got:      %s",
				imageName, meta.Info.Hash, combinedHash)
		} else {
			t.Logf("✓ Combined hash matches for %s", imageName)
		}
	}
}

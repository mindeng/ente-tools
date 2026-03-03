package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ObjectInfo represents information about an S3 object
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified string
	Datacenter   string
	ETag         string
}

// Scanner scans S3 objects
type Scanner struct {
	client *S3Client
}

// NewScanner creates a new scanner for the given S3 client
func NewScanner(client *S3Client) *Scanner {
	return &Scanner{client: client}
}

// ScanPrefix scans all objects with the given prefix
// The callback function is called for each object
// Returns the total count and total size of scanned objects
func (s *Scanner) ScanPrefix(ctx context.Context, prefix string, callback func(obj ObjectInfo) error) (int, int64, error) {
	var total int
	var totalSize int64

	paginator := s3.NewListObjectsV2Paginator(s.client.Client, &s3.ListObjectsV2Input{
		Bucket: &s.client.Bucket,
		Prefix: &prefix,
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return total, totalSize, fmt.Errorf("failed to list objects: %w", err)
		}

		for _, obj := range page.Contents {
			size := int64(0)
			if obj.Size != nil {
				size = *obj.Size
			}
			info := ObjectInfo{
				Key:          *obj.Key,
				Size:         size,
				LastModified: obj.LastModified.String(),
				Datacenter:   s.client.DC,
				ETag:         awsToString(obj.ETag),
			}

			if err := callback(info); err != nil {
				return total, totalSize, err
			}

			total++
			totalSize += size
		}
	}

	return total, totalSize, nil
}

// DeleteObject deletes a single object from S3
func (s *Scanner) DeleteObject(ctx context.Context, key string) error {
	_, err := s.client.Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.client.Bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("failed to delete object %s: %w", key, err)
	}
	return nil
}

// DeleteObjectsBatch deletes multiple objects in a batch
func (s *Scanner) DeleteObjectsBatch(ctx context.Context, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	// Create delete objects input
	objs := make([]types.ObjectIdentifier, len(keys))
	for i, key := range keys {
		objs[i] = types.ObjectIdentifier{
			Key: &key,
		}
	}

	// S3 allows up to 1000 objects per delete request
	batchSize := 1000
	var failedKeys []string

	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}

		batchObjs := objs[i:end]
		resp, err := s.client.Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: &s.client.Bucket,
			Delete: &types.Delete{
				Objects: batchObjs,
			},
		})
		if err != nil {
			return failedKeys, fmt.Errorf("failed to delete objects batch: %w", err)
		}

		// Successfully deleted objects are in resp.Deleted
		for _, errObj := range resp.Errors {
			if errObj.Key != nil {
				failedKeys = append(failedKeys, *errObj.Key)
			}
		}
	}

	return failedKeys, nil
}

// ExtractUserIDFromKey attempts to extract a user ID from an object key
// Object key format examples:
// - "123/uuid-file-name" -> userID = "123"
// - "123/file-data/456/MlData" -> userID = "123"
func ExtractUserIDFromKey(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// awsToString safely converts *string to string
func awsToString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
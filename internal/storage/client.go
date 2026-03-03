package storage

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/viper"
)

// S3Client represents an S3 client for a specific datacenter
type S3Client struct {
	Client *s3.Client
	Bucket string
	DC     string
}

// S3Config holds S3 configuration for multiple datacenters
type S3Config struct {
	Clients map[string]*S3Client
	hotDC   string
}

// NewS3Config creates a new S3 configuration from viper
func NewS3Config() (*S3Config, error) {
	cfg := &S3Config{
		Clients: make(map[string]*S3Client),
	}

	datacenters := []string{"hot", "hot-b2", "wasabi", "scaleway"}

	cfg.hotDC = "hot" // Default
	if hs := viper.GetString("s3.hot_storage.primary"); hs != "" {
		cfg.hotDC = hs
	}

	var failedDCs []string

	for _, dc := range datacenters {
		bucket := viper.GetString(fmt.Sprintf("s3.buckets.%s", dc))
		endpoint := viper.GetString(fmt.Sprintf("s3.endpoints.%s", dc))
		accessKey := viper.GetString(fmt.Sprintf("s3.%s.access_key", dc))
		secretKey := viper.GetString(fmt.Sprintf("s3.%s.secret_key", dc))

		// Support environment variable expansion
		accessKey = expandEnv(accessKey)
		secretKey = expandEnv(secretKey)

		// Skip if bucket or credentials are empty
		if bucket == "" {
			failedDCs = append(failedDCs, fmt.Sprintf("%s (no bucket configured)", dc))
			continue
		}
		if accessKey == "" || secretKey == "" {
			failedDCs = append(failedDCs, fmt.Sprintf("%s (no credentials configured)", dc))
			continue
		}

		// Load AWS configuration
		awsCfg, err := config.LoadDefaultConfig(
			context.TODO(),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
			config.WithRegion("us-east-1"),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to load S3 config for %s: %w", dc, err)
		}

		// Create S3 client
		client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			// Set endpoint for S3-compatible services
			if endpoint != "" {
				o.BaseEndpoint = &endpoint
			}
			// Force path-style URLs for non-AWS S3
			if endpoint != "" && !strings.Contains(endpoint, "amazonaws.com") {
				o.UsePathStyle = true
			}
		})

		cfg.Clients[dc] = &S3Client{
			Client: client,
			Bucket: bucket,
			DC:     dc,
		}
	}

	// Ensure at least one datacenter is configured
	if len(cfg.Clients) == 0 {
		return nil, fmt.Errorf("no S3 datacenters configured properly. Failed: %v", failedDCs)
	}

	return cfg, nil
}

// GetHotClient returns the S3 client for the hot datacenter
func (c *S3Config) GetHotClient() *S3Client {
	if client, ok := c.Clients[c.hotDC]; ok {
		return client
	}
	// Fallback to first available client
	for _, client := range c.Clients {
		return client
	}
	return nil
}

// GetClient returns the S3 client for the specified datacenter
func (c *S3Config) GetClient(dc string) *S3Client {
	return c.Clients[dc]
}

// ListDatacenters returns all configured datacenter names
func (c *S3Config) ListDatacenters() []string {
	dcs := make([]string, 0, len(c.Clients))
	for dc := range c.Clients {
		dcs = append(dcs, dc)
	}
	return dcs
}

// expandEnv expands environment variables in a string
func expandEnv(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		envKey := s[2 : len(s)-1]
		return os.Getenv(envKey)
	}
	return s
}
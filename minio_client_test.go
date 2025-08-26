package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestMinIOClientWithBase64File(t *testing.T) {
	logger := zap.NewNop()
	config := MinIOConfig{
		Endpoint:        "221.230.88.202:9000",
		AccessKeyID:     "weiyi-minio-ak",
		SecretAccessKey: "a52bd3f2c1369db9547b8887891aed8b",
		BucketName:      "ocr",
		UseSSL:          false,
		Region:          "us-east-1",
	}

	client, err := NewMinIOClient(config, logger)
	if err != nil {
		t.Fatalf("Failed to create MinIO client: %v", err)
	}

	base64Data, err := readBase64File("base64.pic")
	if err != nil {
		t.Fatalf("Failed to read base64.pic file: %v", err)
	}

	fileName := ""
	url, err := client.UploadBase64Image(context.Background(), base64Data, fileName)
	if err != nil {
		t.Fatalf("Failed to upload image to MinIO: %v", err)
	}

	assert.NotEmpty(t, url)
	assert.Contains(t, url, config.BucketName)
	assert.Contains(t, url, fileName)

	t.Logf("Successfully uploaded image to MinIO. URL: %s", url)
}

func readBase64File(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	content := strings.TrimSpace(string(data))

	if !strings.HasPrefix(content, "data:image/") {
		content = "data:image/jpeg;base64," + content
	}

	return content, nil
}

func TestValidateBase64Format(t *testing.T) {
	tests := []struct {
		name        string
		base64Data  string
		expectError bool
		description string
	}{
		{
			name:        "Valid format with data URL prefix",
			base64Data:  "data:image/jpeg;base64," + createValidBase64(),
			expectError: false,
			description: "Should accept valid data URL format",
		},
		{
			name:        "Invalid format - missing semicolon",
			base64Data:  "data:image/jpegbase64," + createValidBase64(),
			expectError: true,
			description: "Should reject invalid format",
		},
		{
			name:        "Empty base64 data",
			base64Data:  "",
			expectError: true,
			description: "Should reject empty data",
		},
		{
			name:        "Invalid base64 characters",
			base64Data:  "data:image/jpeg;base64,invalid-base64-data!!!",
			expectError: true,
			description: "Should reject invalid base64 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := strings.Split(strings.TrimPrefix(tt.base64Data, "data:image/"), ";base64,")
			if len(parts) != 2 && tt.expectError {
				return
			}
			if len(parts) == 2 {
				_, err := base64.StdEncoding.DecodeString(parts[1])
				if tt.expectError {
					assert.Error(t, err, tt.description)
				} else {
					assert.NoError(t, err, tt.description)
				}
			}
		})
	}
}

func TestGetFileURL(t *testing.T) {
	config := MinIOConfig{
		Endpoint:        "221.230.88.202:9000",
		AccessKeyID:     "weiyi-minio-ak",
		SecretAccessKey: "a52bd3f2c1369db9547b8887891aed8b",
		BucketName:      "ocr",
		UseSSL:          false,
		Region:          "us-east-1",
	}
	client, err := NewMinIOClient(config, logger)
	if err != nil {
		t.Fatalf("Failed to create MinIO client: %v", err)
	}

	fileName := "image_1753084304_20250721_155157.jpg"
	expectedURL := "http://221.230.88.202:9000/ocr/image_1753084304_20250721_155157.jpg"

	url := client.GetFileURL(fileName)

	assert.Equal(t, expectedURL, url)
}

func TestGetFileURL_WithSSL(t *testing.T) {
	logger := zap.NewNop()
	client := &MinIOClient{
		bucketName: "test-bucket",
		endpoint:   "localhost:9000",
		useSSL:     true,
		logger:     logger,
	}

	fileName := "test-image.jpg"
	expectedURL := "https://localhost:9000/test-bucket/test-image.jpg"

	url := client.GetFileURL(fileName)

	assert.Equal(t, expectedURL, url)
}

func TestGenerateUniqueFileName(t *testing.T) {
	originalName := "test.jpg"

	uniqueName := generateUniqueFileName(originalName)

	assert.True(t, strings.HasPrefix(uniqueName, "test_"))
	assert.True(t, strings.HasSuffix(uniqueName, ".jpg"))
	assert.NotEqual(t, originalName, uniqueName)

	uniqueName2 := generateUniqueFileName(originalName)
	assert.NotEqual(t, uniqueName, uniqueName2)
}

func TestGetFileExtension(t *testing.T) {
	tests := []struct {
		contentType string
		expected    string
	}{
		{"image/jpeg", "jpg"},
		{"image/png", "png"},
		{"image/gif", "gif"},
		{"image/webp", "webp"},
		{"image/unknown", "jpg"},
		{"application/octet-stream", "jpg"},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			ext := getFileExtension(tt.contentType)
			assert.Equal(t, tt.expected, ext)
		})
	}
}

func createValidBase64() string {
	return base64.StdEncoding.EncodeToString([]byte("fake image data"))
}

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap"
)

type MinIOUploader interface {
	UploadBase64Image(ctx context.Context, base64Data string, fileName string) (string, error)
	UploadFile(ctx context.Context, data io.Reader, fileName string, fileSize int64, contentType string) (string, error)
	GetFileURL(fileName string) string
	GetObject(fileName string) ([]byte, error)
}

type MinIOClient struct {
	client     *minio.Client
	bucketName string
	endpoint   string
	useSSL     bool
	timeout    time.Duration
	logger     *zap.Logger
}

func NewMinIOClient(config MinIOConfig, logger *zap.Logger) (*MinIOClient, error) {
	minioClient, err := minio.New(config.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKeyID, config.SecretAccessKey, ""),
		Secure: config.UseSSL,
		Region: config.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MinIO client: %w", err)
	}

	client := &MinIOClient{
		client:     minioClient,
		bucketName: config.BucketName,
		endpoint:   config.Endpoint,
		useSSL:     config.UseSSL,
		timeout:    config.Timeout,
		logger:     logger,
	}

	if err := client.ensureBucketExists(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ensure bucket exists: %w", err)
	}

	return client, nil
}

func (m *MinIOClient) ensureBucketExists(ctx context.Context) error {
	// 为桶操作设置超时
	if m.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.timeout)
		defer cancel()
	}

	exists, err := m.client.BucketExists(ctx, m.bucketName)
	if err != nil {
		return fmt.Errorf("failed to check if bucket exists: %w", err)
	}

	if !exists {
		err = m.client.MakeBucket(ctx, m.bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
		m.logger.Info("Created MinIO bucket", zap.String("bucket", m.bucketName))
	}

	// 设置bucket为公共读取策略，允许永久直接访问
	if err := m.setBucketPublicReadPolicy(ctx); err != nil {
		m.logger.Warn("Failed to set bucket public read policy", 
			zap.String("bucket", m.bucketName), 
			zap.Error(err))
		// 不返回错误，因为bucket策略设置失败不应该阻止程序运行
	} else {
		m.logger.Info("Set bucket public read policy successfully", 
			zap.String("bucket", m.bucketName))
	}

	return nil
}

func (m *MinIOClient) UploadBase64Image(ctx context.Context, base64Data string, fileName string) (string, error) {
	if m == nil {
		return "", fmt.Errorf("MinIOClient is nil")
	}
	var decoded []byte
	var contentType string
	var err error

	// 检查是否包含 data:image/ 前缀
	if strings.HasPrefix(base64Data, "data:image/") {
		// 处理完整的 data URI 格式
		base64Data = strings.TrimPrefix(base64Data, "data:image/")
		parts := strings.Split(base64Data, ";base64,")
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid base64 data URI format")
		}
		contentType = "image/" + parts[0]
		decoded, err = base64.StdEncoding.DecodeString(parts[1])
	} else {
		// 处理纯 base64 数据
		decoded, err = base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			return "", fmt.Errorf("failed to decode base64: %w", err)
		}
		// 通过文件头检测图片类型
		contentType = detectImageContentType(decoded)
	}

	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	reader := bytes.NewReader(decoded)

	if fileName == "" {
		fileName = fmt.Sprintf("image_%d.%s", time.Now().Unix(), getFileExtension(contentType))
	}

	return m.UploadFile(ctx, reader, fileName, int64(len(decoded)), contentType)
}

func (m *MinIOClient) UploadFile(ctx context.Context, data io.Reader, fileName string, fileSize int64, contentType string) (string, error) {
	if m == nil {
		return "", fmt.Errorf("MinIOClient is nil")
	}
	// 为上传操作设置超时
	if m.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.timeout)
		defer cancel()
	}

	fileName = generateUniqueFileName(fileName)

	uploadInfo, err := m.client.PutObject(ctx, m.bucketName, fileName, data, fileSize, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload file to MinIO: %w", err)
	}

	m.logger.Info("File uploaded successfully",
		zap.String("bucket", m.bucketName),
		zap.String("filename", fileName),
		zap.String("etag", uploadInfo.ETag),
		zap.Int64("size", uploadInfo.Size),
	)

	return m.GetFileURL(fileName), nil
}

func (m *MinIOClient) GetFileURL(fileName string) string {
	if m == nil {
		return ""
	}
	protocol := "http"
	if m.useSSL {
		protocol = "https"
	}
	return fmt.Sprintf("%s://%s/%s/%s", protocol, m.endpoint, m.bucketName, fileName)
}

// GetObject 从MinIO获取文件内容
func (m *MinIOClient) GetObject(fileName string) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("MinIOClient is nil")
	}
	ctx := context.Background()
	
	// 为获取操作设置超时
	if m.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.timeout)
		defer cancel()
	}
	
	// 获取对象
	object, err := m.client.GetObject(ctx, m.bucketName, fileName, minio.GetObjectOptions{})
	if err != nil {
		m.logger.Error("获取MinIO对象失败",
			zap.String("bucket", m.bucketName),
			zap.String("fileName", fileName),
			zap.Error(err))
		return nil, fmt.Errorf("获取MinIO对象失败: %w", err)
	}
	defer object.Close()
	
	// 读取所有数据
	data, err := io.ReadAll(object)
	if err != nil {
		m.logger.Error("读取MinIO对象数据失败",
			zap.String("bucket", m.bucketName),
			zap.String("fileName", fileName),
			zap.Error(err))
		return nil, fmt.Errorf("读取MinIO对象数据失败: %w", err)
	}
	
	m.logger.Info("成功获取MinIO对象",
		zap.String("bucket", m.bucketName),
		zap.String("fileName", fileName),
		zap.Int("dataSize", len(data)))
	
	return data, nil
}

func generateUniqueFileName(originalName string) string {
	ext := filepath.Ext(originalName)
	name := strings.TrimSuffix(originalName, ext)
	timestamp := time.Now().Format("20060102_150405")
	return fmt.Sprintf("%s_%s%s", name, timestamp, ext)
}

func getFileExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	default:
		return "jpg"
	}
}

// detectImageContentType 通过文件头检测图片类型
func detectImageContentType(data []byte) string {
	if len(data) < 8 {
		return "image/jpeg" // 默认返回 JPEG
	}

	// 检测 JPEG
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8 {
		return "image/jpeg"
	}

	// 检测 PNG
	if len(data) >= 8 &&
		data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 &&
		data[4] == 0x0D && data[5] == 0x0A && data[6] == 0x1A && data[7] == 0x0A {
		return "image/png"
	}

	// 检测 GIF
	if len(data) >= 6 &&
		((data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x38 && data[4] == 0x37 && data[5] == 0x61) ||
			(data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x38 && data[4] == 0x39 && data[5] == 0x61)) {
		return "image/gif"
	}

	// 检测 WebP
	if len(data) >= 12 &&
		data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 &&
		data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50 {
		return "image/webp"
	}

	// 默认返回 JPEG
	return "image/jpeg"
}

// setBucketPublicReadPolicy 设置bucket为公共读取策略
func (m *MinIOClient) setBucketPublicReadPolicy(ctx context.Context) error {
	// 定义公共读取策略
	policy := map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []map[string]interface{}{
			{
				"Effect": "Allow",
				"Principal": "*",
				"Action": []string{
					"s3:GetObject",
				},
				"Resource": []string{
					fmt.Sprintf("arn:aws:s3:::%s/*", m.bucketName),
				},
			},
		},
	}

	// 将策略转换为JSON字符串
	policyBytes, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("failed to marshal bucket policy: %w", err)
	}

	// 设置bucket策略
	err = m.client.SetBucketPolicy(ctx, m.bucketName, string(policyBytes))
	if err != nil {
		return fmt.Errorf("failed to set bucket policy: %w", err)
	}

	return nil
}

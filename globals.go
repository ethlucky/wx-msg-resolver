package main

import (
	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// 全局变量
var (
	cfg                   *Config
	logger                *zap.Logger
	db                    *gorm.DB
	rdb                   *redis.Client
	redisUtil             RedisUtil
	mqManager             MQManager
	ocrService            OCRService
	wsClient              WebSocketClient
	messageForwarder      MessageForwarder
	wechatBot             WechatBot
	minioClient           MinIOUploader
	ocrWalletService      OcrWalletService
	webhookQueueProcessor WebhookQueueProcessor
	wxMsgAPIService       WxMsgAPIService
)
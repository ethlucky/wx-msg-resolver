package main

import (
	"fmt"
	"log"
	"net/http"

	"go.uber.org/zap"
)


func main() {
	// 初始化配置
	if err := initConfig(); err != nil {
		log.Fatalf("初始化配置失败: %v", err)
	}

	// 初始化日志
	if err := initLogger(); err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}
	defer logger.Sync()

	logger.Info("应用启动", zap.String("name", cfg.App.Name), zap.String("version", cfg.App.Version))

	// 初始化数据库
	if err := initDatabase(); err != nil {
		logger.Fatal("初始化数据库失败", zap.Error(err))
	}

	// 初始化Redis
	if err := initRedis(); err != nil {
		logger.Fatal("初始化Redis失败", zap.Error(err))
	}

	// 初始化MQ管理器
	if err := initMQManager(); err != nil {
		logger.Fatal("初始化MQ管理器失败", zap.Error(err))
	}

	// 初始化OCR服务
	if err := initOCRService(); err != nil {
		logger.Fatal("初始化OCR服务失败", zap.Error(err))
	}

	// 初始化MinIO客户端
	if err := initMinIOClient(); err != nil {
		logger.Fatal("初始化MinIO客户端失败", zap.Error(err))
	}

	// 初始化微信机器人客户端 (从WebSocket配置中解析)
	if err := initWechatBot(); err != nil {
		logger.Fatal("初始化微信机器人客户端失败", zap.Error(err))
	}

	// 初始化微信消息API服务
	if err := initWxMsgAPIService(); err != nil {
		logger.Fatal("初始化微信消息API服务失败", zap.Error(err))
	}


	// 初始化消息转发器 (需要OCR服务和微信机器人客户端)
	if err := initMessageForwarder(); err != nil {
		logger.Fatal("初始化消息转发器失败，消息转发功能是必需的", zap.Error(err))
	}

	// 初始化WebSocket客户端
	if err := initWebSocketClient(); err != nil {
		logger.Fatal("初始化WebSocket客户端失败", zap.Error(err))
	}

	// 初始化OCR钱包服务
	if err := initOcrWalletService(); err != nil {
		logger.Fatal("初始化OCR钱包服务失败", zap.Error(err))
	}

	// 启动HTTP服务器
	startServer()
}

// startServer 启动HTTP服务器
func startServer() {
	// 初始化路由
	router := initRoutes()

	// 创建HTTP服务器
	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// 启动服务器
	go func() {
		logger.Info("HTTP服务器启动", zap.String("address", server.Addr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("HTTP服务器启动失败", zap.Error(err))
		}
	}()

	// 优雅关闭
	gracefulShutdown(server)
}

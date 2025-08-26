package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// gracefulShutdown 优雅关闭
func gracefulShutdown(server *http.Server) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("服务器正在关闭...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 关闭HTTP服务器
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("服务器强制关闭", zap.Error(err))
	}

	// 关闭数据库连接
	if db != nil {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}

	// 关闭Redis连接
	if rdb != nil {
		if err := rdb.Close(); err != nil {
			logger.Error("关闭Redis连接失败", zap.Error(err))
		} else {
			logger.Info("Redis连接已关闭")
		}
	}

	// 关闭MQ管理器
	if mqManager != nil {
		if err := mqManager.Close(); err != nil {
			logger.Error("关闭MQ管理器失败", zap.Error(err))
		} else {
			logger.Info("MQ管理器已关闭")
		}
	}

	// 关闭WebSocket客户端
	if wsClient != nil {
		if err := wsClient.Stop(); err != nil {
			logger.Error("关闭WebSocket客户端失败", zap.Error(err))
		} else {
			logger.Info("WebSocket客户端已关闭")
		}
	}

	// 关闭Webhook队列处理器
	if webhookQueueProcessor != nil {
		if err := webhookQueueProcessor.Stop(); err != nil {
			logger.Error("关闭Webhook队列处理器失败", zap.Error(err))
		} else {
			logger.Info("Webhook队列处理器已关闭")
		}
	}

	logger.Info("服务器已关闭")
}
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// WxMessageSendResponse 微信消息发送响应
type WxMessageSendResponse struct {
	Success bool   `json:"success"` // 发送是否成功
	Message string `json:"message"` // 响应消息
}

// SendTextRequest 发送文本消息请求
type SendTextHTTPRequest struct {
	Content  string `json:"content" binding:"required"`    // 消息内容
	ToUserID string `json:"to_user_id" binding:"required"` // 接收者用户ID
}

// SendTextResponse 发送文本消息响应
type SendTextHTTPResponse struct {
	Success     bool   `json:"success"`                 // 发送是否成功
	Message     string `json:"message"`                 // 响应消息
	ToUserName  string `json:"to_user_name"`            // 接收者用户名
	ClientMsgId int64  `json:"client_msg_id,omitempty"` // 客户端消息ID
	CreateTime  int64  `json:"create_time,omitempty"`   // 创建时间
	NewMsgId    int64  `json:"new_msg_id,omitempty"`    // 新消息ID
}

// SendImageRequest 发送图片消息请求
type SendImageHTTPRequest struct {
	ToUserName   string `json:"to_user_name" binding:"required"`  // 接收者用户名
	ImageContent string `json:"image_content" binding:"required"` // 图片内容(base64编码)
}

// SendImageResponse 发送图片消息响应
type SendImageHTTPResponse struct {
	Success      bool   `json:"success"`                  // 发送是否成功
	Message      string `json:"message"`                  // 响应消息
	ToUserName   string `json:"to_user_name"`             // 接收者用户名
	MsgId        int64  `json:"msg_id,omitempty"`         // 消息ID
	FromUserName string `json:"from_user_name,omitempty"` // 发送者用户名
	CreateTime   int64  `json:"create_time,omitempty"`    // 创建时间
	NewMsgId     int64  `json:"new_msg_id,omitempty"`     // 新消息ID
}

// WxMessageSendRequest 微信消息发送请求
type WxMessageSendRequest struct {
	ID       int64  `json:"id" binding:"required"`           // 微信消息信息ID
	GroupID  string `json:"group_id" binding:"required"`     // 群组ID
	MsgTypes []int  `json:"message_type" binding:"required"` // 消息类型数组：支持1=文本，3=图片
	Content  string `json:"content"`                         // 消息内容（仅文本消息需要）
	PicURL   string `json:"pic_url"`                         // 图片URL（仅图片消息需要）
}

// initRoutes 初始化路由
func initRoutes() *gin.Engine {
	// 设置运行模式
	if !cfg.App.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// 中间件
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	// 健康检查
	router.GET("/health", handleHealthCheck)

	// API路由组
	api := router.Group("/api/v1")
	{
		// OCR相关路由
		ocr := api.Group("/wx")
		{
			ocr.POST("/send/textAndImage", handleWxMessageSend) // 微信消息发送
		}

		// Webhook相关路由
		webhook := api.Group("/webhook")
		{
			webhook.GET("/stats", handleWebhookStats) // 获取Webhook推送统计
		}
	}

	return router
}

// handleWxMessageSend 微信消息发送处理函数
func handleWxMessageSend(c *gin.Context) {
	var req WxMessageSendRequest

	// 绑定请求参数
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, WxMessageSendResponse{
			Success: false,
			Message: fmt.Sprintf("请求参数错误: %v", err),
		})
		return
	}

	// 验证ID必须大于0
	if req.ID <= 0 {
		c.JSON(http.StatusBadRequest, WxMessageSendResponse{
			Success: false,
			Message: "ID必须大于0",
		})
		return
	}

	// 验证消息类型数组
	if len(req.MsgTypes) == 0 {
		c.JSON(http.StatusBadRequest, WxMessageSendResponse{
			Success: false,
			Message: "消息类型数组不能为空",
		})
		return
	}

	// 验证消息类型只能是1或3
	hasTextMsg := false
	hasImageMsg := false
	for _, msgType := range req.MsgTypes {
		if msgType != 1 && msgType != 3 {
			c.JSON(http.StatusBadRequest, WxMessageSendResponse{
				Success: false,
				Message: "不支持的消息类型，仅支持文本(1)和图片(3)",
			})
			return
		}
		if msgType == 1 {
			hasTextMsg = true
		}
		if msgType == 3 {
			hasImageMsg = true
		}
	}

	// 文本消息需要content参数
	if hasTextMsg && req.Content == "" {
		c.JSON(http.StatusBadRequest, WxMessageSendResponse{
			Success: false,
			Message: "包含文本消息时content参数不能为空",
		})
		return
	}

	// 图片消息需要pic_url参数
	if hasImageMsg && req.PicURL == "" {
		c.JSON(http.StatusBadRequest, WxMessageSendResponse{
			Success: false,
			Message: "包含图片消息时pic_url参数不能为空",
		})
		return
	}

	// 检查OCR钱包服务是否可用
	if ocrWalletService == nil {
		c.JSON(http.StatusServiceUnavailable, WxMessageSendResponse{
			Success: false,
			Message: "OCR钱包服务不可用",
		})
		return
	}

	// 调用服务发送消息
	response, err := ocrWalletService.SendWalletMessageWithData(req.ID, req.GroupID, req.MsgTypes, req.Content, req.PicURL)
	if err != nil {
		logger.Error("微信消息发送失败",
			zap.Int64("id", req.ID),
			zap.String("group_id", req.GroupID),
			zap.Ints("msg_types", req.MsgTypes),
			zap.String("content", req.Content),
			zap.String("pic_url", req.PicURL),
			zap.Error(err))

		c.JSON(http.StatusInternalServerError, WxMessageSendResponse{
			Success: false,
			Message: fmt.Sprintf("发送消息失败: %v", err),
		})
		return
	}

	// 返回响应
	if response.Success {
		c.JSON(http.StatusOK, WxMessageSendResponse{
			Success: response.Success,
			Message: response.Message,
		})
	} else {
		c.JSON(http.StatusOK, WxMessageSendResponse{
			Success: response.Success,
			Message: response.Message,
		})
	}

	logger.Info("微信消息发送完成",
		zap.Int64("id", req.ID),
		zap.String("group_id", req.GroupID),
		zap.Ints("msg_types", req.MsgTypes),
		zap.Bool("success", response.Success))
}

// handleHealthCheck 健康检查处理函数
func handleHealthCheck(c *gin.Context) {
	// 检查各个组件的健康状态
	health := gin.H{
		"status":     "ok",
		"message":    "服务正常运行",
		"timestamp":  time.Now().Format(time.RFC3339),
		"components": gin.H{},
	}

	components := health["components"].(gin.H)
	overallStatus := "ok"

	// 检查数据库连接
	if db != nil {
		if sqlDB, err := db.DB(); err == nil {
			if err := sqlDB.Ping(); err == nil {
				components["database"] = gin.H{"status": "ok", "message": "数据库连接正常"}
			} else {
				components["database"] = gin.H{"status": "error", "message": "数据库连接失败", "error": err.Error()}
				overallStatus = "error"
			}
		} else {
			components["database"] = gin.H{"status": "error", "message": "获取数据库实例失败", "error": err.Error()}
			overallStatus = "error"
		}
	} else {
		components["database"] = gin.H{"status": "disabled", "message": "数据库未初始化"}
	}

	// 检查Redis连接
	if rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := rdb.Ping(ctx).Err(); err == nil {
			components["redis"] = gin.H{"status": "ok", "message": "Redis连接正常"}
		} else {
			components["redis"] = gin.H{"status": "error", "message": "Redis连接失败", "error": err.Error()}
			overallStatus = "error"
		}
		cancel()
	} else {
		components["redis"] = gin.H{"status": "disabled", "message": "Redis未初始化"}
	}

	// 检查MQ管理器
	if mqManager != nil {
		if mqManager.HealthCheck() {
			components["rabbitmq"] = gin.H{"status": "ok", "message": "RabbitMQ连接正常"}
		} else {
			components["rabbitmq"] = gin.H{"status": "error", "message": "RabbitMQ连接失败"}
			overallStatus = "error"
		}
	} else {
		components["rabbitmq"] = gin.H{"status": "disabled", "message": "RabbitMQ未初始化"}
	}

	// 检查OCR服务
	if cfg.OCR.Enabled && ocrService != nil {
		components["ocr"] = gin.H{"status": "ok", "message": "OCR服务可用"}
	} else {
		components["ocr"] = gin.H{"status": "disabled", "message": "OCR服务未启用"}
	}

	// 检查MinIO客户端
	if minioClient != nil {
		components["minio"] = gin.H{"status": "ok", "message": "MinIO客户端可用"}
	} else {
		components["minio"] = gin.H{"status": "disabled", "message": "MinIO客户端未启用"}
	}

	// 检查WebSocket客户端
	if cfg.WebSocket.Enabled && wsClient != nil {
		if wsClient.IsConnected() {
			components["websocket"] = gin.H{"status": "ok", "message": "WebSocket客户端连接正常"}
		} else {
			components["websocket"] = gin.H{"status": "error", "message": "WebSocket客户端连接断开"}
			overallStatus = "error"
		}
	} else {
		components["websocket"] = gin.H{"status": "disabled", "message": "WebSocket客户端未启用"}
	}

	// 设置整体状态
	health["status"] = overallStatus
	if overallStatus == "error" {
		health["message"] = "部分组件异常"
	}

	// 根据整体状态返回适当的HTTP状态码
	if overallStatus == "ok" {
		c.JSON(http.StatusOK, health)
	} else {
		c.JSON(http.StatusServiceUnavailable, health)
	}
}

// handleWebhookStats 获取Webhook推送统计 (已弃用 - 不再提供统计功能)
func handleWebhookStats(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Webhook统计功能已移除",
		"data":    map[string]interface{}{},
	})
}

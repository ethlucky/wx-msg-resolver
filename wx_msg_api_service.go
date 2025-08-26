package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// WxMsgAPIService 微信消息API服务接口
type WxMsgAPIService interface {
	// SendTextAndImage 同时发送文字和图片
	SendTextAndImage(req *WxMsgAPITextAndImageRequest) (*WxMsgAPIResponse, error)
}

// WxMsgAPITextAndImageRequest 发送文字和图片请求
type WxMsgAPITextAndImageRequest struct {
	TextContent  string `json:"text_content"`
	ImageContent string `json:"image_content"`
	ToUserName   string `json:"to_user_name"`
}

// WxMsgAPIResponse 微信消息API响应
type WxMsgAPIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// wxMsgAPIServiceImpl 微信消息API服务实现
type wxMsgAPIServiceImpl struct {
	baseURL    string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewWxMsgAPIService 创建微信消息API服务
func NewWxMsgAPIService(baseURL string, logger *zap.Logger) WxMsgAPIService {
	return &wxMsgAPIServiceImpl{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       90 * time.Second,
				DisableKeepAlives:     false,
				ExpectContinueTimeout: 1 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 20 * time.Second,
				DisableCompression:    false,
			},
		},
		logger: logger,
	}
}

// SendTextAndImage 同时发送文字和图片
func (s *wxMsgAPIServiceImpl) SendTextAndImage(req *WxMsgAPITextAndImageRequest) (*WxMsgAPIResponse, error) {
	url := fmt.Sprintf("%s/api/v1/messages/group/send-text-image", s.baseURL)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求数据失败: %w", err)
	}

	s.logger.Info("发送文字和图片消息请求",
		zap.String("url", url),
		zap.String("to_user", req.ToUserName),
		zap.Bool("has_text", req.TextContent != ""),
		zap.Bool("has_image", req.ImageContent != ""))

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建HTTP请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("发送HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应数据失败: %w", err)
	}

	var response WxMsgAPIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("解析响应数据失败: %w", err)
	}

	s.logger.Info("发送文字和图片消息响应",
		zap.Int("status_code", resp.StatusCode),
		zap.Bool("success", response.Success),
		zap.String("message", response.Message))

	// 检查HTTP状态码
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API请求失败: HTTP状态码 %d, 消息: %s", resp.StatusCode, response.Message)
	}

	// 检查业务状态
	if !response.Success {
		return nil, fmt.Errorf("发送消息失败: %s", response.Message)
	}

	s.logger.Info("文字和图片消息发送成功",
		zap.String("to_user", req.ToUserName))

	return &response, nil
}

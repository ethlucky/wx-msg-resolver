package main

import (
	"encoding/base64"
	"fmt"

	"go.uber.org/zap"
)

// OcrWalletService OCR钱包服务接口
type OcrWalletService interface {
	// getImageBase64FromMinIO 从MinIO获取图片的base64内容
	getImageBase64FromMinIO(picURL string) (string, error)
	// SendWalletMessage 发送钱包相关消息
	SendWalletMessage(id int64, msgTypes []int, content string) (*WxMsgAPIResponse, error)
	// SendWalletMessageWithData 使用传入的数据发送钱包相关消息，不查询数据库
	SendWalletMessageWithData(id int64, groupID string, msgTypes []int, content string, picURL string) (*WxMsgAPIResponse, error)
}

// ocrWalletServiceImpl OCR钱包服务实现
type ocrWalletServiceImpl struct {
	logger          *zap.Logger
	minioClient     MinIOUploader
	wxMsgAPIService WxMsgAPIService
}

// NewOcrWalletService 创建OCR钱包服务
func NewOcrWalletService(logger *zap.Logger, minioClient MinIOUploader, wxMsgAPIService WxMsgAPIService) OcrWalletService {
	return &ocrWalletServiceImpl{
		logger:          logger,
		minioClient:     minioClient,
		wxMsgAPIService: wxMsgAPIService,
	}
}

// getImageBase64FromMinIO 从MinIO获取图片的base64内容
func (s *ocrWalletServiceImpl) getImageBase64FromMinIO(picURL string) (string, error) {
	if picURL == "" {
		return "", fmt.Errorf("图片URL为空")
	}

	// 从MinIO获取图片数据
	imageData, err := s.minioClient.GetObject(picURL)
	if err != nil {
		s.logger.Error("从MinIO获取图片失败",
			zap.String("pic_url", picURL),
			zap.Error(err))
		return "", fmt.Errorf("从MinIO获取图片失败: %w", err)
	}

	// 转换为base64
	base64Data := base64.StdEncoding.EncodeToString(imageData)

	s.logger.Info("成功获取图片base64数据",
		zap.String("pic_url", picURL),
		zap.Int("data_size", len(imageData)),
		zap.Int("base64_size", len(base64Data)))

	return base64Data, nil
}

// SendWalletMessage 发送钱包相关消息（已弃用，请使用 SendWalletMessageWithData）
func (s *ocrWalletServiceImpl) SendWalletMessage(id int64, msgTypes []int, content string) (*WxMsgAPIResponse, error) {
	s.logger.Warn("SendWalletMessage 方法已弃用，请使用 SendWalletMessageWithData 方法",
		zap.Int64("id", id))

	return &WxMsgAPIResponse{
		Success: false,
		Message: "该方法已弃用，请使用 SendWalletMessageWithData 方法",
	}, fmt.Errorf("该方法已弃用，请使用 SendWalletMessageWithData 方法")
}

// SendWalletMessageWithData 使用传入的数据发送钱包相关消息，不查询数据库
func (s *ocrWalletServiceImpl) SendWalletMessageWithData(id int64, groupID string, msgTypes []int, content string, picURL string) (*WxMsgAPIResponse, error) {
	// 构建发送请求
	req := &WxMsgAPITextAndImageRequest{
		ToUserName: groupID, // 使用传入的groupID作为接收人
	}

	// 根据消息类型数组设置内容
	for _, msgType := range msgTypes {
		switch msgType {
		case 1: // 文本消息
			req.TextContent = content
			s.logger.Info("准备发送文本消息",
				zap.Int64("id", id),
				zap.String("group_id", groupID),
				zap.String("to_user", req.ToUserName),
				zap.String("text_content", req.TextContent))

		case 3: // 图片消息
			if picURL == "" {
				return &WxMsgAPIResponse{
					Success: false,
					Message: "图片URL为空",
				}, fmt.Errorf("图片URL为空")
			}

			// 从MinIO获取图片base64数据
			imageBase64, err := s.getImageBase64FromMinIO(picURL)
			if err != nil {
				return &WxMsgAPIResponse{
					Success: false,
					Message: fmt.Sprintf("获取图片失败: %v", err),
				}, err
			}

			req.ImageContent = imageBase64
			s.logger.Info("准备发送图片消息",
				zap.Int64("id", id),
				zap.String("group_id", groupID),
				zap.String("to_user", req.ToUserName),
				zap.String("pic_url", picURL))

		default:
			return &WxMsgAPIResponse{
				Success: false,
				Message: fmt.Sprintf("不支持的消息类型: %d", msgType),
			}, fmt.Errorf("不支持的消息类型: %d", msgType)
		}
	}

	// 调用新的微信消息API服务
	response, err := s.wxMsgAPIService.SendTextAndImage(req)
	if err != nil {
		s.logger.Error("发送消息失败",
			zap.Int64("id", id),
			zap.String("group_id", groupID),
			zap.Ints("msg_types", msgTypes),
			zap.Error(err))
		return &WxMsgAPIResponse{
			Success: false,
			Message: fmt.Sprintf("发送消息失败: %v", err),
		}, err
	}

	s.logger.Info("消息发送完成",
		zap.Int64("id", id),
		zap.String("group_id", groupID),
		zap.Ints("msg_types", msgTypes),
		zap.Bool("success", response.Success))

	return response, nil
}

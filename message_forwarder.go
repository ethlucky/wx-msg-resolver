package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MessageForwarder 消息转发器接口
type MessageForwarder interface {
	// ForwardMessage 转发消息到RabbitMQ
	ForwardMessage(msgData map[string]interface{}) error
	// StartConsumer 启动消息消费者
	StartConsumer(ctx context.Context) error
	// Stop 停止消息转发器
	Stop() error
}

// messageForwarderImpl 消息转发器实现
type messageForwarderImpl struct {
	config                *Config
	db                    *gorm.DB
	logger                *zap.Logger
	mqManager             MQManager
	wechatBot             WechatBot
	ocrService            OCRService
	ocrParser             OCRContentParser
	minioClient           MinIOUploader
	redisUtil             RedisUtil             // Redis工具
	ocrProcessor          OCRProcessor          // OCR处理器
	webhookQueueProcessor WebhookQueueProcessor // Webhook队列处理器
	billProcessingService BillProcessingService // 账单处理服务
	stopChannel           chan struct{}
	stopped               int32      // 原子操作标志位，0=运行中，1=已停止
	stopOnce              sync.Once  // 确保Stop方法只执行一次
	dbMutex               sync.Mutex // 数据库操作互斥锁
}

// NewMessageForwarder 创建消息转发器
func NewMessageForwarder(config *Config, db *gorm.DB, logger *zap.Logger, mqManager MQManager, wechatBot WechatBot, ocrService OCRService, minioClient MinIOUploader, redisUtil RedisUtil) (MessageForwarder, error) {
	if config == nil || db == nil || logger == nil || mqManager == nil {
		return nil, fmt.Errorf("参数不能为空")
	}

	// 创建OCR内容解析器
	ocrParser := NewOCRContentParser(logger)

	// 创建Webhook队列处理器
	webhookQueueProcessor := NewWebhookQueueProcessor(
		redisUtil,
		config,
		logger,
		db,
	)

	// 创建OCR处理器（传入Webhook队列处理器）
	ocrProcessor := NewOCRProcessor(
		redisUtil,
		wechatBot,
		ocrService,
		minioClient,
		webhookQueueProcessor, // 改为传入队列处理器而不是直接的推送器
		config,
		logger,
		db,
	)

	// 创建账单处理服务
	billProcessingService := NewBillProcessingService(db, logger)

	return &messageForwarderImpl{
		config:                config,
		db:                    db,
		logger:                logger,
		mqManager:             mqManager,
		wechatBot:             wechatBot,
		ocrService:            ocrService,
		ocrParser:             ocrParser,
		minioClient:           minioClient,
		redisUtil:             redisUtil,
		ocrProcessor:          ocrProcessor,
		webhookQueueProcessor: webhookQueueProcessor,
		billProcessingService: billProcessingService,
		stopChannel:           make(chan struct{}),
	}, nil
}

// ForwardMessage 转发消息到RabbitMQ
func (m *messageForwarderImpl) ForwardMessage(msgData map[string]interface{}) error {
	// 检查是否已停止
	if atomic.LoadInt32(&m.stopped) == 1 {
		return fmt.Errorf("消息转发器已停止")
	}

	// 解析WebSocket配置获取source_token
	sourceToken, err := m.getCurrentKey()
	if err != nil {
		m.logger.Error("解析WebSocket配置失败", zap.Error(err))
		return err
	}

	// 构建转发消息
	forwardedMsg := &ForwardedMessage{
		OwnerID:       m.config.App.OwnerID,
		SourceToken:   sourceToken,
		ImgURL:        "", // 默认为空
		ProcessedTime: time.Now().Unix(),
	}

	// 从消息数据中提取字段
	if msgType, ok := msgData["msg_type"].(float64); ok {
		forwardedMsg.MsgType = int(msgType)
	}

	// 处理msg_id - 可能是直接的float64或string
	if msgID, ok := msgData["msg_id"].(float64); ok {
		forwardedMsg.MsgID = strconv.FormatFloat(msgID, 'f', 0, 64)
	} else if msgID, ok := msgData["msg_id"].(string); ok {
		forwardedMsg.MsgID = msgID
	}

	// 处理new_msg_id - 可能是直接的float64或string
	if newMsgID, ok := msgData["new_msg_id"].(float64); ok {
		forwardedMsg.NewMsgID = strconv.FormatFloat(newMsgID, 'f', 0, 64)
	} else if newMsgID, ok := msgData["new_msg_id"].(string); ok {
		forwardedMsg.NewMsgID = newMsgID
	}

	// 处理from_user_name - 可能是嵌套在对象中的
	if fromUserName, ok := msgData["from_user_name"].(string); ok {
		forwardedMsg.FromUserName = fromUserName
	} else if fromUserMap, ok := msgData["from_user_name"].(map[string]interface{}); ok {
		if fromUserStr, exists := fromUserMap["str"].(string); exists {
			forwardedMsg.FromUserName = fromUserStr
		}
	}

	// 处理to_user_name - 可能是嵌套在对象中的
	if toUserName, ok := msgData["to_user_name"].(string); ok {
		forwardedMsg.ToUserName = toUserName
	} else if toUserMap, ok := msgData["to_user_name"].(map[string]interface{}); ok {
		if toUserStr, exists := toUserMap["str"].(string); exists {
			forwardedMsg.ToUserName = toUserStr
		}
	}

	// 处理消息时间 - 使用create_time字段
	if createTime, ok := msgData["create_time"].(float64); ok {
		forwardedMsg.MsgTime = int64(createTime)
	} else if msgTime, ok := msgData["msg_time"].(float64); ok {
		forwardedMsg.MsgTime = int64(msgTime)
	} else if msgTime, ok := msgData["msg_time"].(string); ok {
		if timestamp, err := strconv.ParseInt(msgTime, 10, 64); err == nil {
			forwardedMsg.MsgTime = timestamp
		}
	}

	// 处理图片URL（如果存在）
	if imgURL, ok := msgData["img_url"].(string); ok && imgURL != "" {
		forwardedMsg.ImgURL = imgURL
	}

	// 处理消息内容，直接使用websocket_client处理后的结构化数据
	if contentJSON, ok := msgData["content_json"].(string); ok {
		// 解析content结构体
		var contentData map[string]interface{}
		if err := json.Unmarshal([]byte(contentJSON), &contentData); err == nil {
			// 直接使用处理后的wx_id
			if wxID, exists := contentData["wx_id"].(string); exists {
				forwardedMsg.WxID = wxID
			}

			// 根据消息类型处理content
			if forwardedMsg.MsgType == 3 { // 图片消息
				// 图片消息使用aeskey和cdnurl
				if aeskey, hasAeskey := contentData["aeskey"].(string); hasAeskey {
					if cdnurl, hasCdnurl := contentData["cdnurl"].(string); hasCdnurl {
						forwardedMsg.Content = fmt.Sprintf(`{"aeskey": "%s", "cdnurl": "%s"}`, aeskey, cdnurl)
					}
				}
			} else {
				// 文本消息使用text字段
				if text, exists := contentData["text"].(string); exists {
					forwardedMsg.Content = text
				}
			}
		} else {
			// 解析失败，直接使用原始内容并清理控制字符
			forwardedMsg.Content = m.cleanControlChars(contentJSON)
		}
	}

	// 判断消息类型：群消息 vs 个人消息
	// 群消息：from_user_name包含@chatroom
	// 个人消息：from_user_name不包含@chatroom
	if strings.Contains(forwardedMsg.FromUserName, "@chatroom") {
		// 群消息
		forwardedMsg.IsGroupMsg = true
		forwardedMsg.GroupID = forwardedMsg.FromUserName // 群ID就是from_user_name
	} else if strings.Contains(forwardedMsg.ToUserName, "@chatroom") {
		// 群消息
		forwardedMsg.IsGroupMsg = true
		forwardedMsg.GroupID = forwardedMsg.ToUserName // 群ID就是from_user_nameelse {
		forwardedMsg.WxID = forwardedMsg.FromUserName
	} else {
		// 个人消息 - 不再处理个人消息，直接跳过
		forwardedMsg.IsGroupMsg = false
		m.logger.Info("个人消息已跳过处理（不转发到RabbitMQ）",
			zap.String("msg_id", forwardedMsg.MsgID),
			zap.Int("msg_type", forwardedMsg.MsgType),
			zap.String("from_user", forwardedMsg.FromUserName),
			zap.String("to_user", forwardedMsg.ToUserName))
		return nil // 直接返回，不进行后续处理
	}

	// 发布到RabbitMQ（使用确认机制）- 只处理群消息
	err = m.mqManager.PublishJSONWithRetry(
		m.config.RabbitMQ.ExchangeName,
		m.config.RabbitMQ.RoutingKey,
		forwardedMsg,
		10,
	)
	if err != nil {
		return fmt.Errorf("发布消息到RabbitMQ失败: %w", err)
	}

	// 只记录群消息的转发日志
	m.logger.Info("群消息已转发到RabbitMQ",
		zap.String("msg_id", forwardedMsg.MsgID),
		zap.Int("msg_type", forwardedMsg.MsgType),
		zap.String("group_id", forwardedMsg.GroupID),
		zap.String("wx_id", forwardedMsg.WxID))

	return nil
}

// StartConsumer 启动消息消费者
func (m *messageForwarderImpl) StartConsumer(ctx context.Context) error {
	// 检查是否已停止
	if atomic.LoadInt32(&m.stopped) == 1 {
		return fmt.Errorf("消息转发器已停止")
	}

	// 启动Webhook队列处理器（使用配置的并发数）
	if m.webhookQueueProcessor != nil {
		workerCount := m.config.Webhook.ConcurrentPushers
		if workerCount <= 0 {
			workerCount = 5 // 默认值
		}
		if err := m.webhookQueueProcessor.StartWorkers(ctx, workerCount); err != nil {
			m.logger.Error("启动Webhook队列处理器失败", zap.Error(err))
			return fmt.Errorf("启动Webhook队列处理器失败: %w", err)
		}
		m.logger.Info("Webhook队列处理器已启动",
			zap.Int("worker_count", workerCount))
	}

	// 启动OCR处理器（从配置文件读取并发数）
	if m.ocrProcessor != nil {
		workerCount := m.config.OCR.ConcurrentWorkers
		if workerCount <= 0 {
			workerCount = 3 // 默认值
		}
		if err := m.ocrProcessor.StartWorkers(ctx, workerCount); err != nil {
			m.logger.Error("启动OCR处理器失败", zap.Error(err))
			return fmt.Errorf("启动OCR处理器失败: %w", err)
		}
		m.logger.Info("OCR处理器已启动",
			zap.Int("worker_count", workerCount))
	}

	// 启动消息消费者
	handler := MessageHandlerFunc(func(ctx context.Context, body []byte) error {
		// 检查是否已停止
		if atomic.LoadInt32(&m.stopped) == 1 {
			m.logger.Info("消息转发器已停止，跳过消息处理")
			return fmt.Errorf("消息转发器已停止")
		}

		// 反序列化消息
		var forwardedMsg ForwardedMessage
		if err := json.Unmarshal(body, &forwardedMsg); err != nil {
			return fmt.Errorf("反序列化消息失败: %w", err)
		}

		m.logger.Info("开始处理消费的消息",
			zap.String("msg_id", forwardedMsg.MsgID),
			zap.Int("msg_type", forwardedMsg.MsgType))

		// 将消息保存到数据库
		if err := m.saveMessageToDB(forwardedMsg); err != nil {
			return fmt.Errorf("保存消息到数据库失败: %w", err)
		}

		m.logger.Info("消息处理完成", zap.String("msg_id", forwardedMsg.MsgID))
		return nil
	})

	return m.mqManager.StartConsumer(ctx, m.config.RabbitMQ.QueueName, handler)
}

// saveMessageToDB 保存消息到数据库（线程安全）
func (m *messageForwarderImpl) saveMessageToDB(forwardedMsg ForwardedMessage) error {
	// 使用互斥锁保护数据库操作
	m.dbMutex.Lock()
	defer m.dbMutex.Unlock()

	// 检查是否已停止
	if atomic.LoadInt32(&m.stopped) == 1 {
		return fmt.Errorf("消息转发器已停止")
	}

	if forwardedMsg.IsGroupMsg {
		// 保存群消息到wx_group_messages表
		return m.saveGroupMessage(forwardedMsg)
	} else {
		// 个人消息不再处理，直接跳过
		m.logger.Info("个人消息跳过数据库保存",
			zap.String("msg_id", forwardedMsg.MsgID),
			zap.Int("msg_type", forwardedMsg.MsgType),
			zap.String("from_user", forwardedMsg.FromUserName),
			zap.String("to_user", forwardedMsg.ToUserName))
		return nil
	}
}

// saveGroupMessage 保存群消息到数据库
func (m *messageForwarderImpl) saveGroupMessage(forwardedMsg ForwardedMessage) error {
	// 获取发送者昵称
	wxNickName := m.getNickNameFromGroupInfo(forwardedMsg.GroupID, forwardedMsg.WxID)

	// 构建群消息数据库记录
	wxGroupMsg := WxGroupMessage{
		OwnerID:     forwardedMsg.OwnerID,
		SourceToken: forwardedMsg.SourceToken,
		GroupID:     forwardedMsg.GroupID,
		MsgType:     forwardedMsg.MsgType,
		MsgSource:   forwardedMsg.MsgSource,
		Content:     forwardedMsg.Content,
		MsgID:       forwardedMsg.MsgID,
		NewMsgID:    forwardedMsg.NewMsgID,
		WxID:        forwardedMsg.WxID,
		WxNickName:  wxNickName,
		MsgTime:     forwardedMsg.MsgTime,
	}

	// 将msg_id转换为int64用于wx_global_id保存
	msgIDInt, err := strconv.ParseInt(forwardedMsg.MsgID, 10, 64)
	if err != nil {
		return fmt.Errorf("无法将msg_id转换为int64: %w", err)
	}

	// 使用事务保存到数据库
	var isNewRecord bool
	err = m.db.Transaction(func(tx *gorm.DB) error {
		// 使用原生SQL插入
		insertSQL := `INSERT INTO wx_group_messages (
			owner_id, source_token, group_id, msg_type, msg_source, content, 
			msg_id, new_msg_id, wx_id, wx_nick_name, msg_time
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		)`

		result := tx.Exec(insertSQL,
			forwardedMsg.OwnerID,
			wxGroupMsg.SourceToken,
			wxGroupMsg.GroupID,
			wxGroupMsg.MsgType,
			wxGroupMsg.MsgSource,
			wxGroupMsg.Content,
			wxGroupMsg.MsgID,
			wxGroupMsg.NewMsgID,
			wxGroupMsg.WxID,
			wxGroupMsg.WxNickName,
			wxGroupMsg.MsgTime,
		)

		if result.Error != nil {
			// 检查是否为唯一键冲突错误
			if strings.Contains(result.Error.Error(), "Duplicate entry") || strings.Contains(result.Error.Error(), "UNIQUE constraint") {
				m.logger.Debug("群消息已存在，跳过保存", zap.String("msg_id", wxGroupMsg.MsgID))
				isNewRecord = false
				return nil // 重复消息，正常返回，不保存wx_global_id
			}
			return fmt.Errorf("插入群消息数据库失败: %w", result.Error)
		}

		// 消息表保存成功，必须保存到wx_global_id表，否则事务回滚
		wxGlobalID := WxGlobalID{
			OwnerID: forwardedMsg.OwnerID,
			MsgID:   msgIDInt,
			MsgTime: forwardedMsg.MsgTime,
		}
		if err := tx.Create(&wxGlobalID).Error; err != nil {
			// wx_global_id保存失败，整个事务回滚
			return fmt.Errorf("插入wx_global_id数据库失败: %w", err)
		}

		// 判断是否需要处理账单
		billInfo, shouldProcess := m.billProcessingService.ShouldProcessBill(wxGroupMsg)
		if shouldProcess {
			// 处理账单
			if err := m.billProcessingService.ProcessBillInTx(tx, wxGroupMsg, billInfo); err != nil {
				return fmt.Errorf("处理账单失败: %w", err)
			}
		}

		isNewRecord = true
		return nil
	})

	if err != nil {
		return err
	}

	// 事务成功后，如果是新记录，同步更新Redis中的last_msg_time
	if isNewRecord && m.redisUtil != nil {
		ctx := context.Background()
		if err := m.redisUtil.SetLastMsgTime(ctx, m.config.App.OwnerID, forwardedMsg.MsgTime); err != nil {
			m.logger.Warn("更新last_msg_time到Redis失败",
				zap.Int64("msg_time", forwardedMsg.MsgTime),
				zap.Error(err))
		} else {
			m.logger.Debug("更新last_msg_time到Redis成功",
				zap.Int64("msg_time", forwardedMsg.MsgTime))
		}
	}

	// 数据库保存成功后，如果是图片消息，同步加入OCR队列
	if isNewRecord && forwardedMsg.MsgType == 3 && forwardedMsg.Content != "" {
		ocrTask := &OCRTask{
			MsgID:      forwardedMsg.MsgID,
			GroupID:    forwardedMsg.GroupID,
			WxID:       forwardedMsg.WxID,
			WxNickName: wxNickName,
			Content:    forwardedMsg.Content,
			OwnerID:    forwardedMsg.OwnerID,
		}

		if err := m.addOCRTask(ocrTask); err != nil {
			m.logger.Error("加入OCR队列失败",
				zap.String("msg_id", forwardedMsg.MsgID),
				zap.Error(err))
		} else {
			m.logger.Info("图片消息已加入OCR处理队列",
				zap.String("msg_id", forwardedMsg.MsgID))
		}
	}

	m.logger.Info("群消息已保存到数据库",
		zap.Int64("id", wxGroupMsg.ID),
		zap.String("msg_id", wxGroupMsg.MsgID),
		zap.Int("msg_type", wxGroupMsg.MsgType),
		zap.String("group_id", wxGroupMsg.GroupID),
		zap.String("wx_id", wxGroupMsg.WxID))

	return nil
}

// savePersonalMessage 保存个人消息到数据库
func (m *messageForwarderImpl) savePersonalMessage(forwardedMsg ForwardedMessage) error {
	// 不再存储个人消息到wx_personal_messages表，相关代码已注释
	/*
		// 构建个人消息数据库记录
		wxPersonalMsg := WxPersonalMessage{
			OwnerID:      forwardedMsg.OwnerID,
			SourceToken:  forwardedMsg.SourceToken,
			MsgType:      forwardedMsg.MsgType,
			MsgSource:    forwardedMsg.MsgSource,
			Content:      forwardedMsg.Content,
			MsgID:        forwardedMsg.MsgID,
			NewMsgID:     forwardedMsg.NewMsgID,
			FromUserName: forwardedMsg.FromUserName,
			ToUserName:   forwardedMsg.ToUserName,
			MsgTime:      forwardedMsg.MsgTime,
		}

		// 将msg_id转换为int64用于wx_global_id保存
		msgIDInt, err := strconv.ParseInt(forwardedMsg.MsgID, 10, 64)
		if err != nil {
			return fmt.Errorf("无法将msg_id转换为int64: %w", err)
		}

		// 使用事务保存到数据库
		var isNewRecord bool
		err = m.db.Transaction(func(tx *gorm.DB) error {
			// 使用原生SQL插入
			insertSQL := `INSERT INTO wx_personal_messages (
				owner_id, source_token, msg_type, msg_source, content,
				msg_id, new_msg_id, from_user_name, to_user_name, img_url, msg_time
			) VALUES (
				?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
			)`

			result := tx.Exec(insertSQL,
				forwardedMsg.OwnerID,
				wxPersonalMsg.SourceToken,
				wxPersonalMsg.MsgType,
				wxPersonalMsg.MsgSource,
				wxPersonalMsg.Content,
				wxPersonalMsg.MsgID,
				wxPersonalMsg.NewMsgID,
				wxPersonalMsg.FromUserName,
				wxPersonalMsg.ToUserName,
				wxPersonalMsg.ImgURL,
				wxPersonalMsg.MsgTime,
			)

			if result.Error != nil {
				// 检查是否为唯一键冲突错误
				if strings.Contains(result.Error.Error(), "Duplicate entry") || strings.Contains(result.Error.Error(), "UNIQUE constraint") {
					m.logger.Debug("个人消息已存在，跳过保存", zap.String("msg_id", wxPersonalMsg.MsgID))
					isNewRecord = false
					return nil // 重复消息，正常返回，不保存wx_global_id
				}
				return fmt.Errorf("插入个人消息数据库失败: %w", result.Error)
			}

			// 消息表保存成功，必须保存到wx_global_id表，否则事务回滚
			wxGlobalID := WxGlobalID{
				OwnerID: forwardedMsg.OwnerID,
				MsgID:   msgIDInt,
				MsgTime: forwardedMsg.MsgTime,
			}
			if err := tx.Create(&wxGlobalID).Error; err != nil {
				// wx_global_id保存失败，整个事务回滚
				return fmt.Errorf("插入wx_global_id数据库失败: %w", err)
			}

			isNewRecord = true
			return nil
		})

		if err != nil {
			return err
		}

		// 事务成功后，如果是新记录，同步更新Redis中的last_msg_time
		if isNewRecord && m.redisUtil != nil {
			ctx := context.Background()
			if err := m.redisUtil.SetLastMsgTime(ctx, m.config.App.OwnerID, forwardedMsg.MsgTime); err != nil {
				m.logger.Warn("更新last_msg_time到Redis失败",
					zap.Int64("msg_time", forwardedMsg.MsgTime),
					zap.Error(err))
			} else {
				m.logger.Debug("更新last_msg_time到Redis成功",
					zap.Int64("msg_time", forwardedMsg.MsgTime))
			}
		}

		if err != nil {
			return err
		}

		m.logger.Info("个人消息已保存到数据库",
			zap.Int64("id", wxPersonalMsg.ID),
			zap.String("msg_id", wxPersonalMsg.MsgID),
			zap.Int("msg_type", wxPersonalMsg.MsgType),
			zap.String("from_user", wxPersonalMsg.FromUserName),
			zap.String("to_user", wxPersonalMsg.ToUserName))
	*/

	m.logger.Info("个人消息处理跳过（不存储到数据库）",
		zap.String("msg_id", forwardedMsg.MsgID),
		zap.Int("msg_type", forwardedMsg.MsgType),
		zap.String("from_user", forwardedMsg.FromUserName),
		zap.String("to_user", forwardedMsg.ToUserName))

	return nil
}

// getNickNameFromGroupInfo 从群信息中获取用户昵称
func (m *messageForwarderImpl) getNickNameFromGroupInfo(groupID, wxID string) string {
	// 如果wxID为空，直接返回空字符串
	if wxID == "" {
		return ""
	}

	// 调用GetChatRoomInfo获取群信息
	req := &GetChatRoomInfoRequest{
		ChatRoomWxIdList: []string{groupID},
	}

	response, err := m.wechatBot.GetChatRoomInfo(req)
	if err != nil {
		m.logger.Debug("获取群信息失败，使用wx_id作为昵称",
			zap.String("group_id", groupID),
			zap.String("wx_id", wxID),
			zap.Error(err))
		return wxID
	}

	// 如果响应为空或没有成员信息，返回wx_id
	if response == nil || response.ChatRoomMembers == nil || len(response.ChatRoomMembers) == 0 {
		m.logger.Debug("群信息为空，使用wx_id作为昵称",
			zap.String("group_id", groupID),
			zap.String("wx_id", wxID))
		return wxID
	}

	// 通过wx_id（key）查找对应的昵称（value）
	if nickName, exists := response.ChatRoomMembers[wxID]; exists {
		return nickName
	}

	// 如果在结果中找不到对应的昵称，返回wx_id
	m.logger.Debug("在群成员中未找到对应昵称，使用wx_id作为昵称",
		zap.String("group_id", groupID),
		zap.String("wx_id", wxID))
	return wxID
}

// parseWebSocketConfig 解析WebSocket配置获取Token
func (m *messageForwarderImpl) getCurrentKey() (string, error) {
	// 直接从wechat_bot获取配置信息，因为MessageForwarder依赖于wechat_bot
	if m.wechatBot == nil {
		return "", fmt.Errorf("wechat_bot未初始化")
	}

	// 从wechat_bot获取key
	wechatBotImpl, ok := m.wechatBot.(*wechatBotImpl)
	if !ok {
		return "", fmt.Errorf("无法获取wechat_bot实现")
	}

	// 使用读锁获取配置
	wechatBotImpl.configMu.RLock()
	key := wechatBotImpl.key
	wechatBotImpl.configMu.RUnlock()

	if key == "" {
		return "", fmt.Errorf("wechat_bot的key配置为空")
	}

	return key, nil
}

// Stop 停止消息转发器（线程安全）
func (m *messageForwarderImpl) Stop() error {
	// 使用sync.Once确保只执行一次
	m.stopOnce.Do(func() {
		// 原子设置停止标志
		atomic.StoreInt32(&m.stopped, 1)

		// 安全关闭stopChannel
		select {
		case <-m.stopChannel:
			// channel已经关闭
		default:
			close(m.stopChannel)
		}

		// 停止OCR处理器
		if m.ocrProcessor != nil {
			if err := m.ocrProcessor.Stop(); err != nil {
				m.logger.Error("停止OCR处理器失败", zap.Error(err))
			}
		}

		// 停止Webhook队列处理器
		if m.webhookQueueProcessor != nil {
			if err := m.webhookQueueProcessor.Stop(); err != nil {
				m.logger.Error("停止Webhook队列处理器失败", zap.Error(err))
			}
		}

		// 关闭MQ管理器
		if m.mqManager != nil {
			if err := m.mqManager.Close(); err != nil {
				m.logger.Error("关闭MQ管理器失败", zap.Error(err))
			}
		}

		m.logger.Info("消息转发器已停止")
	})

	return nil
}

// cleanControlChars 清理字符串中的控制字符（\n、\r、\t等）
func (m *messageForwarderImpl) cleanControlChars(content string) string {
	// 替换常见的控制字符
	content = strings.ReplaceAll(content, "\n", " ") // 换行符替换为空格
	content = strings.ReplaceAll(content, "\r", " ") // 回车符替换为空格
	content = strings.ReplaceAll(content, "\t", " ") // 制表符替换为空格
	content = strings.ReplaceAll(content, "\f", " ") // 换页符替换为空格
	content = strings.ReplaceAll(content, "\v", " ") // 垂直制表符替换为空格

	// 替换多个连续空格为单个空格
	for strings.Contains(content, "  ") {
		content = strings.ReplaceAll(content, "  ", " ")
	}

	// 去除前后空格
	return strings.TrimSpace(content)
}

// addOCRTask 添加OCR任务到队列
func (m *messageForwarderImpl) addOCRTask(task *OCRTask) error {
	if m.ocrProcessor == nil {
		return fmt.Errorf("OCR处理器未初始化")
	}

	ctx := context.Background()
	return m.ocrProcessor.AddTask(ctx, task)
}

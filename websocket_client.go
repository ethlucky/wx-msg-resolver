package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MessageType 消息类型定义
type MessageType int

// 消息类型枚举
const (
	MsgTypeText               MessageType = 1     // 文本消息
	MsgTypeImage              MessageType = 3     // 图片消息
	MsgTypeVoice              MessageType = 34    // 语音消息
	MsgTypeContact            MessageType = 42    // 名片消息
	MsgTypeVideo              MessageType = 43    // 视频消息
	MsgTypeAnimatedEmoji      MessageType = 47    // 动画表情
	MsgTypeLocation           MessageType = 48    // 位置消息
	MsgTypeShare              MessageType = 49    // 分享链接
	MsgTypeVOIP               MessageType = 50    // VOIP消息
	MsgTypeOperation          MessageType = 51    // 操作消息
	MsgTypeShortVideo         MessageType = 62    // 小视频
	MsgTypeSystem             MessageType = 9999  // 系统消息
	MsgTypeSystemNotification MessageType = 10000 // 系统通知
	MsgTypeSystemOperation    MessageType = 10002 // 系统操作
)

// String 返回消息类型的字符串表示
func (mt MessageType) String() string {
	switch mt {
	case MsgTypeText:
		return "文本消息"
	case MsgTypeImage:
		return "图片消息"
	case MsgTypeVoice:
		return "语音消息"
	case MsgTypeContact:
		return "名片消息"
	case MsgTypeVideo:
		return "视频消息"
	case MsgTypeAnimatedEmoji:
		return "动画表情"
	case MsgTypeLocation:
		return "位置消息"
	case MsgTypeShare:
		return "分享链接"
	case MsgTypeVOIP:
		return "VOIP消息"
	case MsgTypeOperation:
		return "操作消息"
	case MsgTypeShortVideo:
		return "小视频"
	case MsgTypeSystem:
		return "系统消息"
	case MsgTypeSystemNotification:
		return "系统通知"
	case MsgTypeSystemOperation:
		return "系统操作"
	default:
		return fmt.Sprintf("未知类型(%d)", int(mt))
	}
}

// GetDetailedDescription 获取详细的消息类型描述
func (mt MessageType) GetDetailedDescription(content string) string {
	baseType := mt.String()

	switch mt {
	case MsgTypeText:
		return baseType + " - 普通文本消息或表情符号"
	case MsgTypeImage:
		return baseType + " - 图片文件消息"
	case MsgTypeVoice:
		return baseType + " - 语音文件消息"
	case MsgTypeContact:
		return baseType + " - 联系人名片消息"
	case MsgTypeVideo:
		return baseType + " - 视频文件消息"
	case MsgTypeAnimatedEmoji:
		return baseType + " - 自定义表情包消息"
	case MsgTypeLocation:
		return baseType + " - 地理位置消息"
	case MsgTypeShare:
		if strings.Contains(content, "appmsg") {
			return baseType + " - 公众号文章消息"
		}
		return baseType + " - 富媒体消息(小程序、文件等)"
	case MsgTypeVOIP:
		return baseType + " - 音视频通话消息"
	case MsgTypeOperation:
		return baseType + " - 微信内部操作消息"
	case MsgTypeShortVideo:
		return baseType + " - 短视频消息"
	case MsgTypeSystem:
		return baseType + " - 系统通知消息"
	case MsgTypeSystemNotification:
		return baseType + " - 系统通知消息"
	case MsgTypeSystemOperation:
		if strings.Contains(content, "revokemsg") {
			return baseType + " - 撤回消息"
		} else if strings.Contains(content, "bizlivenotify") {
			return baseType + " - 直播通知消息"
		} else if strings.Contains(content, "BrandServiceRecommendCard") {
			return baseType + " - 品牌服务推荐消息"
		}
		return baseType + " - 系统操作消息"
	default:
		return fmt.Sprintf("未知类型(%d)", int(mt))
	}
}

// WebSocketClient WebSocket客户端接口
type WebSocketClient interface {
	// Start 启动WebSocket客户端
	Start(ctx context.Context) error
	// Stop 停止WebSocket客户端
	Stop() error
	// IsConnected 检查连接状态
	IsConnected() bool
	// SendMessage 发送消息
	SendMessage(message string) error
	// UpdateWebSocketURL 更新WebSocket URL
	UpdateWebSocketURL(newURL string) error
}

// webSocketClientImpl WebSocket客户端实现
type webSocketClientImpl struct {
	config           WebSocketConfig
	appConfig        *AppConfig // 添加完整的应用配置引用
	logger           *zap.Logger
	conn             *websocket.Conn
	connected        bool
	mu               sync.RWMutex
	pingTicker       *time.Ticker
	reconnectTimer   *time.Timer
	stopChan         chan struct{}
	ctx              context.Context
	cancel           context.CancelFunc
	messageForwarder MessageForwarder
	xmlParser        *XMLParser
	redisUtil        RedisUtil    // Redis工具
	db               *gorm.DB     // 数据库连接
	lastMsgTime      int64        // 本地缓存的最后处理的消息时间戳
	lastMsgTimeMutex sync.RWMutex // 保护lastMsgTime的读写锁
}

// NewWebSocketClient 创建新的WebSocket客户端
func NewWebSocketClient(config WebSocketConfig, appConfig *AppConfig, logger *zap.Logger, messageForwarder MessageForwarder, redisUtil RedisUtil, db *gorm.DB) WebSocketClient {
	return &webSocketClientImpl{
		config:           config,
		appConfig:        appConfig,
		logger:           logger,
		stopChan:         make(chan struct{}),
		messageForwarder: messageForwarder,
		xmlParser:        NewXMLParser(logger),
		redisUtil:        redisUtil,
		db:               db,
	}
}

// Start 启动WebSocket客户端
func (w *webSocketClientImpl) Start(ctx context.Context) error {
	if !w.config.Enabled {
		w.logger.Info("WebSocket客户端已禁用")
		return nil
	}

	w.ctx, w.cancel = context.WithCancel(ctx)

	// 初始化last_msg_time
	if err := w.initLastMsgTime(ctx); err != nil {
		w.logger.Warn("初始化last_msg_time失败", zap.Error(err))
	}

	w.logger.Info("启动WebSocket客户端",
		zap.String("url", w.config.URL),
		zap.Duration("ping_interval", w.config.PingInterval),
		zap.Int64("last_msg_time", w.getLastMsgTime()))

	// 启动连接和重连逻辑
	go w.connectionLoop()

	return nil
}

// Stop 停止WebSocket客户端
func (w *webSocketClientImpl) Stop() error {
	w.logger.Info("正在停止WebSocket客户端")

	if w.cancel != nil {
		w.cancel()
	}

	close(w.stopChan)

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.pingTicker != nil {
		w.pingTicker.Stop()
	}

	if w.reconnectTimer != nil {
		w.reconnectTimer.Stop()
	}

	if w.conn != nil {
		w.conn.Close()
		w.connected = false
	}

	w.logger.Info("WebSocket客户端已停止")
	return nil
}

// IsConnected 检查连接状态
func (w *webSocketClientImpl) IsConnected() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.connected
}

// SendMessage 发送消息
func (w *webSocketClientImpl) SendMessage(message string) error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if !w.connected || w.conn == nil {
		return fmt.Errorf("WebSocket连接未建立")
	}

	return w.conn.WriteMessage(websocket.TextMessage, []byte(message))
}

// connectionLoop 连接循环，处理连接和重连
func (w *webSocketClientImpl) connectionLoop() {
	attempts := 0

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Info("收到停止信号，退出连接循环")
			return
		case <-w.stopChan:
			w.logger.Info("收到停止信号，退出连接循环")
			return
		default:
			if err := w.connect(); err != nil {
				attempts++
				w.logger.Error("WebSocket连接失败",
					zap.Error(err),
					zap.Int("attempt", attempts),
					zap.Int("max_attempts", w.config.MaxReconnectAttempts))

				// 每10次重连尝试失败后，尝试从数据库重新加载配置
				if attempts%10 == 0 {
					w.logger.Info("尝试从数据库重新加载WebSocket配置", zap.Int("attempts", attempts))
					if reloadErr := w.reloadWebSocketURLFromDB(); reloadErr != nil {
						w.logger.Error("重新加载WebSocket配置失败", zap.Error(reloadErr))
					} else {
						w.logger.Info("WebSocket配置重新加载成功，将使用新配置进行连接")
					}
				}

				if w.config.MaxReconnectAttempts > 0 && attempts >= w.config.MaxReconnectAttempts {
					w.logger.Error("达到最大重连次数，停止重连")
					return
				}

				// 等待重连间隔
				select {
				case <-w.ctx.Done():
					return
				case <-time.After(w.config.ReconnectInterval):
					continue
				}
			} else {
				// 连接成功，重置重连次数
				attempts = 0
				w.logger.Info("WebSocket连接成功")

				// 启动消息处理
				w.handleConnection()
			}
		}
	}
}

// connect 建立WebSocket连接
func (w *webSocketClientImpl) connect() error {
	// 解析URL
	u, err := url.Parse(w.config.URL)
	if err != nil {
		return fmt.Errorf("解析WebSocket URL失败: %w", err)
	}

	w.logger.Info("正在连接WebSocket服务器", zap.String("url", u.String()))

	// 设置请求头
	headers := make(map[string][]string)
	headers["User-Agent"] = []string{"wx-msg-chat/1.0.0"}
	headers["Origin"] = []string{fmt.Sprintf("http://%s", u.Host)}
	headers["Sec-WebSocket-Protocol"] = []string{"chat"}

	// 配置 Dialer 以获得更好的连接控制
	dialer := &websocket.Dialer{
		HandshakeTimeout: 45 * time.Second,
		ReadBufferSize:   4096,
		WriteBufferSize:  4096,
	}

	// 建立连接
	conn, resp, err := dialer.Dial(u.String(), headers)
	if err != nil {
		if resp != nil {
			w.logger.Error("WebSocket握手失败",
				zap.String("status", resp.Status),
				zap.Int("status_code", resp.StatusCode))
		}
		return fmt.Errorf("WebSocket连接失败: %w", err)
	}

	w.logger.Info("WebSocket连接建立成功",
		zap.String("url", u.String()),
		zap.String("status", resp.Status))

	w.mu.Lock()
	w.conn = conn
	w.connected = true
	w.mu.Unlock()

	return nil
}

// handleConnection 处理WebSocket连接
func (w *webSocketClientImpl) handleConnection() {
	defer func() {
		w.mu.Lock()
		if w.conn != nil {
			w.conn.Close()
			w.conn = nil
		}
		w.connected = false
		w.mu.Unlock()

		if w.pingTicker != nil {
			w.pingTicker.Stop()
			w.pingTicker = nil
		}
	}()

	// 启动ping定时器
	w.startPingTicker()

	// 处理消息
	for {
		select {
		case <-w.ctx.Done():
			w.logger.Info("收到停止信号，退出消息处理")
			return
		case <-w.stopChan:
			w.logger.Info("收到停止信号，退出消息处理")
			return
		default:
			_, message, err := w.conn.ReadMessage()
			if err != nil {
				w.logger.Error("读取WebSocket消息失败", zap.Error(err))
				return
			}

			// if messageType == websocket.TextMessage {
			w.handleMessage(string(message))
			// }
		}
	}
}

// startPingTicker 启动ping定时器
func (w *webSocketClientImpl) startPingTicker() {
	w.pingTicker = time.NewTicker(w.config.PingInterval)

	go func() {
		defer w.pingTicker.Stop()

		for {
			select {
			case <-w.ctx.Done():
				return
			case <-w.stopChan:
				return
			case <-w.pingTicker.C:
				if err := w.SendMessage("ping"); err != nil {
					w.logger.Error("发送ping消息失败", zap.Error(err))
					return
				}
				w.logger.Debug("发送ping消息")
			}
		}
	}()
}

// handleMessage 处理接收到的消息
func (w *webSocketClientImpl) handleMessage(message string) {
	// 截断过长的消息内容以避免日志文件过大
	logMessage := message
	if len(message) > 1000 {
		logMessage = message[:1000] + "...(truncated)"
	}
	w.logger.Debug("收到WebSocket原始消息", zap.String("raw_message", logMessage))

	// 如果是pong消息，直接返回
	if message == "pong" {
		w.logger.Debug("收到pong消息")
		return
	}

	// 解析JSON消息
	var msgData map[string]interface{}
	if err := json.Unmarshal([]byte(message), &msgData); err != nil {
		// 截断过长的非JSON消息以避免日志文件过大
		logMessage := message
		if len(message) > 500 {
			logMessage = message[:500] + "...(truncated)"
		}
		w.logger.Info("收到非JSON消息", zap.String("message", logMessage))
		return
	}

	// 验证消息格式是否符合标准格式
	if !w.isStandardMessageFormat(msgData) {
		w.logger.Debug("消息格式不符合标准格式，跳过处理", zap.Any("msg_data", msgData))
		return
	}

	// 检查消息类型
	msgTypeVal, ok := msgData["msg_type"].(float64)
	if !ok {
		w.logger.Debug("消息类型字段不存在或格式错误", zap.Any("msg_data", msgData))
		return
	}

	msgTypeInt := MessageType(msgTypeVal)

	// 根据配置决定是否过滤消息类型
	if w.config.FilterMessageTypes {
		if !w.isMessageTypeSupported(int(msgTypeInt)) {
			w.logger.Debug("跳过不支持的消息类型",
				zap.String("msg_type", msgTypeInt.String()),
				zap.Int("msg_type_code", int(msgTypeInt)))
			return
		}
	}

	// 检查消息是否需要处理（基于msg_id过滤）
	if !w.shouldProcessMessage(msgData) {
		w.logger.Debug("消息已处理过，跳过",
			zap.Any("msg_id", msgData["msg_id"]))
		return
	}

	// 解码和美化消息内容
	decodedMsg := w.decodeMessage(msgData)

	// 安全地记录消息摘要，避免记录过大的内容
	logFields := w.createSafeLogFields(decodedMsg)
	w.logger.Info("收到消息", logFields...)

	// 转发解码后的消息到第三方服务器
	if err := w.forwardMessage(decodedMsg); err != nil {
		w.logger.Error("转发消息失败", zap.Error(err),
			zap.String("msg_id", w.getStringFromData(decodedMsg, "msg_id")),
			zap.String("msg_type", w.getStringFromData(decodedMsg, "msg_type")))
	} else {
		// 消息处理成功后更新 last_msg_time
		if createTime, ok := msgData["create_time"].(float64); ok {
			msgTime := int64(createTime)
			if msgTime > w.getLastMsgTime() {
				w.setLastMsgTime(msgTime)
				w.logger.Debug("更新last_msg_time", zap.Int64("new_last_msg_time", msgTime))
			}
		}
	}
}

// forwardMessage 转发消息到第三方服务器
func (w *webSocketClientImpl) forwardMessage(msgData map[string]interface{}) error {
	// 检查是否为个人消息，如果是则直接跳过处理
	if w.isPersonalMessage(msgData) {
		w.logger.Debug("跳过个人消息处理",
			zap.String("from_user", w.extractNestedString(msgData, "from_user_name")),
			zap.String("to_user", w.extractNestedString(msgData, "to_user_name")),
			zap.String("msg_id", w.getStringFromData(msgData, "msg_id")))
		return nil
	}

	// 最后一层检查：根据配置决定是否过滤消息类型
	if w.config.FilterMessageTypes {
		if msgTypeVal, ok := msgData["msg_type"].(float64); ok {
			msgTypeInt := MessageType(msgTypeVal)
			if !w.isMessageTypeSupported(int(msgTypeInt)) {
				w.logger.Debug("跳过转发不支持的消息类型",
					zap.String("msg_type", msgTypeInt.String()),
					zap.Int("msg_type_code", int(msgTypeInt)))
				return nil
			}
		} else {
			w.logger.Debug("消息类型字段不存在，跳过转发", zap.Any("data", msgData))
			return nil
		}
	}

	// 处理消息内容，特别是图片消息的XML解析
	processedMsgData := w.processMessageContent(msgData)

	// 如果processedMsgData为nil，表示消息应该被跳过
	if processedMsgData == nil {
		w.logger.Debug("消息被跳过，不进行转发")
		return nil
	}

	// 使用新的MessageForwarder转发消息
	if w.messageForwarder != nil {
		if err := w.messageForwarder.ForwardMessage(processedMsgData); err != nil {
			w.logger.Error("使用MessageForwarder转发消息失败", zap.Error(err))
			return fmt.Errorf("MessageForwarder转发失败: %w", err)
		}
		w.logger.Info("消息已通过MessageForwarder转发")
		return nil
	}

	// 如果没有MessageForwarder，记录错误
	w.logger.Error("MessageForwarder未配置，无法转发消息", zap.Any("data", msgData))
	return fmt.Errorf("MessageForwarder未配置，无法转发消息")
}

// processMessageContent 处理消息内容，特别是图片消息的XML解析
func (w *webSocketClientImpl) processMessageContent(msgData map[string]interface{}) map[string]interface{} {
	// 创建消息副本，避免修改原始数据
	processedData := make(map[string]interface{})
	for k, v := range msgData {
		processedData[k] = v
	}

	// 获取消息类型和内容
	msgType := w.getMsgType(msgData)
	contentStr := w.getContentString(msgData)

	// 处理消息内容
	contentJSON := w.processContent(msgType, contentStr, msgData)

	// 如果contentJSON为空，表示消息应该被跳过
	if contentJSON == "" {
		w.logger.Debug("消息处理被跳过")
		return nil // 返回nil表示跳过该消息
	}

	// 添加处理后的content字段
	processedData["content_json"] = contentJSON

	return processedData
}

// getMsgType 获取消息类型
func (w *webSocketClientImpl) getMsgType(msgData map[string]interface{}) MessageType {
	if msgTypeVal, ok := msgData["msg_type"].(float64); ok {
		return MessageType(msgTypeVal)
	}
	return MessageType(0)
}

// getContentString 获取消息内容字符串
func (w *webSocketClientImpl) getContentString(msgData map[string]interface{}) string {
	contentMap, exists := msgData["content"].(map[string]interface{})
	if !exists {
		return ""
	}

	contentStr, ok := contentMap["str"].(string)
	if !ok {
		return ""
	}

	return w.decodeString(contentStr)
}

// processContent 处理具体的消息内容，统一处理wx_id提取和内容解析
func (w *webSocketClientImpl) processContent(msgType MessageType, contentStr string, msgData map[string]interface{}) string {
	// 如果内容为空，返回空字符串
	if contentStr == "" {
		return `{"wx_id": "", "text": "", "aeskey": "", "cdnurl": ""}`
	}

	var wxID, text, aeskey, cdnurl string

	// 通过':'分割内容，提取wx_id
	if strings.Contains(contentStr, ":") {
		parts := strings.SplitN(contentStr, ":", 2)
		if len(parts) == 2 {
			wxID = strings.TrimSpace(parts[0])
			text = strings.TrimSpace(parts[1])
		} else {
			text = contentStr
		}
	} else {
		text = contentStr
	}

	// 如果是图片消息，解析XML内容
	if msgType == MsgTypeImage {
		if w.xmlParser.IsImageMessage(text) {
			var err error
			var cdnType string
			aeskey, cdnurl, cdnType, err = w.xmlParser.ExtractAesKeyAndCdnDetails(text)
			if err != nil {
				w.logger.Warn("解析图片消息XML内容失败，跳过该消息",
					zap.Error(err),
					zap.String("xml_content", text))
				return "" // 返回空字符串，表示跳过该消息
			} else {
				w.logger.Info("成功解析图片消息XML内容",
					zap.String("aeskey", aeskey),
					zap.String("cdnurl", cdnurl),
					zap.String("cdn_type", cdnType))
			}
		}
		// 图片消息text设置为空，使用aeskey和cdnurl
		text = ""
	}
	// 其他消息类型（包括文本消息）保持text内容不变

	// 返回结构化的JSON数据
	return fmt.Sprintf(`{"wx_id": "%s", "text": "%s", "aeskey": "%s", "cdnurl": "%s"}`,
		w.escapeJSON(wxID), w.escapeJSON(text), w.escapeJSON(aeskey), w.escapeJSON(cdnurl))
}

// cleanXMLContent 清理XML内容，移除可能影响解析的字符
func (w *webSocketClientImpl) cleanXMLContent(content string) string {
	// 移除前后空白字符
	cleaned := strings.TrimSpace(content)

	// 检查是否有用户ID前缀（如 "wxid_xxx:"），如果有则移除
	if strings.Contains(cleaned, "<?xml") {
		// 找到XML声明的位置
		xmlStart := strings.Index(cleaned, "<?xml")
		if xmlStart > 0 {
			// 只保留XML部分
			cleaned = cleaned[xmlStart:]
		}
	}

	// 如果没有XML声明但有<msg>标签，也尝试提取
	if !strings.Contains(cleaned, "<?xml") && strings.Contains(cleaned, "<msg>") {
		msgStart := strings.Index(cleaned, "<msg>")
		if msgStart > 0 {
			cleaned = cleaned[msgStart:]
		}
	}

	// 移除所有换行符、回车符和制表符，保持XML在一行
	cleaned = strings.ReplaceAll(cleaned, "\n", "")
	cleaned = strings.ReplaceAll(cleaned, "\r", "")
	cleaned = strings.ReplaceAll(cleaned, "\t", "")

	// 再次移除前后空白字符
	cleaned = strings.TrimSpace(cleaned)

	return cleaned
}

// getStringFromData 从map中安全获取字符串值
func (w *webSocketClientImpl) getStringFromData(data map[string]interface{}, key string) string {
	if val, exists := data[key]; exists {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// isPersonalMessage 判断是否为个人消息
func (w *webSocketClientImpl) isPersonalMessage(msgData map[string]interface{}) bool {
	fromUserName := w.extractNestedString(msgData, "from_user_name")
	toUserName := w.extractNestedString(msgData, "to_user_name")

	// 群消息：from_user_name 或 to_user_name 包含 @chatroom
	// 个人消息：from_user_name 和 to_user_name 都不包含 @chatroom
	if strings.Contains(fromUserName, "@chatroom") || strings.Contains(toUserName, "@chatroom") {
		return false // 群消息
	}
	return true // 个人消息
}

// extractNestedString 提取嵌套结构中的字符串值，支持直接字符串和 {"str": "value"} 格式
func (w *webSocketClientImpl) extractNestedString(data map[string]interface{}, key string) string {
	if val, exists := data[key]; exists {
		// 尝试直接转换为字符串
		if str, ok := val.(string); ok {
			return str
		}
		// 尝试处理嵌套结构 {"str": "value"}
		if nested, ok := val.(map[string]interface{}); ok {
			if strVal, exists := nested["str"]; exists {
				if str, ok := strVal.(string); ok {
					return str
				}
			}
		}
	}
	return ""
}

// createSafeLogFields 创建安全的日志字段，避免记录过大的内容（如base64图片）
func (w *webSocketClientImpl) createSafeLogFields(data map[string]interface{}) []zap.Field {
	fields := []zap.Field{}

	// 记录基本信息字段
	safeFields := []string{"msg_id", "msg_type", "from_user_name", "to_user_name", "create_time", "group_id", "wx_id"}

	for _, field := range safeFields {
		if val, exists := data[field]; exists {
			switch v := val.(type) {
			case string:
				fields = append(fields, zap.String(field, v))
			case float64:
				fields = append(fields, zap.Float64(field, v))
			case int64:
				fields = append(fields, zap.Int64(field, v))
			default:
				fields = append(fields, zap.Any(field, v))
			}
		}
	}

	// 对于content字段，进行长度检查
	if contentVal, exists := data["content"]; exists {
		if contentStr, ok := contentVal.(string); ok {
			if len(contentStr) > 1000 {
				fields = append(fields, zap.String("content", contentStr[:1000]+"...(truncated)"))
			} else {
				fields = append(fields, zap.String("content", contentStr))
			}
		}
	}

	return fields
}

// escapeJSON 转义JSON字符串
func (w *webSocketClientImpl) escapeJSON(s string) string {
	// 简单的JSON转义
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// decodeMessage 解码和美化消息内容
func (w *webSocketClientImpl) decodeMessage(msgData map[string]interface{}) map[string]interface{} {
	decoded := make(map[string]interface{})

	// 复制基本字段
	for key, value := range msgData {
		decoded[key] = value
	}

	// 处理特殊字段的解码
	if contentMap, ok := msgData["content"].(map[string]interface{}); ok {
		if contentStr, exists := contentMap["str"].(string); exists {
			decodedContent := w.decodeString(contentStr)
			decoded["content_decoded"] = decodedContent

			// 提取实际的消息内容（去掉用户ID前缀）
			actualContent := w.extractActualContent(decodedContent)
			decoded["actual_content"] = actualContent
		}
	}

	// 处理from_user_name
	if fromMap, ok := msgData["from_user_name"].(map[string]interface{}); ok {
		if fromStr, exists := fromMap["str"].(string); exists {
			decoded["from_user_decoded"] = w.decodeString(fromStr)
		}
	}

	// 处理to_user_name
	if toMap, ok := msgData["to_user_name"].(map[string]interface{}); ok {
		if toStr, exists := toMap["str"].(string); exists {
			decoded["to_user_decoded"] = w.decodeString(toStr)
		}
	}

	// 处理msg_source（XML内容）
	if msgSource, ok := msgData["msg_source"].(string); ok {
		decodedSource := w.decodeString(msgSource)
		decoded["msg_source_decoded"] = decodedSource
	}

	// 添加时间戳转换
	if createTime, ok := msgData["create_time"].(float64); ok {
		t := time.Unix(int64(createTime), 0)
		decoded["create_time_formatted"] = t.Format("2006-01-02 15:04:05")
	}

	return decoded
}

// decodeString 解码字符串中的转义字符
func (w *webSocketClientImpl) decodeString(s string) string {
	// 第一步：尝试JSON解码
	jsonStr := fmt.Sprintf(`"%s"`, s)
	var unescaped string
	if err := json.Unmarshal([]byte(jsonStr), &unescaped); err != nil {
		// JSON解析失败，手动处理转义字符
		unescaped = s
	}

	// 第二步：手动处理剩余的转义字符
	unescaped = strings.ReplaceAll(unescaped, "\\n", "\n")
	unescaped = strings.ReplaceAll(unescaped, "\\t", "\t")
	unescaped = strings.ReplaceAll(unescaped, "\\r", "\r")
	unescaped = strings.ReplaceAll(unescaped, "\\\"", "\"")
	unescaped = strings.ReplaceAll(unescaped, "\\/", "/")
	unescaped = strings.ReplaceAll(unescaped, "\\\\", "\\")

	// 第三步：处理HTML实体编码
	unescaped = html.UnescapeString(unescaped)

	// 第四步：处理Unicode转义序列（如 \u003c）
	unescaped = w.decodeUnicodeEscapes(unescaped)

	return unescaped
}

// decodeUnicodeEscapes 解码Unicode转义序列
func (w *webSocketClientImpl) decodeUnicodeEscapes(s string) string {
	// 查找所有 \uXXXX 模式
	result := s
	for {
		start := strings.Index(result, "\\u")
		if start == -1 {
			break
		}

		if start+6 <= len(result) {
			// 提取十六进制部分
			hexStr := result[start+2 : start+6]

			// 尝试解析为十六进制数
			var codePoint int
			if n, err := fmt.Sscanf(hexStr, "%x", &codePoint); err == nil && n == 1 {
				// 转换为对应的字符
				char := string(rune(codePoint))
				// 替换原始的转义序列
				result = result[:start] + char + result[start+6:]
			} else {
				// 如果解析失败，跳过这个序列
				result = result[:start] + result[start+1:]
			}
		} else {
			// 如果长度不够，直接跳过
			break
		}
	}
	return result
}

// extractActualContent 提取实际的消息内容（去掉用户ID前缀）
func (w *webSocketClientImpl) extractActualContent(content string) string {
	// 查找换行符，通常用户ID在第一行，实际内容在后面
	if strings.Contains(content, "\n") {
		parts := strings.SplitN(content, "\n", 2)
		if len(parts) > 1 {
			// 返回第二部分（实际内容）
			return strings.TrimSpace(parts[1])
		}
	}
	return content
}

// isStandardMessageFormat 检查消息是否符合标准格式
func (w *webSocketClientImpl) isStandardMessageFormat(msgData map[string]interface{}) bool {
	// 检查关键字段是否存在，这些字段的存在表明这是标准格式的消息
	requiredFields := []string{"msg_id", "from_user_name", "to_user_name", "msg_type", "content"}

	for _, field := range requiredFields {
		if _, exists := msgData[field]; !exists {
			return false
		}
	}

	// 检查 from_user_name 是否为对象且包含 str 字段
	if fromUserName, ok := msgData["from_user_name"].(map[string]interface{}); ok {
		if _, exists := fromUserName["str"]; !exists {
			return false
		}
	} else {
		return false
	}

	// 检查 to_user_name 是否为对象且包含 str 字段
	if toUserName, ok := msgData["to_user_name"].(map[string]interface{}); ok {
		if _, exists := toUserName["str"]; !exists {
			return false
		}
	} else {
		return false
	}

	// 检查 content 是否为对象且包含 str 字段
	if content, ok := msgData["content"].(map[string]interface{}); ok {
		if _, exists := content["str"]; !exists {
			return false
		}
	} else {
		return false
	}

	return true
}

// isMessageTypeSupported 检查消息类型是否在支持列表中
func (w *webSocketClientImpl) isMessageTypeSupported(msgType int) bool {
	// 如果没有配置支持的消息类型列表，默认支持文本消息和图片消息
	if len(w.config.SupportedMessageTypes) == 0 {
		return msgType == int(MsgTypeText) || msgType == int(MsgTypeImage)
	}

	// 检查消息类型是否在配置的支持列表中
	for _, supportedType := range w.config.SupportedMessageTypes {
		if supportedType == msgType {
			return true
		}
	}
	return false
}

// initLastMsgTime 初始化last_msg_time
func (w *webSocketClientImpl) initLastMsgTime(ctx context.Context) error {
	if w.redisUtil == nil {
		w.logger.Debug("RedisUtil未初始化，跳过last_msg_time初始化")
		return nil
	}

	lastMsgTime, err := w.redisUtil.GetLastMsgTimeWithFallback(ctx, w.db, w.appConfig.OwnerID)
	if err != nil {
		return fmt.Errorf("获取last_msg_time失败: %w", err)
	}

	w.setLastMsgTime(lastMsgTime)
	w.logger.Info("初始化last_msg_time完成", zap.Int64("last_msg_time", lastMsgTime))
	return nil
}

// shouldProcessMessage 检查消息是否应该被处理
func (w *webSocketClientImpl) shouldProcessMessage(msgData map[string]interface{}) bool {
	// 检查消息ID是否存在，防止处理重复消息
	msgID, msgIDExists := msgData["msg_id"]
	if !msgIDExists || msgID == nil {
		w.logger.Debug("消息缺少msg_id，允许处理")
		return true
	}

	// 获取消息时间戳 - 使用create_time字段
	var msgTime int64
	if createTime, ok := msgData["create_time"].(float64); ok {
		msgTime = int64(createTime)
	} else if msgTimeVal, ok := msgData["msg_time"].(float64); ok {
		msgTime = int64(msgTimeVal)
	} else if msgTimeStr, ok := msgData["msg_time"].(string); ok {
		var err error
		msgTime, err = strconv.ParseInt(msgTimeStr, 10, 64)
		if err != nil {
			w.logger.Warn("无法解析msg_time", zap.Any("msg_time", msgTimeStr), zap.Error(err))
			return true // 解析失败时允许处理
		}
	} else {
		w.logger.Debug("消息中没有时间戳字段，允许处理")
		return true
	}

	// 获取当前的last_msg_time
	lastMsgTime := w.getLastMsgTime()

	// 放宽时间戳过滤条件：只有当消息时间戳明显早于last_msg_time（超过5分钟）时才跳过
	if lastMsgTime > 0 && msgTime <= lastMsgTime {
		w.logger.Debug("消息时间戳明显早于last_msg_time（超过5分钟），跳过处理",
			zap.Int64("msg_time", msgTime),
			zap.Int64("last_msg_time", lastMsgTime))
		return false
	}

	w.logger.Debug("消息通过时间戳检查，允许处理",
		zap.Int64("msg_time", msgTime),
		zap.Int64("last_msg_time", lastMsgTime))
	return true
}

// getLastMsgTime 线程安全地获取last_msg_time
func (w *webSocketClientImpl) getLastMsgTime() int64 {
	w.lastMsgTimeMutex.RLock()
	defer w.lastMsgTimeMutex.RUnlock()
	return w.lastMsgTime
}

// setLastMsgTime 线程安全地设置last_msg_time
func (w *webSocketClientImpl) setLastMsgTime(msgTime int64) {
	w.lastMsgTimeMutex.Lock()
	defer w.lastMsgTimeMutex.Unlock()
	w.lastMsgTime = msgTime
}

// reloadWebSocketURLFromDB 从数据库重新加载WebSocket URL配置
func (w *webSocketClientImpl) reloadWebSocketURLFromDB() error {
	// 使用配置管理器强制重新加载配置
	// ReloadConfig会自动检查配置变化并通知所有观察者（包括自己）
	// 观察者通知会调用OnConfigChanged，进而处理URL更新
	ctx := context.Background()
	_, err := wsConfigService.ReloadConfig(ctx, int64(w.appConfig.OwnerID))
	if err != nil {
		return fmt.Errorf("重新加载WebSocket配置失败: %w", err)
	}

	w.logger.Info("WebSocket配置重新加载完成，已通过观察者模式更新相关组件",
		zap.Int("owner_id", w.appConfig.OwnerID))

	return nil
}

// UpdateWebSocketURL 更新WebSocket URL
func (w *webSocketClientImpl) UpdateWebSocketURL(newURL string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	oldURL := w.config.URL
	if oldURL == newURL {
		w.logger.Debug("WebSocket URL无变化，跳过更新",
			zap.String("url", newURL))
		return nil
	}

	w.config.URL = newURL

	// 如果当前已连接，需要重新连接
	if w.connected {
		w.logger.Info("WebSocket URL已变更，将重新连接",
			zap.String("old_url", oldURL),
			zap.String("new_url", newURL))

		// 先断开现有连接
		if w.conn != nil {
			w.conn.Close()
			w.connected = false
		}

		// 重新连接将由reconnect逻辑自动处理
	} else {
		w.logger.Info("WebSocket URL已更新",
			zap.String("old_url", oldURL),
			zap.String("new_url", newURL))
	}

	return nil
}

// OnConfigChanged 实现ConfigObserver接口
func (w *webSocketClientImpl) OnConfigChanged(config *WebSocketConfigData) error {
	return w.UpdateWebSocketURL(config.URL)
}

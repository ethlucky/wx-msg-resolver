package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/panjf2000/ants/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// WebhookPayload Webhook推送数据结构
type WebhookPayload struct {
	EventType string                 `json:"event_type"` // 事件类型
	Timestamp int64                  `json:"timestamp"`  // 时间戳
	Data      interface{}            `json:"data"`       // 数据内容
	Metadata  map[string]interface{} `json:"metadata"`   // 元数据
	Version   string                 `json:"version"`    // 数据版本
	Source    string                 `json:"source"`     // 数据来源
}

// OcrWalletWebhookData OCR钱包数据推送结构
type OcrWalletWebhookData struct {
	ID          int64  `json:"id"`
	OwnerID     int    `json:"owner_id"` // 所有者ID
	MsgID       string `json:"msg_id"`
	GroupID     string `json:"group_id"`
	WalletCode  string `json:"wallet_code"`
	SerialNo    string `json:"serial_no"`
	WxID        string `json:"wx_id"`
	WxNickName  string `json:"wx_nick_name"`
	PicURL      string `json:"pic_url,omitempty"`
	CreateTime  int64  `json:"create_time"`  // Unix时间戳
	CallbackURL string `json:"callback_url"` // 回调URL
}

// WebhookPushTask Webhook推送任务
type WebhookPushTask struct {
	DataID    int64       `json:"data_id"`    // 数据ID
	MsgID     string      `json:"msg_id"`     // 消息ID
	Data      interface{} `json:"data"`       // 要推送的数据
	CreatedAt int64       `json:"created_at"` // 创建时间
}

// WebhookQueueProcessor Webhook推送队列处理器接口
type WebhookQueueProcessor interface {
	// AddTask 添加Webhook推送任务到队列
	AddTask(ctx context.Context, task *WebhookPushTask) error
	// StartWorkers 启动并发处理器
	StartWorkers(ctx context.Context, workerCount int) error
	// Stop 停止处理器
	Stop() error
	// GetQueueLength 获取队列长度
	GetQueueLength(ctx context.Context) (int64, error)
}

// webhookQueueProcessorImpl Webhook推送队列处理器实现
type webhookQueueProcessorImpl struct {
	redisUtil   RedisUtil
	config      *Config
	logger      *zap.Logger
	db          *gorm.DB
	httpClient  *http.Client
	processPool *ants.Pool // ants协程池用于队列任务处理

	stopOnce sync.Once
	stopped  bool
	mutex    sync.RWMutex
	stopCh   chan struct{} // 统一的停止信号
}

// 常量定义
const ()

// 队列key生成函数
func getWebhookQueueKey(ownerID int) string {
	return fmt.Sprintf("webhook_push_queue_%d", ownerID)
}

func getWebhookActiveQueueKey(ownerID int) string {
	return fmt.Sprintf("webhook_push_active_%d", ownerID)
}

// NewWebhookQueueProcessor 创建Webhook推送队列处理器
func NewWebhookQueueProcessor(
	redisUtil RedisUtil,
	config *Config,
	logger *zap.Logger,
	db *gorm.DB,
) WebhookQueueProcessor {
	return &webhookQueueProcessorImpl{
		redisUtil: redisUtil,
		config:    config,
		logger:    logger,
		db:        db,
		httpClient: &http.Client{
			Timeout: config.Webhook.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		stopped: false,
	}
}

// AddTask 添加Webhook推送任务到Redis队列
func (w *webhookQueueProcessorImpl) AddTask(ctx context.Context, task *WebhookPushTask) error {

	task.CreatedAt = time.Now().Unix()

	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("序列化Webhook任务失败: %w", err)
	}

	// 使用Redis列表作为队列
	queueKey := getWebhookQueueKey(w.config.App.OwnerID)
	if err := w.redisUtil.LPush(ctx, queueKey, string(taskJSON)); err != nil {
		return fmt.Errorf("添加Webhook任务到Redis队列失败: %w", err)
	}

	w.logger.Info("Webhook推送任务已添加到队列",
		zap.Int64("data_id", task.DataID),
		zap.String("msg_id", task.MsgID))

	return nil
}

// StartWorkers 启动指定数量的并发处理器
func (w *webhookQueueProcessorImpl) StartWorkers(ctx context.Context, workerCount int) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.stopped {
		return fmt.Errorf("Webhook推送处理器已停止")
	}

	// 创建ants协程池用于队列任务处理
	processPool, err := ants.NewPool(workerCount, ants.WithPreAlloc(true))
	if err != nil {
		w.logger.Error("创建队列处理协程池失败", zap.Error(err))
		return fmt.Errorf("创建队列处理协程池失败: %w", err)
	}
	w.processPool = processPool
	w.logger.Info("创建队列处理协程池成功",
		zap.Int("pool_size", workerCount))

	w.logger.Info("启动Webhook队列处理服务",
		zap.Int("worker_count", workerCount))

	// 创建统一的停止信号
	w.stopCh = make(chan struct{})

	// 通过ants协程池启动队列消费者
	err = w.processPool.Submit(func() {
		w.queueConsumer(ctx)
	})
	if err != nil {
		w.logger.Error("提交队列消费者到协程池失败", zap.Error(err))
		return fmt.Errorf("提交队列消费者到协程池失败: %w", err)
	}

	w.logger.Info("队列消费者已提交到协程池")

	return nil
}

// queueConsumer 队列消费者，从Redis队列获取任务并提交到ants协程池处理
func (w *webhookQueueProcessorImpl) queueConsumer(ctx context.Context) {
	w.logger.Info("Webhook队列消费者启动")

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Webhook队列消费者因上下文取消而退出")
			return
		case <-w.stopCh:
			w.logger.Info("Webhook队列消费者接收到停止信号")
			return
		default:
			// 检查是否已停止
			w.mutex.RLock()
			if w.stopped {
				w.mutex.RUnlock()
				w.logger.Info("Webhook队列消费者因处理器停止而退出")
				return
			}
			w.mutex.RUnlock()

			// 尝试从队列获取任务并提交到协程池处理
			if err := w.fetchAndProcessTask(ctx); err != nil {
				w.logger.Error("处理Webhook任务失败", zap.Error(err))
				// 发生错误时短暂等待后重试
				select {
				case <-time.After(time.Second):
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

// fetchAndProcessTask 从队列获取任务并提交到协程池处理
func (w *webhookQueueProcessorImpl) fetchAndProcessTask(ctx context.Context) error {
	// 使用BRPOPLPUSH实现可靠队列：从待处理队列移动到处理中队列
	queueKey := getWebhookQueueKey(w.config.App.OwnerID)
	activeQueueKey := getWebhookActiveQueueKey(w.config.App.OwnerID)
	taskJSON, err := w.redisUtil.BRPopLPush(ctx, queueKey, activeQueueKey, 0)
	if err != nil {
		return fmt.Errorf("从队列获取任务失败: %w", err)
	}

	// 解析任务
	var task WebhookPushTask
	if err := json.Unmarshal([]byte(taskJSON), &task); err != nil {
		w.logger.Error("反序列化Webhook任务失败",
			zap.String("task_json", taskJSON),
			zap.Error(err))
		return err
	}

	// 直接处理任务（已经在协程池中运行）
	w.processTaskWithACK(ctx, &task, taskJSON)
	return nil
}

// processTaskWithACK 处理任务并进行ACK确认
func (w *webhookQueueProcessorImpl) processTaskWithACK(ctx context.Context, task *WebhookPushTask, taskJSON string) {
	// 获取处理中队列key
	activeQueueKey := getWebhookActiveQueueKey(w.config.App.OwnerID)

	w.logger.Info("开始处理Webhook推送任务",
		zap.Int64("data_id", task.DataID))

	// 处理任务
	err := w.processWebhookTask(ctx, task)

	if err != nil {
		w.logger.Error("Webhook推送任务处理失败，重新入队",
			zap.Int64("data_id", task.DataID),
			zap.Error(err))

		// 处理失败，直接重新入队
		if requeueErr := w.AddTask(ctx, task); requeueErr != nil {
			w.logger.Error("重新加入Webhook队列失败，任务保留在处理中队列",
				zap.Int64("data_id", task.DataID),
				zap.Error(requeueErr))
		} else {
			// 成功重新入队，从处理中队列删除
			if removeErr := w.redisUtil.LRem(ctx, activeQueueKey, 1, taskJSON); removeErr != nil {
				w.logger.Error("从处理中队列删除任务失败",
					zap.Int64("data_id", task.DataID),
					zap.Error(removeErr))
			}
		}
		return
	}

	// 任务处理成功，从处理中队列删除任务（ACK确认）
	if removeErr := w.redisUtil.LRem(ctx, activeQueueKey, 1, taskJSON); removeErr != nil {
		w.logger.Error("从处理中队列删除任务失败",
			zap.Int64("data_id", task.DataID),
			zap.Error(removeErr))
		// 删除失败不影响主流程，任务已经处理完成
	}

	w.logger.Info("Webhook推送任务处理完成并已确认",
		zap.Int64("data_id", task.DataID))
}

// processWebhookTask 处理具体的Webhook推送任务
func (w *webhookQueueProcessorImpl) processWebhookTask(ctx context.Context, task *WebhookPushTask) error {
	// 继续执行webhook处理逻辑，enabled设置将在URL调用时检查
	return w.processOCRWalletBatchTask(ctx, task)
}

// Stop 停止Webhook推送处理器
func (w *webhookQueueProcessorImpl) Stop() error {
	w.stopOnce.Do(func() {
		w.mutex.Lock()
		w.stopped = true
		w.mutex.Unlock()

		w.logger.Info("正在停止Webhook推送处理器...")

		// 发送停止信号
		if w.stopCh != nil {
			close(w.stopCh)
		}

		// 释放ants协程池
		if w.processPool != nil {
			w.processPool.Release()
			w.logger.Info("已释放队列处理协程池")
		}

		w.logger.Info("Webhook推送处理器已停止")
	})

	return nil
}

// GetQueueLength 获取队列长度
func (w *webhookQueueProcessorImpl) GetQueueLength(ctx context.Context) (int64, error) {
	queueKey := getWebhookQueueKey(w.config.App.OwnerID)
	return w.redisUtil.LLen(ctx, queueKey)
}

// pushToAllURLs 推送到所有URL，顺序执行
func (w *webhookQueueProcessorImpl) pushToAllURLs(ctx context.Context, payload *WebhookPayload) error {
	// 检查是否启用了推送功能
	if !w.config.Webhook.Enabled {
		w.logger.Info("Webhook推送已禁用，跳过URL调用",
			zap.Bool("enabled", w.config.Webhook.Enabled))
		return nil
	}

	if len(w.config.Webhook.URLs) == 0 {
		w.logger.Info("未配置Webhook URL，跳过URL调用",
			zap.Int("url_count", len(w.config.Webhook.URLs)))
		return nil
	}

	var failedURLs []string

	// 顺序推送到每个URL
	for _, url := range w.config.Webhook.URLs {
		success := w.pushToURL(ctx, payload, url)
		if !success {
			failedURLs = append(failedURLs, url)
		}
	}

	// 检查是否有失败的推送
	if len(failedURLs) > 0 {
		w.logger.Error("部分或全部URL推送失败",
			zap.Strings("failed_urls", failedURLs),
			zap.Int("failed_count", len(failedURLs)),
			zap.Int("total_count", len(w.config.Webhook.URLs)))
		return fmt.Errorf("推送失败，失败URL数量: %d/%d", len(failedURLs), len(w.config.Webhook.URLs))
	}

	w.logger.Info("所有URL推送成功",
		zap.Int("total_count", len(w.config.Webhook.URLs)))
	return nil
}

// processOCRWalletBatchTask 处理OCR钱包批量推送任务
func (w *webhookQueueProcessorImpl) processOCRWalletBatchTask(ctx context.Context, task *WebhookPushTask) error {
	// 获取钱包信息数组，处理JSON反序列化后的类型转换
	var walletInfos []OcrWalletInfo

	// 处理从JSON反序列化得到的[]interface{}
	if dataSlice, ok := task.Data.([]interface{}); ok {
		walletInfos = make([]OcrWalletInfo, len(dataSlice))
		for i, item := range dataSlice {
			if itemMap, ok := item.(map[string]interface{}); ok {
				// 将map[string]interface{}转换为JSON，再反序列化为OcrWalletInfo
				jsonBytes, err := json.Marshal(itemMap)
				if err != nil {
					w.logger.Error("序列化钱包信息失败",
						zap.String("msg_id", task.MsgID),
						zap.Int("item_index", i),
						zap.Error(err))
					return err
				}

				var walletInfo OcrWalletInfo
				if err := json.Unmarshal(jsonBytes, &walletInfo); err != nil {
					w.logger.Error("反序列化钱包信息失败",
						zap.String("msg_id", task.MsgID),
						zap.Int("item_index", i),
						zap.Error(err))
					return err
				}
				walletInfos[i] = walletInfo
			} else {
				return fmt.Errorf("钱包信息项格式错误，期望map[string]interface{}，实际为: %T", item)
			}
		}
	} else if directWalletInfos, ok := task.Data.([]OcrWalletInfo); ok {
		// 直接是[]OcrWalletInfo类型（向后兼容）
		walletInfos = directWalletInfos
	} else {
		return fmt.Errorf("无效的钱包信息数据类型: %T, 期望[]interface{}或[]OcrWalletInfo", task.Data)
	}

	w.logger.Info("开始处理OCR钱包批量推送任务",
		zap.String("msg_id", task.MsgID),
		zap.Int("cards_count", len(walletInfos)))

	// 构建批量推送载荷
	payload := w.buildBatchWebhookPayload(walletInfos, task.MsgID)

	// 使用ants协程池并发推送到所有URL
	return w.pushToAllURLs(ctx, payload)
}

// buildBatchWebhookPayload 构建批量推送载荷
func (w *webhookQueueProcessorImpl) buildBatchWebhookPayload(walletInfos []OcrWalletInfo, msgID string) *WebhookPayload {
	// 构建批量数据
	batchData := make([]OcrWalletWebhookData, 0, len(walletInfos))

	for _, walletInfo := range walletInfos {
		webhookData := OcrWalletWebhookData{
			ID:          walletInfo.ID,
			OwnerID:     walletInfo.OwnerID,
			MsgID:       walletInfo.MsgID,
			GroupID:     walletInfo.GroupID,
			WalletCode:  walletInfo.WalletCode,
			SerialNo:    walletInfo.SerialNo,
			WxID:        walletInfo.WxID,
			WxNickName:  walletInfo.WxNickName,
			CreateTime:  walletInfo.CreateTime.Unix(),
			CallbackURL: w.config.Webhook.CallbackURL,
		}

		if walletInfo.PicURL != nil {
			webhookData.PicURL = *walletInfo.PicURL
		}

		batchData = append(batchData, webhookData)
	}

	// 打印batchData内容的日志
	w.logger.Info("批量数据构建完成",
		zap.String("msg_id", msgID),
		zap.Int("data_count", len(batchData)),
		zap.Any("batch_data", batchData))

	return &WebhookPayload{
		EventType: "ocr_wallet_batch_data", // 使用不同的事件类型
		Timestamp: time.Now().Unix(),
		Data:      batchData, // 数组形式的数据
		Metadata: map[string]interface{}{
			"cards_count":  len(walletInfos),
			"push_source":  "wx-msg-chat",
			"batch_msg_id": msgID,
			"single_image": true, // 标识这些卡片来自同一张图片
		},
		Version: "1.0",
		Source:  "ocr_wallet_service",
	}
}

// pushToURL 推送到单个URL，单次尝试，返回是否成功
func (w *webhookQueueProcessorImpl) pushToURL(ctx context.Context, payload *WebhookPayload, url string) bool {
	return w.sendRequest(ctx, payload, url)
}

// sendRequest 发送HTTP请求
func (w *webhookQueueProcessorImpl) sendRequest(ctx context.Context, payload *WebhookPayload, url string) bool {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		w.logger.Error("序列化推送数据失败",
			zap.String("url", url),
			zap.Error(err))
		return false
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		w.logger.Error("创建HTTP请求失败",
			zap.String("url", url),
			zap.Error(err))
		return false
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "wx-msg-chat-webhook/1.0")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		w.logger.Warn("推送请求失败",
			zap.String("url", url),
			zap.Error(err))
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		w.logger.Debug("推送成功",
			zap.String("url", url),
			zap.Int("status_code", resp.StatusCode))
		return true
	} else {
		w.logger.Warn("推送失败",
			zap.String("url", url),
			zap.Int("status_code", resp.StatusCode))
		return false
	}
}

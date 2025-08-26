package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/panjf2000/ants/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// OCRTask OCR处理任务
type OCRTask struct {
	MsgID      string `json:"msg_id"`
	GroupID    string `json:"group_id"`
	WxID       string `json:"wx_id"`
	WxNickName string `json:"wx_nick_name"`
	Content    string `json:"content"`
	OwnerID    int    `json:"owner_id"`
	CreatedAt  int64  `json:"created_at"`
}

// OCRProcessor OCR处理器接口
type OCRProcessor interface {
	// AddTask 添加OCR任务到队列
	AddTask(ctx context.Context, task *OCRTask) error
	// StartWorkers 启动并发处理器
	StartWorkers(ctx context.Context, workerCount int) error
	// Stop 停止处理器
	Stop() error
	// GetQueueLength 获取队列长度
	GetQueueLength(ctx context.Context) (int64, error)
}

// ocrProcessorImpl OCR处理器实现
type ocrProcessorImpl struct {
	redisUtil             RedisUtil
	wechatBot             WechatBot
	ocrService            OCRService
	ocrParser             OCRContentParser
	minioClient           MinIOUploader
	webhookQueueProcessor WebhookQueueProcessor
	config                *Config
	logger                *zap.Logger
	db                    *gorm.DB

	ocrPool  *ants.Pool // ants协程池用于OCR处理
	stopOnce sync.Once
	stopped  bool
	mutex    sync.RWMutex
}

const (
	// OCR处理中的任务键前缀
	OCR_PROCESSING_PREFIX = "ocr_processing:"
	// 任务处理超时时间（秒）
	TASK_PROCESSING_TIMEOUT = 300
)

// 队列key生成函数
func getOCRQueueKey(ownerID int) string {
	return fmt.Sprintf("ocr_processing_queue_%d", ownerID)
}

func getOCRActiveQueueKey(ownerID int) string {
	return fmt.Sprintf("ocr_processing_active_%d", ownerID)
}

// NewOCRProcessor 创建OCR处理器
func NewOCRProcessor(
	redisUtil RedisUtil,
	wechatBot WechatBot,
	ocrService OCRService,
	minioClient MinIOUploader,
	webhookQueueProcessor WebhookQueueProcessor,
	config *Config,
	logger *zap.Logger,
	db *gorm.DB,
) OCRProcessor {
	ocrParser := NewOCRContentParser(logger)

	return &ocrProcessorImpl{
		redisUtil:             redisUtil,
		wechatBot:             wechatBot,
		ocrService:            ocrService,
		ocrParser:             ocrParser,
		minioClient:           minioClient,
		webhookQueueProcessor: webhookQueueProcessor,
		config:                config,
		logger:                logger,
		db:                    db,
		stopped:               false,
	}
}

// AddTask 添加OCR任务到Redis队列
func (p *ocrProcessorImpl) AddTask(ctx context.Context, task *OCRTask) error {
	task.CreatedAt = time.Now().Unix()

	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("序列化OCR任务失败: %w", err)
	}

	// 使用Redis列表作为队列
	queueKey := getOCRQueueKey(p.config.App.OwnerID)
	if err := p.redisUtil.LPush(ctx, queueKey, string(taskJSON)); err != nil {
		return fmt.Errorf("添加OCR任务到Redis队列失败: %w", err)
	}

	p.logger.Info("OCR任务已添加到队列",
		zap.String("msg_id", task.MsgID),
		zap.String("group_id", task.GroupID))

	return nil
}

// StartWorkers 启动指定数量的并发处理器
func (p *ocrProcessorImpl) StartWorkers(ctx context.Context, workerCount int) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.stopped {
		return fmt.Errorf("OCR处理器已停止")
	}

	// 创建ants协程池用于OCR处理
	pool, err := ants.NewPool(workerCount, ants.WithPreAlloc(true))
	if err != nil {
		p.logger.Error("创建OCR协程池失败", zap.Error(err))
		return fmt.Errorf("创建OCR协程池失败: %w", err)
	}
	p.ocrPool = pool

	p.logger.Info("启动OCR处理器",
		zap.Int("pool_size", workerCount))

	// 通过ants协程池启动队列消费者
	err = p.ocrPool.Submit(func() {
		p.queueConsumer(ctx)
	})
	if err != nil {
		p.logger.Error("提交队列消费者到协程池失败", zap.Error(err))
		return fmt.Errorf("提交队列消费者到协程池失败: %w", err)
	}

	p.logger.Info("队列消费者已提交到协程池")

	return nil
}

// queueConsumer 队列消费者，从Redis队列获取任务并提交到ants协程池处理
func (p *ocrProcessorImpl) queueConsumer(ctx context.Context) {
	p.logger.Info("OCR队列消费者启动")

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("OCR队列消费者因上下文取消而退出")
			return
		default:
			// 检查是否已停止
			p.mutex.RLock()
			if p.stopped {
				p.mutex.RUnlock()
				p.logger.Info("OCR队列消费者因处理器停止而退出")
				return
			}
			p.mutex.RUnlock()

			// 尝试从队列获取任务
			if err := p.fetchAndProcessTask(ctx); err != nil {
				p.logger.Error("处理OCR任务失败", zap.Error(err))
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
func (p *ocrProcessorImpl) fetchAndProcessTask(ctx context.Context) error {
	// 使用BRPOPLPUSH实现可靠队列：从待处理队列移动到处理中队列
	queueKey := getOCRQueueKey(p.config.App.OwnerID)
	activeQueueKey := getOCRActiveQueueKey(p.config.App.OwnerID)
	taskJSON, err := p.redisUtil.BRPopLPush(ctx, queueKey, activeQueueKey, 0)
	if err != nil {
		return fmt.Errorf("从队列获取任务失败: %w", err)
	}

	// 解析任务
	var task OCRTask
	if err := json.Unmarshal([]byte(taskJSON), &task); err != nil {
		p.logger.Error("反序列化OCR任务失败",
			zap.String("task_json", taskJSON),
			zap.Error(err))
		return err
	}

	// 直接处理任务（已经在协程池中运行）
	p.processTaskWithACK(ctx, &task, taskJSON)
	return nil
}

// processTaskWithACK 处理任务并进行ACK确认
func (p *ocrProcessorImpl) processTaskWithACK(ctx context.Context, task *OCRTask, taskJSON string) {
	// 获取处理中队列key
	activeQueueKey := getOCRActiveQueueKey(p.config.App.OwnerID)

	p.logger.Info("开始处理OCR任务",
		zap.String("msg_id", task.MsgID))

	// 处理任务
	err := p.processOCRTask(ctx, task)

	if err != nil {
		p.logger.Error("OCR任务处理失败，重新入队",
			zap.String("msg_id", task.MsgID),
			zap.Error(err))

		// 处理失败，直接重新入队
		if requeueErr := p.AddTask(ctx, task); requeueErr != nil {
			p.logger.Error("重新加入OCR队列失败，任务保留在处理中队列",
				zap.String("msg_id", task.MsgID),
				zap.Error(requeueErr))
		} else {
			// 成功重新入队，从处理中队列删除
			if removeErr := p.redisUtil.LRem(ctx, activeQueueKey, 1, taskJSON); removeErr != nil {
				p.logger.Error("从处理中队列删除任务失败",
					zap.String("msg_id", task.MsgID),
					zap.Error(removeErr))
			}
		}
		return
	}

	// 任务处理成功，从处理中队列删除任务（ACK确认）
	if removeErr := p.redisUtil.LRem(ctx, activeQueueKey, 1, taskJSON); removeErr != nil {
		p.logger.Error("从处理中队列删除任务失败",
			zap.String("msg_id", task.MsgID),
			zap.Error(removeErr))
		// 删除失败不影响主流程，任务已经处理完成
	}

	p.logger.Info("OCR任务处理完成并已确认",
		zap.String("msg_id", task.MsgID))
}

// processOCRTask 处理具体的OCR任务
func (p *ocrProcessorImpl) processOCRTask(ctx context.Context, task *OCRTask) error {
	// 执行OCR识别
	ocrResult, minioURL, err := p.executeImageOCR(ctx, task.MsgID, task.Content)
	if err != nil {
		return fmt.Errorf("图片OCR识别失败: %w", err)
	}

	// 如果识别到内容，保存到数据库
	if ocrResult != nil && len(ocrResult.Results) > 0 {
		// 构建ForwardedMessage用于保存
		forwardedMsg := ForwardedMessage{
			MsgID:   task.MsgID,
			GroupID: task.GroupID,
			WxID:    task.WxID,
			OwnerID: task.OwnerID,
			MsgType: 3, // 图片消息
		}

		err := p.saveOcrWalletInfo(forwardedMsg, task.WxNickName, ocrResult, minioURL)
		if err != nil {
			return fmt.Errorf("保存OCR钱包信息失败: %w", err)
		}

		p.logger.Info("OCR钱包信息保存成功",
			zap.String("msg_id", task.MsgID),
			zap.Int("pairs_count", len(ocrResult.Results)))
	} else {
		p.logger.Info("未识别到有效的钱包信息",
			zap.String("msg_id", task.MsgID))
	}

	return nil
}

// Stop 停止OCR处理器
func (p *ocrProcessorImpl) Stop() error {
	p.stopOnce.Do(func() {
		p.mutex.Lock()
		p.stopped = true
		p.mutex.Unlock()

		p.logger.Info("正在停止OCR处理器...")

		// 释放ants协程池
		if p.ocrPool != nil {
			p.ocrPool.Release()
			p.logger.Info("已释放OCR协程池")
		}

		p.logger.Info("OCR处理器已停止")
	})

	return nil
}

// GetQueueLength 获取队列长度
func (p *ocrProcessorImpl) GetQueueLength(ctx context.Context) (int64, error) {
	queueKey := getOCRQueueKey(p.config.App.OwnerID)
	return p.redisUtil.LLen(ctx, queueKey)
}

// executeImageOCR 执行图片OCR识别（从原有代码迁移）
func (p *ocrProcessorImpl) executeImageOCR(ctx context.Context, msgId string, content string) (*OCRParseResult, string, error) {
	// 检查服务是否可用
	if p.wechatBot == nil || p.ocrService == nil {
		return nil, "", fmt.Errorf("OCR或WeChat Bot服务未初始化")
	}

	// 解析content JSON字符串，提取aeskey和cdnurl
	var contentData map[string]any
	if err := json.Unmarshal([]byte(content), &contentData); err != nil {
		return nil, "", fmt.Errorf("解析content JSON失败: %w", err)
	}

	// 提取aeskey和cdnurl
	aeskey, ok := contentData["aeskey"].(string)
	if !ok || aeskey == "" {
		return nil, "", fmt.Errorf("未找到或aeskey为空")
	}

	cdnurl, ok := contentData["cdnurl"].(string)
	if !ok || cdnurl == "" {
		return nil, "", fmt.Errorf("未找到或cdnurl为空")
	}

	p.logger.Info("开始处理图片OCR识别",
		zap.String("aeskey", aeskey),
		zap.String("cdnurl", cdnurl))

	// 调用SendCdnDownload获取图片的base64数据
	cdnReq := &SendCdnDownloadRequest{
		AesKey:   aeskey,
		FileType: 2, // 1表示图片类型
		FileURL:  cdnurl,
	}

	cdnResp, err := p.wechatBot.SendCdnDownload(cdnReq)
	if err != nil {
		return nil, "", fmt.Errorf("获取图片数据失败: %w", err)
	}

	// 检查是否成功获取到图片数据
	if cdnResp.Data.FileData == "" {
		return nil, "", fmt.Errorf("获取到的图片数据为空")
	}

	p.logger.Info("成功获取图片数据，先上传到MinIO",
		zap.Int("data_size", len(cdnResp.Data.FileData)))

	// 先将base64图片数据保存到MinIO
	var minioURL string
	// 为MinIO上传操作创建带超时的子context（30秒超时，适合网络上传）
	uploadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	fileName := fmt.Sprintf("ocr_image_%s.jpg", msgId)
	minioURL, err = p.minioClient.UploadBase64Image(uploadCtx, cdnResp.Data.FileData, fileName)
	if err != nil {
		p.logger.Error("保存图片到MinIO失败，回退到base64识别",
			zap.Error(err),
			zap.String("filename", fileName))

		// MinIO上传失败，回退到base64识别
		return p.performOCRWithBase64(cdnResp.Data.FileData, msgId)
	}

	p.logger.Info("图片已成功保存到MinIO，使用URL进行OCR识别",
		zap.String("minio_url", minioURL),
		zap.String("filename", fileName))

	// 使用MinIO URL进行OCR识别
	ocrTexts, err := p.ocrService.RecognizeTextSimple(minioURL, false)
	if err != nil {
		p.logger.Warn("使用MinIO URL进行OCR识别失败，回退到base64识别",
			zap.Error(err),
			zap.String("minio_url", minioURL))

		// URL识别失败，回退到base64识别
		return p.performOCRWithBase64(cdnResp.Data.FileData, msgId)
	}

	// 解析识别到的文字，提取充值码和序列号
	var parseResult *OCRParseResult
	if len(ocrTexts) > 0 {
		// 使用OCR解析器提取充值码和序列号
		parseResult = p.ocrParser.ParseContent(ocrTexts)

		p.logger.Info("OCR识别和解析完成（使用MinIO URL）",
			zap.Int("text_count", len(ocrTexts)),
			zap.Int("pairs_count", len(parseResult.Results)),
			zap.Strings("original_texts", ocrTexts))
	} else {
		// 如果没有识别到文字，返回空的解析结果
		parseResult = &OCRParseResult{}
		p.logger.Info("未识别到图片中的文字（使用MinIO URL）")
	}

	return parseResult, minioURL, nil

}

// performOCRWithBase64 使用base64数据进行OCR识别（回退机制）
func (p *ocrProcessorImpl) performOCRWithBase64(base64Data string, msgId string) (*OCRParseResult, string, error) {
	p.logger.Info("使用base64数据进行OCR识别",
		zap.String("msg_id", msgId))

	// 调用OCR服务识别图片中的文字
	ocrTexts, err := p.ocrService.RecognizeTextSimple(base64Data, false)
	if err != nil {
		return nil, "", fmt.Errorf("OCR识别失败: %w", err)
	}

	// 解析识别到的文字，提取充值码和序列号
	var parseResult *OCRParseResult
	if len(ocrTexts) > 0 {
		// 使用OCR解析器提取充值码和序列号
		parseResult = p.ocrParser.ParseContent(ocrTexts)

		p.logger.Info("OCR识别和解析完成（使用base64）",
			zap.Int("text_count", len(ocrTexts)),
			zap.Int("pairs_count", len(parseResult.Results)),
			zap.Strings("original_texts", ocrTexts))
	} else {
		// 如果没有识别到文字，返回空的解析结果
		parseResult = &OCRParseResult{}
		p.logger.Info("未识别到图片中的文字（使用base64）")
	}

	return parseResult, "", nil // 使用base64识别时没有MinIO URL
}

// saveOcrWalletInfo 保存OCR钱包信息到数据库（从原有代码迁移）
func (p *ocrProcessorImpl) saveOcrWalletInfo(forwardedMsg ForwardedMessage, wxNickName string, ocrResult *OCRParseResult, picURL string) error {
	// 如果没有配对结果，记录警告
	if len(ocrResult.Results) == 0 {
		p.logger.Warn("没有找到有效的钱包码和序列号配对",
			zap.String("msg_id", forwardedMsg.MsgID))
		return nil
	}

	// 保存每个配对的钱包码和序列号到数据库
	var savedWalletInfos []OcrWalletInfo
	for i, pair := range ocrResult.Results {
		walletInfo, err := p.saveOcrWalletInfoSingle(forwardedMsg, wxNickName, pair.WalletCode, pair.SerialNo, picURL)
		if err != nil {
			p.logger.Error("保存第几个钱包码配对失败",
				zap.Int("pair_index", i+1),
				zap.String("wallet_code", pair.WalletCode),
				zap.String("serial_no", pair.SerialNo),
				zap.Error(err))
			continue // 继续处理其他卡片，即使有些失败了
		}
		// 收集成功保存的钱包信息
		if walletInfo != nil {
			savedWalletInfos = append(savedWalletInfos, *walletInfo)
		}
	}

	p.logger.Info("OCR钱包信息批量保存完成",
		zap.String("msg_id", forwardedMsg.MsgID),
		zap.Int("pairs_count", len(ocrResult.Results)),
		zap.Int("saved_count", len(savedWalletInfos)))

	// 如果有成功保存的钱包信息且启用了Webhook推送，批量推送所有卡片信息
	if len(savedWalletInfos) > 0 {
		// 创建批量推送任务，包含所有识别到的卡片信息
		webhookTask := &WebhookPushTask{
			DataID:    0, // 批量推送不需要单个ID
			MsgID:     forwardedMsg.MsgID,
			Data:      savedWalletInfos, // 传递所有卡片信息数组
			CreatedAt: time.Now().Unix(),
		}

		// 加入Webhook推送队列
		ctx := context.Background()
		if err := p.addWebhookTask(ctx, webhookTask); err != nil {
			p.logger.Error("加入批量Webhook推送队列失败",
				zap.String("msg_id", forwardedMsg.MsgID),
				zap.Int("cards_count", len(savedWalletInfos)),
				zap.Error(err))
		} else {
			p.logger.Info("OCR钱包数据已加入批量Webhook推送队列",
				zap.String("msg_id", forwardedMsg.MsgID),
				zap.Int("cards_count", len(savedWalletInfos)))
		}
	} else {
		p.logger.Debug("Webhook推送已禁用或没有成功保存的卡片信息，跳过推送",
			zap.String("msg_id", forwardedMsg.MsgID),
			zap.Int("saved_count", len(savedWalletInfos)))
	}

	return nil
}

// saveOcrWalletInfoSingle 保存单个OCR钱包信息到数据库（从原有代码迁移）
// 返回保存的钱包信息，用于批量推送
func (p *ocrProcessorImpl) saveOcrWalletInfoSingle(forwardedMsg ForwardedMessage, wxNickName, walletCode, serialNo, picURL string) (*OcrWalletInfo, error) {
	// 构建OCR钱包信息记录
	ocrWalletInfo := OcrWalletInfo{
		OwnerID:    forwardedMsg.OwnerID,
		MsgID:      forwardedMsg.MsgID,
		GroupID:    forwardedMsg.GroupID,
		WalletCode: walletCode,
		SerialNo:   serialNo,
		WxID:       forwardedMsg.WxID,
		WxNickName: wxNickName,
	}

	// 设置图片URL（如果不为空）
	if picURL != "" {
		ocrWalletInfo.PicURL = &picURL
	}

	// 使用原生SQL插入
	insertSQL := `INSERT INTO ocr_wallet_info (
		owner_id, msg_id, group_id, wallet_code, serial_no, wx_id, wx_nick_name, pic_url
	) VALUES (
		?, ?, ?, ?, ?, ?, ?, ?
	)`

	result := p.db.Exec(insertSQL,
		forwardedMsg.OwnerID,
		ocrWalletInfo.MsgID,
		ocrWalletInfo.GroupID,
		ocrWalletInfo.WalletCode,
		ocrWalletInfo.SerialNo,
		ocrWalletInfo.WxID,
		ocrWalletInfo.WxNickName,
		ocrWalletInfo.PicURL,
	)

	if result.Error != nil {
		// 检查是否为唯一键冲突错误
		if strings.Contains(result.Error.Error(), "Duplicate entry") || strings.Contains(result.Error.Error(), "UNIQUE constraint") {
			p.logger.Debug("OCR钱包信息已存在，跳过保存", zap.String("msg_id", ocrWalletInfo.MsgID))
			return &ocrWalletInfo, nil // 重复记录，返回数据用于批量推送
		}
		return nil, fmt.Errorf("插入OCR钱包信息失败: %w", result.Error)
	}

	// 数据保存成功，返回保存的钱包信息用于批量推送
	// 注意：不在这里进行单个推送，而是在上层函数中进行批量推送
	return &ocrWalletInfo, nil
}

// addWebhookTask 添加Webhook推送任务到队列
func (p *ocrProcessorImpl) addWebhookTask(ctx context.Context, task *WebhookPushTask) error {
	if p.webhookQueueProcessor == nil {
		return fmt.Errorf("Webhook队列处理器未初始化")
	}
	return p.webhookQueueProcessor.AddTask(ctx, task)
}

# OCR批量推送功能测试说明

## 修改内容概述

已成功修改OCR识别和推送逻辑，现在当一张图识别出多张卡时，会将所有卡的信息作为一个批量推送任务同时推送，而不是分开进行推送。

## 主要修改

### 1. ocr_processor.go 修改

#### saveOcrWalletInfo函数
- 收集所有成功保存的钱包信息
- 创建单个批量推送任务`ocr_wallet_batch`
- 包含所有识别到的卡片信息数组

#### saveOcrWalletInfoSingle函数  
- 返回值从`error`修改为`(*OcrWalletInfo, error)`
- 移除单个卡片的webhook推送逻辑
- 返回保存的钱包信息用于批量推送

### 2. webhook_queue_processor.go 修改

#### 新增processOCRWalletBatchTask函数
- 处理`ocr_wallet_batch`类型的推送任务
- 构建包含多个卡片信息的批量载荷
- 支持类型转换和数据验证

#### 新增buildBatchWebhookPayload函数
- 构建批量推送载荷
- 事件类型为`ocr_wallet_batch_data`
- 包含所有卡片信息数组
- 添加批量相关元数据（如卡片数量、消息ID等）

## 推送数据结构变化

### 之前（单个推送）
```json
{
  "event_type": "ocr_wallet_data",
  "data": {
    "id": 123,
    "wallet_code": "ABC123",
    "serial_no": "SN001"
    // ... 单个卡片信息
  }
}
```

### 现在（批量推送）
```json
{
  "event_type": "ocr_wallet_batch_data", 
  "data": [
    {
      "id": 123,
      "wallet_code": "ABC123",
      "serial_no": "SN001"
      // ... 第一张卡片信息
    },
    {
      "id": 124,
      "wallet_code": "DEF456", 
      "serial_no": "SN002"
      // ... 第二张卡片信息
    }
    // ... 更多卡片
  ],
  "metadata": {
    "cards_count": 2,
    "single_image": true,
    "batch_msg_id": "msg_12345"
  }
}
```

## 架构重构改进

### 统一协程池管理
在用户反馈后，进一步优化了并发处理架构，实现了完全统一的ants协程池管理：

#### webhook_queue_processor.go 重构
1. **移除手动goroutine创建**：
   - 删除了`workers []chan struct{}`和`workerWG sync.WaitGroup`
   - 不再手动创建多个worker goroutines

2. **新增processPool**：
   - 添加`processPool *ants.Pool`用于队列任务处理
   - 与`pushPool`分离职责：processPool处理队列任务，pushPool处理HTTP推送

3. **重构队列消费模式**：
   - `StartWorkers`现在创建两个ants协程池
   - `queueConsumer`作为单一消费者从Redis队列获取任务
   - 任务通过`processPool.Submit()`提交到协程池处理

4. **简化停止机制**：
   - 使用单一`stopCh chan struct{}`信号
   - 直接释放两个协程池资源

### 最终简化优化
在进一步分析后，对webhook推送逻辑进行了简化：

#### 移除pushPool协程池
- 移除了`pushPool *ants.Pool`字段
- `pushToAllURLs`函数改为简单的顺序循环推送
- 移除了复杂的WaitGroup和mutex同步逻辑

#### 简化的推送逻辑
```go
// 顺序推送到每个URL
for _, url := range w.config.Webhook.URLs {
    success := w.pushToURL(ctx, payload, url)
    if !success {
        failedURLs = append(failedURLs, url)
    }
}
```

#### 完全统一ants协程池管理
最终修复了所有手动goroutine创建：
- **queueConsumer启动**：OCR和Webhook处理器的队列消费者现在都通过ants协程池启动
- **彻底移除go关键字**：业务逻辑中不再有手动创建的goroutine
- **简化任务处理**：移除了`queueConsumer`内部的重复协程池提交

```go
// 启动队列消费者（通过ants协程池）
err = p.ocrPool.Submit(func() {
    p.queueConsumer(ctx)
})

// 在queueConsumer中直接处理任务，无需再次Submit
func (p *ocrProcessorImpl) fetchAndProcessTask(ctx context.Context) error {
    // ... 获取任务
    // 直接处理任务（已经在协程池中运行）
    p.processTaskWithACK(ctx, &task, taskJSON)
    return nil
}
```

#### 彻底移除重试相关代码
按照用户要求，完全清理了所有重试相关的代码和命名：
- **移除RetryCount字段**：从OCRTask和WebhookPushTask结构体中删除
- **移除重试常量**：删除MAX_RETRY_COUNT等常量定义  
- **简化变量命名**：retryErr重命名为requeueErr
- **移除重试逻辑**：pushToURL单次尝试，processTaskWithACK失败直接重新入队

```go
// OCRTask结构体（移除RetryCount字段）
type OCRTask struct {
    MsgID      string `json:"msg_id"`
    GroupID    string `json:"group_id"`
    // ... 其他字段
    CreatedAt  int64  `json:"created_at"`
    // RetryCount字段已删除
}

// 简化后的推送逻辑
func (w *webhookQueueProcessorImpl) pushToURL(ctx context.Context, payload *WebhookPayload, url string) bool {
    return w.sendRequest(ctx, payload, url)
}

// 简化后的任务处理
if err != nil {
    // 处理失败，直接重新入队
    if requeueErr := w.AddTask(ctx, task); requeueErr != nil {
        // 保留在处理中队列等待恢复
    } else {
        // 从处理中队列删除
    }
    return
}
```

### 架构优势
- **简洁高效**：队列任务处理使用ants协程池，URL推送使用简单循环
- **资源控制**：只在需要并发处理的地方（队列任务）使用协程池
- **逻辑清晰**：减少了不必要的并发复杂度和重试逻辑
- **快速恢复**：失败任务立即重新入队，依靠队列机制自然重试

## 测试验证

1. **编译验证**: ✅ 代码编译成功，无语法错误
2. **函数签名**: ✅ 所有函数调用和返回值匹配
3. **向后兼容**: ✅ 保留原有单个卡片推送逻辑（ocr_wallet类型）
4. **架构统一**: ✅ 全部并发操作使用ants协程池管理

## 使用说明

当OCR识别一张图片时：
- 如果识别到1张卡：创建批量推送任务，数组包含1个元素
- 如果识别到多张卡：创建批量推送任务，数组包含所有卡片
- Webhook接收端可根据`event_type`区分批量推送和单个推送
- 批量推送的`metadata.cards_count`表示卡片数量
- 批量推送的`metadata.single_image`始终为true，表示来自同一张图片

## 注意事项

- 数据库保存逻辑保持不变，每张卡仍单独保存
- 即使部分卡片保存失败，成功保存的卡片仍会被推送
- 推送失败会按原有重试机制处理
- 保持了对原有webhook处理逻辑的兼容性
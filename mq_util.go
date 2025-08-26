package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/wagslane/go-rabbitmq"
	"go.uber.org/zap"
)

// MQManager RabbitMQ管理器接口
type MQManager interface {
	// 初始化连接
	Init() error
	// 发送消息
	Publish(exchange, routingKey string, body []byte, contentType string) error
	// 发送JSON消息
	PublishJSON(exchange, routingKey string, data interface{}) error
	// 启动消费者
	StartConsumer(ctx context.Context, queueName string, handler MessageHandler) error
	// 获取连接状态
	IsConnected() bool
	// 健康检查
	HealthCheck() bool
	// 关闭连接
	Close() error
	// 带确认机制的发布消息
	PublishWithConfirm(ctx context.Context, exchange, routingKey string, body []byte, contentType string) error
	// 带确认机制的发布JSON消息
	PublishJSONWithConfirm(ctx context.Context, exchange, routingKey string, data interface{}) error
	// 带确认机制和重试的发布消息
	PublishWithConfirmAndRetry(ctx context.Context, exchange, routingKey string, body []byte, contentType string, maxRetries int) error
	// 带确认机制和重试的发布JSON消息
	PublishJSONWithConfirmAndRetry(ctx context.Context, exchange, routingKey string, data interface{}, maxRetries int) error
	// 带重试的发布消息
	PublishWithRetry(exchange, routingKey string, body []byte, contentType string, maxRetries int) error
	// 带重试的发布JSON消息
	PublishJSONWithRetry(exchange, routingKey string, data interface{}, maxRetries int) error
}

// MessageHandler 消息处理器接口
type MessageHandler interface {
	HandleMessage(ctx context.Context, body []byte) error
}

// MessageHandlerFunc 消息处理器函数类型
type MessageHandlerFunc func(ctx context.Context, body []byte) error

// HandleMessage 实现MessageHandler接口
func (f MessageHandlerFunc) HandleMessage(ctx context.Context, body []byte) error {
	return f(ctx, body)
}

// mqManagerImpl RabbitMQ管理器实现
type mqManagerImpl struct {
	config        *RabbitMQConfig
	logger        *zap.Logger
	publisher     *rabbitmq.Publisher
	consumers     []*rabbitmq.Consumer
	mutex         sync.RWMutex
	stopChan      chan struct{}
	stopOnce      sync.Once
	isConnected   bool
	connectionURL string
	retryHeaders  map[string]string
}

// NewMQManager 创建RabbitMQ管理器
func NewMQManager(config *RabbitMQConfig, logger *zap.Logger) MQManager {
	return &mqManagerImpl{
		config:       config,
		logger:       logger,
		stopChan:     make(chan struct{}),
		consumers:    make([]*rabbitmq.Consumer, 0),
		isConnected:  false,
		retryHeaders: make(map[string]string),
	}
}

// Init 初始化RabbitMQ连接
func (m *mqManagerImpl) Init() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 构造连接URL
	vhost := m.config.Vhost
	if vhost == "/" {
		vhost = ""
	}

	m.connectionURL = fmt.Sprintf("amqp://%s:%s@%s:%d/%s",
		url.QueryEscape(m.config.Username),
		url.QueryEscape(m.config.Password),
		m.config.Host,
		m.config.Port,
		vhost,
	)

	m.logger.Info("正在连接RabbitMQ服务器",
		zap.String("host", m.config.Host),
		zap.Int("port", m.config.Port),
		zap.String("vhost", m.config.Vhost),
		zap.String("username", m.config.Username))

	// 创建连接
	conn, err := rabbitmq.NewConn(m.connectionURL, rabbitmq.WithConnectionOptionsLogging)
	if err != nil {
		return fmt.Errorf("创建RabbitMQ连接失败: %w", err)
	}

	// 创建发布者
	publisher, err := rabbitmq.NewPublisher(
		conn,
		rabbitmq.WithPublisherOptionsLogging,
		rabbitmq.WithPublisherOptionsExchangeName(m.config.ExchangeName),
		rabbitmq.WithPublisherOptionsExchangeDeclare,
		rabbitmq.WithPublisherOptionsExchangeDurable,
	)
	if err != nil {
		return fmt.Errorf("创建RabbitMQ发布者失败: %w", err)
	}
	m.publisher = publisher

	m.isConnected = true

	// 设置死信队列
	if err := m.setupDeadLetterQueue(); err != nil {
		m.logger.Warn("设置死信队列失败", zap.Error(err))
	}

	m.logger.Info("RabbitMQ初始化完成",
		zap.String("exchange", m.config.ExchangeName),
		zap.String("queue", m.config.QueueName),
		zap.String("routing_key", m.config.RoutingKey),
		zap.Bool("durable", m.config.Durable))

	return nil
}

// Publish 发送消息到RabbitMQ
func (m *mqManagerImpl) Publish(exchange, routingKey string, body []byte, contentType string) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if m.publisher == nil {
		return fmt.Errorf("RabbitMQ发布者未初始化")
	}

	err := m.publisher.Publish(
		body,
		[]string{routingKey},
		rabbitmq.WithPublishOptionsContentType(contentType),
		rabbitmq.WithPublishOptionsExchange(exchange),
		rabbitmq.WithPublishOptionsMandatory,
		rabbitmq.WithPublishOptionsPersistentDelivery,
	)
	if err != nil {
		return fmt.Errorf("发布消息失败: %w", err)
	}

	m.logger.Info("消息发布成功",
		zap.String("exchange", exchange),
		zap.String("routing_key", routingKey),
		zap.String("content_type", contentType),
		zap.Int("body_size", len(body)))

	return nil
}

// PublishJSON 发送JSON消息到RabbitMQ
func (m *mqManagerImpl) PublishJSON(exchange, routingKey string, data interface{}) error {
	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化JSON消息失败: %w", err)
	}

	return m.Publish(exchange, routingKey, body, "application/json")
}

// StartConsumer 启动消费者
func (m *mqManagerImpl) StartConsumer(ctx context.Context, queueName string, handler MessageHandler) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 创建连接
	conn, err := rabbitmq.NewConn(m.connectionURL)
	if err != nil {
		return fmt.Errorf("创建RabbitMQ连接失败: %w", err)
	}

	// 创建消费者处理函数
	consumerHandler := func(d rabbitmq.Delivery) rabbitmq.Action {
		// 获取消息重试次数
		retryCount := m.getRetryCount(d.Headers)
		maxRetries := m.config.MaxRetries
		if maxRetries <= 0 {
			maxRetries = 10 // 默认最大重试10次
		}

		// 处理消息
		if err := handler.HandleMessage(ctx, d.Body); err != nil {
			m.logger.Error("处理消息失败",
				zap.Error(err),
				zap.String("queue", queueName),
				zap.String("message_id", d.MessageId),
				zap.Int("retry_count", retryCount))

			// 判断是否应该重试
			shouldRetry := isRetryableError(err) && retryCount < maxRetries

			if shouldRetry {
				// 可重试且未达到最大重试次数，重新发布消息
				m.logger.Info("消息将被重新发布用于重试",
					zap.String("message_id", d.MessageId),
					zap.Int("current_retry_count", retryCount),
					zap.Int("next_retry_count", retryCount+1),
					zap.Int("max_retries", maxRetries))

				// 延迟重试
				if m.config.RetryDelay > 0 {
					time.Sleep(m.config.RetryDelay)
				}

				// 重新发布消息并增加重试次数
				if err := m.republishWithRetryCount(d, retryCount+1); err != nil {
					m.logger.Error("重新发布消息失败",
						zap.Error(err),
						zap.String("message_id", d.MessageId))
					// 如果重新发布失败，发送到死信队列
					m.sendToDeadLetter(d, err)
				}

				// 确认原消息
				return rabbitmq.Ack
			} else {
				// 不可重试或已达最大重试次数，发送到死信队列
				var reason string
				if retryCount >= maxRetries {
					reason = "已达到最大重试次数"
				} else {
					reason = "消息处理失败且错误不可重试"
				}

				m.logger.Warn("消息将发送到死信队列",
					zap.String("message_id", d.MessageId),
					zap.Int("retry_count", retryCount),
					zap.Int("max_retries", maxRetries),
					zap.String("reason", reason),
					zap.Bool("is_retryable_error", isRetryableError(err)),
					zap.Error(err))

				m.sendToDeadLetter(d, err)
				// 确认消息以避免重复处理
				return rabbitmq.Ack
			}
		}

		m.logger.Debug("消息处理成功", zap.String("message_id", d.MessageId))
		return rabbitmq.Ack
	}

	// 创建消费者
	consumer, err := rabbitmq.NewConsumer(
		conn,
		queueName,
		rabbitmq.WithConsumerOptionsRoutingKey(m.config.RoutingKey),
		rabbitmq.WithConsumerOptionsExchangeName(m.config.ExchangeName),
		rabbitmq.WithConsumerOptionsExchangeDeclare,
		rabbitmq.WithConsumerOptionsExchangeDurable,
		rabbitmq.WithConsumerOptionsQueueDurable,
		rabbitmq.WithConsumerOptionsQOSPrefetch(m.config.PrefetchCount),
		rabbitmq.WithConsumerOptionsConcurrency(m.config.Concurrency),
		rabbitmq.WithConsumerOptionsConsumerName(fmt.Sprintf("consumer_%s_%d", queueName, time.Now().Unix())),
	)
	if err != nil {
		return fmt.Errorf("创建消费者失败: %w", err)
	}

	// 启动消费者 - 在goroutine中运行以避免阻塞
	go func() {
		err = consumer.Run(consumerHandler)
		if err != nil {
			m.logger.Error("消费者运行失败", zap.Error(err))
		}
	}()

	m.consumers = append(m.consumers, consumer)
	m.logger.Info("消费者启动成功", zap.String("queue", queueName))

	// 监听停止信号
	go func() {
		select {
		case <-ctx.Done():
			m.logger.Info("收到上下文取消信号，关闭消费者")
			consumer.Close()
		case <-m.stopChan:
			m.logger.Info("收到停止信号，关闭消费者")
			consumer.Close()
		}
	}()

	return nil
}

// IsConnected 检查连接状态
func (m *mqManagerImpl) IsConnected() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.isConnected && m.publisher != nil
}

// HealthCheck 健康检查
func (m *mqManagerImpl) HealthCheck() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if !m.isConnected || m.publisher == nil {
		return false
	}

	// 尝试发送一个轻量级的消息来测试连接
	testMessage := []byte("health_check")
	err := m.publisher.Publish(
		testMessage,
		[]string{"health_check"},
		rabbitmq.WithPublishOptionsContentType("text/plain"),
		rabbitmq.WithPublishOptionsExchange(""), // 使用默认交换机
	)

	return err == nil
}

// Close 关闭连接
func (m *mqManagerImpl) Close() error {
	m.stopOnce.Do(func() {
		m.mutex.Lock()
		defer m.mutex.Unlock()

		// 关闭停止通道
		select {
		case <-m.stopChan:
			// 通道已关闭
		default:
			close(m.stopChan)
		}

		// 关闭所有消费者
		for i, consumer := range m.consumers {
			if consumer != nil {
				consumer.Close()
				m.logger.Info("消费者已关闭", zap.Int("consumer_index", i))
			}
		}
		m.consumers = nil

		// 关闭发布者
		if m.publisher != nil {
			m.publisher.Close()
			m.logger.Info("RabbitMQ发布者已关闭")
			m.publisher = nil
		}

		m.isConnected = false
		m.logger.Info("RabbitMQ连接已关闭")
	})

	return nil
}

// isRetryableError 判断错误是否可重试
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// 将错误信息转为小写便于匹配
	errorMsg := strings.ToLower(err.Error())

	// 可重试的错误类型（网络、连接、临时性错误）
	retryableErrors := []string{
		// 网络相关错误
		"connection refused",
		"connection reset",
		"connection timeout",
		"network is unreachable",
		"no route to host",
		"broken pipe",
		"i/o timeout",
		"timeout",
		"dial tcp",

		// 服务器临时错误
		"server is not available",
		"service unavailable",
		"temporarily unavailable",
		"too many connections",
		"server overloaded",
		"resource temporarily unavailable",

		// 数据库临时错误
		"database is locked",
		"lock wait timeout",
		"deadlock found",
		"connection lost",
		"mysql server has gone away",
		"postgres connection",

		// 上下文和并发错误
		"context deadline exceeded",
		"context canceled",
		"operation was canceled",

		// HTTP相关临时错误
		"502 bad gateway",
		"503 service unavailable",
		"504 gateway timeout",
		"429 too many requests",

		// 其他临时性错误
		"temporary failure",
		"transient error",
		"retry later",
		"please retry",
	}

	// 检查是否包含可重试的错误信息
	for _, retryableError := range retryableErrors {
		if strings.Contains(errorMsg, retryableError) {
			return true
		}
	}

	// 不可重试的错误类型（业务逻辑错误、数据格式错误等）
	nonRetryableErrors := []string{
		// JSON和数据格式错误
		"json",
		"unmarshal",
		"parsing",
		"parse error",
		"invalid json",
		"malformed",

		// 验证和业务逻辑错误
		"validation",
		"invalid",
		"illegal",
		"forbidden",
		"unauthorized",
		"access denied",
		"permission denied",
		"not found",
		"does not exist",

		// 数据库约束错误
		"duplicate",
		"constraint",
		"foreign key",
		"unique constraint",
		"primary key",
		"check constraint",
		"not null constraint",

		// 序列化相关错误
		"反序列化消息失败",
		"序列化失败",
		"编码错误",
		"解码错误",

		// 配置和参数错误
		"missing required",
		"required field",
		"bad request",
		"invalid parameter",
		"configuration error",
	}

	// 检查是否包含不可重试的错误信息
	for _, nonRetryableError := range nonRetryableErrors {
		if strings.Contains(errorMsg, nonRetryableError) {
			return false
		}
	}

	// 对于未明确分类的错误，采用保守策略：不重试
	// 这样可以避免因为未知错误导致的无限重试
	return false
}

// GetPublisher 获取发布者（供其他地方使用）
func (m *mqManagerImpl) GetPublisher() *rabbitmq.Publisher {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.publisher
}

// GetConsumerCount 获取消费者数量
func (m *mqManagerImpl) GetConsumerCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.consumers)
}

// PublishWithRetry 带重试的发布消息
func (m *mqManagerImpl) PublishWithRetry(exchange, routingKey string, body []byte, contentType string, maxRetries int) error {
	var lastErr error

	for i := 0; i <= maxRetries; i++ {
		err := m.Publish(exchange, routingKey, body, contentType)
		if err == nil {
			return nil
		}

		lastErr = err
		if i < maxRetries {
			// 指数退避
			backoff := time.Duration(i+1) * time.Second
			m.logger.Warn("发布消息失败，重试中",
				zap.Error(err),
				zap.Int("retry_count", i+1),
				zap.Duration("backoff", backoff))
			time.Sleep(backoff)
		}
	}

	return fmt.Errorf("发布消息失败，已达到最大重试次数 %d: %w", maxRetries, lastErr)
}

// PublishJSONWithRetry 带重试的发布JSON消息
func (m *mqManagerImpl) PublishJSONWithRetry(exchange, routingKey string, data interface{}, maxRetries int) error {
	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化JSON消息失败: %w", err)
	}

	return m.PublishWithRetry(exchange, routingKey, body, "application/json", maxRetries)
}

// PublishWithConfirm 带确认机制的发布消息
func (m *mqManagerImpl) PublishWithConfirm(ctx context.Context, exchange, routingKey string, body []byte, contentType string) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if m.publisher == nil {
		return fmt.Errorf("RabbitMQ发布者未初始化")
	}

	// 使用带确认的发布方法
	confirms, err := m.publisher.PublishWithDeferredConfirmWithContext(
		ctx,
		body,
		[]string{routingKey},
		rabbitmq.WithPublishOptionsContentType(contentType),
		rabbitmq.WithPublishOptionsExchange(exchange),
		rabbitmq.WithPublishOptionsMandatory,
		rabbitmq.WithPublishOptionsPersistentDelivery,
	)
	if err != nil {
		return fmt.Errorf("发布消息失败: %w", err)
	}

	// 等待确认
	if len(confirms) > 0 && confirms[0] != nil {
		confirmed, err := confirms[0].WaitContext(ctx)
		if err != nil {
			return fmt.Errorf("等待消息确认失败: %w", err)
		}
		if !confirmed {
			return fmt.Errorf("消息发布未得到确认")
		}
	}

	m.logger.Info("消息发布成功并已确认",
		zap.String("exchange", exchange),
		zap.String("routing_key", routingKey),
		zap.String("content_type", contentType),
		zap.Int("body_size", len(body)))

	return nil
}

// PublishJSONWithConfirm 带确认机制的发布JSON消息
func (m *mqManagerImpl) PublishJSONWithConfirm(ctx context.Context, exchange, routingKey string, data interface{}) error {
	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化JSON消息失败: %w", err)
	}

	return m.PublishWithConfirm(ctx, exchange, routingKey, body, "application/json")
}

// PublishWithConfirmAndRetry 带确认机制和重试的发布消息
func (m *mqManagerImpl) PublishWithConfirmAndRetry(ctx context.Context, exchange, routingKey string, body []byte, contentType string, maxRetries int) error {
	var lastErr error

	for i := 0; i <= maxRetries; i++ {
		err := m.PublishWithConfirm(ctx, exchange, routingKey, body, contentType)
		if err == nil {
			return nil
		}

		lastErr = err
		if i < maxRetries {
			// 指数退避
			backoff := time.Duration(i+1) * time.Second
			m.logger.Warn("发布消息失败，重试中",
				zap.Error(err),
				zap.Int("retry_count", i+1),
				zap.Duration("backoff", backoff))

			// 使用context的超时机制
			select {
			case <-ctx.Done():
				return fmt.Errorf("发布消息被取消: %w", ctx.Err())
			case <-time.After(backoff):
				// 继续重试
			}
		}
	}

	return fmt.Errorf("发布消息失败，已达到最大重试次数 %d: %w", maxRetries, lastErr)
}

// PublishJSONWithConfirmAndRetry 带确认机制和重试的发布JSON消息
func (m *mqManagerImpl) PublishJSONWithConfirmAndRetry(ctx context.Context, exchange, routingKey string, data interface{}, maxRetries int) error {
	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化JSON消息失败: %w", err)
	}

	return m.PublishWithConfirmAndRetry(ctx, exchange, routingKey, body, "application/json", maxRetries)
}

// getRetryCount 从消息头中获取重试次数
func (m *mqManagerImpl) getRetryCount(headers amqp091.Table) int {
	if headers == nil {
		return 0
	}

	if retryCountValue, exists := headers["x-retry-count"]; exists {
		switch v := retryCountValue.(type) {
		case int:
			return v
		case int32:
			return int(v)
		case int64:
			return int(v)
		case string:
			if count, err := strconv.Atoi(v); err == nil {
				return count
			}
		}
	}

	return 0
}

// sendToDeadLetter 将消息发送到死信队列
func (m *mqManagerImpl) sendToDeadLetter(delivery rabbitmq.Delivery, processError error) {
	// 如果没有配置死信队列，则跳过
	if m.config.DeadLetterExchange == "" {
		m.logger.Warn("未配置死信交换机，消息将被丢弃",
			zap.String("message_id", delivery.MessageId),
			zap.Error(processError))
		return
	}

	// 构造死信消息头
	deadLetterHeaders := make(amqp091.Table)
	if delivery.Headers != nil {
		// 复制原始消息头
		for k, v := range delivery.Headers {
			deadLetterHeaders[k] = v
		}
	}

	// 添加死信相关信息
	deadLetterHeaders["x-death-reason"] = processError.Error()
	deadLetterHeaders["x-death-timestamp"] = time.Now().Unix()
	deadLetterHeaders["x-original-exchange"] = delivery.Exchange
	deadLetterHeaders["x-original-routing-key"] = delivery.RoutingKey
	deadLetterHeaders["x-original-queue"] = delivery.ConsumerTag

	// 获取死信路由键，如果没有配置则使用原路由键
	deadLetterRoutingKey := m.config.DeadLetterRoutingKey
	if deadLetterRoutingKey == "" {
		deadLetterRoutingKey = delivery.RoutingKey
	}

	// 转换headers类型
	convertedHeaders := make(rabbitmq.Table)
	for k, v := range deadLetterHeaders {
		convertedHeaders[k] = v
	}

	// 发布到死信交换机
	if m.publisher != nil {
		err := m.publisher.Publish(
			delivery.Body,
			[]string{deadLetterRoutingKey},
			rabbitmq.WithPublishOptionsContentType(delivery.ContentType),
			rabbitmq.WithPublishOptionsExchange(m.config.DeadLetterExchange),
			rabbitmq.WithPublishOptionsHeaders(convertedHeaders),
			rabbitmq.WithPublishOptionsMandatory,
			rabbitmq.WithPublishOptionsPersistentDelivery,
		)

		if err != nil {
			m.logger.Error("发送消息到死信队列失败",
				zap.Error(err),
				zap.String("message_id", delivery.MessageId),
				zap.String("dead_letter_exchange", m.config.DeadLetterExchange),
				zap.String("dead_letter_routing_key", deadLetterRoutingKey))
		} else {
			m.logger.Info("消息已发送到死信队列",
				zap.String("message_id", delivery.MessageId),
				zap.String("dead_letter_exchange", m.config.DeadLetterExchange),
				zap.String("dead_letter_routing_key", deadLetterRoutingKey),
				zap.String("death_reason", processError.Error()))
		}
	} else {
		m.logger.Error("发布者未初始化，无法发送消息到死信队列",
			zap.String("message_id", delivery.MessageId))
	}
}

// setupDeadLetterQueue 设置死信队列和交换机
func (m *mqManagerImpl) setupDeadLetterQueue() error {
	// 如果没有配置死信交换机，则跳过设置
	if m.config.DeadLetterExchange == "" {
		m.logger.Info("未配置死信交换机，跳过死信队列设置")
		return nil
	}

	// 创建AMQP连接来设置死信队列
	conn, err := amqp091.Dial(m.connectionURL)
	if err != nil {
		return fmt.Errorf("创建RabbitMQ连接失败: %w", err)
	}
	defer conn.Close()

	// 获取通道
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("创建RabbitMQ通道失败: %w", err)
	}
	defer ch.Close()

	// 声明死信交换机
	err = ch.ExchangeDeclare(
		m.config.DeadLetterExchange, // name
		"direct",                    // type
		true,                        // durable
		false,                       // auto-deleted
		false,                       // internal
		false,                       // no-wait
		nil,                         // arguments
	)
	if err != nil {
		return fmt.Errorf("声明死信交换机失败: %w", err)
	}

	// 如果配置了死信队列，则声明队列并绑定
	if m.config.DeadLetterQueue != "" {
		_, err = ch.QueueDeclare(
			m.config.DeadLetterQueue, // name
			true,                     // durable
			false,                    // delete when unused
			false,                    // exclusive
			false,                    // no-wait
			nil,                      // arguments
		)
		if err != nil {
			return fmt.Errorf("声明死信队列失败: %w", err)
		}

		// 绑定死信队列到死信交换机
		deadLetterRoutingKey := m.config.DeadLetterRoutingKey
		if deadLetterRoutingKey == "" {
			deadLetterRoutingKey = m.config.RoutingKey
		}

		err = ch.QueueBind(
			m.config.DeadLetterQueue,    // queue name
			deadLetterRoutingKey,        // routing key
			m.config.DeadLetterExchange, // exchange
			false,
			nil,
		)
		if err != nil {
			return fmt.Errorf("绑定死信队列失败: %w", err)
		}

		m.logger.Info("死信队列设置完成",
			zap.String("dead_letter_exchange", m.config.DeadLetterExchange),
			zap.String("dead_letter_queue", m.config.DeadLetterQueue),
			zap.String("dead_letter_routing_key", deadLetterRoutingKey))
	} else {
		m.logger.Info("死信交换机设置完成",
			zap.String("dead_letter_exchange", m.config.DeadLetterExchange))
	}

	return nil
}

// republishWithRetryCount 重新发布消息并增加重试次数
func (m *mqManagerImpl) republishWithRetryCount(delivery rabbitmq.Delivery, newRetryCount int) error {
	if m.publisher == nil {
		return fmt.Errorf("发布者未初始化")
	}

	// 构造新的消息头
	newHeaders := make(rabbitmq.Table)
	if delivery.Headers != nil {
		// 复制原始消息头
		for k, v := range delivery.Headers {
			newHeaders[k] = v
		}
	}

	// 更新重试次数
	newHeaders["x-retry-count"] = newRetryCount
	newHeaders["x-retry-timestamp"] = time.Now().Unix()

	// 确定交换机和路由键
	exchange := delivery.Exchange
	if exchange == "" {
		exchange = m.config.ExchangeName
	}

	routingKey := delivery.RoutingKey
	if routingKey == "" {
		routingKey = m.config.RoutingKey
	}

	// 重新发布消息
	err := m.publisher.Publish(
		delivery.Body,
		[]string{routingKey},
		rabbitmq.WithPublishOptionsContentType(delivery.ContentType),
		rabbitmq.WithPublishOptionsExchange(exchange),
		rabbitmq.WithPublishOptionsHeaders(newHeaders),
		rabbitmq.WithPublishOptionsMandatory,
		rabbitmq.WithPublishOptionsPersistentDelivery,
	)

	if err != nil {
		return fmt.Errorf("重新发布消息失败: %w", err)
	}

	m.logger.Debug("消息重新发布成功",
		zap.String("message_id", delivery.MessageId),
		zap.String("exchange", exchange),
		zap.String("routing_key", routingKey),
		zap.Int("new_retry_count", newRetryCount))

	return nil
}

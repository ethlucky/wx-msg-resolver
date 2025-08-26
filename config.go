package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Config 配置结构体
type Config struct {
	App        AppConfig        `mapstructure:"app"`
	Server     ServerConfig     `mapstructure:"server"`
	Log        LogConfig        `mapstructure:"log"`
	Database   DatabaseConfig   `mapstructure:"database"`
	Redis      RedisConfig      `mapstructure:"redis"`
	RabbitMQ   RabbitMQConfig   `mapstructure:"rabbitmq"`
	Upload     UploadConfig     `mapstructure:"upload"`
	Message    MessageConfig    `mapstructure:"message"`
	OCR        OCRConfig        `mapstructure:"ocr"`
	MinIO      MinIOConfig      `mapstructure:"minio"`
	WebSocket  WebSocketConfig  `mapstructure:"websocket"`
	Webhook    WebhookConfig    `mapstructure:"webhook"`
	APIGateway APIGatewayConfig `mapstructure:"apigateway"`
}

type AppConfig struct {
	Name    string `mapstructure:"name"`
	Version string `mapstructure:"version"`
	Env     string `mapstructure:"env"`
	Port    string `mapstructure:"port"`
	Debug   bool   `mapstructure:"debug"`
	OwnerID int    `mapstructure:"owner_id"` // 所有者ID
}

type ServerConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	IdleTimeout  time.Duration `mapstructure:"idle_timeout"`
}

type LogConfig struct {
	Level      string `mapstructure:"level"`
	Format     string `mapstructure:"format"`
	Output     string `mapstructure:"output"`
	FilePath   string `mapstructure:"file_path"`
	MaxSize    int    `mapstructure:"max_size"`
	MaxAge     int    `mapstructure:"max_age"`
	MaxBackups int    `mapstructure:"max_backups"`
	Compress   bool   `mapstructure:"compress"`
}

type DatabaseConfig struct {
	Host                     string        `mapstructure:"host"`
	Port                     int           `mapstructure:"port"`
	Username                 string        `mapstructure:"username"`
	Password                 string        `mapstructure:"password"`
	Database                 string        `mapstructure:"database"`
	Charset                  string        `mapstructure:"charset"`
	ParseTime                bool          `mapstructure:"parse_time"`
	Loc                      string        `mapstructure:"loc"`
	MaxIdleConns             int           `mapstructure:"max_idle_conns"`
	MaxOpenConns             int           `mapstructure:"max_open_conns"`
	ConnMaxLifetime          time.Duration `mapstructure:"conn_max_lifetime"`
	LogLevel                 string        `mapstructure:"log_level"`
	AllowMultiQueries        bool          `mapstructure:"allow_multi_queries"`
	UseCursorFetch           bool          `mapstructure:"use_cursor_fetch"`
	RewriteBatchedStatements bool          `mapstructure:"rewrite_batched_statements"`
}

type RedisConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	Password     string        `mapstructure:"password"`
	Database     int           `mapstructure:"database"`
	PoolSize     int           `mapstructure:"pool_size"`
	MinIdleConns int           `mapstructure:"min_idle_conns"`
	MaxRetries   int           `mapstructure:"max_retries"`
	DialTimeout  time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	PoolTimeout  time.Duration `mapstructure:"pool_timeout"`
	IdleTimeout  time.Duration `mapstructure:"idle_timeout"`
}

type RabbitMQConfig struct {
	Host              string        `mapstructure:"host"`
	Port              int           `mapstructure:"port"`
	Username          string        `mapstructure:"username"`
	Password          string        `mapstructure:"password"`
	Vhost             string        `mapstructure:"vhost"`
	Heartbeat         time.Duration `mapstructure:"heartbeat"`
	ConnectionTimeout time.Duration `mapstructure:"connection_timeout"`
	ExchangeName      string        `mapstructure:"exchange_name"`
	QueueName         string        `mapstructure:"queue_name"`
	RoutingKey        string        `mapstructure:"routing_key"`
	Durable           bool          `mapstructure:"durable"`
	AutoDelete        bool          `mapstructure:"auto_delete"`
	// 新增的配置选项
	MaxRetries    int           `mapstructure:"max_retries"`
	RetryDelay    time.Duration `mapstructure:"retry_delay"`
	PrefetchCount int           `mapstructure:"prefetch_count"`
	Concurrency   int           `mapstructure:"concurrency"`
	// 死信队列配置
	DeadLetterExchange   string `mapstructure:"dead_letter_exchange"`
	DeadLetterQueue      string `mapstructure:"dead_letter_queue"`
	DeadLetterRoutingKey string `mapstructure:"dead_letter_routing_key"`
}

type UploadConfig struct {
	MaxSize      string   `mapstructure:"max_size"`
	AllowedTypes []string `mapstructure:"allowed_types"`
	UploadPath   string   `mapstructure:"upload_path"`
}

type MessageConfig struct {
	MaxRetryCount int           `mapstructure:"max_retry_count"`
	RetryInterval time.Duration `mapstructure:"retry_interval"`
	BatchSize     int           `mapstructure:"batch_size"`
	WorkerCount   int           `mapstructure:"worker_count"`
	QueueSize     int           `mapstructure:"queue_size"`
}

type OCRConfig struct {
	BaseURL           string        `mapstructure:"base_url"`
	Timeout           time.Duration `mapstructure:"timeout"`
	Enabled           bool          `mapstructure:"enabled"`
	ConcurrentWorkers int           `mapstructure:"concurrent_workers"` // OCR并发处理器数量
}

type WebSocketConfig struct {
	URL                   string        // 动态从数据库获取，不从配置文件读取
	Enabled               bool          `mapstructure:"enabled"`
	PingInterval          time.Duration `mapstructure:"ping_interval"`
	ReconnectInterval     time.Duration `mapstructure:"reconnect_interval"`
	MaxReconnectAttempts  int           `mapstructure:"max_reconnect_attempts"`
	FilterMessageTypes    bool          `mapstructure:"filter_message_types"`
	SupportedMessageTypes []int         `mapstructure:"supported_message_types"`
}

type MinIOConfig struct {
	Endpoint        string        `mapstructure:"endpoint"`
	AccessKeyID     string        `mapstructure:"access_key_id"`
	SecretAccessKey string        `mapstructure:"secret_access_key"`
	BucketName      string        `mapstructure:"bucket_name"`
	UseSSL          bool          `mapstructure:"use_ssl"`
	Region          string        `mapstructure:"region"`
	Timeout         time.Duration `mapstructure:"timeout"` // 请求超时时间
}

type WebhookConfig struct {
	Enabled           bool          `mapstructure:"enabled"`            // 是否启用
	URLs              []string      `mapstructure:"urls"`               // 推送URL列表
	Timeout           time.Duration `mapstructure:"timeout"`            // 请求超时时间
	MaxRetries        int           `mapstructure:"max_retries"`        // 最大重试次数
	RetryInterval     time.Duration `mapstructure:"retry_interval"`     // 重试间隔
	ConcurrentPushers int           `mapstructure:"concurrent_pushers"` // 并发推送协程数
	BatchSize         int           `mapstructure:"batch_size"`         // 批量推送大小
	CallbackURL       string        `mapstructure:"callback_url"`       // 回调URL
}

type APIGatewayConfig struct {
	WxMsgAPIURL string `mapstructure:"wx_msg_api_url"` // 微信消息API地址
}

// 全局WebSocket配置服务实例
var wsConfigService WebSocketConfigService


// initConfig 初始化配置
func initConfig() error {
	// 获取环境变量
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "dev"
	}

	// 根据环境变量确定配置文件名
	var configName string
	switch env {
	case "prod", "production":
		configName = "config-prod"
	case "test", "testing":
		configName = "config-test"
	case "staging":
		configName = "config-staging"
	case "dev", "development":
		configName = "config-dev"
	default:
		configName = "config"
	}

	viper.SetConfigName(configName)
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")
	viper.AddConfigPath("./configs")

	// 环境变量支持
	viper.AutomaticEnv()

	// 尝试读取配置文件，如果特定环境的配置文件不存在，则回退到默认配置
	if err := viper.ReadInConfig(); err != nil {
		// 如果特定环境配置文件不存在，尝试使用默认配置文件
		if configName != "config" {
			viper.SetConfigName("config")
			if err := viper.ReadInConfig(); err != nil {
				return fmt.Errorf("读取配置文件失败 (尝试了 %s 和 config): %w", configName, err)
			}
		} else {
			return fmt.Errorf("读取配置文件失败: %w", err)
		}
	}

	cfg = &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 根据环境变量覆盖配置
	applyEnvironmentConfig(env)

	// 注意：WebSocket配置服务在数据库初始化后创建

	cfg.App.Env = env
	return nil
}

// applyEnvironmentConfig 根据环境应用特定配置
func applyEnvironmentConfig(env string) {
	switch env {
	case "prod", "production":
		cfg.App.Debug = false
		if cfg.Log.Level == "" || cfg.Log.Level == "debug" {
			cfg.Log.Level = "info"
		}
		// 生产环境特定配置
		if cfg.Log.Output == "" {
			cfg.Log.Output = "file"
		}
	case "test", "testing":
		cfg.App.Debug = false
		if cfg.Log.Level == "" {
			cfg.Log.Level = "warn"
		}
		// 测试环境特定配置
		if cfg.Log.Output == "" {
			cfg.Log.Output = "stdout"
		}
	case "staging":
		cfg.App.Debug = false
		if cfg.Log.Level == "" {
			cfg.Log.Level = "info"
		}
		// 预发布环境特定配置
		if cfg.Log.Output == "" {
			cfg.Log.Output = "both"
		}
	case "dev", "development":
		cfg.App.Debug = true
		if cfg.Log.Level == "" {
			cfg.Log.Level = "debug"
		}
		// 开发环境特定配置
		if cfg.Log.Output == "" {
			cfg.Log.Output = "stdout"
		}
	default:
		// 默认为开发环境配置
		cfg.App.Debug = true
		if cfg.Log.Level == "" {
			cfg.Log.Level = "debug"
		}
		if cfg.Log.Output == "" {
			cfg.Log.Output = "stdout"
		}
	}
}

// initLogger 初始化日志
func initLogger() error {
	var level zapcore.Level
	switch cfg.Log.Level {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	default:
		level = zapcore.InfoLevel
	}

	// 创建日志目录
	if err := os.MkdirAll("logs", 0755); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}

	// 编码器配置
	encoderConfig := zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		TimeKey:        "time",
		NameKey:        "logger",
		CallerKey:      "caller",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// 创建编码器
	var encoder zapcore.Encoder
	if cfg.Log.Format == "json" {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	var cores []zapcore.Core

	// 控制台输出
	if cfg.Log.Output == "stdout" || cfg.Log.Output == "both" {
		consoleCore := zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level)
		cores = append(cores, consoleCore)
	}

	// 文件输出（带轮转）
	if cfg.Log.Output == "file" || cfg.Log.Output == "both" {
		// 配置日志轮转
		logRotate := &lumberjack.Logger{
			Filename:   cfg.Log.FilePath,
			MaxSize:    cfg.Log.MaxSize,    // MB
			MaxAge:     cfg.Log.MaxAge,     // 天
			MaxBackups: cfg.Log.MaxBackups, // 保留文件数
			Compress:   cfg.Log.Compress,   // 压缩
			LocalTime:  true,               // 使用本地时间
		}

		fileCore := zapcore.NewCore(encoder, zapcore.AddSync(logRotate), level)
		cores = append(cores, fileCore)
	}

	// 创建核心
	core := zapcore.NewTee(cores...)

	// 创建日志器
	logger = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	logger.Info("日志系统初始化完成",
		zap.String("level", cfg.Log.Level),
		zap.String("format", cfg.Log.Format),
		zap.String("output", cfg.Log.Output),
		zap.String("file_path", cfg.Log.FilePath),
		zap.Int("max_age_days", cfg.Log.MaxAge),
		zap.Int("max_size_mb", cfg.Log.MaxSize),
		zap.Int("max_backups", cfg.Log.MaxBackups),
		zap.Bool("compress", cfg.Log.Compress))

	return nil
}

// initDatabase 初始化数据库
func initDatabase() error {
	// 构建简化的DSN - 先用最基本的参数测试
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&loc=Local",
		cfg.Database.Username,
		cfg.Database.Password,
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.Database,
	)

	logger.Info("正在连接数据库", zap.String("dsn", dsn))
	// GORM日志级别
	var logLevel gormlogger.LogLevel
	switch cfg.Database.LogLevel {
	case "silent":
		logLevel = gormlogger.Silent
	case "error":
		logLevel = gormlogger.Error
	case "warn":
		logLevel = gormlogger.Warn
	case "info":
		logLevel = gormlogger.Info
	default:
		logLevel = gormlogger.Info
	}

	var err error
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(logLevel),
	})
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("获取数据库实例失败: %w", err)
	}

	// 设置连接池参数
	sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	// 测试连接
	if err := sqlDB.Ping(); err != nil {
		logger.Error("数据库连接测试失败", zap.Error(err))
		return fmt.Errorf("数据库连接测试失败: %w", err)
	}

	// 初始化WebSocket配置服务（在数据库初始化后）
	wsConfigService = NewWebSocketConfigService(db, logger)

	logger.Info("数据库连接成功",
		zap.String("host", cfg.Database.Host),
		zap.Int("port", cfg.Database.Port),
		zap.String("database", cfg.Database.Database),
		zap.String("charset", cfg.Database.Charset))
	return nil
}

// initRedis 初始化Redis
func initRedis() error {
	rdb = redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.Database,
		PoolSize:     cfg.Redis.PoolSize,
		MinIdleConns: cfg.Redis.MinIdleConns,
		MaxRetries:   cfg.Redis.MaxRetries,
		DialTimeout:  cfg.Redis.DialTimeout,
		ReadTimeout:  cfg.Redis.ReadTimeout,
		WriteTimeout: cfg.Redis.WriteTimeout,
		PoolTimeout:  cfg.Redis.PoolTimeout,
		IdleTimeout:  cfg.Redis.IdleTimeout,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis连接测试失败: %w", err)
	}

	// 初始化RedisUtil
	redisUtil = NewRedisUtil(rdb, logger)

	logger.Info("Redis连接成功")
	return nil
}

// initMQManager 初始化MQ管理器
func initMQManager() error {
	// 修改队列名称包含ownerId
	ownerIDStr := strconv.Itoa(cfg.App.OwnerID)
	cfg.RabbitMQ.QueueName = cfg.RabbitMQ.QueueName + "_" + ownerIDStr
	cfg.RabbitMQ.ExchangeName = cfg.RabbitMQ.ExchangeName + "_" + ownerIDStr
	cfg.RabbitMQ.DeadLetterExchange = cfg.RabbitMQ.DeadLetterExchange + "_" + ownerIDStr
	cfg.RabbitMQ.DeadLetterQueue = cfg.RabbitMQ.DeadLetterQueue + "_" + ownerIDStr

	// 创建MQ管理器
	mqManager = NewMQManager(&cfg.RabbitMQ, logger)

	// 初始化连接
	if err := mqManager.Init(); err != nil {
		return fmt.Errorf("初始化MQ管理器失败: %w", err)
	}

	logger.Info("MQ管理器初始化完成",
		zap.String("queue_name", cfg.RabbitMQ.QueueName),
		zap.String("exchange_name", cfg.RabbitMQ.ExchangeName))
	return nil
}

// initMessageForwarder 初始化消息转发器
func initMessageForwarder() error {
	if db == nil {
		return fmt.Errorf("数据库未初始化，无法创建消息转发器")
	}

	// 使用配置服务获取WebSocket配置
	ctx := context.Background()
	config, err := wsConfigService.GetConfig(ctx, int64(cfg.App.OwnerID))
	if err != nil {
		return fmt.Errorf("获取WebSocket配置失败: %w", err)
	}

	// 更新配置中的WebSocket URL
	cfg.WebSocket.URL = config.URL
	logger.Info("已加载WebSocket URL到消息转发器配置", zap.String("url", config.URL))

	// 创建消息转发器
	messageForwarder, err = NewMessageForwarder(cfg, db, logger, mqManager, wechatBot, ocrService, minioClient, redisUtil)
	if err != nil {
		return fmt.Errorf("创建消息转发器失败: %w", err)
	}

	// 获取webhookQueueProcessor的引用（需要添加getter方法）
	if mf, ok := messageForwarder.(*messageForwarderImpl); ok {
		webhookQueueProcessor = mf.webhookQueueProcessor
	}

	// 启动消息消费者
	if err := messageForwarder.StartConsumer(ctx); err != nil {
		return fmt.Errorf("启动消息消费者失败: %w", err)
	}

	logger.Info("消息转发器初始化成功")
	return nil
}

// initOCRService 初始化OCR服务
func initOCRService() error {
	if !cfg.OCR.Enabled {
		logger.Info("OCR服务已禁用")
		return nil
	}

	ocrService = NewOCRService(cfg.OCR.BaseURL, cfg.OCR.Timeout, logger)
	logger.Info("OCR服务初始化成功",
		zap.String("base_url", cfg.OCR.BaseURL),
		zap.Duration("timeout", cfg.OCR.Timeout))
	return nil
}

// initMinIOClient 初始化MinIO客户端
func initMinIOClient() error {

	var err error
	minioClient, err = NewMinIOClient(cfg.MinIO, logger)
	if err != nil {
		return fmt.Errorf("创建MinIO客户端失败: %w", err)
	}

	logger.Info("MinIO客户端初始化成功",
		zap.String("endpoint", cfg.MinIO.Endpoint),
		zap.String("bucket_name", cfg.MinIO.BucketName),
		zap.Bool("use_ssl", cfg.MinIO.UseSSL))
	return nil
}

// initWebSocketClient 初始化WebSocket客户端
func initWebSocketClient() error {
	if !cfg.WebSocket.Enabled {
		logger.Info("WebSocket客户端已禁用")
		return nil
	}

	// 使用配置服务获取WebSocket配置
	ctx := context.Background()
	config, err := wsConfigService.GetConfig(ctx, int64(cfg.App.OwnerID))
	if err != nil {
		return fmt.Errorf("获取WebSocket配置失败: %w", err)
	}

	// 记录当前WebSocket连接配置
	logCurrentWebSocketConfig(int64(cfg.App.OwnerID), config.URL)

	// 创建包含动态URL的WebSocket配置
	wsConfig := WebSocketConfig{
		URL:                   config.URL,
		Enabled:               cfg.WebSocket.Enabled,
		PingInterval:          cfg.WebSocket.PingInterval,
		ReconnectInterval:     cfg.WebSocket.ReconnectInterval,
		MaxReconnectAttempts:  cfg.WebSocket.MaxReconnectAttempts,
		FilterMessageTypes:    cfg.WebSocket.FilterMessageTypes,
		SupportedMessageTypes: cfg.WebSocket.SupportedMessageTypes,
	}

	wsClient = NewWebSocketClient(wsConfig, &cfg.App, logger, messageForwarder, redisUtil, db)

	// 订阅配置变更通知
	wsConfigService.Subscribe(wsClient.(ConfigObserver))

	// 启动WebSocket客户端
	if err := wsClient.Start(ctx); err != nil {
		return fmt.Errorf("启动WebSocket客户端失败: %w", err)
	}

	logger.Info("WebSocket客户端初始化成功",
		zap.String("url", config.URL),
		zap.Duration("ping_interval", cfg.WebSocket.PingInterval),
		zap.Duration("reconnect_interval", cfg.WebSocket.ReconnectInterval),
		zap.Int("max_reconnect_attempts", cfg.WebSocket.MaxReconnectAttempts))

	return nil
}

// initWechatBot 初始化微信机器人客户端
func initWechatBot() error {
	if !cfg.WebSocket.Enabled {
		logger.Info("WebSocket客户端已禁用，跳过微信机器人客户端初始化")
		return nil
	}

	// 使用配置服务获取WebSocket配置
	ctx := context.Background()
	config, err := wsConfigService.GetConfig(ctx, int64(cfg.App.OwnerID))
	if err != nil {
		return fmt.Errorf("获取WebSocket配置失败: %w", err)
	}

	// 创建微信机器人客户端
	wechatBot = NewWechatBot(config.BaseURL, config.Token, logger)

	// 订阅配置变更通知
	wsConfigService.Subscribe(wechatBot.(ConfigObserver))

	logger.Info("微信机器人客户端初始化成功",
		zap.String("base_url", config.BaseURL),
		zap.String("token", config.Token))
	return nil
}

// initOcrWalletService 初始化OCR钱包服务
func initOcrWalletService() error {
	if db == nil {
		return fmt.Errorf("数据库未初始化，无法创建OCR钱包服务")
	}

	if wxMsgAPIService == nil {
		return fmt.Errorf("微信消息API服务未初始化，无法创建OCR钱包服务")
	}

	if minioClient == nil {
		return fmt.Errorf("MinIO客户端未初始化，无法创建OCR钱包服务")
	}

	// 创建OCR钱包服务
	ocrWalletService = NewOcrWalletService(logger, minioClient, wxMsgAPIService)

	logger.Info("OCR钱包服务初始化成功")
	return nil
}

// initWxMsgAPIService 初始化微信消息API服务
func initWxMsgAPIService() error {
	if cfg.APIGateway.WxMsgAPIURL == "" {
		return fmt.Errorf("微信消息API URL未配置")
	}

	// 创建微信消息API服务
	wxMsgAPIService = NewWxMsgAPIService(cfg.APIGateway.WxMsgAPIURL, logger)

	logger.Info("微信消息API服务初始化成功",
		zap.String("wx_msg_api_url", cfg.APIGateway.WxMsgAPIURL))
	return nil
}

// logCurrentWebSocketConfig 记录当前WebSocket连接配置
func logCurrentWebSocketConfig(ownerID int64, wsURL string) {
	logger.Info("=== 当前WebSocket连接配置 ===",
		zap.Int64("owner_id", ownerID),
		zap.String("websocket_url", wsURL),
		zap.String("status", "启动时获取"))
}

package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// WebSocketConfigData WebSocket连接配置数据
type WebSocketConfigData struct {
	OwnerID int64
	URL     string
	BaseURL string
	Token   string
}

// ConfigObserver 配置观察者接口
type ConfigObserver interface {
	OnConfigChanged(config *WebSocketConfigData) error
}

// WebSocketConfigService WebSocket配置服务接口
type WebSocketConfigService interface {
	// 获取配置
	GetConfig(ctx context.Context, ownerID int64) (*WebSocketConfigData, error)
	// 重新加载配置
	ReloadConfig(ctx context.Context, ownerID int64) (*WebSocketConfigData, error)
	// 订阅配置变更
	Subscribe(observer ConfigObserver)
	// 取消订阅
	Unsubscribe(observer ConfigObserver)
	// 清理资源
	Close() error
}

// webSocketConfigServiceImpl WebSocket配置服务实现
type webSocketConfigServiceImpl struct {
	db          *gorm.DB
	logger      *zap.Logger
	cache       map[int64]*WebSocketConfigData
	cacheMu     sync.RWMutex
	observers   []ConfigObserver
	observerMu  sync.RWMutex
	cacheExpiry time.Duration
}

// NewWebSocketConfigService 创建WebSocket配置服务
func NewWebSocketConfigService(db *gorm.DB, logger *zap.Logger) WebSocketConfigService {
	return &webSocketConfigServiceImpl{
		db:          db,
		logger:      logger,
		cache:       make(map[int64]*WebSocketConfigData),
		observers:   make([]ConfigObserver, 0),
		cacheExpiry: 5 * time.Minute,
	}
}

// GetConfig 获取WebSocket配置（优先从缓存获取）
func (s *webSocketConfigServiceImpl) GetConfig(ctx context.Context, ownerID int64) (*WebSocketConfigData, error) {
	s.cacheMu.RLock()
	if cached, exists := s.cache[ownerID]; exists {
		s.cacheMu.RUnlock()
		return cached, nil
	}
	s.cacheMu.RUnlock()

	// 缓存中没有，从数据库加载
	config, err := s.loadConfigFromDB(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	// 加载成功后更新缓存
	s.cacheMu.Lock()
	s.cache[ownerID] = config
	s.cacheMu.Unlock()

	return config, nil
}

// ReloadConfig 强制从数据库重新加载配置
func (s *webSocketConfigServiceImpl) ReloadConfig(ctx context.Context, ownerID int64) (*WebSocketConfigData, error) {
	newConfig, err := s.loadConfigFromDB(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	// 检查配置是否发生变化
	s.cacheMu.RLock()
	oldConfig, exists := s.cache[ownerID]
	configChanged := !exists || oldConfig.URL != newConfig.URL || oldConfig.Token != newConfig.Token
	s.cacheMu.RUnlock()

	// 更新缓存
	s.cacheMu.Lock()
	s.cache[ownerID] = newConfig
	s.cacheMu.Unlock()

	// 如果配置发生变化，通知观察者
	if configChanged {
		s.logger.Info("WebSocket配置发生变化，通知观察者",
			zap.Int64("owner_id", ownerID),
			zap.String("new_url", newConfig.URL),
			zap.String("new_token", newConfig.Token))
		s.notifyObservers(newConfig)
	}

	return newConfig, nil
}

// Subscribe 订阅配置变更通知
func (s *webSocketConfigServiceImpl) Subscribe(observer ConfigObserver) {
	s.observerMu.Lock()
	defer s.observerMu.Unlock()
	s.observers = append(s.observers, observer)
	s.logger.Debug("新观察者已订阅配置变更通知", zap.Int("total_observers", len(s.observers)))
}

// Unsubscribe 取消订阅配置变更通知
func (s *webSocketConfigServiceImpl) Unsubscribe(observer ConfigObserver) {
	s.observerMu.Lock()
	defer s.observerMu.Unlock()

	for i, obs := range s.observers {
		if obs == observer {
			s.observers = append(s.observers[:i], s.observers[i+1:]...)
			s.logger.Debug("观察者已取消订阅配置变更通知", zap.Int("total_observers", len(s.observers)))
			break
		}
	}
}

// Close 清理资源
func (s *webSocketConfigServiceImpl) Close() error {
	s.cacheMu.Lock()
	s.cache = make(map[int64]*WebSocketConfigData)
	s.cacheMu.Unlock()

	s.observerMu.Lock()
	s.observers = make([]ConfigObserver, 0)
	s.observerMu.Unlock()

	s.logger.Info("WebSocket配置服务已关闭")
	return nil
}

// notifyObservers 通知所有观察者配置发生变化
func (s *webSocketConfigServiceImpl) notifyObservers(config *WebSocketConfigData) {
	s.observerMu.RLock()
	observers := make([]ConfigObserver, len(s.observers))
	copy(observers, s.observers)
	s.observerMu.RUnlock()

	for _, observer := range observers {
		go func(obs ConfigObserver) {
			if err := obs.OnConfigChanged(config); err != nil {
				s.logger.Error("通知观察者配置变更失败",
					zap.Int64("owner_id", config.OwnerID),
					zap.Error(err))
			}
		}(observer)
	}
}

// loadConfigFromDB 从数据库加载WebSocket配置（内部方法）
func (s *webSocketConfigServiceImpl) loadConfigFromDB(ctx context.Context, ownerID int64) (*WebSocketConfigData, error) {
	if s.db == nil {
		return nil, fmt.Errorf("数据库未初始化")
	}

	// 查询机器人配置和用户登录信息
	var result struct {
		Address string
		Token   string
	}

	err := s.db.WithContext(ctx).Table("wx_robot_configs r").
		Select("r.address, u.token").
		Joins("INNER JOIN wx_user_logins u ON r.id = u.robot_id").
		Where("r.owner_id = ? AND u.is_message_bot = 0 AND is_initialized = 1 AND u.status = 1", ownerID).
		Order("u.id ASC").
		Limit(1).
		Scan(&result).Error

	if err != nil {
		return nil, fmt.Errorf("查询WebSocket配置失败: %w", err)
	}

	if result.Address == "" || result.Token == "" {
		return nil, fmt.Errorf("未找到有效的WebSocket配置 (owner_id: %d)", ownerID)
	}

	// 处理address字段，移除可能的http://或https://前缀
	baseURL := result.Address
	if strings.HasPrefix(baseURL, "http://") {
		baseURL = strings.TrimPrefix(baseURL, "http://")
	} else if strings.HasPrefix(baseURL, "https://") {
		baseURL = strings.TrimPrefix(baseURL, "https://")
	}

	// 构建WebSocket URL
	wsURL := fmt.Sprintf("ws://%s/ws/GetSyncMsg?key=%s", baseURL, result.Token)

	config := &WebSocketConfigData{
		OwnerID: ownerID,
		URL:     wsURL,
		BaseURL: fmt.Sprintf("http://%s", baseURL),
		Token:   result.Token,
	}

	s.logger.Debug("从数据库加载WebSocket配置成功",
		zap.Int64("owner_id", ownerID),
		zap.String("base_url", config.BaseURL),
		zap.String("ws_url", config.URL))

	return config, nil
}

// GetBaseURLFromWebSocketURL 从WebSocket URL中提取BaseURL
func GetBaseURLFromWebSocketURL(wsURL string) (string, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return "", fmt.Errorf("解析WebSocket URL失败: %w", err)
	}
	return fmt.Sprintf("http://%s", u.Host), nil
}

// GetTokenFromWebSocketURL 从WebSocket URL中提取Token
func GetTokenFromWebSocketURL(wsURL string) (string, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return "", fmt.Errorf("解析WebSocket URL失败: %w", err)
	}
	token := u.Query().Get("key")
	if token == "" {
		return "", fmt.Errorf("WebSocket URL中未找到key参数")
	}
	return token, nil
}

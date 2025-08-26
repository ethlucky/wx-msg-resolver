package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// RedisUtil Redis工具接口
type RedisUtil interface {
	// GetLastMsgTime 获取最后处理的消息时间戳
	GetLastMsgTime(ctx context.Context, ownerID int) (int64, error)
	// SetLastMsgTime 设置最后处理的消息时间戳
	SetLastMsgTime(ctx context.Context, ownerID int, msgTime int64) error
	// GetLastMsgTimeWithFallback 获取最后处理的消息时间戳，如果Redis中没有则从数据库查询
	GetLastMsgTimeWithFallback(ctx context.Context, db *gorm.DB, ownerID int) (int64, error)
	
	// 队列操作方法
	// LPush 向列表左侧添加元素
	LPush(ctx context.Context, key string, values ...interface{}) error
	// BRPop 阻塞式从列表右侧弹出元素
	BRPop(ctx context.Context, timeout time.Duration, keys ...string) (string, error)
	// LLen 获取列表长度
	LLen(ctx context.Context, key string) (int64, error)
	// Set 设置键值对
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	// Del 删除键
	Del(ctx context.Context, keys ...string) error
	// BRPopLPush 阻塞式从source右侧弹出并推送到destination左侧
	BRPopLPush(ctx context.Context, source, destination string, timeout time.Duration) (string, error)
	// LRem 从列表中移除指定值
	LRem(ctx context.Context, key string, count int64, value interface{}) error
}

// redisUtilImpl Redis工具实现
type redisUtilImpl struct {
	client *redis.Client
	logger *zap.Logger
}

const (
	// DefaultExpiration Redis key的默认过期时间（24小时）
	DefaultExpiration = 365 * time.Hour
)

// generateLastMsgTimeKey 生成带owner_id的last_msg_time key
func generateLastMsgTimeKey(ownerID int) string {
	return fmt.Sprintf("wx_msg_chat:last_msg_time_%d", ownerID)
}

// NewRedisUtil 创建Redis工具实例
func NewRedisUtil(client *redis.Client, logger *zap.Logger) RedisUtil {
	return &redisUtilImpl{
		client: client,
		logger: logger,
	}
}

// GetLastMsgTime 获取最后处理的消息时间戳
func (r *redisUtilImpl) GetLastMsgTime(ctx context.Context, ownerID int) (int64, error) {
	if r.client == nil {
		return 0, fmt.Errorf("Redis客户端未初始化")
	}

	key := generateLastMsgTimeKey(ownerID)
	result := r.client.Get(ctx, key)
	if result.Err() != nil {
		if result.Err() == redis.Nil {
			r.logger.Debug("Redis中未找到last_msg_time")
			return 0, nil // 返回0表示没有找到
		}
		return 0, fmt.Errorf("从Redis获取last_msg_time失败: %w", result.Err())
	}

	msgTime, err := strconv.ParseInt(result.Val(), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("解析last_msg_time失败: %w", err)
	}

	r.logger.Debug("从Redis获取到last_msg_time", zap.Int64("msg_time", msgTime))
	return msgTime, nil
}

// SetLastMsgTime 设置最后处理的消息时间戳
func (r *redisUtilImpl) SetLastMsgTime(ctx context.Context, ownerID int, msgTime int64) error {
	if r.client == nil {
		return fmt.Errorf("Redis客户端未初始化")
	}

	key := generateLastMsgTimeKey(ownerID)
	result := r.client.Set(ctx, key, msgTime, DefaultExpiration)
	if result.Err() != nil {
		return fmt.Errorf("设置last_msg_time到Redis失败: %w", result.Err())
	}

	r.logger.Debug("设置last_msg_time到Redis成功", zap.Int64("msg_time", msgTime))
	return nil
}

// GetLastMsgTimeWithFallback 获取最后处理的消息时间戳，如果Redis中没有则从数据库查询
func (r *redisUtilImpl) GetLastMsgTimeWithFallback(ctx context.Context, db *gorm.DB, ownerID int) (int64, error) {
	// 首先尝试从Redis获取
	lastMsgTime, err := r.GetLastMsgTime(ctx, ownerID)
	if err != nil {
		r.logger.Warn("从Redis获取last_msg_time失败，尝试从数据库查询", zap.Error(err))
	} else if lastMsgTime > 0 {
		// Redis中有数据，直接返回
		return lastMsgTime, nil
	}

	// Redis中没有数据，从数据库查询
	r.logger.Info("Redis中未找到last_msg_time，从数据库查询max(msg_time)", zap.Int("owner_id", ownerID))
	maxMsgTime, err := r.getMaxMsgTimeFromDB(db, ownerID)
	if err != nil {
		return 0, fmt.Errorf("从数据库查询max(msg_time)失败: %w", err)
	}

	// 如果数据库中有数据，更新到Redis
	if maxMsgTime > 0 {
		if setErr := r.SetLastMsgTime(ctx, ownerID, maxMsgTime); setErr != nil {
			r.logger.Warn("将max(msg_time)更新到Redis失败",
				zap.Int64("max_msg_time", maxMsgTime),
				zap.Error(setErr))
		}
	}

	r.logger.Info("从数据库获取到max(msg_time)", zap.Int64("max_msg_time", maxMsgTime))
	return maxMsgTime, nil
}

// getMaxMsgTimeFromDB 从数据库wx_global_id表查询最大的msg_time
func (r *redisUtilImpl) getMaxMsgTimeFromDB(db *gorm.DB, ownerID int) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("数据库连接未初始化")
	}

	var maxMsgTime int64
	err := db.Model(&WxGlobalID{}).
		Where("owner_id = ?", ownerID).
		Select("COALESCE(MAX(msg_time), 0)").
		Scan(&maxMsgTime).Error
	if err != nil {
		return 0, fmt.Errorf("查询数据库max(msg_time)失败: %w", err)
	}

	return maxMsgTime, nil
}

// LPush 向列表左侧添加元素
func (r *redisUtilImpl) LPush(ctx context.Context, key string, values ...interface{}) error {
	if r.client == nil {
		return fmt.Errorf("Redis客户端未初始化")
	}

	result := r.client.LPush(ctx, key, values...)
	if result.Err() != nil {
		return fmt.Errorf("Redis LPush失败: %w", result.Err())
	}

	return nil
}

// BRPop 阻塞式从列表右侧弹出元素
func (r *redisUtilImpl) BRPop(ctx context.Context, timeout time.Duration, keys ...string) (string, error) {
	if r.client == nil {
		return "", fmt.Errorf("Redis客户端未初始化")
	}

	result := r.client.BRPop(ctx, timeout, keys...)
	if result.Err() != nil {
		if result.Err() == redis.Nil {
			return "", fmt.Errorf("redis: nil")
		}
		return "", fmt.Errorf("Redis BRPop失败: %w", result.Err())
	}

	if len(result.Val()) < 2 {
		return "", fmt.Errorf("Redis BRPop返回数据格式错误")
	}

	return result.Val()[1], nil // 返回值，跳过键名
}

// LLen 获取列表长度
func (r *redisUtilImpl) LLen(ctx context.Context, key string) (int64, error) {
	if r.client == nil {
		return 0, fmt.Errorf("Redis客户端未初始化")
	}

	result := r.client.LLen(ctx, key)
	if result.Err() != nil {
		return 0, fmt.Errorf("Redis LLen失败: %w", result.Err())
	}

	return result.Val(), nil
}

// Set 设置键值对
func (r *redisUtilImpl) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	if r.client == nil {
		return fmt.Errorf("Redis客户端未初始化")
	}

	result := r.client.Set(ctx, key, value, expiration)
	if result.Err() != nil {
		return fmt.Errorf("Redis Set失败: %w", result.Err())
	}

	return nil
}

// Del 删除键
func (r *redisUtilImpl) Del(ctx context.Context, keys ...string) error {
	if r.client == nil {
		return fmt.Errorf("Redis客户端未初始化")
	}

	result := r.client.Del(ctx, keys...)
	if result.Err() != nil {
		return fmt.Errorf("Redis Del失败: %w", result.Err())
	}

	return nil
}

// BRPopLPush 阻塞式从source右侧弹出并推送到destination左侧
func (r *redisUtilImpl) BRPopLPush(ctx context.Context, source, destination string, timeout time.Duration) (string, error) {
	if r.client == nil {
		return "", fmt.Errorf("Redis客户端未初始化")
	}

	result := r.client.BRPopLPush(ctx, source, destination, timeout)
	if result.Err() != nil {
		if result.Err() == redis.Nil {
			return "", fmt.Errorf("redis: nil")
		}
		return "", fmt.Errorf("Redis BRPopLPush失败: %w", result.Err())
	}

	return result.Val(), nil
}

// LRem 从列表中移除指定值
func (r *redisUtilImpl) LRem(ctx context.Context, key string, count int64, value interface{}) error {
	if r.client == nil {
		return fmt.Errorf("Redis客户端未初始化")
	}

	result := r.client.LRem(ctx, key, count, value)
	if result.Err() != nil {
		return fmt.Errorf("Redis LRem失败: %w", result.Err())
	}

	return nil
}

package main

import (
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// BillProcessingService 账单处理服务接口
type BillProcessingService interface {
	// ShouldProcessBill 判断消息是否满足账单条件，返回解析结果和是否处理的标志
	ShouldProcessBill(message WxGroupMessage) (*ParsedBillInfo, bool)
	// ProcessBillInTx 在事务中处理账单
	ProcessBillInTx(tx *gorm.DB, message WxGroupMessage, billInfo *ParsedBillInfo) error
	// GetAllAdminUsers 获取所有管理员用户名称
	getAllAdminUsers(ownerID int) (string, error)
}

// DefaultBillProcessingService 默认的账单处理服务实现
type DefaultBillProcessingService struct {
	logger     *zap.Logger
	billParser BillParser
	db         *gorm.DB
}

// NewBillProcessingService 创建新的账单处理服务
func NewBillProcessingService(db *gorm.DB, logger *zap.Logger) BillProcessingService {
	return &DefaultBillProcessingService{
		logger:     logger,
		billParser: NewBillParser(),
		db:         db,
	}
}

// GetAllAdminUsers 获取所有管理员用户名称
func (s *DefaultBillProcessingService) getAllAdminUsers(ownerID int) (string, error) {
	var robot WxRobotConfig
	if err := s.db.Select("admin_users").Where("owner_id = ?", ownerID).First(&robot).Error; err != nil {
		s.logger.Error("获取机器人配置失败", zap.Error(err), zap.Int("owner_id", ownerID))
		return "", err
	}

	if robot.AdminUsers != nil && *robot.AdminUsers != "" {
		return *robot.AdminUsers, nil
	}

	return "", nil
}

// ShouldProcessBill 判断消息是否满足账单条件，返回解析结果和是否处理的标志
func (s *DefaultBillProcessingService) ShouldProcessBill(message WxGroupMessage) (*ParsedBillInfo, bool) {
	// 只处理文本消息类型（msg_type = 1）
	if message.MsgType != 1 {
		return nil, false
	}

	// 获取管理员用户列表
	adminUsers, err := s.getAllAdminUsers(message.OwnerID)
	if err != nil {
		s.logger.Error("获取管理员用户列表失败", zap.Error(err))
		return nil, false
	}

	// 检查是否是管理员发送的消息
	if !strings.Contains(adminUsers, message.WxNickName) {
		s.logger.Debug("非管理员消息，跳过账单处理",
			zap.String("wx_nick_name", message.WxNickName))
		return nil, false
	}

	// 检查消息内容是否符合账单格式
	billInfo, err := s.billParser.ParseBillFromContent(message.Content)
	if err != nil {
		s.logger.Debug("消息不包含账单信息，跳过账单处理",
			zap.Int64("message_id", message.ID),
			zap.String("content", message.Content),
			zap.String("wx_nick_name", message.WxNickName))
		return nil, false
	}

	return billInfo, true
}

// ProcessBillInTx 在事务中处理账单
func (s *DefaultBillProcessingService) ProcessBillInTx(tx *gorm.DB, message WxGroupMessage, billInfo *ParsedBillInfo) error {
	// 获取群组信息
	var group WxGroup
	if err := tx.Where("group_id = ?", message.GroupID).First(&group).Error; err != nil {
		s.logger.Error("获取群组信息失败",
			zap.String("group_id", message.GroupID),
			zap.Error(err))
		return err
	}

	// 创建账单记录
	bill := &WxBillInfo{
		GroupName: group.GroupNickName,
		GroupID:   message.GroupID,
		Dollar:    billInfo.Dollar,
		Rate:      billInfo.Rate,
		Amount:    billInfo.Amount,
		Remark:    billInfo.Remark,
		Operator:  message.WxNickName,
		MsgTime:   message.MsgTime,
		Status:    "0", // 默认未清账
		OwnerID:   uint(message.OwnerID),
	}

	// 在事务中保存账单
	if err := tx.Create(bill).Error; err != nil {
		s.logger.Error("保存账单失败", zap.Error(err))
		return err
	}

	s.logger.Info("账单处理成功",
		zap.Int64("message_id", message.ID),
		zap.String("operator", message.WxNickName),
		zap.String("amount", billInfo.Amount),
		zap.String("dollar", billInfo.Dollar))

	return nil
}

package main

import (
	"time"
)

// ForwardedMessage 转发消息结构体（用于消息队列传输）
type ForwardedMessage struct {
	MsgType       int    `json:"msg_type"`       // 消息类型
	OwnerID       int    `json:"owner_id"`       // 所有者ID
	SourceToken   string `json:"source_token"`   // 来源Token
	GroupID       string `json:"group_id"`       // 群ID（仅群消息使用）
	WxID          string `json:"wx_id"`          // 发送者微信ID（仅群消息使用）
	FromUserName  string `json:"from_user_name"` // 发送者用户名
	ToUserName    string `json:"to_user_name"`   // 接收者用户名
	MsgID         string `json:"msg_id"`         // 消息ID
	NewMsgID      string `json:"new_msg_id"`     // 新消息ID
	ImgURL        string `json:"img_url"`        // 图片URL（默认空）
	MsgTime       int64  `json:"msg_time"`       // 消息时间戳
	ProcessedTime int64  `json:"processed_time"` // 处理时间戳
	Content       string `json:"content"`        // 消息内容
	MsgSource     string `json:"msg_source"`     // 消息来源
	IsGroupMsg    bool   `json:"is_group_msg"`   // 是否为群消息
}

// WxGroupMessage 群消息数据库模型
type WxGroupMessage struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	OwnerID     int       `gorm:"column:owner_id;not null" json:"owner_id"`                                    // 所有者ID
	SourceToken string    `gorm:"column:source_token;not null" json:"source_token"`                            // 来源Token
	GroupID     string    `gorm:"column:group_id;not null;default:''" json:"group_id"`                         // 群ID
	MsgType     int       `gorm:"column:msg_type;not null" json:"msg_type"`                                    // 消息类型
	MsgSource   string    `gorm:"column:msg_source;not null;default:''" json:"msg_source"`                     // 消息来源
	Content     string    `gorm:"column:content;type:text;not null" json:"content"`                            // 消息内容
	MsgID       string    `gorm:"column:msg_id;not null;default:''" json:"msg_id"`                             // 消息ID
	NewMsgID    string    `gorm:"column:new_msg_id;not null;default:''" json:"new_msg_id"`                     // 新消息ID
	WxID        string    `gorm:"column:wx_id;not null;default:''" json:"wx_id"`                               // 发送者微信ID
	WxNickName  string    `gorm:"column:wx_nick_name;not null;default:''" json:"wx_nick_name"`                 // 发送者昵称
	MsgTime     int64     `gorm:"column:msg_time;not null" json:"msg_time"`                                    // 消息时间戳
	CreateTime  time.Time `gorm:"column:create_time;not null;default:CURRENT_TIMESTAMP;->" json:"create_time"` // 创建时间
	UpdateTime  time.Time `gorm:"column:update_time;not null;default:CURRENT_TIMESTAMP;->" json:"update_time"` // 更新时间
}

// TableName 指定表名
func (WxGroupMessage) TableName() string {
	return "wx_group_messages"
}

// WxPersonalMessage 个人消息数据库模型（已停用，不再存储个人消息）
// 保留结构定义以避免编译错误，但不再使用
type WxPersonalMessage struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	OwnerID      int       `gorm:"column:owner_id;not null" json:"owner_id"`                                    // 所有者ID
	SourceToken  string    `gorm:"column:source_token;not null" json:"source_token"`                            // 来源Token
	MsgType      int       `gorm:"column:msg_type;not null" json:"msg_type"`                                    // 消息类型
	MsgSource    string    `gorm:"column:msg_source;not null;default:''" json:"msg_source"`                     // 消息来源
	Content      string    `gorm:"column:content;type:text;not null" json:"content"`                            // 消息内容
	MsgID        string    `gorm:"column:msg_id;not null;default:''" json:"msg_id"`                             // 消息ID
	NewMsgID     string    `gorm:"column:new_msg_id;not null;default:''" json:"new_msg_id"`                     // 新消息ID
	FromUserName string    `gorm:"column:from_user_name;not null;default:''" json:"from_user_name"`             // 发送者用户名
	ToUserName   string    `gorm:"column:to_user_name;not null;default:''" json:"to_user_name"`                 // 接收者用户名
	ImgURL       *string   `gorm:"column:img_url" json:"img_url"`                                               // 图片URL（可为空）
	MsgTime      int64     `gorm:"column:msg_time;not null" json:"msg_time"`                                    // 消息时间戳
	CreateTime   time.Time `gorm:"column:create_time;not null;default:CURRENT_TIMESTAMP;->" json:"create_time"` // 创建时间
	UpdateTime   time.Time `gorm:"column:update_time;not null;default:CURRENT_TIMESTAMP;->" json:"update_time"` // 更新时间
}

// TableName 指定表名（已停用）
func (WxPersonalMessage) TableName() string {
	return "wx_personal_messages"
}

// WxGlobalID 全局ID数据库模型
type WxGlobalID struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`                                                            // 主键ID
	OwnerID    int       `gorm:"column:owner_id;not null" json:"owner_id"`                                                                // 所有者ID
	MsgID      int64     `gorm:"column:msg_id" json:"msg_id"`                                                                             // 消息ID
	MsgTime    int64     `gorm:"column:msg_time;not null" json:"msg_time"`                                                                // 消息时间戳
	CreateTime time.Time `gorm:"column:create_time;not null;default:CURRENT_TIMESTAMP;->" json:"create_time"`                             // 创建时间
	UpdateTime time.Time `gorm:"column:update_time;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;->" json:"update_time"` // 更新时间
}

// TableName 指定表名
func (WxGlobalID) TableName() string {
	return "wx_global_id"
}

// OcrWalletInfo OCR钱包信息数据库模型
type OcrWalletInfo struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`                                                            // 主键ID
	OwnerID    int       `gorm:"column:owner_id;not null" json:"owner_id"`                                                                // 所有者ID
	MsgID      string    `gorm:"column:msg_id;not null;default:''" json:"msg_id"`                                                         // 消息ID
	GroupID    string    `gorm:"column:group_id;not null;default:''" json:"group_id"`                                                     // 群ID
	WalletCode string    `gorm:"column:wallet_code;not null;default:''" json:"wallet_code"`                                               // 钱包代码
	SerialNo   string    `gorm:"column:serial_no;not null;default:''" json:"serial_no"`                                                   // 序列号
	WxID       string    `gorm:"column:wx_id;not null;default:''" json:"wx_id"`                                                           // 发送者微信ID
	WxNickName string    `gorm:"column:wx_nick_name;not null;default:''" json:"wx_nick_name"`                                             // 发送者昵称
	PicURL     *string   `gorm:"column:pic_url" json:"pic_url"`                                                                           // 图片URL地址
	CreateTime time.Time `gorm:"column:create_time;not null;default:CURRENT_TIMESTAMP;->" json:"create_time"`                             // 创建时间
	UpdateTime time.Time `gorm:"column:update_time;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;->" json:"update_time"` // 更新时间
}

// TableName 指定表名
func (OcrWalletInfo) TableName() string {
	return "ocr_wallet_info"
}

// WxRobotConfig 微信机器人配置表
type WxRobotConfig struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`                                                            // 主键ID
	Address     string    `gorm:"column:address;not null" json:"address"`                                                                  // 机器人地址
	AdminKey    string    `gorm:"column:admin_key;not null" json:"admin_key"`                                                              // 管理密钥
	OwnerID     int64     `gorm:"column:owner_id;not null" json:"owner_id"`                                                                // 所属公司ID
	Description *string   `gorm:"column:description" json:"description"`                                                                   // 文本描述
	AdminUsers  *string   `gorm:"column:admin_users;type:text" json:"admin_users"`                                                         // 管理员用户列表，用逗号分隔
	CreateTime  time.Time `gorm:"column:create_time;not null;default:CURRENT_TIMESTAMP;->" json:"create_time"`                             // 创建时间
	UpdateTime  time.Time `gorm:"column:update_time;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;->" json:"update_time"` // 修改时间
}

// TableName 指定表名
func (WxRobotConfig) TableName() string {
	return "wx_robot_configs"
}

// WxUserLogin 微信用户登录信息表
type WxUserLogin struct {
	ID              int64      `gorm:"column:id;primaryKey;autoIncrement" json:"id"`                                                            // 主键ID
	RobotID         int64      `gorm:"column:robot_id;not null" json:"robot_id"`                                                                // 关联的机器人ID
	Token           *string    `gorm:"column:token" json:"token"`                                                                               // 登录令牌
	WxID            *string    `gorm:"column:wx_id" json:"wx_id"`                                                                               // 微信ID
	NickName        *string    `gorm:"column:nick_name" json:"nick_name"`                                                                       // 微信昵称
	ExtensionTime   *time.Time `gorm:"column:extension_time" json:"extension_time"`                                                             // 延期时间
	HasSecurityRisk int        `gorm:"column:has_security_risk;default:0" json:"has_security_risk"`                                             // 是否有安全风险 0否 1是
	ExpirationTime  *time.Time `gorm:"column:expiration_time" json:"expiration_time"`                                                           // 过期时间
	Status          int        `gorm:"column:status;default:1" json:"status"`                                                                   // 状态 1正常 2风控 3过期
	IsInitialized   int        `gorm:"column:is_initialized;default:0" json:"is_initialized"`                                                   // 是否初始化完成 0未初始化 1初始化完成
	IsMessageBot    int        `gorm:"column:is_message_bot;default:0" json:"is_message_bot"`                                                   // 是否是消息机器人 0不是 1是
	CreateTime      time.Time  `gorm:"column:create_time;not null;default:CURRENT_TIMESTAMP;->" json:"create_time"`                             // 创建时间
	UpdateTime      time.Time  `gorm:"column:update_time;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;->" json:"update_time"` // 修改时间
}

// TableName 指定表名
func (WxUserLogin) TableName() string {
	return "wx_user_logins"
}

type WxGroup struct {
	ID            uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	WxID          string    `json:"wx_id" gorm:"type:varchar(100);not null;comment:微信ID"`
	GroupID       string    `json:"group_id" gorm:"type:varchar(100);not null;comment:群组ID"`
	GroupNickName string    `json:"group_nick_name" gorm:"type:varchar(200);comment:群组昵称"`
	CreateTime    time.Time `json:"create_time" gorm:"autoCreateTime;comment:创建时间"`
	UpdateTime    time.Time `json:"update_time" gorm:"autoUpdateTime;comment:修改时间"`
}

func (WxGroup) TableName() string {
	return "wx_groups"
}

type WxBillInfo struct {
	ID         uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	GroupName  string    `json:"group_name" gorm:"type:varchar(50);not null;comment:群组名称"`
	GroupID    string    `json:"group_id" gorm:"type:varchar(50);not null;comment:群组Id"`
	Dollar     string    `json:"dollar" gorm:"type:varchar(20);comment:金额(外币)"`
	Rate       string    `json:"rate" gorm:"type:varchar(20);comment:汇率"`
	Amount     string    `json:"amount" gorm:"type:decimal(15,2);comment:金额(RMB)"`
	Remark     string    `json:"remark" gorm:"type:text;comment:备注"`
	Operator   string    `json:"operator" gorm:"type:varchar(20);comment:操作人名称"`
	MsgTime    int64     `json:"msg_time" gorm:"comment:账单时间"`
	Status     string    `json:"status" gorm:"type:char(2);comment:清账状态(0 为未清账, 1 为已清账)"`
	OwnerID    uint      `json:"owner_id" gorm:"not null;comment:所属公司ID"`
	CreateTime time.Time `json:"create_time" gorm:"autoCreateTime;comment:创建时间"`
	UpdateTime time.Time `json:"update_time" gorm:"autoUpdateTime;comment:修改时间"`
}

func (WxBillInfo) TableName() string {
	return "wx_bill_info"
}

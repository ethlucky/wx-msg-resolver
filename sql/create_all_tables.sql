-- 完整的数据库建表脚本，包含所有migrate变更
-- 数据库名: wx_msg_chat

USE wx_msg_chat;

-- 微信全局消息ID表（已包含migrate_wx_global_id.sql的变更）
CREATE TABLE IF NOT EXISTS `wx_global_id` (
    `id` BIGINT NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `owner_id` INT NOT NULL DEFAULT 1 COMMENT '所有者ID',
    `msg_id` BIGINT NULL COMMENT '消息ID',
    `msg_time` INT NOT NULL COMMENT '消息时间戳',
    `create_time` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `update_time` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',

    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_owner_msg_id` (`owner_id`, `msg_id`),
    KEY `idx_msg_id` (`msg_id`),
    KEY `idx_msg_time` (`msg_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='微信全局消息ID表';

-- 微信群消息聊天记录表（已包含migrate_content_to_text.sql的变更）
CREATE TABLE IF NOT EXISTS `wx_group_messages` (
    `id` BIGINT NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `owner_id` INT NOT NULL COMMENT '所有者ID',
    `source_token` VARCHAR(200) NOT NULL COMMENT '来源，使用来源对应的token,微信机器人的话现在分多个',
    `group_id` VARCHAR(50) NOT NULL DEFAULT '' COMMENT '群id',
    `msg_type` INT NOT NULL COMMENT '消息类型 (1=文本, 3=图片, 34=语音, 47=表情, 49=链接等)',
    `msg_source` VARCHAR(50) NOT NULL DEFAULT '' COMMENT '消息来源',
    `content` TEXT NOT NULL COMMENT '消息内容',
    `msg_id` VARCHAR(100) NOT NULL DEFAULT '' COMMENT '消息ID',
    `new_msg_id` VARCHAR(100) NOT NULL DEFAULT '' COMMENT '新消息ID',
    `wx_id` VARCHAR(100) NOT NULL DEFAULT '' COMMENT '发送者用户名',
    `wx_nick_name` VARCHAR(200) NOT NULL DEFAULT '' COMMENT '发送者昵称',
    `msg_time` INT NOT NULL COMMENT '消息时间戳',
    `create_time` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `update_time` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_msg_id` (`msg_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='微信群消息聊天记录表'; 

-- 微信个人消息聊天记录表（已包含migrate_content_to_text.sql的变更）
CREATE TABLE IF NOT EXISTS `wx_personal_messages` (
    `id` BIGINT NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `owner_id` INT NOT NULL COMMENT '所有者ID',
    `source_token` VARCHAR(200) NOT NULL COMMENT '来源，使用来源对应的token,微信机器人的话现在分多个',
    `msg_type` INT NOT NULL COMMENT '消息类型 (1=文本, 3=图片, 34=语音, 47=表情, 49=链接等)',
    `msg_source` VARCHAR(50) NOT NULL DEFAULT '' COMMENT '消息来源',
    `content` TEXT NOT NULL COMMENT '消息内容',
    `msg_id` VARCHAR(100) NOT NULL DEFAULT '' COMMENT '消息ID',
    `new_msg_id` VARCHAR(100) NOT NULL DEFAULT '' COMMENT '新消息ID',
    `from_user_name` VARCHAR(100) NOT NULL DEFAULT '' COMMENT '发送者用户名',
    `img_url` VARCHAR(500) DEFAULT NULL COMMENT '图片URL地址',
    `to_user_name` VARCHAR(100) NOT NULL DEFAULT '' COMMENT '接收者用户名',
    `msg_time` INT NOT NULL COMMENT '消息时间戳',
    `create_time` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `update_time` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_msg_id` (`msg_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='微信个人消息聊天记录表'; 

-- OCR钱包信息表（已包含migrate_content_to_text.sql的字符集变更）
CREATE TABLE IF NOT EXISTS `ocr_wallet_info` (
    `id` BIGINT NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `owner_id` INT NOT NULL COMMENT '所有者ID',
    `msg_id` VARCHAR(100) NOT NULL DEFAULT '' COMMENT '消息ID',
    `group_id` VARCHAR(50) NOT NULL DEFAULT '' COMMENT '群ID',
    `wallet_code` VARCHAR(100) NOT NULL DEFAULT '' COMMENT '钱包代码',
    `serial_no` VARCHAR(100) NOT NULL DEFAULT '' COMMENT '序列号',
    `wx_id` VARCHAR(100) NOT NULL DEFAULT '' COMMENT '发送者微信ID',
    `wx_nick_name` VARCHAR(200) NOT NULL DEFAULT '' COMMENT '发送者昵称',
    `pic_url` VARCHAR(500) DEFAULT NULL COMMENT '图片URL地址',
    `create_time` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `update_time` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_msg_id` (`msg_id`),
    KEY `idx_wallet_code` (`wallet_code`),
    KEY `idx_group_wx_id` (`group_id`, `wx_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='OCR钱包信息表';
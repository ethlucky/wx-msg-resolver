# 微信消息聊天系统 - 数据库脚本

此文件夹包含微信消息聊天系统的数据库建表语句和相关脚本。

## 文件说明

### 1. `init_database.sql`
- **用途**: 数据库初始化脚本，包含所有建表语句
- **使用**: 用于首次设置数据库时执行
- **包含**: 完整的数据库初始化，包括字符集设置和所有表创建

### 2. `create_messages_table.sql`
- **用途**: 创建消息表的独立脚本
- **使用**: 当只需要创建消息表时使用
- **包含**: wx_group_messages和wx_personal_messages表的完整建表语句

### 3. `drop_messages_table.sql`
- **用途**: 删除消息表
- **警告**: ⚠️ 执行此脚本将永久删除表及其所有数据
- **使用**: 仅在需要重新创建表或清理数据库时使用

## 数据库表结构

### `wx_group_messages` 表
微信群消息聊天记录表，包含以下字段：

| 字段名 | 数据类型 | 约束 | 说明 |
|--------|----------|------|------|
| id | BIGINT | 主键，自增 | 主键ID |
| source_ip | VARCHAR(20) | 非空 | 来源IP地址 |
| source_token | VARCHAR(200) | 非空 | 来源Token |
| group_id | VARCHAR(50) | 非空 | 群ID |
| msg_type | INT | 非空 | 消息类型 (1=文本, 3=图片, 34=语音, 47=表情, 49=链接等) |
| msg_source | VARCHAR(50) | 非空 | 消息来源 |
| content | VARCHAR(500) | 非空 | 消息内容 |
| msg_id | VARCHAR(100) | 非空，唯一 | 消息ID |
| new_msg_id | VARCHAR(100) | 非空 | 新消息ID |
| wx_id | VARCHAR(100) | 非空 | 发送者微信ID |
| img_url | VARCHAR(500) | 可空 | 图片URL地址 |
| msg_time | DATETIME | 非空 | 消息时间 |
| create_time | DATETIME | 非空，默认当前时间 | 创建时间 |
| update_time | DATETIME | 非空，自动更新 | 更新时间 |

### `wx_personal_messages` 表
微信个人消息聊天记录表，包含以下字段：

| 字段名 | 数据类型 | 约束 | 说明 |
|--------|----------|------|------|
| id | BIGINT | 主键，自增 | 主键ID |
| source_ip | VARCHAR(20) | 非空 | 来源IP地址 |
| source_token | VARCHAR(200) | 非空 | 来源Token |
| msg_type | INT | 非空 | 消息类型 (1=文本, 3=图片, 34=语音, 47=表情, 49=链接等) |
| msg_source | VARCHAR(50) | 非空 | 消息来源 |
| content | VARCHAR(500) | 非空 | 消息内容 |
| msg_id | VARCHAR(100) | 非空，唯一 | 消息ID |
| new_msg_id | VARCHAR(100) | 非空 | 新消息ID |
| from_user_name | VARCHAR(100) | 非空 | 发送者用户名 |
| to_user_name | VARCHAR(100) | 非空 | 接收者用户名 |
| img_url | VARCHAR(500) | 可空 | 图片URL地址 |
| msg_time | DATETIME | 非空 | 消息时间 |
| create_time | DATETIME | 非空，默认当前时间 | 创建时间 |
| update_time | DATETIME | 非空，自动更新 | 更新时间 |

### 索引说明
- `PRIMARY KEY (id)`: 主键索引
- `UNIQUE KEY uk_msg_id (msg_id)`: 消息ID唯一索引
- `KEY idx_msg_type (msg_type)`: 消息类型索引
- `KEY idx_from_user (from_user_name)`: 发送者索引
- `KEY idx_to_user (to_user_name)`: 接收者索引
- `KEY idx_msg_time (msg_time)`: 消息时间索引
- `KEY idx_create_time (create_time)`: 创建时间索引

## 使用方法

### 1. 初次建表
```bash
# 连接到MySQL数据库
mysql -u 用户名 -p 数据库名 < sql/init_database.sql
```

### 2. 单独创建消息表
```bash
mysql -u 用户名 -p 数据库名 < sql/create_messages_table.sql
```

### 3. 删除表（谨慎使用）
```bash
mysql -u 用户名 -p 数据库名 < sql/drop_messages_table.sql
```

### 4. 使用数据库管理工具
您也可以使用phpMyAdmin、Navicat、DBeaver等数据库管理工具直接执行这些SQL文件。

## 注意事项

1. **字符集**: 所有表均使用 `utf8mb4` 字符集，支持完整的Unicode字符集（包括emoji）
2. **存储引擎**: 使用InnoDB存储引擎，支持事务和外键
3. **时区**: 建议在应用程序中正确设置时区
4. **备份**: 在执行删除操作前，请确保已备份重要数据
5. **权限**: 确保数据库用户有足够的权限执行这些SQL语句

## 版本信息
- 版本: 1.0
- 创建时间: 2024年
- 适用于: 微信消息聊天系统 
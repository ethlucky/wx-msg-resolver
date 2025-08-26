# 微信消息聊天应用

基于Go语言开发的微信消息处理聊天应用，集成了Gin框架、GORM、Redis、RabbitMQ等组件。

## 🚀 技术栈

- **Web框架**: [Gin](https://github.com/gin-gonic/gin) - 高性能HTTP框架
- **配置管理**: [Viper](https://github.com/spf13/viper) - 支持TOML格式配置
- **日志系统**: [Zap](https://github.com/uber-go/zap) - 高性能结构化日志
- **数据库ORM**: [GORM](https://gorm.io/) - 功能强大的Go ORM
- **缓存**: [Redis](https://redis.io/) - 内存数据库
- **消息队列**: [RabbitMQ](https://www.rabbitmq.com/) - 可靠的消息中间件

## 📋 功能特性

- ✅ 完整的微信消息处理架构
- ✅ 支持多种配置环境（开发/测试/生产）
- ✅ 结构化日志记录
- ✅ 数据库连接池管理
- ✅ Redis缓存支持
- ✅ RabbitMQ消息队列
- ✅ OCR文字识别服务
- ✅ 优雅的服务关闭
- ✅ 健康检查接口
- ✅ RESTful API设计

## 🛠️ 安装配置

### 环境要求

- Go 1.24.3+
- MySQL 5.7+
- Redis 5.0+
- RabbitMQ 3.8+

### 克隆项目

```bash
git clone <your-repo-url>
cd wx-msg-chat
```

### 安装依赖

```bash
go mod download
```

### 配置文件

1. 复制配置示例文件：
```bash
cp config.example.toml config.toml
```

2. 编辑 `config.toml` 文件，填入实际的配置信息：
   - 数据库连接信息
   - Redis连接信息  
   - RabbitMQ连接信息
   - 微信开发者配置
   - 其他自定义配置

### 数据库设置

创建MySQL数据库：
```sql
CREATE DATABASE wx_msg_chat CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

### 运行应用

#### 开发环境

```bash
go run main.go
```

#### 编译运行

```bash
# 编译
go build -o wx-msg-chat main.go

# 运行
./wx-msg-chat
```

## 📖 API接口

### 健康检查

```
GET /health
```

### 微信相关

```
GET  /api/v1/wechat/verify    # 微信验证接口
POST /api/v1/wechat/message   # 微信消息处理接口
```

### 消息管理

```
GET  /api/v1/message/list     # 消息列表接口
POST /api/v1/message/send     # 发送消息接口
```

### OCR识别

```
POST /api/v1/ocr/recognize        # 单张图片OCR识别
POST /api/v1/ocr/recognize/batch  # 批量图片OCR识别
```

### RabbitMQ消息队列

```
POST /api/v1/message/send         # 发送消息到队列
GET  /health                      # 检查RabbitMQ连接状态
```

#### 消息发送示例

```bash
# 发送文本消息
curl -X POST http://localhost:8080/api/v1/message/send \
  -H "Content-Type: application/json" \
  -d '{
    "content": "Hello, World!",
    "type": "text",
    "user_id": "user123"
  }'

# 发送图片消息
curl -X POST http://localhost:8080/api/v1/message/send \
  -H "Content-Type: application/json" \
  -d '{
    "content": "https://example.com/image.jpg",
    "type": "image",
    "user_id": "user123"
  }'
```

#### 响应示例

```json
{
  "success": true,
  "message": "消息发送成功",
  "message_id": "msg_1640995200000_user123"
}
```

#### 健康检查示例

```bash
curl http://localhost:8080/health
```

```json
{
  "status": "ok",
  "message": "服务正常运行",
  "timestamp": "2024-01-01T10:00:00Z",
  "components": {
    "database": {
      "status": "ok",
      "message": "数据库连接正常"
    },
    "redis": {
      "status": "ok", 
      "message": "Redis连接正常"
    },
    "rabbitmq": {
      "status": "ok",
      "message": "RabbitMQ连接正常"
    },
    "ocr": {
      "status": "ok",
      "message": "OCR服务可用"
    }
  }
}
```

## 🏗️ 项目结构

```
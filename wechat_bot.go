package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"go.uber.org/zap"
)

// WechatBot 微信机器人接口
type WechatBot interface {
	// SendCdnDownload 获取附件数据
	SendCdnDownload(req *SendCdnDownloadRequest) (*SendCdnDownloadResponse, error)
	// GetChatRoomInfo 获取群聊信息
	GetChatRoomInfo(req *GetChatRoomInfoRequest) (*GetChatRoomInfoResponse, error)
	// UpdateConfig 更新配置
	UpdateConfig(baseURL, key string) error
}

// GetChatRoomInfoRequest 获取群聊信息请求
type GetChatRoomInfoRequest struct {
	ChatRoomWxIdList []string `json:"ChatRoomWxIdList"` // 群聊微信ID列表
}

// ChatRoomMember 群聊成员信息
type ChatRoomMember struct {
	UserName           string `json:"user_name"`            // 用户微信ID
	NickName           string `json:"nick_name"`            // 用户昵称
	ChatRoomMemberFlag int    `json:"chatroom_member_flag"` // 成员标志
	Unknow             string `json:"unknow,omitempty"`     // 未知字段
}

// GetChatRoomInfoResponse 获取群聊信息响应
type GetChatRoomInfoResponse struct {
	ChatRoomMembers map[string]string `json:"chatroom_members"` // key是user_name，value是nick_name
}

// GetChatRoomInfoRawResponse 原始API响应结构
type GetChatRoomInfoRawResponse struct {
	Code int    `json:"Code"`
	Text string `json:"Text"`
	Data struct {
		BaseResponse struct {
			Ret    int      `json:"ret"`
			ErrMsg struct{} `json:"errMsg"`
		} `json:"baseResponse"`
		ContactCount int `json:"contactCount"`
		ContactList  []struct {
			UserName struct {
				Str string `json:"str"`
			} `json:"userName"`
			NickName struct {
				Str string `json:"str"`
			} `json:"nickName"`
			NewChatroomData struct {
				MemberCount        int              `json:"member_count"`
				ChatroomMemberList []ChatRoomMember `json:"chatroom_member_list"`
			} `json:"newChatroomData"`
		} `json:"contactList"`
	} `json:"Data"`
}

// SendCdnDownloadRequest 获取附件数据请求
type SendCdnDownloadRequest struct {
	AesKey   string `json:"AesKey"`   // AES解密密钥
	FileType int    `json:"FileType"` // 文件类型
	FileURL  string `json:"FileURL"`  // 文件URL
}

// SendCdnDownloadResponse 获取附件数据响应
type SendCdnDownloadResponse struct {
	Code int    `json:"Code"`
	Text string `json:"Text"`
	Data struct {
		Ver             int    `json:"Ver"`
		Seq             int    `json:"Seq"`
		VideoFormat     int    `json:"VideoFormat"`
		RspPicFormat    int    `json:"RspPicFormat"`
		RangeStart      int    `json:"RangeStart"`
		RangeEnd        int    `json:"RangeEnd"`
		TotalSize       int    `json:"TotalSize"`
		SrcSize         int    `json:"SrcSize"`
		RetCode         int    `json:"RetCode"`
		SubStituteFType int    `json:"SubStituteFType"`
		RetrySec        int    `json:"RetrySec"`
		IsRetry         int    `json:"IsRetry"`
		IsOverLoad      int    `json:"IsOverLoad"`
		IsGetCdn        int    `json:"IsGetCdn"`
		XClientIP       string `json:"XClientIP"`
		FileData        string `json:"FileData"` // 文件数据(Base64编码)
	} `json:"Data"`
}


// wechatBotImpl 微信机器人实现
type wechatBotImpl struct {
	baseURL    string
	key        string
	httpClient *http.Client
	logger     *zap.Logger
	configMu   sync.RWMutex // 配置读写锁
}

// NewWechatBot 创建微信机器人客户端
func NewWechatBot(baseURL, key string, logger *zap.Logger) WechatBot {
	return &wechatBotImpl{
		baseURL: baseURL,
		key:     key,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:          100,              // 最大空闲连接数
				MaxIdleConnsPerHost:   10,               // 每个host的最大空闲连接数
				IdleConnTimeout:       90 * time.Second, // 空闲连接超时时间
				DisableKeepAlives:     false,            // 启用Keep-Alive
				ExpectContinueTimeout: 1 * time.Second,  // 100-continue超时
				TLSHandshakeTimeout:   10 * time.Second, // TLS握手超时
				ResponseHeaderTimeout: 20 * time.Second, // 响应头超时
				DisableCompression:    false,            // 启用压缩
			},
		},
		logger: logger,
	}
}


// SendCdnDownload 获取附件数据
func (w *wechatBotImpl) SendCdnDownload(req *SendCdnDownloadRequest) (*SendCdnDownloadResponse, error) {
	w.configMu.RLock()
	url := fmt.Sprintf("%s/message/SendCdnDownload?key=%s", w.baseURL, w.key)
	w.configMu.RUnlock()

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("SendCdnDownload 序列化请求数据失败: %w", err)
	}

	w.logger.Info("SendCdnDownload 获取附件数据请求",
		zap.String("url", url),
		zap.String("file_url", req.FileURL),
		zap.Int("file_type", req.FileType))

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("SendCdnDownload 创建HTTP请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := w.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("SendCdnDownload 发送HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("SendCdnDownload 读取响应数据失败: %w", err)
	}

	var response SendCdnDownloadResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("SendCdnDownload 解析响应数据失败: %w", err)
	}

	w.logger.Info("SendCdnDownload 获取附件数据响应",
		zap.Int("code", response.Code),
		zap.Int("ret_code", response.Data.RetCode),
		zap.Int("total_size", response.Data.TotalSize),
		zap.String("client_ip", response.Data.XClientIP))

	// 检查HTTP状态码
	if response.Code != 200 {
		return nil, fmt.Errorf("SendCdnDownload 获取附件数据失败: HTTP状态码 %d", response.Code)
	}

	// 检查返回码
	if response.Data.RetCode != 0 {
		return nil, fmt.Errorf("SendCdnDownload 获取附件数据失败: 返回码 %d", response.Data.RetCode)
	}

	// 检查是否有文件数据
	if response.Data.FileData == "" {
		return nil, fmt.Errorf("SendCdnDownload 获取附件数据失败: 无文件数据")
	}

	// w.logger.Info("SendCdnDownload 附件数据获取成功",
	// 	zap.Int("file_size", response.Data.TotalSize),
	// 	zap.Int("data_length", len(response.Data.FileData)))

	return &response, nil
}

// GetChatRoomInfo 获取群聊信息
func (w *wechatBotImpl) GetChatRoomInfo(req *GetChatRoomInfoRequest) (*GetChatRoomInfoResponse, error) {
	w.configMu.RLock()
	url := fmt.Sprintf("%s/group/GetChatRoomInfo?key=%s", w.baseURL, w.key)
	w.configMu.RUnlock()

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("GetChatRoomInfo 序列化请求数据失败: %w", err)
	}

	w.logger.Info("GetChatRoomInfo 获取群聊信息请求",
		zap.String("url", url),
		zap.Strings("chatroom_ids", req.ChatRoomWxIdList))

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("GetChatRoomInfo 创建HTTP请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := w.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("GetChatRoomInfo 发送HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("GetChatRoomInfo 读取响应数据失败: %w", err)
	}

	var rawResponse GetChatRoomInfoRawResponse
	if err := json.Unmarshal(body, &rawResponse); err != nil {
		return nil, fmt.Errorf("GetChatRoomInfo 解析响应数据失败: %w", err)
	}

	w.logger.Info("GetChatRoomInfo 获取群聊信息响应",
		zap.Int("code", rawResponse.Code),
		zap.Int("contact_count", rawResponse.Data.ContactCount))

	// 检查HTTP状态码
	if rawResponse.Code != 200 {
		return nil, fmt.Errorf("GetChatRoomInfo 获取群聊信息失败: HTTP状态码 %d", rawResponse.Code)
	}

	// 检查返回码
	if rawResponse.Data.BaseResponse.Ret != 0 {
		return nil, fmt.Errorf("GetChatRoomInfo 获取群聊信息失败: 返回码 %d", rawResponse.Data.BaseResponse.Ret)
	}

	// 检查是否有联系人数据
	if rawResponse.Data.ContactCount == 0 || len(rawResponse.Data.ContactList) == 0 {
		return nil, fmt.Errorf("GetChatRoomInfo 获取群聊信息失败: 无群聊数据")
	}

	// 构建响应数据
	response := &GetChatRoomInfoResponse{
		ChatRoomMembers: make(map[string]string),
	}

	// 遍历所有联系人（群聊）
	for _, contact := range rawResponse.Data.ContactList {
		// 遍历群聊成员列表
		for _, member := range contact.NewChatroomData.ChatroomMemberList {
			if member.UserName != "" && member.NickName != "" {
				response.ChatRoomMembers[member.UserName] = member.NickName
			}
		}
	}

	w.logger.Info("GetChatRoomInfo 群聊信息获取成功",
		zap.Int("member_count", len(response.ChatRoomMembers)))

	return response, nil
}

// UpdateConfig 更新配置
func (w *wechatBotImpl) UpdateConfig(baseURL, key string) error {
	w.configMu.Lock()
	defer w.configMu.Unlock()

	oldBaseURL := w.baseURL
	oldKey := w.key

	w.baseURL = baseURL
	w.key = key

	w.logger.Info("WechatBot配置已更新",
		zap.String("old_base_url", oldBaseURL),
		zap.String("new_base_url", baseURL),
		zap.String("old_key", oldKey),
		zap.String("new_key", key))

	return nil
}

// OnConfigChanged 实现ConfigObserver接口
func (w *wechatBotImpl) OnConfigChanged(config *WebSocketConfigData) error {
	// 从WebSocket URL中解析出baseURL
	baseURL := config.BaseURL
	if baseURL == "" {
		// 如果BaseURL为空，从URL中解析
		wsURL, err := url.Parse(config.URL)
		if err != nil {
			return fmt.Errorf("解析WebSocket URL失败: %w", err)
		}
		baseURL = fmt.Sprintf("http://%s", wsURL.Host)
	}

	return w.UpdateConfig(baseURL, config.Token)
}

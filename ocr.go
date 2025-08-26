package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// OCRInput OCR请求输入结构
type OCRInput struct {
	Name     string   `json:"name"`
	Shape    []int    `json:"shape"`
	Datatype string   `json:"datatype"`
	Data     []string `json:"data"`
}

// OCROutput OCR请求输出结构
type OCROutput struct {
	Name string `json:"name"`
}

// OCRRequest OCR请求结构
type OCRRequest struct {
	Inputs  []OCRInput  `json:"inputs"`
	Outputs []OCROutput `json:"outputs"`
}

// OCRResponseOutput OCR响应输出结构
type OCRResponseOutput struct {
	Name     string   `json:"name"`
	Datatype string   `json:"datatype"`
	Shape    []int    `json:"shape"`
	Data     []string `json:"data"`
}

// OCRResponse OCR响应结构
type OCRResponse struct {
	ModelName    string              `json:"model_name"`
	ModelVersion string              `json:"model_version"`
	Outputs      []OCRResponseOutput `json:"outputs"`
	Error        string              `json:"error,omitempty"`
}

// OCRResult OCR结果结构（解析data中的JSON字符串）
type OCRResult struct {
	LogId     string           `json:"logId"`
	Result    OCRResultContent `json:"result"`
	ErrorCode int              `json:"errorCode"`
	ErrorMsg  string           `json:"errorMsg"`
}

// OCRResultContent OCR结果内容
type OCRResultContent struct {
	OCRResults []OCRTextResult `json:"ocrResults"`
	DataInfo   OCRDataInfo     `json:"dataInfo"`
}

// OCRDataInfo OCR数据信息
type OCRDataInfo struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Type   string `json:"type"`
}

// OCRTextResult OCR文本识别结果
type OCRTextResult struct {
	PrunedResult OCRPrunedResult `json:"prunedResult"`
}

// OCRPrunedResult OCR处理后的结果
type OCRPrunedResult struct {
	ModelSettings     OCRModelSettings   `json:"model_settings"`
	DocPreprocessor   OCRDocPreprocessor `json:"doc_preprocessor_res"`
	DtPolys           [][][]int          `json:"dt_polys"`
	TextDetParams     OCRTextDetParams   `json:"text_det_params"`
	TextType          string             `json:"text_type"`
	OrientationAngles []int              `json:"textline_orientation_angles"`
	RecScoreThresh    float64            `json:"text_rec_score_thresh"`
	RecTexts          []string           `json:"rec_texts"`
	RecScores         []float64          `json:"rec_scores"`
	RecPolys          [][][]int          `json:"rec_polys"`
	RecBoxes          [][]int            `json:"rec_boxes"`
}

// OCRModelSettings OCR模型设置
type OCRModelSettings struct {
	UseDocPreprocessor     bool `json:"use_doc_preprocessor"`
	UseTextlineOrientation bool `json:"use_textline_orientation"`
}

// OCRDocPreprocessor OCR文档预处理器
type OCRDocPreprocessor struct {
	ModelSettings OCRDocModelSettings `json:"model_settings"`
	Angle         int                 `json:"angle"`
}

// OCRDocModelSettings OCR文档模型设置
type OCRDocModelSettings struct {
	UseDocOrientationClassify bool `json:"use_doc_orientation_classify"`
	UseDocUnwarping           bool `json:"use_doc_unwarping"`
}

// OCRTextDetParams OCR文本检测参数
type OCRTextDetParams struct {
	LimitSideLen int     `json:"limit_side_len"`
	LimitType    string  `json:"limit_type"`
	Thresh       float64 `json:"thresh"`
	MaxSideLimit int     `json:"max_side_limit"`
	BoxThresh    float64 `json:"box_thresh"`
	UnclipRatio  float64 `json:"unclip_ratio"`
}

// OCRService OCR服务接口
type OCRService interface {
	// RecognizeText 识别图片中的文本
	RecognizeText(imageURL string, visualize bool) (*OCRResult, error)

	// RecognizeTextSimple 识别图片中的文本（简化版，只返回文本内容）
	RecognizeTextSimple(imageURL string, visualize bool) ([]string, error)

	// BatchRecognizeText 批量识别图片中的文本
	BatchRecognizeText(imageURLs []string, visualize bool) ([]*OCRResult, error)
}

// OCRServiceImpl OCR服务实现
type OCRServiceImpl struct {
	baseURL    string        // OCR服务基础URL
	timeout    time.Duration // 请求超时时间
	httpClient *http.Client  // HTTP客户端
	logger     *zap.Logger   // 日志记录器
}

// NewOCRService 创建OCR服务实例
func NewOCRService(baseURL string, timeout time.Duration, logger *zap.Logger) OCRService {
	// 创建优化的HTTP传输配置，支持更多并发请求
	transport := &http.Transport{
		MaxIdleConns:        100,              // 最大空闲连接数
		MaxConnsPerHost:     50,               // 每个主机最大连接数
		MaxIdleConnsPerHost: 20,               // 每个主机最大空闲连接数
		IdleConnTimeout:     90 * time.Second, // 空闲连接超时时间
		TLSHandshakeTimeout: 10 * time.Second, // TLS握手超时时间
		DisableKeepAlives:   false,            // 启用Keep-Alive
		DisableCompression:  false,            // 启用压缩
		ForceAttemptHTTP2:   false,            // 禁用HTTP/2以提高性能
	}

	return &OCRServiceImpl{
		baseURL: baseURL,
		timeout: timeout,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		logger: logger,
	}
}

// RecognizeText 识别图片中的文本
func (s *OCRServiceImpl) RecognizeText(imageURL string, visualize bool) (*OCRResult, error) {
	// 构造内部数据结构
	inputData := map[string]interface{}{
		"file":      imageURL,
		"visualize": visualize,
		"fileType":  1,
	}

	// 将数据转换为转义的JSON字符串
	dataJSON, err := json.Marshal(inputData)
	if err != nil {
		return nil, fmt.Errorf("ocr 序列化输入数据失败: %w", err)
	}

	// 构造请求体
	request := &OCRRequest{
		Inputs: []OCRInput{
			{
				Name:     "input",
				Shape:    []int{1, 1},
				Datatype: "BYTES",
				Data:     []string{string(dataJSON)},
			},
		},
		Outputs: []OCROutput{
			{
				Name: "output",
			},
		},
	}

	return s.makeOCRRequest(request)
}

// RecognizeTextSimple 识别图片中的文本（简化版，只返回文本内容）
func (s *OCRServiceImpl) RecognizeTextSimple(imageURL string, visualize bool) ([]string, error) {
	// 构造内部数据结构
	inputData := map[string]interface{}{
		"file":      imageURL,
		"visualize": visualize,
		"fileType":  1,
	}

	// 将数据转换为转义的JSON字符串
	dataJSON, err := json.Marshal(inputData)
	if err != nil {
		return nil, fmt.Errorf("序列化输入数据失败: %w", err)
	}

	// 构造请求体
	request := &OCRRequest{
		Inputs: []OCRInput{
			{
				Name:     "input",
				Shape:    []int{1, 1},
				Datatype: "BYTES",
				Data:     []string{string(dataJSON)},
			},
		},
		Outputs: []OCROutput{
			{
				Name: "output",
			},
		},
	}

	return s.makeOCRRequestSimple(request)
}

// BatchRecognizeText 批量识别图片中的文本
func (s *OCRServiceImpl) BatchRecognizeText(imageURLs []string, visualize bool) ([]*OCRResult, error) {
	var results []*OCRResult

	for _, imageURL := range imageURLs {
		result, err := s.RecognizeText(imageURL, visualize)
		if err != nil {
			s.logger.Error("批量OCR识别失败",
				zap.String("image_url", imageURL),
				zap.Error(err))
			return nil, fmt.Errorf("批量OCR识别失败: %w", err)
		}
		results = append(results, result)
	}

	return results, nil
}

// makeOCRRequest 发送OCR请求
func (s *OCRServiceImpl) makeOCRRequest(request *OCRRequest) (*OCRResult, error) {
	// 序列化请求体
	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("ocr 序列化请求体失败: %w", err)
	}

	// s.logger.Debug("发送OCR请求",
	// 	zap.String("url", s.baseURL+"/v2/models/ocr/infer"),
	// 	zap.String("request", string(requestBody)))

	// 创建HTTP请求
	req, err := http.NewRequest("POST", s.baseURL+"/v2/models/ocr/infer", bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("创建ocr HTTP请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ocr发送HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}

	s.logger.Debug("收到OCR响应",
		zap.Int("status_code", resp.StatusCode),
		zap.Int("response_size", len(responseBody)))

	// 检查HTTP状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OCR服务返回错误状态码: %d, 响应: %s", resp.StatusCode, string(responseBody))
	}

	// 解析响应
	var ocrResponse OCRResponse
	if err := json.Unmarshal(responseBody, &ocrResponse); err != nil {
		return nil, fmt.Errorf("解析OCR响应失败: %w", err)
	}

	// 检查是否有错误信息（直接在响应中的错误）
	if ocrResponse.Error != "" {
		return nil, fmt.Errorf("OCR服务返回错误: %s", ocrResponse.Error)
	}

	// 检查响应格式
	if len(ocrResponse.Outputs) == 0 || len(ocrResponse.Outputs[0].Data) == 0 {
		return nil, fmt.Errorf("OCR响应格式不正确")
	}

	// 解析结果数据
	var ocrResult OCRResult
	if err := json.Unmarshal([]byte(ocrResponse.Outputs[0].Data[0]), &ocrResult); err != nil {
		return nil, fmt.Errorf("解析OCR结果数据失败: %w", err)
	}

	// 检查业务错误码（在结果数据中的错误）
	if ocrResult.ErrorCode != 0 {
		errorMsg := ocrResult.ErrorMsg
		if errorMsg == "" {
			errorMsg = "未知错误"
		}
		return nil, fmt.Errorf("OCR识别失败, 错误码: %d, 错误信息: %s", ocrResult.ErrorCode, errorMsg)
	}

	return &ocrResult, nil
}

// makeOCRRequestSimple 发送OCR请求（简化版，只返回文本内容）
func (s *OCRServiceImpl) makeOCRRequestSimple(request *OCRRequest) ([]string, error) {
	// 序列化请求体
	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	s.logger.Debug("发送OCR请求（简化版）",
		zap.String("url", s.baseURL+"/v2/models/ocr/infer"),
		zap.Int("request", len(string(requestBody))))

	// 创建HTTP请求
	req, err := http.NewRequest("POST", s.baseURL+"/v2/models/ocr/infer", bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("创建HTTP请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ocr 发送HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ocr 读取响应体失败: %w", err)
	}

	s.logger.Debug("收到OCR响应（简化版）",
		zap.Int("status_code", resp.StatusCode),
		zap.Int("response_size", len(responseBody)))

	// 检查HTTP状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OCR服务返回错误状态码: %d, 响应: %s", resp.StatusCode, string(responseBody))
	}

	// 解析响应
	var ocrResponse OCRResponse
	if err := json.Unmarshal(responseBody, &ocrResponse); err != nil {
		return nil, fmt.Errorf("解析OCR响应失败: %w", err)
	}

	// 检查是否有错误信息（直接在响应中的错误）
	if ocrResponse.Error != "" {
		return nil, fmt.Errorf("OCR服务返回错误: %s", ocrResponse.Error)
	}

	// 检查响应格式
	if len(ocrResponse.Outputs) == 0 || len(ocrResponse.Outputs[0].Data) == 0 {
		return nil, fmt.Errorf("OCR响应格式不正确")
	}

	// 只解析rec_texts内容
	return s.parseRecTexts(ocrResponse.Outputs[0].Data[0])
}

// parseRecTexts 解析响应中的rec_texts内容
func (s *OCRServiceImpl) parseRecTexts(responseData string) ([]string, error) {
	// 解析JSON数据
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(responseData), &jsonData); err != nil {
		return nil, fmt.Errorf("解析响应数据失败: %w", err)
	}

	// 检查错误码
	if errorCode, ok := jsonData["errorCode"].(float64); ok && errorCode != 0 {
		errorMsg := "未知错误"
		if msg, ok := jsonData["errorMsg"].(string); ok {
			errorMsg = msg
		}
		return nil, fmt.Errorf("OCR识别失败, 错误码: %.0f, 错误信息: %s", errorCode, errorMsg)
	}

	// 解析result -> ocrResults -> prunedResult -> rec_texts
	result, ok := jsonData["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("无法解析result字段")
	}

	ocrResults, ok := result["ocrResults"].([]interface{})
	if !ok || len(ocrResults) == 0 {
		return nil, fmt.Errorf("无法解析ocrResults字段")
	}

	firstResult, ok := ocrResults[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("无法解析第一个ocrResult")
	}

	prunedResult, ok := firstResult["prunedResult"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("无法解析prunedResult字段")
	}

	recTexts, ok := prunedResult["rec_texts"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("无法解析rec_texts字段")
	}

	// 转换为字符串切片
	var texts []string
	for _, text := range recTexts {
		if str, ok := text.(string); ok {
			texts = append(texts, str)
		}
	}

	return texts, nil
}

// GetRecognizedTexts 获取识别的文本结果
func (r *OCRResult) GetRecognizedTexts() []string {
	if len(r.Result.OCRResults) == 0 {
		return []string{}
	}

	return r.Result.OCRResults[0].PrunedResult.RecTexts
}

// GetRecognizedTextsWithScores 获取识别的文本结果和置信度
func (r *OCRResult) GetRecognizedTextsWithScores() []OCRTextWithScore {
	if len(r.Result.OCRResults) == 0 {
		return []OCRTextWithScore{}
	}

	prunedResult := r.Result.OCRResults[0].PrunedResult
	results := make([]OCRTextWithScore, 0, len(prunedResult.RecTexts))

	for i, text := range prunedResult.RecTexts {
		score := 0.0
		if i < len(prunedResult.RecScores) {
			score = prunedResult.RecScores[i]
		}

		results = append(results, OCRTextWithScore{
			Text:  text,
			Score: score,
		})
	}

	return results
}

// OCRTextWithScore 带置信度的文本结果
type OCRTextWithScore struct {
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

// OCRRequestDTO OCR请求DTO
type OCRRequestDTO struct {
	ImageURL  string `json:"image_url" binding:"required"` // 图片URL
	Visualize bool   `json:"visualize"`                    // 是否可视化，默认false
}

// OCRBatchRequestDTO OCR批量请求DTO
type OCRBatchRequestDTO struct {
	ImageURLs []string `json:"image_urls" binding:"required"` // 图片URL列表
	Visualize bool     `json:"visualize"`                     // 是否可视化，默认false
}

// OCRResponseDTO OCR响应DTO
type OCRResponseDTO struct {
	Success bool             `json:"success"`
	Message string           `json:"message"`
	Data    *OCRResponseData `json:"data,omitempty"`
	Error   string           `json:"error,omitempty"`
}

// OCRBatchResponseDTO OCR批量响应DTO
type OCRBatchResponseDTO struct {
	Success bool               `json:"success"`
	Message string             `json:"message"`
	Data    []*OCRResponseData `json:"data,omitempty"`
	Error   string             `json:"error,omitempty"`
}

// OCRResponseData OCR响应数据
type OCRResponseData struct {
	Texts           []string           `json:"texts"`             // 识别的文本数组
	TextsWithScores []OCRTextWithScore `json:"texts_with_scores"` // 带置信度的文本结果
	LogId           string             `json:"log_id"`            // 日志ID
	DataInfo        *OCRDataInfo       `json:"data_info"`         // 数据信息
}

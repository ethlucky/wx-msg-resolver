package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// ImageInfo 图片信息结构
type ImageInfo struct {
	AesKey        string `json:"aeskey"`
	CdnBigImgURL  string `json:"cdnbigimgurl"`
	CdnThumbURL   string `json:"cdnthumburl"`
	CdnMidImgURL  string `json:"cdnmidimgurl"`
	Length        string `json:"length"`
	MD5           string `json:"md5"`
	Width         string `json:"width"`
	Height        string `json:"height"`
	ThumbWidth    string `json:"thumb_width"`
	ThumbHeight   string `json:"thumb_height"`
}

// ImgMsg 图片消息XML结构
type ImgMsg struct {
	XMLName         xml.Name `xml:"msg"`
	Img             ImgNode  `xml:"img"`
	PlatformSig     string   `xml:"platform_signature"`
	ImgDataHash     string   `xml:"imgdatahash"`
	ImgSourceInfo   ImgSourceInfo `xml:"ImgSourceInfo"`
}

// ImgNode 图片节点
type ImgNode struct {
	AesKey           string `xml:"aeskey,attr"`
	EncryVer         string `xml:"encryver,attr"`
	CdnThumbAesKey   string `xml:"cdnthumbaeskey,attr"`
	CdnThumbURL      string `xml:"cdnthumburl,attr"`
	CdnThumbLength   string `xml:"cdnthumblength,attr"`
	CdnThumbHeight   string `xml:"cdnthumbheight,attr"`
	CdnThumbWidth    string `xml:"cdnthumbwidth,attr"`
	CdnMidHeight     string `xml:"cdnmidheight,attr"`
	CdnMidWidth      string `xml:"cdnmidwidth,attr"`
	CdnHdHeight      string `xml:"cdnhdheight,attr"`
	CdnHdWidth       string `xml:"cdnhdwidth,attr"`
	CdnMidImgURL     string `xml:"cdnmidimgurl,attr"`
	Length           string `xml:"length,attr"`
	CdnBigImgURL     string `xml:"cdnbigimgurl,attr"`
	HdLength         string `xml:"hdlength,attr"`
	MD5              string `xml:"md5,attr"`
	SecHashInfoBase64 string `xml:"secHashInfoBase64"`
	Live             LiveInfo `xml:"live"`
}

// LiveInfo 直播信息
type LiveInfo struct {
	Duration            string `xml:"duration"`
	Size                string `xml:"size"`
	MD5                 string `xml:"md5"`
	FileId              string `xml:"fileid"`
	HdSize              string `xml:"hdsize"`
	HdMD5               string `xml:"hdmd5"`
	HdFileId            string `xml:"hdfileid"`
	StillImageTimeMs    string `xml:"stillimagetimems"`
}

// ImgSourceInfo 图片来源信息
type ImgSourceInfo struct {
	ImgSourceURL string `xml:"ImgSourceUrl"`
	BizType      string `xml:"BizType"`
}

// XMLParser XML解析器
type XMLParser struct {
	logger *zap.Logger
}

// NewXMLParser 创建XML解析器
func NewXMLParser(logger *zap.Logger) *XMLParser {
	return &XMLParser{
		logger: logger,
	}
}

// ParseImageContent 解析图片消息内容，提取aeskey和cdnbigimgurl
func (p *XMLParser) ParseImageContent(content string) (*ImageInfo, error) {
	// 清理内容，移除前后空白字符
	content = strings.TrimSpace(content)
	
	// 检查是否包含XML标签
	if !strings.Contains(content, "<msg>") || !strings.Contains(content, "<img") {
		return nil, fmt.Errorf("不是有效的图片消息XML内容")
	}

	// 解析XML
	var imgMsg ImgMsg
	err := xml.Unmarshal([]byte(content), &imgMsg)
	if err != nil {
		p.logger.Error("解析XML失败", zap.Error(err), zap.String("content", content))
		return nil, fmt.Errorf("解析XML失败: %w", err)
	}

	// 提取关键信息
	imageInfo := &ImageInfo{
		AesKey:        imgMsg.Img.AesKey,
		CdnBigImgURL:  imgMsg.Img.CdnBigImgURL,
		CdnThumbURL:   imgMsg.Img.CdnThumbURL,
		CdnMidImgURL:  imgMsg.Img.CdnMidImgURL,
		Length:        imgMsg.Img.Length,
		MD5:           imgMsg.Img.MD5,
		Width:         imgMsg.Img.CdnThumbWidth,
		Height:        imgMsg.Img.CdnThumbHeight,
		ThumbWidth:    imgMsg.Img.CdnThumbWidth,
		ThumbHeight:   imgMsg.Img.CdnThumbHeight,
	}

	p.logger.Info("成功解析图片消息内容",
		zap.String("aeskey", imageInfo.AesKey),
		zap.String("cdnbigimgurl", imageInfo.CdnBigImgURL),
		zap.String("md5", imageInfo.MD5),
		zap.String("length", imageInfo.Length))

	return imageInfo, nil
}

// ParseImageContentToJSON 解析图片消息内容并返回JSON字符串
func (p *XMLParser) ParseImageContentToJSON(content string) (string, error) {
	imageInfo, err := p.ParseImageContent(content)
	if err != nil {
		return "", err
	}

	// 转换为JSON字符串
	jsonBytes, err := json.Marshal(imageInfo)
	if err != nil {
		return "", fmt.Errorf("转换为JSON失败: %w", err)
	}

	return string(jsonBytes), nil
}

// IsImageMessage 检查内容是否为图片消息
func (p *XMLParser) IsImageMessage(content string) bool {
	content = strings.TrimSpace(content)
	// 检查是否包含XML标签，支持用户ID前缀
	if strings.Contains(content, "<?xml") || strings.Contains(content, "<msg>") {
		return strings.Contains(content, "<img")
	}
	return false
}

// ExtractAesKeyAndCdnURL 快速提取aeskey和cdnurl（不完整解析）
func (p *XMLParser) ExtractAesKeyAndCdnURL(content string) (string, string, error) {
	// 使用字符串查找方式快速提取关键信息
	var aesKey, cdnURL string
	
	// 提取aeskey
	if start := strings.Index(content, `aeskey="`); start != -1 {
		start += len(`aeskey="`)
		if end := strings.Index(content[start:], `"`); end != -1 {
			aesKey = content[start : start+end]
		}
	}
	
	// 按优先级提取CDN URL：优先cdnbigimgurl，然后cdnmidimgurl，最后cdnthumburl
	for _, urlType := range []string{"cdnbigimgurl", "cdnmidimgurl", "cdnthumburl"} {
		searchStr := urlType + `="`
		if start := strings.Index(content, searchStr); start != -1 {
			start += len(searchStr)
			if end := strings.Index(content[start:], `"`); end != -1 {
				cdnURL = content[start : start+end]
				break
			}
		}
	}
	
	if aesKey == "" {
		return "", "", fmt.Errorf("未找到aeskey")
	}
	
	if cdnURL == "" {
		return "", "", fmt.Errorf("未找到CDN URL")
	}
	
	return aesKey, cdnURL, nil
}

// ExtractAesKeyAndCdnDetails 提取aeskey和CDN详细信息，返回具体的字段名和值
func (p *XMLParser) ExtractAesKeyAndCdnDetails(content string) (string, string, string, error) {
	// 使用字符串查找方式快速提取关键信息
	var aesKey, cdnURL, cdnType string
	
	// 提取aeskey
	if start := strings.Index(content, `aeskey="`); start != -1 {
		start += len(`aeskey="`)
		if end := strings.Index(content[start:], `"`); end != -1 {
			aesKey = content[start : start+end]
		}
	}
	
	// 按优先级提取CDN URL：优先cdnbigimgurl，然后cdnmidimgurl，最后cdnthumburl
	for _, urlType := range []string{"cdnbigimgurl", "cdnmidimgurl", "cdnthumburl"} {
		searchStr := urlType + `="`
		if start := strings.Index(content, searchStr); start != -1 {
			start += len(searchStr)
			if end := strings.Index(content[start:], `"`); end != -1 {
				cdnURL = content[start : start+end]
				cdnType = urlType
				break
			}
		}
	}
	
	if aesKey == "" {
		return "", "", "", fmt.Errorf("未找到aeskey")
	}
	
	if cdnURL == "" {
		return "", "", "", fmt.Errorf("未找到CDN URL")
	}
	
	return aesKey, cdnURL, cdnType, nil
}
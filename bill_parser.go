package main

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// ParsedBillInfo 账单解析结果
type ParsedBillInfo struct {
	Remark string
	Dollar string
	Rate   string
	Amount string
}

// BillParser 账单解析器接口
type BillParser interface {
	// ParseBillFromContent 从消息内容中解析账单信息
	ParseBillFromContent(content string) (*ParsedBillInfo, error)
}

// DefaultBillParser 默认的账单解析器实现
type DefaultBillParser struct{}

// NewBillParser 创建新的账单解析器
func NewBillParser() BillParser {
	return &DefaultBillParser{}
}

// ParseBillFromContent 从消息内容中解析账单信息
func (p *DefaultBillParser) ParseBillFromContent(content string) (*ParsedBillInfo, error) {
	// 正则表达式：匹配 (任意内容+数字*数字) 或 (任意内容-数字*数字) 或 (+数字*数字) 或 (-数字*数字)
	// 解释：
	// - (.*)? 可选的任意内容作为备注
	// - [+\-] 匹配+或-号
	// - (\d+(?:\.\d+)?) 匹配数字（支持小数）作为dollar
	// - \* 匹配*号
	// - (\d+(?:\.\d+)?) 匹配数字（支持小数）作为rate
	pattern := `^(.*?)?([+\-])(\d+(?:\.\d+)?)\*(\d+(?:\.\d+)?).*?$`
	
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(strings.TrimSpace(content))
	
	if len(matches) != 5 {
		return nil, fmt.Errorf("消息格式不匹配账单格式")
	}

	remark := strings.TrimSpace(matches[1])
	sign := matches[2]
	dollarStr := matches[3]
	rateStr := matches[4]

	// 解析数字
	dollarFloat, err := strconv.ParseFloat(dollarStr, 64)
	if err != nil {
		return nil, fmt.Errorf("解析dollar失败: %v", err)
	}

	rateFloat, err := strconv.ParseFloat(rateStr, 64)
	if err != nil {
		return nil, fmt.Errorf("解析rate失败: %v", err)
	}

	// 计算amount
	var amount float64
	var amountStr string
	if sign == "+" {
		amount = dollarFloat * rateFloat
		// 保留2位小数，并保留+号
		amountRounded := math.Round(amount*100) / 100
		amountStr = fmt.Sprintf("+%.2f", amountRounded)
	} else {
		amount = -dollarFloat * rateFloat
		// 保留2位小数，负号会自动保留
		amountRounded := math.Round(amount*100) / 100
		amountStr = fmt.Sprintf("%.2f", amountRounded)
	}

	return &ParsedBillInfo{
		Remark: remark,
		Dollar: dollarStr,
		Rate:   rateStr,
		Amount: amountStr,
	}, nil
}
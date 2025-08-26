package main

import (
	"regexp"
	"strings"

	"go.uber.org/zap"
)

// OCRParseResult OCR解析结果结构
type OCRParseResult struct {
	Results []WalletCodePair `json:"results"` // 配对的钱包码和序列号列表
}

// WalletCodePair 钱包码和序列号配对
type WalletCodePair struct {
	WalletCode string `json:"walletCode"` // 充值码
	SerialNo   string `json:"serialNo"`   // 对应的序列号
}

// OCRContentParser OCR内容解析器接口
type OCRContentParser interface {
	// ParseContent 解析OCR识别的文本内容，提取充值码和序列号
	ParseContent(texts []string) *OCRParseResult
}

// OCRContentParserImpl OCR内容解析器实现
type OCRContentParserImpl struct {
	logger *zap.Logger
}

// NewOCRContentParser 创建OCR内容解析器实例
func NewOCRContentParser(logger *zap.Logger) OCRContentParser {
	return &OCRContentParserImpl{
		logger: logger,
	}
}

// ParseContent 解析OCR识别的文本内容，提取充值码和序列号
func (p *OCRContentParserImpl) ParseContent(texts []string) *OCRParseResult {
	result := &OCRParseResult{
		Results: []WalletCodePair{},
	}

	p.logger.Debug("开始解析OCR内容", zap.Strings("texts", texts))

	// 按照规则顺序解析：walletcode, serialNo, walletcode, serialNo, walletcode, serialNo
	// 其中serialNo一定在两个walletcode中间，最后一个walletcode对应的serialNo在其后面
	result.Results = p.parseWalletCodesAndSerialsInOrder(texts)

	p.logger.Info("OCR内容解析完成",
		zap.Int("results_count", len(result.Results)))

	for i, pair := range result.Results {
		p.logger.Debug("配对结果",
			zap.Int("index", i+1),
			zap.String("wallet_code", pair.WalletCode),
			zap.String("serial_no", pair.SerialNo))
	}

	return result
}

// extractWalletCode 提取充值码（17位，格式：XXXXX-XXXXX-XXXXX）
func (p *OCRContentParserImpl) extractWalletCode(text string) string {
	upperText := strings.ToUpper(text)

	// 首先尝试匹配已经格式化好的充值码（带连字符的精确格式）
	formattedPattern := regexp.MustCompile(`\b([A-Z0-9]{5}-[A-Z0-9]{5}-[A-Z0-9]{5})\b`)
	if match := formattedPattern.FindString(upperText); match != "" && len(match) == 17 {
		// 检查是否包含字母（充值码必须包含字母，不能全是数字）
		if regexp.MustCompile(`[A-Z]`).MatchString(match) {
			p.logger.Debug("提取到格式化充值码", zap.String("original", text), zap.String("extracted", match))
			return match
		}
	}

	// 其次尝试匹配没有连字符但可能有其他分隔符的15位字符
	noHyphenPattern := regexp.MustCompile(`\b([A-Z0-9]{5}[-_\s]?[A-Z0-9]{5}[-_\s]?[A-Z0-9]{5})\b`)
	matches := noHyphenPattern.FindAllString(upperText, -1)

	for _, match := range matches {
		// 移除所有分隔符并检查是否为15位字符
		cleanMatch := regexp.MustCompile(`[-_\s]`).ReplaceAllString(match, "")
		if len(cleanMatch) == 15 && regexp.MustCompile(`[A-Z]`).MatchString(cleanMatch) {
			// 格式化为标准格式：XXXXX-XXXXX-XXXXX
			formatted := cleanMatch[:5] + "-" + cleanMatch[5:10] + "-" + cleanMatch[10:]
			p.logger.Debug("提取到充值码", zap.String("original", text), zap.String("extracted", formatted))
			return formatted
		}
	}

	// 最后尝试匹配连续的15位字母数字组合（无分隔符）
	continuousPattern := regexp.MustCompile(`\b([A-Z0-9]{15})\b`)
	if match := continuousPattern.FindString(upperText); match != "" && len(match) == 15 {
		// 检查是否包含字母（充值码必须包含字母，不能全是数字）
		if regexp.MustCompile(`[A-Z]`).MatchString(match) {
			// 格式化为标准格式：XXXXX-XXXXX-XXXXX
			formatted := match[:5] + "-" + match[5:10] + "-" + match[10:]
			p.logger.Debug("提取到连续充值码", zap.String("original", text), zap.String("extracted", formatted))
			return formatted
		}
	}

	return ""
}

// extractSerialNumber 提取序列号（20-30位数字）
func (p *OCRContentParserImpl) extractSerialNumber(text string) string {
	// 序列号模式：连续的20-30位数字，使用单词边界确保精确匹配
	serialPattern := regexp.MustCompile(`\b(\d{20,30})\b`)

	matches := serialPattern.FindAllString(text, -1)

	for _, match := range matches {
		// 额外检查长度确保在范围内
		if len(match) >= 20 && len(match) <= 30 {
			p.logger.Debug("提取到序列号", zap.String("original", text), zap.String("extracted", match))
			return match
		}
	}

	// 尝试提取带有分隔符的数字序列（移除分隔符后检查长度）
	numbersWithSeparators := regexp.MustCompile(`[\d\s\-_]{25,35}`)
	potentialSerials := numbersWithSeparators.FindAllString(text, -1)

	for _, potential := range potentialSerials {
		// 移除所有非数字字符
		cleanSerial := regexp.MustCompile(`[^\d]`).ReplaceAllString(potential, "")
		if len(cleanSerial) >= 20 && len(cleanSerial) <= 30 {
			p.logger.Debug("提取到清理后的序列号", zap.String("original", text), zap.String("extracted", cleanSerial))
			return cleanSerial
		}
	}

	return ""
}

// parseWalletCodesAndSerialsInOrder 按照规则顺序解析钱包码和序列号
func (p *OCRContentParserImpl) parseWalletCodesAndSerialsInOrder(texts []string) []WalletCodePair {
	var pairs []WalletCodePair

	// 合并所有文本内容
	allText := strings.Join(texts, " ")

	// 按位置顺序找到所有可能的钱包码和序列号
	items := p.findAllItemsInOrder(allText)

	p.logger.Debug("找到的所有项目", zap.Any("items", items))

	// 按照规则配对
	// 规则：walletcode, serialNo, walletcode, serialNo, walletcode, serialNo
	// serialNo一定在两个walletcode中间，最后一个walletcode对应的serialNo在其后面

	var walletCodes []ItemPosition
	var serialNos []ItemPosition

	// 分类钱包码和序列号
	for _, item := range items {
		if item.Type == "wallet" {
			walletCodes = append(walletCodes, item)
		} else if item.Type == "serial" {
			serialNos = append(serialNos, item)
		}
	}

	if len(walletCodes) == 0 {
		return pairs
	}

	// 按照规则配对
	for i, wallet := range walletCodes {
		walletCode := wallet.Value
		var serialNo string

		if i < len(walletCodes)-1 {
			// 不是最后一个钱包码，找到它和下一个钱包码之间的序列号
			nextWallet := walletCodes[i+1]
			serialNo = p.findSerialBetweenWallets(serialNos, wallet.Position, nextWallet.Position)
		} else {
			// 最后一个钱包码，找到它之后的第一个序列号
			serialNo = p.findSerialAfterWallet(serialNos, wallet.Position)
		}

		if serialNo != "" {
			pairs = append(pairs, WalletCodePair{
				WalletCode: walletCode,
				SerialNo:   serialNo,
			})
			p.logger.Debug("配对成功",
				zap.Int("wallet_index", i+1),
				zap.String("wallet_code", walletCode),
				zap.String("serial_no", serialNo))
		}
	}

	return pairs
}

// ItemPosition 项目位置结构体
type ItemPosition struct {
	Type     string // 类型："wallet" 或 "serial"
	Value    string // 文本值
	Position int    // 在合并文本中的位置
}

// TextPosition 文本位置结构体（保持向后兼容）
type TextPosition struct {
	Value     string // 文本值
	Position  int    // 在合并文本中的位置
	TextIndex int    // 在原始texts数组中的索引
}

// findPositionsInTexts 在文本中找到所有项目的位置
func (p *OCRContentParserImpl) findPositionsInTexts(texts []string, items []string, isWallet bool) []TextPosition {
	var positions []TextPosition
	allText := strings.Join(texts, " ")
	upperAllText := strings.ToUpper(allText)

	for _, item := range items {
		upperItem := strings.ToUpper(item)
		index := strings.Index(upperAllText, upperItem)
		if index != -1 {
			positions = append(positions, TextPosition{
				Value:    item,
				Position: index,
			})
		}
	}

	// 按位置排序
	for i := 0; i < len(positions)-1; i++ {
		for j := i + 1; j < len(positions); j++ {
			if positions[i].Position > positions[j].Position {
				positions[i], positions[j] = positions[j], positions[i]
			}
		}
	}

	return positions
}

// findSerialBetweenPositions 找到两个位置之间的序列号
func (p *OCRContentParserImpl) findSerialBetweenPositions(serialPositions []TextPosition, startPos, endPos int) string {
	for _, serialPos := range serialPositions {
		if serialPos.Position > startPos && serialPos.Position < endPos {
			return serialPos.Value
		}
	}
	return ""
}

// findSerialAfterPosition 找到位置之后的第一个序列号
func (p *OCRContentParserImpl) findSerialAfterPosition(serialPositions []TextPosition, pos int) string {
	for _, serialPos := range serialPositions {
		if serialPos.Position > pos {
			return serialPos.Value
		}
	}
	return ""
}

// contains 检查切片是否包含指定的字符串
func (p *OCRContentParserImpl) contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// findAllItemsInOrder 按位置顺序找到所有钱包码和序列号
func (p *OCRContentParserImpl) findAllItemsInOrder(text string) []ItemPosition {
	var items []ItemPosition

	upperText := strings.ToUpper(text)

	// 找到所有钱包码位置
	walletPattern := regexp.MustCompile(`\b([A-Z0-9]{5}[-_\s]?[A-Z0-9]{5}[-_\s]?[A-Z0-9]{5})\b`)
	walletMatches := walletPattern.FindAllStringIndex(upperText, -1)

	for _, match := range walletMatches {
		matchStr := upperText[match[0]:match[1]]
		// 清理并格式化钱包码
		cleanMatch := regexp.MustCompile(`[-_\s]`).ReplaceAllString(matchStr, "")
		if len(cleanMatch) == 15 && regexp.MustCompile(`[A-Z]`).MatchString(cleanMatch) {
			formatted := cleanMatch[:5] + "-" + cleanMatch[5:10] + "-" + cleanMatch[10:]
			items = append(items, ItemPosition{
				Type:     "wallet",
				Value:    formatted,
				Position: match[0],
			})
		}
	}

	// 找到所有序列号位置
	serialPattern := regexp.MustCompile(`\b(\d{20,30})\b`)
	serialMatches := serialPattern.FindAllStringIndex(text, -1)

	for _, match := range serialMatches {
		matchStr := text[match[0]:match[1]]
		if len(matchStr) >= 20 && len(matchStr) <= 30 {
			items = append(items, ItemPosition{
				Type:     "serial",
				Value:    matchStr,
				Position: match[0],
			})
		}
	}

	// 按位置排序
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].Position > items[j].Position {
				items[i], items[j] = items[j], items[i]
			}
		}
	}

	return items
}

// findSerialBetweenWallets 找到两个钱包码之间的序列号
func (p *OCRContentParserImpl) findSerialBetweenWallets(serialNos []ItemPosition, startPos, endPos int) string {
	for _, serial := range serialNos {
		if serial.Position > startPos && serial.Position < endPos {
			return serial.Value
		}
	}
	return ""
}

// findSerialAfterWallet 找到钱包码之后的第一个序列号
func (p *OCRContentParserImpl) findSerialAfterWallet(serialNos []ItemPosition, pos int) string {
	for _, serial := range serialNos {
		if serial.Position > pos {
			return serial.Value
		}
	}
	return ""
}

// ValidateParseResult 验证解析结果
func (p *OCRContentParserImpl) ValidateParseResult(result *OCRParseResult) bool {
	isValid := true

	walletCodePattern := regexp.MustCompile(`^[A-Z0-9]{5}-[A-Z0-9]{5}-[A-Z0-9]{5}$`)
	serialPattern := regexp.MustCompile(`^\d{20,30}$`)

	// 验证配对结果
	for _, pair := range result.Results {
		if pair.WalletCode != "" {
			if !walletCodePattern.MatchString(pair.WalletCode) || len(pair.WalletCode) != 17 {
				p.logger.Warn("配对中的充值码格式不正确", zap.String("walletCode", pair.WalletCode))
				isValid = false
			}
		}
		if pair.SerialNo != "" {
			if !serialPattern.MatchString(pair.SerialNo) {
				p.logger.Warn("配对中的序列号格式不正确", zap.String("serialNo", pair.SerialNo))
				isValid = false
			}
		}
	}

	return isValid
}

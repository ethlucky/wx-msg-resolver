package main

import (
	"testing"

	"go.uber.org/zap"
)

func TestOCRContentParser(t *testing.T) {
	// 创建测试用的logger
	logger := zap.NewNop()
	parser := NewOCRContentParser(logger)

	tests := []struct {
		name         string
		texts        []string
		expectedPairs []WalletCodePair
	}{
		{
			name: "Steam Card Sample Data - No Valid Serials",
			texts: []string{
				"tocompleteyourpurchase",
				"SYSTEMREQUIREMENTS:",
				"Please check the Steam store page for the most up-to-dateSystem Requirements",
				"Terms and Conditions:",
				"Use of this cardis subject to the Steam Subscriber Agreement,available at",
				"www.steampowered.com/subscriber_agreement.Cardisnotredeemable for cash or",
				"credit.You must activate thisproductvia the Internet byregistering foraSteam account",
				"shouldretumthiscardtoyourretailerinaccordancewiththeirreturnpolicy.@2017Valve",
				"STEAM",
				"PDE:",
				"FPD3G-B32E7-0CHI0",
				"79936601027", // 太短，不是有效序列号
				"14299728570", // 太短，不是有效序列号
				"ro* isa registi",
				"MXRFJ-INEAX-AMKJ8",
				"POWERED",
				"STEAM",
				"14299366909", // 太短，不是有效序列号
				"06/1>",
				"0.0 082208286002",
				"3601",
				"VMEMBER101-*#7539",
				"THANK YOU,",
				"Hanuel",
				"447496",
				"3728",
				"SUBTOTAL",
				"N",
				"V",
				"CASH.TEND",
				"TOTAL",
				"CHANGE DUE",
				"Download the San's Club app &make",
				"5.04",
				"more.Visit SansClub.con/ShopEasy.",
			},
			expectedPairs: []WalletCodePair{}, // 没有有效序列号，无法配对
		},
		{
			name:  "Standard Wallet Code Format - No Serial",
			texts: []string{"393NI-ZFQIG-NT5JT"},
			expectedPairs: []WalletCodePair{}, // 只有钱包码，没有序列号，无法配对
		},
		{
			name:  "Standard Serial Number - No Wallet",
			texts: []string{"799366010276058120064274902355"},
			expectedPairs: []WalletCodePair{}, // 只有序列号，没有钱包码，无法配对
		},
		{
			name:  "Perfect Pairing - Wallet Serial Wallet Serial",
			texts: []string{"ABC12-DEF34-GH567", "901234567890123456222222", "XYZ89-PQR12-STU34", "555666777888999000111222"},
			expectedPairs: []WalletCodePair{
				{WalletCode: "ABC12-DEF34-GH567", SerialNo: "901234567890123456222222"},
				{WalletCode: "XYZ89-PQR12-STU34", SerialNo: "555666777888999000111222"},
			},
		},
		{
			name:  "Rule Test - SerialNo Between Wallets with Other Text",
			texts: []string{
				"Some prefix text",
				"ABC12-DEF34-GH567", // Wallet 1
				"random text here",
				"901234567890123456222222", // Serial 1 (for Wallet 1)
				"more random content",
				"XYZ89-PQR12-STU34", // Wallet 2  
				"other stuff",
				"555666777888999000111222", // Serial 2 (for Wallet 2)
				"DEF99-GHI88-JKL77", // Wallet 3 (last one)
				"final text",
				"333444555666777888999000", // Serial 3 (for Wallet 3, after last wallet)
				"end text",
			},
			expectedPairs: []WalletCodePair{
				{WalletCode: "ABC12-DEF34-GH567", SerialNo: "901234567890123456222222"},
				{WalletCode: "XYZ89-PQR12-STU34", SerialNo: "555666777888999000111222"},
				{WalletCode: "DEF99-GHI88-JKL77", SerialNo: "333444555666777888999000"},
			},
		},
		{
			name:  "Wallet Code Without Hyphens - No Serial",
			texts: []string{"ABC12DEF34GH567"},
			expectedPairs: []WalletCodePair{}, // 只有钱包码，没有序列号，无法配对
		},
		{
			name:  "Multiple Wallets and Serials with Interleaved Text",
			texts: []string{"FPD3G-B32E7-0CHI0", "some random text", "12345678901234567890123456", "more text here", "MXRFJ-INEAX-AMKJ8", "extra content", "98765432109876543210987654"},
			expectedPairs: []WalletCodePair{
				{WalletCode: "FPD3G-B32E7-0CHI0", SerialNo: "12345678901234567890123456"},
				{WalletCode: "MXRFJ-INEAX-AMKJ8", SerialNo: "98765432109876543210987654"},
			},
		},
		{
			name:  "Empty Input",
			texts: []string{},
			expectedPairs: []WalletCodePair{},
		},
		{
			name:  "No Matches",
			texts: []string{"Some random text", "123", "ABCDEF"},
			expectedPairs: []WalletCodePair{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.ParseContent(tt.texts)

			// 检查配对结果
			if len(result.Results) != len(tt.expectedPairs) {
				t.Errorf("Results count = %d, expected %d", len(result.Results), len(tt.expectedPairs))
			} else {
				for i, expected := range tt.expectedPairs {
					if i < len(result.Results) {
						actual := result.Results[i]
						if actual.WalletCode != expected.WalletCode || actual.SerialNo != expected.SerialNo {
							t.Errorf("Results[%d] = {WalletCode: %q, SerialNo: %q}, expected {WalletCode: %q, SerialNo: %q}", 
								i, actual.WalletCode, actual.SerialNo, expected.WalletCode, expected.SerialNo)
						}
					}
				}
			}

			// Print debug info for failed cases
			if len(result.Results) != len(tt.expectedPairs) {
				t.Logf("Debug info for test %s:", tt.name)
				t.Logf("Input texts: %v", tt.texts)
				t.Logf("Got Results: %v, expected: %v", result.Results, tt.expectedPairs)
			}
		})
	}
}

func TestWalletCodeExtraction(t *testing.T) {
	logger := zap.NewNop()
	parser := &OCRContentParserImpl{logger: logger}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Standard Format",
			input:    "FPD3G-B32E7-0CHI0",
			expected: "FPD3G-B32E7-0CHI0",
		},
		{
			name:     "Another Standard Format",
			input:    "MXRFJ-INEAX-AMKJ8",
			expected: "MXRFJ-INEAX-AMKJ8",
		},
		{
			name:     "Original Example Format",
			input:    "393NI-ZFQIG-NT5JT",
			expected: "393NI-ZFQIG-NT5JT",
		},
		{
			name:     "Without Hyphens",
			input:    "FPD3GB32E70CHI0",
			expected: "FPD3G-B32E7-0CHI0",
		},
		{
			name:     "Mixed Text",
			input:    "Some text FPD3G-B32E7-0CHI0 more text",
			expected: "FPD3G-B32E7-0CHI0",
		},
		{
			name:     "Lowercase",
			input:    "fpd3g-b32e7-0chi0",
			expected: "FPD3G-B32E7-0CHI0",
		},
		{
			name:     "No Match",
			input:    "Some random text",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.extractWalletCode(tt.input)
			if result != tt.expected {
				t.Errorf("extractWalletCode(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSerialNumberExtraction(t *testing.T) {
	logger := zap.NewNop()
	parser := &OCRContentParserImpl{logger: logger}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Original Example",
			input:    "799366010276058120064274902355",
			expected: "799366010276058120064274902355",
		},
		{
			name:     "Steam Sample Short",
			input:    "79936601027",
			expected: "", // Too short (11 digits, need 20-30)
		},
		{
			name:     "Steam Sample Long",
			input:    "14299728570",
			expected: "", // Too short (11 digits, need 20-30)
		},
		{
			name:     "20 digits exactly",
			input:    "12345678901234567890",
			expected: "12345678901234567890",
		},
		{
			name:     "30 digits exactly",
			input:    "123456789012345678901234567890",
			expected: "123456789012345678901234567890",
		},
		{
			name:     "31 digits (too long)",
			input:    "1234567890123456789012345678901",
			expected: "", // Too long
		},
		{
			name:     "19 digits (too short)",
			input:    "1234567890123456789",
			expected: "",
		},
		{
			name:     "Mixed Text",
			input:    "Some text 12345678901234567890 more text",
			expected: "12345678901234567890",
		},
		{
			name:     "No Match",
			input:    "Some random text",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.extractSerialNumber(tt.input)
			if result != tt.expected {
				t.Errorf("extractSerialNumber(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}
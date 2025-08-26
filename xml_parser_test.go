package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestXMLParser_ExtractAesKeyAndCdnURL(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	parser := NewXMLParser(logger)

	// 测试数据 - 来自用户提供的XML内容
	testXML := `<?xml version="1.0"?>
<msg>
	<img aeskey="ebb4a8a82cbb20c429ead1f3e5d353d5" encryver="1" cdnthumbaeskey="ebb4a8a82cbb20c429ead1f3e5d353d5" cdnthumburl="3057020100044b304902010002045812196002032f57ed02045388e67202046878a720042464623133663637612d386632332d343965322d626364332d3463323732326264353837320204052438010201000405004c50b900" cdnthumblength="9576" cdnthumbheight="180" cdnthumbwidth="159" cdnmidheight="0" cdnmidwidth="0" cdnhdheight="0" cdnhdwidth="0" cdnmidimgurl="3057020100044b304902010002045812196002032f57ed02045388e67202046878a720042464623133663637612d386632332d343965322d626364332d3463323732326264353837320204052438010201000405004c50b900" length="79031" cdnbigimgurl="3057020100044b304902010002045812196002032f57ed02045388e67202046878a720042464623133663637612d386632332d343965322d626364332d3463323732326264353837320204052438010201000405004c50b900" hdlength="79031" md5="4237333c9b5b36ac56437777f06ff9bc">
		<secHashInfoBase64 />
		<live>
			<duration>0</duration>
			<size>0</size>
			<md5 />
			<fileid />
			<hdsize>0</hdsize>
			<hdmd5 />
			<hdfileid />
			<stillimagetimems>0</stillimagetimems>
		</live>
	</img>
	<platform_signature />
	<imgdatahash />
	<ImgSourceInfo>
		<ImgSourceUrl />
		<BizType>0</BizType>
	</ImgSourceInfo>
</msg>`

	t.Run("ExtractAesKeyAndCdnURL_Success", func(t *testing.T) {
		aeskey, cdnbigimgurl, err := parser.ExtractAesKeyAndCdnURL(testXML)
		
		assert.NoError(t, err)
		assert.Equal(t, "ebb4a8a82cbb20c429ead1f3e5d353d5", aeskey)
		assert.Equal(t, "3057020100044b304902010002045812196002032f57ed02045388e67202046878a720042464623133663637612d386632332d343965322d626364332d3463323732326264353837320204052438010201000405004c50b900", cdnbigimgurl)
		
		t.Logf("提取的aeskey: %s", aeskey)
		t.Logf("提取的cdnbigimgurl: %s", cdnbigimgurl)
	})

	t.Run("ExtractAesKeyAndCdnURL_InvalidXML", func(t *testing.T) {
		invalidXML := "这不是XML内容"
		
		aeskey, cdnbigimgurl, err := parser.ExtractAesKeyAndCdnURL(invalidXML)
		
		assert.Error(t, err)
		assert.Empty(t, aeskey)
		assert.Empty(t, cdnbigimgurl)
	})

	t.Run("ExtractAesKeyAndCdnURL_MissingFields", func(t *testing.T) {
		missingFieldsXML := `<?xml version="1.0"?>
<msg>
	<img encryver="1">
		<secHashInfoBase64 />
	</img>
</msg>`
		
		aeskey, cdnbigimgurl, err := parser.ExtractAesKeyAndCdnURL(missingFieldsXML)
		
		assert.Error(t, err)
		assert.Empty(t, aeskey)
		assert.Empty(t, cdnbigimgurl)
	})
}

func TestXMLParser_IsImageMessage(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	parser := NewXMLParser(logger)

	testCases := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "ValidImageMessage",
			content:  `<?xml version="1.0"?><msg><img aeskey="test"></img></msg>`,
			expected: true,
		},
		{
			name:     "InvalidImageMessage",
			content:  "这是普通文本消息",
			expected: false,
		},
		{
			name:     "XMLButNotImageMessage",
			content:  `<?xml version="1.0"?><msg><text>hello</text></msg>`,
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parser.IsImageMessage(tc.content)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestXMLParser_ParseImageContentToJSON(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	parser := NewXMLParser(logger)

	testXML := `<?xml version="1.0"?>
<msg>
	<img aeskey="ebb4a8a82cbb20c429ead1f3e5d353d5" cdnbigimgurl="3057020100044b304902010002045812196002032f57ed02045388e67202046878a720042464623133663637612d386632332d343965322d626364332d3463323732326264353837320204052438010201000405004c50b900" md5="4237333c9b5b36ac56437777f06ff9bc" length="79031">
	</img>
</msg>`

	t.Run("ParseImageContentToJSON_Success", func(t *testing.T) {
		jsonStr, err := parser.ParseImageContentToJSON(testXML)
		
		assert.NoError(t, err)
		assert.NotEmpty(t, jsonStr)
		
		// 验证JSON包含必要的字段
		assert.Contains(t, jsonStr, "aeskey")
		assert.Contains(t, jsonStr, "cdnbigimgurl")
		assert.Contains(t, jsonStr, "md5")
		assert.Contains(t, jsonStr, "length")
		
		t.Logf("生成的JSON: %s", jsonStr)
	})
}
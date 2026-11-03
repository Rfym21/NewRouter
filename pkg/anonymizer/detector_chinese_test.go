package anonymizer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChineseStructuredDetectionMasksIdentifiersAndLabeledNames(t *testing.T) {
	const text = "姓名：张三，手机号：13800138000，身份证：11010519491231002X，地址：北京市海淀区中关村一号楼  "
	matches := Detect(text)
	require.Len(t, matches, 4)
	byType := make(map[string]string)
	for _, match := range matches {
		byType[match.EntityType] = match.Text
		assert.Equal(t, match.Text, text[match.Start:match.End])
	}
	assert.Equal(t, "张三", byType["PERSON"])
	assert.Equal(t, "13800138000", byType["PHONE_NUMBER"])
	assert.Equal(t, "11010519491231002X", byType["CN_ID_CARD"])
	assert.Equal(t, "北京市海淀区中关村一号楼", byType["ADDRESS"])
	session := NewSession("chinese", NewVault())
	masked := session.AnonymizeString(text)
	for _, private := range byType {
		assert.NotContains(t, masked, private)
	}
	assert.Equal(t, text, session.DeanonymizeString(masked))
}

func TestChineseIdentifierValidationAndNumericUsageContexts(t *testing.T) {
	for _, id := range []string{"11010519491231002X", "11010519491231002x"} {
		assert.True(t, validChineseID(id))
	}
	for _, id := range []string{"110105194912310021", "00000019491231002X", "11010519490231002X", "11010529991231002X", "11010519491231000X", "123456", "123456789012345678"} {
		assert.False(t, validChineseID(id), id)
	}
	for _, text := range []string{"input_tokens: 13800138000", `"total_tokens": 13800138000`, "token_count=13800138000", "数量：13800138000"} {
		for _, match := range Detect(text) {
			assert.NotEqual(t, "PHONE_NUMBER", match.EntityType, text)
		}
	}
	assert.Empty(t, Detect("张三介绍了北京市的一般情况"), "unlabeled prose is not guessed to be a name or address")
	matches := Detect(`{"姓名":"张三","地址":"北京市海淀区中关村一号楼"}`)
	require.Len(t, matches, 2)
}

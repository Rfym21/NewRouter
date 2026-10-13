package service

import (
	"errors"
	"strings"

	"new-router/relaykit/dto"
	"new-router/setting"
	"new-router/types"
)

func CheckSensitiveMessages(messages []dto.Message) ([]string, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	for _, message := range messages {
		arrayContent := message.ParseContent()
		for _, m := range arrayContent {
			if m.Type == "image_url" {
				// TODO: check image url
				continue
			}
			// 检查 text 是否为空
			if m.Text == "" {
				continue
			}
			if ok, words := SensitiveWordContains(m.Text); ok {
				return words, errors.New("sensitive words detected")
			}
		}
	}
	return nil, nil
}

func CheckSensitiveText(text string) (bool, []string) {
	return CheckSensitiveTextWithWords(text, setting.SensitiveWords)
}

// CheckSensitiveTextWithWords scans text with the effective word list selected for the channel.
func CheckSensitiveTextWithWords(text string, words []string) (bool, []string) {
	if len(words) == 0 || len(text) == 0 {
		return false, nil
	}
	return AcSearch(strings.ToLower(text), words, true)
}

// CheckSensitiveTokenMetaWithWords scans request text with the effective channel word list.
func CheckSensitiveTokenMetaWithWords(meta *types.TokenCountMeta, words []string) (bool, []string) {
	if meta == nil || len(words) == 0 {
		return false, nil
	}
	return CheckSensitiveTextWithWords(meta.CombineText, words)
}

// SensitiveWordContains 是否包含敏感词，返回是否包含敏感词和敏感词列表
func SensitiveWordContains(text string) (bool, []string) {
	return CheckSensitiveTextWithWords(text, setting.SensitiveWords)
}

// SensitiveWordReplace 敏感词替换，返回是否包含敏感词和替换后的文本
func SensitiveWordReplace(text string, returnImmediately bool) (bool, []string, string) {
	if len(setting.SensitiveWords) == 0 {
		return false, nil, text
	}
	checkText := strings.ToLower(text)
	m := getOrBuildAC(setting.SensitiveWords)
	hits := m.MultiPatternSearch([]rune(checkText), returnImmediately)
	if len(hits) > 0 {
		words := make([]string, 0, len(hits))
		var builder strings.Builder
		builder.Grow(len(text))
		lastPos := 0

		for _, hit := range hits {
			pos := hit.Pos
			word := string(hit.Word)
			builder.WriteString(text[lastPos:pos])
			builder.WriteString("**###**")
			lastPos = pos + len(word)
			words = append(words, word)
		}
		builder.WriteString(text[lastPos:])
		return true, words, builder.String()
	}
	return false, nil, text
}

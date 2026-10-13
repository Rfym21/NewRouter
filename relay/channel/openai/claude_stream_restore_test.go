package openai

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"new-router/constant"
	"new-router/pkg/anonymizer"
	"new-router/relaykit/types"
)

func TestResponsesToClaudeRestoresSplitSurrogateAndClientModel(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	vault := anonymizer.NewVault()
	mapping := vault.GetOrCreate("migration", "alice@company.example", "EMAIL")
	middle := len(mapping.Surrogate) / 2
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test"}}`,
		fmt.Sprintf(`data: {"type":"response.output_text.delta","delta":%q}`, mapping.Surrogate[:middle]),
		fmt.Sprintf(`data: {"type":"response.output_text.delta","delta":%q}`, mapping.Surrogate[middle:]),
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`,
		`data: [DONE]`, "",
	}, "\n")
	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)
	info.RelayFormat = types.RelayFormatClaude
	info.OriginModelName = "client-model"
	info.ChannelSetting.UnifyRequestResponseModelNameEnabled = true
	anonymizer.Attach(c, anonymizer.NewSession("migration", vault))
	_, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	var content strings.Builder
	for line := range strings.SplitSeq(recorder.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if gjson.Get(data, "type").String() == "content_block_delta" {
			content.WriteString(gjson.Get(data, "delta.text").String())
		}
	}
	require.Equal(t, mapping.Original, content.String())
	require.Contains(t, recorder.Body.String(), `"model":"client-model"`)
	require.NotContains(t, recorder.Body.String(), mapping.Surrogate)
}

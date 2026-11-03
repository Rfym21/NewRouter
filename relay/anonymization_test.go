package relay

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/common"
	"new-router/pkg/anonymizer"
	relaycommon "new-router/relay/common"
	relayconstant "new-router/relay/constant"
	"new-router/relaykit/dto"
	"new-router/relaykit/types"
)

func TestAnonymizationDispatchUsesActualUpstreamFormat(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format types.RelayFormat
		mode   int
		body   string
	}{
		{"responses converted to Claude", types.RelayFormatClaude, relayconstant.RelayModeResponses, `{"messages":[{"role":"user","content":[{"type":"text","text":"alice@private.example"}]}]}`},
		{"responses converted to Gemini", types.RelayFormatGemini, relayconstant.RelayModeResponses, `{"contents":[{"parts":[{"text":"alice@private.example"}]}]}`},
		{"responses converted to Chat", types.RelayFormatOpenAI, relayconstant.RelayModeResponses, `{"messages":[{"role":"assistant","content":"alice@private.example"}]}`},
		{"legacy completion", types.RelayFormatOpenAI, relayconstant.RelayModeCompletions, `{"prompt":"alice@private.example"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			info := &relaycommon.RelayInfo{UserId: 1, TokenId: 2, RelayMode: tc.mode, ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{AnonymizationEnabled: common.GetPointer(true)}}}
			body, err := anonymizeRelayRequestJSON(c, info, tc.format, []byte(tc.body))
			require.NoError(t, err)
			assert.NotContains(t, string(body), "alice@private.example")
			session := anonymizer.FromContext(c)
			require.NotNil(t, session)
			assert.Equal(t, "alice@private.example", session.Snapshot()[0].Original)
			info.ChannelSetting.AnonymizationEnabled = common.GetPointer(false)
			info.ChannelId++
			unmodified, err := anonymizeRelayRequestJSON(c, info, tc.format, []byte(tc.body))
			require.NoError(t, err)
			assert.JSONEq(t, tc.body, string(unmodified))
			assert.Nil(t, anonymizer.FromContext(c), "a disabled retry cannot reuse another attempt's restoration context")
		})
	}
}

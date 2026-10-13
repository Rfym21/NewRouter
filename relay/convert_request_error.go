package relay

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	relaycommon "new-router/relay/common"
	kitreasoning "new-router/relaykit/relayconvert/reasoning"
	"new-router/relaykit/types"
)

func newConvertRequestFailedError(c *gin.Context, info *relaycommon.RelayInfo, err error) *types.NewAPIError {
	var loss *types.ConversionLossError
	if errors.As(err, &loss) {
		info.RecordConversionDiagnostics(c, loss.Diagnostics)
		return types.NewErrorWithStatusCode(err, types.ErrorCodeConvertRequestFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if kitreasoning.IsClientError(err) {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeConvertRequestFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
}

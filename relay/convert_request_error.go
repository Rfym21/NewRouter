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
	// A request plugin's rejection or failure keeps its own status and code.
	var pluginErr *types.NewAPIError
	if errors.As(err, &pluginErr) && (pluginErr.GetErrorCode() == types.ErrorCodeRequestRejected || pluginErr.GetErrorCode() == types.ErrorCodeRequestPluginFailed) {
		return pluginErr
	}
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

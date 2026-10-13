package controller

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"new-router/common"
	"new-router/model"
	"new-router/service"
)

func TestChannelExportProofBindsSelectionAndRejectsReplay(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleRootUser).Error)
	require.NoError(t, model.PublishUserAuthCache(user.Id))
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}))
	channel := model.Channel{Type: 1, Name: "exported-channel", Key: "sensitive-export-key", Models: "test", Group: "default"}
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, model.DB.Create(&model.TwoFA{UserId: user.Id, Secret: "JBSWY3DPEHPK3PXP", IsEnabled: true}).Error)
	operationContext, err := common.Marshal(service.ChannelKeyExportContext{ChannelIDs: []int{channel.Id}})
	require.NoError(t, err)
	proof := issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: service.VerificationScopeChannelKeyExport, Context: operationContext}, service.VerificationMethodTwoFA)
	requestBody, err := common.Marshal(ExportChannelsRequest{Ids: []int{channel.Id}})
	require.NoError(t, err)
	handler := func(c *gin.Context) { c.Set("role", common.RoleRootUser); ExportChannels(c) }
	missing := securityEnrollmentRequest(http.MethodPost, "/api/channel/export", string(requestBody), "", identity, handler)
	assert.Equal(t, http.StatusForbidden, missing.Code)
	assert.Contains(t, missing.Body.String(), "SECURITY_PROOF_REQUIRED")
	mismatched := securityEnrollmentRequest(http.MethodPost, "/api/channel/export", `{"ids":[]}`, proof, identity, handler)
	assert.Equal(t, http.StatusForbidden, mismatched.Code)
	assert.Contains(t, mismatched.Body.String(), "SECURITY_PROOF_CONTEXT_MISMATCH")
	accepted := securityEnrollmentRequest(http.MethodPost, "/api/channel/export", string(requestBody), proof, identity, handler)
	assert.Equal(t, http.StatusOK, accepted.Code)
	assert.Contains(t, accepted.Body.String(), "sensitive-export-key")
	repeated := securityEnrollmentRequest(http.MethodPost, "/api/channel/export", string(requestBody), proof, identity, handler)
	assert.Equal(t, http.StatusForbidden, repeated.Code)
	assert.Contains(t, repeated.Body.String(), "SECURITY_PROOF_CONSUMED")
	assert.NotContains(t, repeated.Body.String(), "sensitive-export-key")
}

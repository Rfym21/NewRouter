package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"new-router/setting"
)

func TestRequestPolicyNeedsManagedRebuild(t *testing.T) {
	assert.True(t, requestPolicyNeedsManagedRebuild(map[string]string{"SensitiveWords": "word"}))
	assert.True(t, requestPolicyNeedsManagedRebuild(map[string]string{"AnonymizationEnabled": "true"}))
	assert.True(t, requestPolicyNeedsManagedRebuild(map[string]string{setting.AnonymizationRulesOptionKey: "[]"}))
	assert.False(t, requestPolicyNeedsManagedRebuild(map[string]string{"RetryTimes": "3"}))
	assert.False(t, requestPolicyNeedsManagedRebuild(nil))
}

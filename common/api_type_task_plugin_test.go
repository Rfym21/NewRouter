package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"new-router/constant"
)

func TestTaskPluginChannelHasNoOrdinaryAPIType(t *testing.T) {
	apiType, ok := ChannelType2APIType(constant.ChannelTypeTaskPlugin)
	assert.Equal(t, -1, apiType)
	assert.False(t, ok)
}

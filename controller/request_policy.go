package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"new-router/model"
)

func GetRequestPolicy(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"options": model.CurrentRequestPolicy().Options}})
}

func UpdateRequestPolicy(c *gin.Context) {
	var request struct {
		Options map[string]string `json:"options"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.UpdateRequestPolicyOptions(request.Options); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	// The batch save is the console's main path, so it must refresh the
	// managed policy instances the same way a single option update does.
	if requestPolicyNeedsManagedRebuild(request.Options) {
		rebuildManagedPolicies()
	}
	GetRequestPolicy(c)
}

// requestPolicyNeedsManagedRebuild reports whether these options feed a
// managed request-plugin policy.
func requestPolicyNeedsManagedRebuild(options map[string]string) bool {
	for key := range options {
		if isSensitivePolicyOption(key) || isAnonymizationPolicyOption(key) {
			return true
		}
	}
	return false
}

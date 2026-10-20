package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"new-router/common"
	"new-router/model"
)

func GetChannelPriorityOrder(c *gin.Context) {
	order, err := model.GetChannelPriorityOrder(getAdminManagedTags(c)...)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": order})
}

func ReorderChannelPriority(c *gin.Context) {
	var move model.ChannelPriorityMove
	if err := common.DecodeJson(c.Request.Body, &move); err != nil {
		common.ApiErrorT(c, "Invalid channel priority move")
		return
	}
	order, err := model.ReorderChannelPriority(move, getAdminManagedTags(c)...)
	if err != nil {
		if errors.Is(err, model.ErrChannelPriorityConflict) || errors.Is(err, model.ErrChannelPriorityInvalid) || errors.Is(err, model.ErrChannelPriorityRange) {
			common.ApiErrorT(c, err.Error())
		} else {
			common.ApiError(c, err)
		}
		return
	}
	if !move.Preview && len(order.Changes) > 0 {
		model.InitChannelCache()
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": order})
}

package controller

import (
	"errors"
	"os"
	"strconv"

	"new-router/common"
	"new-router/i18n"
	"new-router/model"
	"new-router/service"
	"new-router/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

type LogExportRequest struct {
	UpstreamRequestId string   `json:"upstream_request_id,omitempty"`
	Type              int      `json:"type"`
	Username          string   `json:"username"`
	TokenName         string   `json:"token_name"`
	ModelName         string   `json:"model_name"`
	StartTimestamp    int64    `json:"start_timestamp"`
	EndTimestamp      int64    `json:"end_timestamp"`
	Channel           int      `json:"channel"`
	Group             string   `json:"group"`
	RequestId         string   `json:"request_id"`
	Scope             string   `json:"scope,omitempty"`
	Columns           []string `json:"columns"`
}

type logExportBatchRequest struct {
	Ids         []int `json:"ids"`
	ExtendHours int   `json:"extend_hours"`
}

type logExportShareRequest struct {
	MaxDownloads int  `json:"max_downloads"`
	RequireLogin bool `json:"require_login"`
}

func currentLogExportUser(c *gin.Context) (userId int, username string, isAdmin bool) {
	role := c.GetInt("role")
	isAdmin = role >= common.RoleRootUser
	if isAdmin && c.GetBool("use_access_token") && !c.GetBool("access_token_legacy") {
		scopes, _ := c.Get("access_token_scopes")
		granted, _ := scopes.([]string)
		// A root-owned token with only personal usage access remains scoped to
		// its owner's exports; all-user exports additionally require log:read.
		isAdmin = service.AccessTokenScopeGranted(granted, "log:read")
	}
	return c.GetInt("id"), c.GetString("username"), isAdmin
}

func logExportScope(c *gin.Context, allUsers bool) (userId int, isAdmin bool) {
	if allUsers {
		return 0, true
	}
	userId, _, isAdmin = currentLogExportUser(c)
	return userId, isAdmin
}

// CreateLogExport creates a server-side compressed usage-log export job.
func CreateLogExport(c *gin.Context) {
	if c.GetInt("role") < common.RoleAdminUser && !operation_setting.GetLogSetting().UserExportEnabled {
		common.ApiErrorT(c, "Log export is disabled")
		return
	}
	var req LogExportRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	if req.StartTimestamp <= 0 || req.EndTimestamp <= 0 || req.EndTimestamp <= req.StartTimestamp {
		common.ApiErrorMsg(c, "请选择有效的导出时间范围")
		return
	}
	maxDays := common.LogExportMaxDays
	if maxDays < 1 {
		maxDays = 1
	}
	maxDays = min(maxDays, max(operation_setting.GetLogSetting().MaxRangeDays, 1))
	if req.EndTimestamp-req.StartTimestamp > int64(maxDays)*24*3600 {
		common.ApiError(c, common.NewMessage("The time range cannot exceed {{days}} days", map[string]any{"days": maxDays}))
		return
	}
	if _, err := limitLogTimeRange(req.StartTimestamp, req.EndTimestamp); err != nil {
		common.ApiError(c, err)
		return
	}

	userId, username, isAdmin := currentLogExportUser(c)
	isAdmin = isAdmin && req.Scope != "self"
	if !isAdmin {
		req.Username = ""
		req.Channel = 0
	}
	columns := model.NormalizeLogExportColumns(req.Columns, isAdmin)
	if len(columns) == 0 {
		common.ApiErrorMsg(c, "请至少选择一个导出列")
		return
	}

	filters := model.LogExportFilters{
		LogType:           req.Type,
		UpstreamRequestId: req.UpstreamRequestId,
		Username:          req.Username,
		TokenName:         req.TokenName,
		ModelName:         req.ModelName,
		StartTimestamp:    req.StartTimestamp,
		EndTimestamp:      req.EndTimestamp,
		Channel:           req.Channel,
		Group:             req.Group,
		RequestId:         req.RequestId,
		Language:          i18n.StatedLang(c),
	}
	release, ok, err := service.AcquireLogExportLock(userId)
	if err != nil {
		common.ApiErrorT(c, "Failed to export logs")
		return
	}
	if !ok {
		common.ApiErrorT(c, "Another log export is running. Try again after it finishes.")
		return
	}
	job, err := model.CreateLogExportJob(userId, username, isAdmin, filters, columns)
	if err != nil {
		release()
		common.ApiError(c, err)
		return
	}
	go func() {
		defer release()
		model.RunLogExportJob(job.Id)
	}()
	common.ApiSuccess(c, job)
}

// ListLogExports lists export jobs visible to the current user.
func ListLogExports(c *gin.Context) {
	listLogExports(c, false)
}

// ListManagedLogExports lists all export jobs for root management.
func ListManagedLogExports(c *gin.Context) {
	listLogExports(c, true)
}

func listLogExports(c *gin.Context, allUsers bool) {
	pageInfo := common.GetPageQuery(c)
	userId, isAdmin := logExportScope(c, allUsers)
	jobs, total, err := model.ListLogExportJobs(userId, isAdmin, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(jobs)
	common.ApiSuccess(c, pageInfo)
}

// DownloadLogExport downloads a completed compressed export file.
func DownloadLogExport(c *gin.Context) {
	downloadLogExport(c, false)
}

// DownloadManagedLogExport downloads any completed export file for root management.
func DownloadManagedLogExport(c *gin.Context) {
	downloadLogExport(c, true)
}

func downloadLogExport(c *gin.Context, allUsers bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的导出记录")
		return
	}
	userId, isAdmin := logExportScope(c, allUsers)
	job, err := model.GetLogExportJob(id, userId, isAdmin)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !isAdmin && job.IsAdmin {
		common.ApiErrorMsg(c, "无权访问该导出记录")
		return
	}
	if job.Status != model.LogExportStatusCompleted {
		common.ApiErrorMsg(c, "导出文件尚未生成完成")
		return
	}
	if job.FilePath == "" {
		common.ApiErrorMsg(c, "导出文件已过期")
		return
	}
	if _, err := os.Stat(job.FilePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			common.ApiErrorMsg(c, "导出文件不存在或已被清理")
			return
		}
		common.ApiError(c, err)
		return
	}
	c.FileAttachment(job.FilePath, job.FileName)
}

// UpsertLogExportShare creates or updates a shareable download link.
func UpsertLogExportShare(c *gin.Context) {
	upsertLogExportShare(c, false)
}

// UpsertManagedLogExportShare creates or updates a shareable download link for root management.
func UpsertManagedLogExportShare(c *gin.Context) {
	upsertLogExportShare(c, true)
}

func upsertLogExportShare(c *gin.Context, allUsers bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的导出记录")
		return
	}
	var req logExportShareRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	scopeUserId, scopeIsAdmin := logExportScope(c, allUsers)
	creatorId, creatorUsername, _ := currentLogExportUser(c)
	share, err := model.UpsertLogExportShare(
		id,
		scopeUserId,
		scopeIsAdmin,
		creatorId,
		creatorUsername,
		req.MaxDownloads,
		req.RequireLogin,
	)
	if err != nil {
		handleLogExportShareError(c, err)
		return
	}
	common.ApiSuccess(c, share)
}

// DeleteLogExportShare revokes a shareable download link.
func DeleteLogExportShare(c *gin.Context) {
	deleteLogExportShare(c, false)
}

// DeleteManagedLogExportShare revokes a shareable download link for root management.
func DeleteManagedLogExportShare(c *gin.Context) {
	deleteLogExportShare(c, true)
}

func deleteLogExportShare(c *gin.Context, allUsers bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的导出记录")
		return
	}
	userId, isAdmin := logExportScope(c, allUsers)
	if err := model.DeleteLogExportShare(id, userId, isAdmin); err != nil {
		handleLogExportShareError(c, err)
		return
	}
	common.ApiSuccess(c, true)
}

// DownloadLogExportShare downloads an exported file through a share link.
func DownloadLogExportShare(c *gin.Context) {
	job, share, err := model.GetLogExportShareDownload(c.Param("token"), isValidLogExportShareSession(c))
	if err != nil {
		handleLogExportShareError(c, err)
		return
	}
	if _, err := os.Stat(job.FilePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			common.ApiErrorMsg(c, "导出文件不存在或已被清理")
			return
		}
		common.ApiError(c, err)
		return
	}
	if err := model.ConsumeLogExportShareDownload(share.Id); err != nil {
		handleLogExportShareError(c, err)
		return
	}
	c.FileAttachment(job.FilePath, job.FileName)
}

func isValidLogExportShareSession(c *gin.Context) bool {
	userId := c.GetInt("id")
	if userId == 0 {
		return false
	}
	user, err := model.GetUserCache(userId)
	return err == nil && user != nil && user.Status == common.UserStatusEnabled
}

func handleLogExportShareError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrLogExportShareInvalidLimit):
		common.ApiErrorMsg(c, "下载次数必须大于 0")
	case errors.Is(err, model.ErrLogExportShareRequiresLogin):
		common.ApiErrorMsg(c, "请登录后下载该分享文件")
	case errors.Is(err, model.ErrLogExportShareDownloadLimit):
		common.ApiErrorMsg(c, "分享链接下载次数已用完")
	case errors.Is(err, model.ErrLogExportShareExpired):
		common.ApiErrorMsg(c, "分享链接已过期")
	case errors.Is(err, model.ErrLogExportShareUnavailable):
		common.ApiErrorMsg(c, "分享链接不可用")
	default:
		common.ApiError(c, err)
	}
}

// DeleteLogExport deletes an export job and its generated file.
func DeleteLogExport(c *gin.Context) {
	deleteLogExport(c, false)
}

// DeleteManagedLogExport deletes any export job and its generated file for root management.
func DeleteManagedLogExport(c *gin.Context) {
	deleteLogExport(c, true)
}

func deleteLogExport(c *gin.Context, allUsers bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的导出记录")
		return
	}
	userId, isAdmin := logExportScope(c, allUsers)
	if err := model.DeleteLogExportJob(id, userId, isAdmin); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, true)
}

// BatchDeleteManagedLogExports deletes selected export jobs for root management.
func BatchDeleteManagedLogExports(c *gin.Context) {
	var req logExportBatchRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	if len(req.Ids) == 0 {
		common.ApiErrorMsg(c, "请选择要操作的导出记录")
		return
	}
	count, err := model.BatchDeleteLogExportJobs(req.Ids, 0, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"count": count})
}

// ExtendManagedLogExportsExpiration extends expiration for selected export jobs.
func ExtendManagedLogExportsExpiration(c *gin.Context) {
	var req logExportBatchRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	if len(req.Ids) == 0 {
		common.ApiErrorMsg(c, "请选择要操作的导出记录")
		return
	}
	if req.ExtendHours < 1 {
		common.ApiErrorMsg(c, "延长时间必须大于 0 小时")
		return
	}
	count, err := model.ExtendLogExportJobsExpiration(req.Ids, 0, true, req.ExtendHours)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"count": count})
}

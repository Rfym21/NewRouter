package model

import (
	"compress/gzip"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"new-router/common"

	"gorm.io/gorm"
)

const (
	LogExportStatusPending   = "pending"
	LogExportStatusRunning   = "running"
	LogExportStatusCompleted = "completed"
	LogExportStatusFailed    = "failed"
	LogExportStatusExpired   = "expired"
)

const logExportBatchSize = 5000

const logExportShareTokenLength = 40

var (
	// ErrLogExportShareRequiresLogin indicates that the share link requires an authenticated session.
	ErrLogExportShareRequiresLogin = errors.New("log export share requires login")
	// ErrLogExportShareDownloadLimit indicates that the share link has no downloads remaining.
	ErrLogExportShareDownloadLimit = errors.New("log export share download limit reached")
	// ErrLogExportShareInvalidLimit indicates that the configured download limit is invalid.
	ErrLogExportShareInvalidLimit = errors.New("log export share download limit must be positive")
	// ErrLogExportShareUnavailable indicates that the share link or its export file cannot be used.
	ErrLogExportShareUnavailable = errors.New("log export share unavailable")
	// ErrLogExportShareExpired indicates that the share link expired with its export file.
	ErrLogExportShareExpired = errors.New("log export share expired")
)

// LogExportJob records a server-side compressed usage log export task. The job and
// share metadata live in the main DB (not LOG_DB) because they need row-level
// updates/deletes and unique indexes, which the log database may not support when
// backed by ClickHouse; only the exported Log rows are read from LOG_DB.
type LogExportJob struct {
	Id             int             `json:"id"`
	UserId         int             `json:"user_id" gorm:"index"`
	Username       string          `json:"username" gorm:"index;default:''"`
	IsAdmin        bool            `json:"is_admin" gorm:"index"`
	Status         string          `json:"status" gorm:"index;default:'pending'"`
	FileName       string          `json:"file_name" gorm:"default:''"`
	FilePath       string          `json:"-" gorm:"default:''"`
	FileSize       int64           `json:"file_size" gorm:"default:0"`
	StartTimestamp int64           `json:"start_timestamp" gorm:"bigint;index"`
	EndTimestamp   int64           `json:"end_timestamp" gorm:"bigint;index"`
	Columns        string          `json:"columns" gorm:"type:text"`
	Filters        string          `json:"filters" gorm:"type:text"`
	Total          int64           `json:"total" gorm:"default:0"`
	Exported       int64           `json:"exported" gorm:"default:0"`
	Error          string          `json:"error" gorm:"type:text"`
	CreatedAt      int64           `json:"created_at" gorm:"bigint;index"`
	UpdatedAt      int64           `json:"updated_at" gorm:"bigint;index"`
	CompletedAt    int64           `json:"completed_at" gorm:"bigint"`
	ExpiresAt      int64           `json:"expires_at" gorm:"bigint;index"`
	Share          *LogExportShare `json:"share,omitempty" gorm:"-"`
}

// LogExportShare stores a shareable download link for one exported log file.
type LogExportShare struct {
	Id                int    `json:"id"`
	JobId             int    `json:"job_id" gorm:"uniqueIndex;not null"`
	Token             string `json:"token" gorm:"size:64;uniqueIndex;not null"`
	CreatedByUserId   int    `json:"created_by_user_id" gorm:"index"`
	CreatedByUsername string `json:"created_by_username" gorm:"default:''"`
	MaxDownloads      int    `json:"max_downloads" gorm:"default:1"`
	DownloadCount     int    `json:"download_count" gorm:"default:0"`
	RequireLogin      bool   `json:"require_login" gorm:"default:false"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt         int64  `json:"updated_at" gorm:"bigint;index"`
	LastDownloadedAt  int64  `json:"last_downloaded_at" gorm:"bigint"`
	DownloadPath      string `json:"download_path" gorm:"-"`
	ExpiresAt         int64  `json:"expires_at" gorm:"-"`
}

// LogExportFilters stores the log query constraints used by an export job.
type LogExportFilters struct {
	LogType        int    `json:"type"`
	Username       string `json:"username,omitempty"`
	TokenName      string `json:"token_name,omitempty"`
	ModelName      string `json:"model_name,omitempty"`
	StartTimestamp int64  `json:"start_timestamp"`
	EndTimestamp   int64  `json:"end_timestamp"`
	Channel        int    `json:"channel,omitempty"`
	Group          string `json:"group,omitempty"`
	RequestId      string `json:"request_id,omitempty"`
	// Language records the creator's language alongside structured log content.
	// CSV details retain the readable content fallback; raw exports keep parts.
	Language string `json:"language,omitempty"`
}

type logExportColumn struct {
	Key   string
	Label string
}

var logExportColumns = []logExportColumn{
	{Key: "time", Label: "时间"},
	{Key: "channel", Label: "渠道"},
	{Key: "username", Label: "用户"},
	{Key: "token", Label: "令牌"},
	{Key: "group", Label: "分组"},
	{Key: "type", Label: "类型"},
	{Key: "model", Label: "模型"},
	{Key: "use_time", Label: "用时/首字"},
	{Key: "prompt", Label: "输入"},
	{Key: "completion", Label: "输出"},
	{Key: "cost", Label: "花费"},
	{Key: "retry", Label: "重试"},
	{Key: "ip", Label: "IP"},
	{Key: "details", Label: "详情"},
}

func logExportColumnMap() map[string]logExportColumn {
	m := make(map[string]logExportColumn, len(logExportColumns))
	for _, column := range logExportColumns {
		m[column.Key] = column
	}
	return m
}

// NormalizeLogExportColumns validates and orders user-selected export columns.
func NormalizeLogExportColumns(columns []string, isAdmin bool) []string {
	allowed := logExportColumnMap()
	selected := make(map[string]bool, len(columns))
	for _, column := range columns {
		column = strings.TrimSpace(column)
		if _, ok := allowed[column]; ok {
			selected[column] = true
		}
	}
	if len(selected) == 0 {
		for _, column := range logExportColumns {
			selected[column.Key] = true
		}
	}
	if !isAdmin {
		delete(selected, "channel")
		delete(selected, "username")
		delete(selected, "retry")
	}
	ordered := make([]string, 0, len(selected))
	for _, column := range logExportColumns {
		if selected[column.Key] {
			ordered = append(ordered, column.Key)
		}
	}
	return ordered
}

// CreateLogExportJob persists a pending export job.
func CreateLogExportJob(userId int, username string, isAdmin bool, filters LogExportFilters, columns []string) (*LogExportJob, error) {
	now := time.Now().Unix()
	retentionHours := common.LogExportFileRetentionHours
	if retentionHours < 1 {
		retentionHours = 1
	}
	columnJSON, err := common.Marshal(columns)
	if err != nil {
		return nil, err
	}
	filterJSON, err := common.Marshal(filters)
	if err != nil {
		return nil, err
	}
	job := &LogExportJob{
		UserId:         userId,
		Username:       username,
		IsAdmin:        isAdmin,
		Status:         LogExportStatusPending,
		StartTimestamp: filters.StartTimestamp,
		EndTimestamp:   filters.EndTimestamp,
		Columns:        string(columnJSON),
		Filters:        string(filterJSON),
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now + int64(retentionHours)*3600,
	}
	if err := DB.Create(job).Error; err != nil {
		return nil, err
	}
	return job, nil
}

// ListLogExportJobs returns export jobs visible to the current user.
func ListLogExportJobs(userId int, isAdmin bool, startIdx int, num int) ([]*LogExportJob, int64, error) {
	if err := CleanupExpiredLogExportJobs(); err != nil {
		common.SysLog("failed to cleanup expired log export jobs: " + err.Error())
	}
	tx := DB.Model(&LogExportJob{})
	if !isAdmin {
		tx = tx.Where("user_id = ?", userId)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var jobs []*LogExportJob
	err := tx.Order("id desc").Limit(num).Offset(startIdx).Find(&jobs).Error
	if err == nil {
		err = attachLogExportShares(jobs)
	}
	return jobs, total, err
}

// GetLogExportJob returns a single export job visible to the current user.
func GetLogExportJob(id int, userId int, isAdmin bool) (*LogExportJob, error) {
	var job LogExportJob
	tx := DB.Where("id = ?", id)
	if !isAdmin {
		tx = tx.Where("user_id = ?", userId)
	}
	if err := tx.First(&job).Error; err != nil {
		return nil, err
	}
	if job.Status == LogExportStatusCompleted && job.ExpiresAt > 0 && job.ExpiresAt < time.Now().Unix() {
		_ = expireLogExportJob(&job)
		job.Status = LogExportStatusExpired
	}
	if err := attachLogExportShare(&job); err != nil {
		return nil, err
	}
	return &job, nil
}

// DeleteLogExportJob removes a job record and its generated file.
func DeleteLogExportJob(id int, userId int, isAdmin bool) error {
	job, err := GetLogExportJob(id, userId, isAdmin)
	if err != nil {
		return err
	}
	if job.FilePath != "" {
		_ = os.Remove(job.FilePath)
	}
	if err := deleteLogExportSharesByJobIDs([]int{job.Id}); err != nil {
		return err
	}
	return DB.Delete(&LogExportJob{}, job.Id).Error
}

// BatchDeleteLogExportJobs removes export job records and generated files.
func BatchDeleteLogExportJobs(ids []int, userId int, isAdmin bool) (int64, error) {
	jobs, err := listLogExportJobsByIDs(ids, userId, isAdmin)
	if err != nil {
		return 0, err
	}
	if len(jobs) == 0 {
		return 0, nil
	}
	deleteIDs := make([]int, 0, len(jobs))
	for _, job := range jobs {
		deleteIDs = append(deleteIDs, job.Id)
		if job.FilePath != "" {
			_ = os.Remove(job.FilePath)
		}
	}
	if err := deleteLogExportSharesByJobIDs(deleteIDs); err != nil {
		return 0, err
	}
	result := DB.Where("id IN ?", deleteIDs).Delete(&LogExportJob{})
	return result.RowsAffected, result.Error
}

// ExtendLogExportJobsExpiration extends export file expiration for visible jobs.
func ExtendLogExportJobsExpiration(ids []int, userId int, isAdmin bool, extendHours int) (int64, error) {
	if extendHours < 1 {
		return 0, errors.New("extend hours must be positive")
	}
	if err := CleanupExpiredLogExportJobs(); err != nil {
		common.SysLog("failed to cleanup expired log export jobs: " + err.Error())
	}
	jobs, err := listLogExportJobsByIDs(ids, userId, isAdmin)
	if err != nil {
		return 0, err
	}
	now := time.Now().Unix()
	extendSeconds := int64(extendHours) * 3600
	var affected int64
	for _, job := range jobs {
		if job.Status == LogExportStatusExpired {
			continue
		}
		base := job.ExpiresAt
		if base < now {
			base = now
		}
		result := DB.Model(&LogExportJob{}).Where("id = ?", job.Id).Updates(map[string]any{
			"expires_at": base + extendSeconds,
			"updated_at": now,
		})
		if result.Error != nil {
			return affected, result.Error
		}
		affected += result.RowsAffected
	}
	return affected, nil
}

func listLogExportJobsByIDs(ids []int, userId int, isAdmin bool) ([]LogExportJob, error) {
	ids = normalizeLogExportIDs(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	tx := DB.Where("id IN ?", ids)
	if !isAdmin {
		tx = tx.Where("user_id = ?", userId)
	}
	var jobs []LogExportJob
	if err := tx.Find(&jobs).Error; err != nil {
		return nil, err
	}
	return jobs, nil
}

func normalizeLogExportIDs(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	normalized := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized
}

// UpsertLogExportShare creates or updates a shareable link for a completed export job.
func UpsertLogExportShare(jobId int, scopeUserId int, scopeIsAdmin bool, creatorId int, creatorUsername string, maxDownloads int, requireLogin bool) (*LogExportShare, error) {
	if maxDownloads < 1 {
		return nil, ErrLogExportShareInvalidLimit
	}
	job, err := GetLogExportJob(jobId, scopeUserId, scopeIsAdmin)
	if err != nil {
		return nil, err
	}
	if job.Status != LogExportStatusCompleted || job.FilePath == "" {
		return nil, ErrLogExportShareUnavailable
	}
	if _, err := os.Stat(job.FilePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrLogExportShareUnavailable
		}
		return nil, err
	}

	now := time.Now().Unix()
	var share LogExportShare
	err = DB.Where("job_id = ?", job.Id).First(&share).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		token, err := generateUniqueLogExportShareToken()
		if err != nil {
			return nil, err
		}
		share = LogExportShare{
			JobId:             job.Id,
			Token:             token,
			CreatedByUserId:   creatorId,
			CreatedByUsername: creatorUsername,
			MaxDownloads:      maxDownloads,
			RequireLogin:      requireLogin,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if err := DB.Create(&share).Error; err != nil {
			return nil, err
		}
		decorateLogExportShare(&share, job.ExpiresAt)
		return &share, nil
	}

	if err := DB.Model(&share).Updates(map[string]any{
		"max_downloads": maxDownloads,
		"require_login": requireLogin,
		"updated_at":    now,
	}).Error; err != nil {
		return nil, err
	}
	share.MaxDownloads = maxDownloads
	share.RequireLogin = requireLogin
	share.UpdatedAt = now
	decorateLogExportShare(&share, job.ExpiresAt)
	return &share, nil
}

// DeleteLogExportShare removes a shareable link for a visible export job.
func DeleteLogExportShare(jobId int, userId int, isAdmin bool) error {
	job, err := GetLogExportJob(jobId, userId, isAdmin)
	if err != nil {
		return err
	}
	return deleteLogExportSharesByJobIDs([]int{job.Id})
}

// GetLogExportShareDownload resolves and validates a share link without consuming a download.
func GetLogExportShareDownload(token string, loggedIn bool) (*LogExportJob, *LogExportShare, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil, ErrLogExportShareUnavailable
	}
	var share LogExportShare
	if err := DB.Where("token = ?", token).First(&share).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrLogExportShareUnavailable
		}
		return nil, nil, err
	}
	if share.RequireLogin && !loggedIn {
		return nil, nil, ErrLogExportShareRequiresLogin
	}
	if share.MaxDownloads > 0 && share.DownloadCount >= share.MaxDownloads {
		return nil, nil, ErrLogExportShareDownloadLimit
	}

	var job LogExportJob
	if err := DB.Where("id = ?", share.JobId).First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrLogExportShareUnavailable
		}
		return nil, nil, err
	}
	if job.Status == LogExportStatusCompleted && job.ExpiresAt > 0 && job.ExpiresAt < time.Now().Unix() {
		_ = expireLogExportJob(&job)
		return nil, nil, ErrLogExportShareExpired
	}
	if job.Status != LogExportStatusCompleted || job.FilePath == "" {
		return nil, nil, ErrLogExportShareUnavailable
	}
	decorateLogExportShare(&share, job.ExpiresAt)
	return &job, &share, nil
}

// ConsumeLogExportShareDownload atomically records one successful share download.
func ConsumeLogExportShareDownload(shareId int) error {
	now := time.Now().Unix()
	result := DB.Model(&LogExportShare{}).
		Where("id = ? AND (max_downloads <= 0 OR download_count < max_downloads)", shareId).
		Updates(map[string]any{
			"download_count":     gorm.Expr("download_count + ?", 1),
			"last_downloaded_at": now,
			"updated_at":         now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrLogExportShareDownloadLimit
	}
	return nil
}

func generateUniqueLogExportShareToken() (string, error) {
	for i := 0; i < 5; i++ {
		token, err := common.GenerateRandomCharsKey(logExportShareTokenLength)
		if err != nil {
			return "", err
		}
		var count int64
		if err := DB.Model(&LogExportShare{}).Where("token = ?", token).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return token, nil
		}
	}
	return "", errors.New("failed to generate unique log export share token")
}

func attachLogExportShares(jobs []*LogExportJob) error {
	jobIds := make([]int, 0, len(jobs))
	for _, job := range jobs {
		if job != nil && job.Status == LogExportStatusCompleted {
			jobIds = append(jobIds, job.Id)
		}
	}
	if len(jobIds) == 0 {
		return nil
	}
	var shares []LogExportShare
	if err := DB.Where("job_id IN ?", jobIds).Find(&shares).Error; err != nil {
		return err
	}
	sharesByJob := make(map[int]*LogExportShare, len(shares))
	for i := range shares {
		sharesByJob[shares[i].JobId] = &shares[i]
	}
	for _, job := range jobs {
		if job == nil {
			continue
		}
		if share := sharesByJob[job.Id]; share != nil {
			decorateLogExportShare(share, job.ExpiresAt)
			job.Share = share
		}
	}
	return nil
}

func attachLogExportShare(job *LogExportJob) error {
	if job == nil {
		return nil
	}
	if job.Status != LogExportStatusCompleted {
		return nil
	}
	var share LogExportShare
	err := DB.Where("job_id = ?", job.Id).First(&share).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	decorateLogExportShare(&share, job.ExpiresAt)
	job.Share = &share
	return nil
}

func decorateLogExportShare(share *LogExportShare, expiresAt int64) {
	share.DownloadPath = fmt.Sprintf("/api/log/export-share/%s/download", share.Token)
	share.ExpiresAt = expiresAt
}

func deleteLogExportSharesByJobIDs(jobIds []int) error {
	if len(jobIds) == 0 {
		return nil
	}
	return DB.Where("job_id IN ?", jobIds).Delete(&LogExportShare{}).Error
}

// RunLogExportJob generates the gzip-compressed CSV file for a pending job.
func RunLogExportJob(id int) {
	var job LogExportJob
	if err := DB.First(&job, id).Error; err != nil {
		common.SysLog(fmt.Sprintf("log export job %d not found: %s", id, err.Error()))
		return
	}
	if job.Status != LogExportStatusPending {
		return
	}
	now := time.Now().Unix()
	if err := DB.Model(&job).Updates(map[string]any{
		"status":     LogExportStatusRunning,
		"updated_at": now,
	}).Error; err != nil {
		common.SysLog(fmt.Sprintf("failed to mark log export job %d running: %s", id, err.Error()))
		return
	}
	if err := generateLogExportFile(&job); err != nil {
		_ = markLogExportFailed(job.Id, err)
		common.SysLog(fmt.Sprintf("failed to generate log export job %d: %s", id, err.Error()))
	}
}

func markLogExportFailed(id int, err error) error {
	return DB.Model(&LogExportJob{}).Where("id = ?", id).Updates(map[string]any{
		"status":     LogExportStatusFailed,
		"error":      err.Error(),
		"updated_at": time.Now().Unix(),
	}).Error
}

func generateLogExportFile(job *LogExportJob) error {
	var filters LogExportFilters
	if err := common.Unmarshal([]byte(job.Filters), &filters); err != nil {
		return fmt.Errorf("parse export filters: %w", err)
	}
	var columns []string
	if err := common.Unmarshal([]byte(job.Columns), &columns); err != nil {
		return fmt.Errorf("parse export columns: %w", err)
	}
	columns = NormalizeLogExportColumns(columns, job.IsAdmin)
	if len(columns) == 0 {
		return errors.New("no export columns selected")
	}

	total, err := countLogsForExport(filters, job.UserId, job.IsAdmin)
	if err != nil {
		return fmt.Errorf("count logs: %w", err)
	}
	if err := DB.Model(job).Updates(map[string]any{
		"total":      total,
		"updated_at": time.Now().Unix(),
	}).Error; err != nil {
		return fmt.Errorf("update export total: %w", err)
	}

	exportDir := getLogExportDir()
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return fmt.Errorf("create export directory: %w", err)
	}
	fileName := fmt.Sprintf("usage-logs-%d-%s.csv.gz", job.Id, time.Now().Format("20060102150405"))
	filePath := filepath.Join(exportDir, fileName)
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create export file: %w", err)
	}

	gzipWriter := gzip.NewWriter(file)
	csvWriter := csv.NewWriter(gzipWriter)
	closed := false
	completed := false
	defer func() {
		if !closed {
			_ = gzipWriter.Close()
			_ = file.Close()
		}
		if !completed {
			_ = os.Remove(filePath)
		}
	}()

	if _, err := gzipWriter.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return fmt.Errorf("write csv bom: %w", err)
	}
	if err := csvWriter.Write(buildLogExportHeader(columns)); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}

	var exported int64
	lastId := 0
	for {
		logs, err := listLogsForExportBatch(filters, job.UserId, job.IsAdmin, lastId, logExportBatchSize)
		if err != nil {
			return fmt.Errorf("query export batch: %w", err)
		}
		if len(logs) == 0 {
			break
		}
		if !job.IsAdmin {
			sanitizeUserLogs(logs)
		} else {
			if err := fillLogChannelNames(logs); err != nil {
				return fmt.Errorf("fill channel names: %w", err)
			}
		}
		for _, log := range logs {
			if err := csvWriter.Write(buildLogExportRow(log, columns)); err != nil {
				return fmt.Errorf("write csv row: %w", err)
			}
			exported++
			lastId = log.Id
		}
		csvWriter.Flush()
		if err := csvWriter.Error(); err != nil {
			return fmt.Errorf("flush csv: %w", err)
		}
		if exported%int64(logExportBatchSize*4) == 0 || exported == total {
			_ = DB.Model(job).Updates(map[string]any{
				"exported":   exported,
				"updated_at": time.Now().Unix(),
			}).Error
		}
		if len(logs) < logExportBatchSize {
			break
		}
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return fmt.Errorf("flush csv: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("close gzip writer: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close export file: %w", err)
	}
	closed = true
	stat, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("stat export file: %w", err)
	}
	now := time.Now().Unix()
	if err := DB.Model(job).Updates(map[string]any{
		"status":       LogExportStatusCompleted,
		"file_name":    fileName,
		"file_path":    filePath,
		"file_size":    stat.Size(),
		"exported":     exported,
		"completed_at": now,
		"updated_at":   now,
	}).Error; err != nil {
		return err
	}
	completed = true
	return nil
}

func getLogExportDir() string {
	if common.LogDir == nil || *common.LogDir == "" {
		return filepath.Join(".", "logs", "exports")
	}
	return filepath.Join(*common.LogDir, "exports")
}

// CleanupExpiredLogExportJobs removes expired generated files and marks jobs expired.
func CleanupExpiredLogExportJobs() error {
	now := time.Now().Unix()
	var jobs []LogExportJob
	if err := DB.Where("status = ? AND expires_at > 0 AND expires_at < ?", LogExportStatusCompleted, now).Find(&jobs).Error; err != nil {
		return err
	}
	for i := range jobs {
		if err := expireLogExportJob(&jobs[i]); err != nil {
			return err
		}
	}
	return nil
}

func expireLogExportJob(job *LogExportJob) error {
	if job.FilePath != "" {
		_ = os.Remove(job.FilePath)
	}
	if err := deleteLogExportSharesByJobIDs([]int{job.Id}); err != nil {
		return err
	}
	return DB.Model(job).Updates(map[string]any{
		"status":     LogExportStatusExpired,
		"file_path":  "",
		"file_size":  0,
		"updated_at": time.Now().Unix(),
	}).Error
}

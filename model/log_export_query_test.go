package model

import (
	"compress/gzip"
	"encoding/csv"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"new-router/common"
	"os"
	"testing"
)

func TestExportUsesLogListFiltersAndUserScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	previous := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = previous })
	require.NoError(t, db.AutoMigrate(&Log{}))
	require.NoError(t, db.Create(&[]Log{
		{UserId: 1, ModelName: "gpt-4o", Username: "alice", UpstreamRequestId: "up-1"},
		{UserId: 2, ModelName: "gpt-4o", Username: "bob", UpstreamRequestId: "up-1"},
		{UserId: 1, ModelName: "other", Username: "alice", UpstreamRequestId: "up-2"},
	}).Error)
	filter := LogExportFilters{ModelName: "gpt-%", UpstreamRequestId: "up-1", Username: "bob", Channel: 999}
	count, err := countLogsForExport(filter, 1, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "personal scope ignores admin filters and excludes other owners")
	filter.Channel = 0
	count, err = countLogsForExport(filter, 1, true)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestExportCsvCellsAndCacheColumns(t *testing.T) {
	log := &Log{TokenName: "=HYPERLINK(1)", RequestId: "req-1", Other: common.MapToJsonStr(map[string]any{
		"cache_tokens": 12, "cache_creation_tokens_5m": 3, "cache_creation_tokens_1h": 4,
	})}
	require.Equal(t, []string{"'=HYPERLINK(1)", "req-1", "12", "7"}, buildLogExportRow(log, []string{"token", "request_id", "cache_read", "cache_write"}))
}

func TestBackgroundExportGeneratesCompressedCsv(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousLog, previousDir := DB, LOG_DB, common.LogDir
	dir := t.TempDir()
	DB, LOG_DB, common.LogDir = db, db, &dir
	t.Cleanup(func() { DB, LOG_DB, common.LogDir = previousDB, previousLog, previousDir })
	require.NoError(t, db.AutoMigrate(&Log{}, &LogExportJob{}))
	require.NoError(t, db.Create(&[]Log{{UserId: 1, CreatedAt: 10, RequestId: "first"}, {UserId: 1, CreatedAt: 20, RequestId: "second"}, {UserId: 2, CreatedAt: 30, RequestId: "other"}}).Error)
	job, err := CreateLogExportJob(1, "alice", false, LogExportFilters{StartTimestamp: 1, EndTimestamp: 40}, []string{"request_id"})
	require.NoError(t, err)
	RunLogExportJob(job.Id)
	require.NoError(t, db.First(job, job.Id).Error)
	require.Equal(t, LogExportStatusCompleted, job.Status, job.Error)
	require.EqualValues(t, 2, job.Exported)
	file, err := os.Open(job.FilePath)
	require.NoError(t, err)
	defer file.Close()
	gz, err := gzip.NewReader(file)
	require.NoError(t, err)
	defer gz.Close()
	rows, err := csv.NewReader(gz).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Equal(t, "second", rows[1][0])
	require.Equal(t, "first", rows[2][0])
}
